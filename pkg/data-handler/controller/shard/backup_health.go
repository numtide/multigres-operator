package shard

import (
	"context"
	"fmt"
	"time"

	"github.com/multigres/multigres/go/common/topoclient"
	clustermetadatapb "github.com/multigres/multigres/go/pb/clustermetadata"
	multipoolermanagerdatapb "github.com/multigres/multigres/go/pb/multipoolermanagerdata"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/monitoring"
)

const (
	// backupStaleThreshold is the maximum age of a completed backup before it
	// is considered stale. Set to 25h to accommodate daily backup schedules
	// with a 1-hour buffer for execution time.
	backupStaleThreshold = 25 * time.Hour

	// backupQueryLimit caps the number of backup records fetched per RPC call.
	backupQueryLimit = 10

	conditionBackupHealthy = "BackupHealthy"
)

// backupHealthResult holds the computed backup health information.
type backupHealthResult struct {
	Healthy        bool
	LastBackupTime *metav1.Time
	LastBackupType string
	Message        string
}

// evaluateBackupHealth discovers the primary pooler from the topology and
// queries it for backup metadata. Returns nil (no error) if no primary pooler
// is registered yet, which is normal during cluster bootstrap.
func (r *ShardReconciler) evaluateBackupHealth(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
) (*backupHealthResult, error) {
	logger := log.FromContext(ctx)

	store, err := r.getTopoStore(shard)
	if err != nil {
		return nil, fmt.Errorf("creating topology store: %w", err)
	}
	defer func() { _ = store.Close() }()

	cells := collectCells(shard)
	primary, err := findPrimaryPooler(ctx, store, shard, cells)
	if err != nil {
		return nil, fmt.Errorf("finding primary pooler: %w", err)
	}
	if primary == nil {
		logger.V(1).Info("No primary pooler registered in topology, skipping backup health check")
		return nil, nil
	}

	backups, err := r.getBackups(ctx, primary)
	if err != nil {
		return nil, fmt.Errorf("querying backups from primary %s: %w",
			topoclient.MultiPoolerIDString(primary.Id), err)
	}

	return evaluateBackups(shard, backups), nil
}

// getBackups calls the GetBackups RPC on the given multipooler.
func (r *ShardReconciler) getBackups(
	ctx context.Context,
	pooler *clustermetadatapb.MultiPooler,
) ([]*multipoolermanagerdatapb.BackupMetadata, error) {
	resp, err := r.rpcClient.GetBackups(ctx, pooler,
		&multipoolermanagerdatapb.GetBackupsRequest{Limit: backupQueryLimit})
	if err != nil {
		return nil, err
	}
	return resp.Backups, nil
}

// findPrimaryPooler discovers the PRIMARY multipooler from the given cells
// in the topology. Returns nil (no error) if no primary is found.
func findPrimaryPooler(
	ctx context.Context,
	store topoclient.Store,
	shard *multigresv1alpha1.Shard,
	cells []string,
) (*clustermetadatapb.MultiPooler, error) {
	for _, cell := range cells {
		opt := &topoclient.GetMultiPoolersByCellOptions{
			DatabaseShard: &topoclient.DatabaseShard{
				Database:   string(shard.Spec.DatabaseName),
				TableGroup: string(shard.Spec.TableGroupName),
				Shard:      string(shard.Spec.ShardName),
			},
		}
		poolers, err := store.GetMultiPoolersByCell(ctx, cell, opt)
		if err != nil {
			if isTopoUnavailable(err) {
				continue
			}
			return nil, fmt.Errorf("listing poolers in cell %q: %w", cell, err)
		}
		for _, p := range poolers {
			if p.Type == clustermetadatapb.PoolerType_PRIMARY {
				return p.MultiPooler, nil
			}
		}
	}
	return nil, nil
}

