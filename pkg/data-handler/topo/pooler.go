package topo

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/multigres/multigres/go/common/topoclient"
	clustermetadatapb "github.com/multigres/multigres/go/pb/clustermetadata"
	"sigs.k8s.io/controller-runtime/pkg/log"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

// PoolerStatusResult holds the result of querying pooler roles from the topology.
type PoolerStatusResult struct {
	// Roles maps hostname to role string (PRIMARY, REPLICA, DRAINED).
	Roles map[string]string
	// QuerySuccess indicates whether all topology queries succeeded.
	QuerySuccess bool
}

// GetPoolerStatus queries the topology for all poolers belonging to a shard
// and returns a map of hostname to role string.
func GetPoolerStatus(
	ctx context.Context,
	store topoclient.Store,
	shard *multigresv1alpha1.Shard,
) *PoolerStatusResult {
	result := &PoolerStatusResult{
		Roles:        make(map[string]string),
		QuerySuccess: true,
	}

	cells := CollectCells(shard)
	for _, cell := range cells {
		opt := ShardFilter(shard)
		poolers, err := store.GetMultiPoolersByCell(ctx, cell, opt)
		if err == nil {
			for _, p := range poolers {
				roleName := "REPLICA"
				switch p.Type {
				case clustermetadatapb.PoolerType_PRIMARY:
					roleName = "PRIMARY"
				case clustermetadatapb.PoolerType_DRAINED:
					roleName = "DRAINED"
				}
				hostname := p.GetHostname()
				if p.Id != nil && p.Id.Name != "" {
					hostname = p.Id.Name
				} else if hostname == "" {
					hostname = fmt.Sprintf("unknown-pooler-%v", p.Id)
				}
				result.Roles[hostname] = roleName
			}
		} else {
			result.QuerySuccess = false
		}
	}
	return result
}

// FindPrimaryPooler discovers the PRIMARY multipooler from the given cells
// in the topology. Returns (nil, nil) only if at least one cell was
// successfully queried but no primary was found. Returns an error if all
// cells are unavailable.
func FindPrimaryPooler(
	ctx context.Context,
	store topoclient.Store,
	shard *multigresv1alpha1.Shard,
	cells []string,
) (*clustermetadatapb.MultiPooler, error) {
	var anySuccess bool
	for _, cell := range cells {
		opt := ShardFilter(shard)
		poolers, err := store.GetMultiPoolersByCell(ctx, cell, opt)
		if err != nil {
			if IsTopoUnavailable(err) {
				continue
			}
			return nil, fmt.Errorf("listing poolers in cell %q: %w", cell, err)
		}
		anySuccess = true
		for _, p := range poolers {
			if p.Type == clustermetadatapb.PoolerType_PRIMARY {
				return p.MultiPooler, nil
			}
		}
	}
	if !anySuccess && len(cells) > 0 {
		return nil, fmt.Errorf("all %d cell(s) unavailable, cannot determine primary", len(cells))
	}
	return nil, nil
}

// CollectCells returns the sorted, deduplicated set of cell names from the shard's pools.
func CollectCells(shard *multigresv1alpha1.Shard) []string {
	seen := make(map[string]bool)
	for _, pool := range shard.Spec.Pools {
		for _, cell := range pool.Cells {
			seen[string(cell)] = true
		}
	}
	cells := make([]string, 0, len(seen))
	for cell := range seen {
		cells = append(cells, cell)
	}
	slices.Sort(cells)
	return cells
}

// ShardFilter builds a GetMultiPoolersByCellOptions filter for a shard.
func ShardFilter(shard *multigresv1alpha1.Shard) *topoclient.GetMultiPoolersByCellOptions {
	return &topoclient.GetMultiPoolersByCellOptions{
		DatabaseShard: &topoclient.DatabaseShard{
			Database:   string(shard.Spec.DatabaseName),
			TableGroup: string(shard.Spec.TableGroupName),
			Shard:      string(shard.Spec.ShardName),
		},
	}
}

// PodMatchesPooler checks if the topology pooler record corresponds to the given Kubernetes pod name.
func PodMatchesPooler(podName string, p *topoclient.MultiPoolerInfo) bool {
	if p.Id != nil && p.Id.Name == podName {
		return true
	}
	h := p.GetHostname()
	return h == podName || strings.HasPrefix(h, podName+".")
}

// ForceUnregisterPod unregisters a specific pod's pooler from the topology.
func ForceUnregisterPod(
	ctx context.Context,
	store topoclient.Store,
	shard *multigresv1alpha1.Shard,
	podName string,
	cellName string,
) error {
	if cellName == "" {
		return nil
	}

	opt := ShardFilter(shard)
	poolers, err := store.GetMultiPoolersByCell(ctx, cellName, opt)
	if err != nil {
		return err
	}

	for _, p := range poolers {
		if PodMatchesPooler(podName, p) {
			return store.UnregisterMultiPooler(ctx, p.Id)
		}
	}
	log.FromContext(ctx).
		Info("No matching pooler found in topology for pod, skipping unregistration",
			"pod", podName, "cell", cellName)
	return nil
}

// PrunePoolers removes topology entries for poolers that have no corresponding
// running pod. activePodNames should contain the names of all pods currently
// managed by the shard. Returns the number of pruned entries.
func PrunePoolers(
	ctx context.Context,
	store topoclient.Store,
	shard *multigresv1alpha1.Shard,
	activePodNames map[string]bool,
) (int, error) {
	logger := log.FromContext(ctx)
	pruned := 0

	cells := CollectCells(shard)
	for _, cell := range cells {
		opt := ShardFilter(shard)
		poolers, err := store.GetMultiPoolersByCell(ctx, cell, opt)
		if err != nil {
			if IsTopoUnavailable(err) {
				continue
			}
			return pruned, fmt.Errorf("listing poolers in cell %q for pruning: %w", cell, err)
		}

		for _, p := range poolers {
			if !poolerMatchesAnyActivePod(p, activePodNames) {
				hostname := p.GetHostname()
				if hostname == "" && p.Id != nil {
					hostname = p.Id.Name
				}
				if err := store.UnregisterMultiPooler(ctx, p.Id); err != nil {
					logger.Error(err, "Failed to prune stale pooler",
						"pooler", hostname, "cell", cell)
					continue
				}
				logger.Info("Pruned stale pooler from topology",
					"pooler", hostname, "cell", cell)
				pruned++
			}
		}
	}

	return pruned, nil
}

// poolerMatchesAnyActivePod returns true when the pooler record corresponds to
// any pod in activePodNames. Uses PodMatchesPooler for FQDN-aware comparison.
func poolerMatchesAnyActivePod(p *topoclient.MultiPoolerInfo, activePodNames map[string]bool) bool {
	for podName := range activePodNames {
		if PodMatchesPooler(podName, p) {
			return true
		}
	}
	return false
}
