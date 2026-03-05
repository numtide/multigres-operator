package observer

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/tools/chaos/pkg/common"
	"github.com/numtide/multigres-operator/tools/chaos/pkg/report"
)

func (o *Observer) checkTopology(ctx context.Context) {
	etcdAddr := o.findEtcdAddress(ctx)
	if etcdAddr == "" {
		o.reporter.Report(report.Finding{
			Severity: report.SeverityWarn,
			Check:    "topology",
			Message:  "topology validation skipped: etcd unreachable",
		})
		return
	}

	o.checkCellRegistration(ctx, etcdAddr)
	o.checkPoolerRegistration(ctx, etcdAddr)
}

func (o *Observer) findEtcdAddress(ctx context.Context) string {
	var svcs corev1.ServiceList
	if err := o.client.List(ctx, &svcs,
		o.listOpts(client.MatchingLabels{
			common.LabelAppManagedBy: common.ManagedByMultigres,
			common.LabelAppComponent: common.ComponentGlobalTopo,
		})...,
	); err != nil || len(svcs.Items) == 0 {
		return ""
	}

	// Use the first topo service found.
	svc := &svcs.Items[0]
	addr := fmt.Sprintf("http://%s.%s.svc:%d", svc.Name, svc.Namespace, common.PortEtcdClient)

	// Verify connectivity with a health check.
	resp, err := o.httpClient.Get(addr + "/health")
	if err != nil {
		return ""
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return ""
	}
	return addr
}

// etcdRangeResponse is the minimal JSON response from the etcd v3 gRPC-gateway range API.
type etcdRangeResponse struct {
	Kvs []struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	} `json:"kvs"`
	Count string `json:"count"`
}

// etcdRange queries etcd via the v3 gRPC-gateway REST API for keys with the given prefix.
func (o *Observer) etcdRange(ctx context.Context, etcdAddr, prefix string) (*etcdRangeResponse, error) {
	keyBytes := []byte(prefix)
	rangeEnd := make([]byte, len(keyBytes))
	copy(rangeEnd, keyBytes)
	rangeEnd[len(rangeEnd)-1]++

	body, err := json.Marshal(map[string]string{
		"key":       base64.StdEncoding.EncodeToString(keyBytes),
		"range_end": base64.StdEncoding.EncodeToString(rangeEnd),
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, etcdAddr+"/v3/kv/range", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("etcd range request failed: %w", err)
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("etcd range returned HTTP %d", resp.StatusCode)
	}

	var result etcdRangeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding etcd response: %w", err)
	}
	return &result, nil
}

func (o *Observer) checkCellRegistration(ctx context.Context, etcdAddr string) {
	var cells multigresv1alpha1.CellList
	if err := o.client.List(ctx, &cells, o.listOpts()...); err != nil {
		return
	}

	result, err := o.etcdRange(ctx, etcdAddr, "/cells/")
	if err != nil {
		o.logger.Debug("failed to query etcd for cells", "error", err)
		return
	}

	registeredCells := make(map[string]bool)
	for _, kv := range result.Kvs {
		key, err := base64.StdEncoding.DecodeString(kv.Key)
		if err != nil {
			continue
		}
		parts := strings.Split(string(key), "/")
		if len(parts) >= 3 {
			registeredCells[parts[2]] = true
		}
	}

	// Check that every Cell CRD is registered.
	for i := range cells.Items {
		cell := &cells.Items[i]
		if cell.DeletionTimestamp != nil {
			continue
		}
		cellName := cell.Name
		if !registeredCells[cellName] {
			o.reporter.Report(report.Finding{
				Severity:  report.SeverityError,
				Check:     "topology",
				Component: fmt.Sprintf("cell/%s/%s", cell.Namespace, cellName),
				Message:   fmt.Sprintf("Cell %s exists as CRD but not registered in etcd", cellName),
			})
		}
		delete(registeredCells, cellName)
	}

	// Check for orphaned etcd entries.
	for name := range registeredCells {
		o.reporter.Report(report.Finding{
			Severity:  report.SeverityWarn,
			Check:     "topology",
			Component: fmt.Sprintf("cell/%s/%s", o.namespace, name),
			Message:   fmt.Sprintf("Cell %s registered in etcd but no corresponding CRD", name),
		})
	}
}

func (o *Observer) checkPoolerRegistration(ctx context.Context, etcdAddr string) {
	var pods corev1.PodList
	if err := o.client.List(ctx, &pods,
		o.listOpts(client.MatchingLabels{
			common.LabelAppManagedBy: common.ManagedByMultigres,
			common.LabelAppComponent: common.ComponentPool,
		})...,
	); err != nil {
		return
	}

	result, err := o.etcdRange(ctx, etcdAddr, "/poolers/")
	if err != nil {
		o.logger.Debug("failed to query etcd for poolers", "error", err)
		return
	}

	registeredPoolers := make(map[string]bool)
	for _, kv := range result.Kvs {
		key, err := base64.StdEncoding.DecodeString(kv.Key)
		if err != nil {
			continue
		}
		// Extract pod name from key path, e.g. /poolers/cell-name/pod-name
		parts := strings.Split(string(key), "/")
		if len(parts) >= 4 {
			registeredPoolers[parts[3]] = true
		}
	}

	// Check running pool pods are registered.
	for i := range pods.Items {
		pod := &pods.Items[i]
		drainState := pod.Annotations[common.AnnotationDrainState]

		// Pods in ready-for-deletion should NOT be registered.
		if drainState == common.DrainStateReadyForDeletion {
			if registeredPoolers[pod.Name] {
				o.reporter.Report(report.Finding{
					Severity:  report.SeverityError,
					Check:     "topology",
					Component: componentForPod(pod),
					Message:   fmt.Sprintf("Pod %s is in ready-for-deletion but still registered in etcd", pod.Name),
				})
			}
			continue
		}

		// Running pods without drain state should be registered.
		if pod.Status.Phase == corev1.PodRunning && drainState == "" && pod.DeletionTimestamp == nil {
			if !registeredPoolers[pod.Name] {
				o.reporter.Report(report.Finding{
					Severity:  report.SeverityWarn,
					Check:     "topology",
					Component: componentForPod(pod),
					Message:   fmt.Sprintf("Running pool pod %s not registered in etcd", pod.Name),
				})
			}
		}

		delete(registeredPoolers, pod.Name)
	}

	// Remaining entries are orphaned pooler registrations.
	for name := range registeredPoolers {
		o.reporter.Report(report.Finding{
			Severity:  report.SeverityWarn,
			Check:     "topology",
			Component: "pooler/" + name,
			Message:   fmt.Sprintf("Pooler %s registered in etcd but no corresponding pod", name),
		})
	}
}