// evaluateBackups inspects backup metadata and produces a health result.
// It looks for the most recent COMPLETE backup.
func evaluateBackups(
	shard *multigresv1alpha1.Shard,
	backups []*multipoolermanagerdatapb.BackupMetadata,
) *backupHealthResult {
	if len(backups) == 0 {
		return &backupHealthResult{
			Healthy: false,
			Message: "No backups found",
		}
	}

	// Find the most recent COMPLETE backup. Backups are returned newest-first
	// by pgBackRest, but we iterate all to be safe.
	var latest *multipoolermanagerdatapb.BackupMetadata
	for _, b := range backups {
		if b.Status != multipoolermanagerdatapb.BackupMetadata_COMPLETE {
			continue
		}
		if latest == nil {
			latest = b
			continue
		}
		// BackupId contains a timestamp prefix (YYYYMMDD-HHMMSS), lexicographic
		// comparison is sufficient for recency ordering.
		if b.BackupId > latest.BackupId {
			latest = b
		}
	}

	if latest == nil {
		return &backupHealthResult{
			Healthy: false,
			Message: "No completed backups found",
		}
	}

	now := time.Now()
	backupTime := parseBackupTime(latest.BackupId)
	age := now.Sub(backupTime)

	clusterName := shard.Labels["multigres.com/cluster"]
	monitoring.SetLastBackupAge(clusterName, shard.Name, shard.Namespace, age)

	result := &backupHealthResult{
		LastBackupTime: &metav1.Time{Time: backupTime},
		LastBackupType: latest.Type,
	}

	if age > backupStaleThreshold {
		result.Healthy = false
		result.Message = fmt.Sprintf("Last backup is %.1fh old (threshold: %.0fh)",
			age.Hours(), backupStaleThreshold.Hours())
	} else {
		result.Healthy = true
		result.Message = fmt.Sprintf("Last %s backup completed %.1fh ago", latest.Type, age.Hours())
	}

	return result
}

// parseBackupTime extracts the timestamp from a pgBackRest backup ID.
// Format: YYYYMMDD-HHMMSSF (F = fractional seconds suffix).
func parseBackupTime(backupID string) time.Time {
	if len(backupID) < 15 {
		return time.Time{}
	}
	t, err := time.Parse("20060102-150405", backupID[:15])
	if err != nil {
		return time.Time{}
	}
	return t
}

// applyBackupHealth updates the shard status with backup health information
// and sets the BackupHealthy condition.
func applyBackupHealth(shard *multigresv1alpha1.Shard, result *backupHealthResult) {
	if result == nil {
		return
	}

	shard.Status.LastBackupTime = result.LastBackupTime
	shard.Status.LastBackupType = result.LastBackupType

	condition := metav1.Condition{
		Type:               conditionBackupHealthy,
		ObservedGeneration: shard.Generation,
		LastTransitionTime: metav1.Now(),
		Message:            result.Message,
	}

	if result.Healthy {
		condition.Status = metav1.ConditionTrue
		condition.Reason = "BackupRecent"
	} else {
		condition.Status = metav1.ConditionFalse
		condition.Reason = "BackupStale"
	}

	setCondition(&shard.Status.Conditions, condition)
}

// setCondition adds or updates a condition in the slice.
func setCondition(conditions *[]metav1.Condition, condition metav1.Condition) {
	for i, c := range *conditions {
		if c.Type == condition.Type {
			if c.Status != condition.Status {
				(*conditions)[i] = condition
			} else {
				// Preserve transition time when status hasn't changed.
				condition.LastTransitionTime = c.LastTransitionTime
				(*conditions)[i] = condition
			}
			return
		}
	}
	*conditions = append(*conditions, condition)
}

// collectCells returns the deduplicated set of cell names from the shard's pools.
func collectCells(shard *multigresv1alpha1.Shard) []string {
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
	return cells
}

// isConditionTrue returns true if the named condition exists and has status True.
func isConditionTrue(conditions []metav1.Condition, condType string) bool {
	for _, c := range conditions {
		if c.Type == condType {
			return c.Status == metav1.ConditionTrue
		}
	}
	return false
}
