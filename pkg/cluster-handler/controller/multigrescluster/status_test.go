package multigrescluster

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/testutil"
)

func TestReconcile_Status(t *testing.T) {
	_, _, _, _, clusterName, _ := setupFixtures(t)
	errSimulated := errors.New("simulated error for testing")

	tests := map[string]reconcileTestCase{
		"Error: UpdateStatus (List Cells Failed)": {
			failureConfig: &testutil.FailureConfig{
				OnList: func() func(client.ObjectList) error {
					count := 0
					return func(list client.ObjectList) error {
						if _, ok := list.(*multigresv1alpha1.CellList); ok {
							count++
							if count > 1 {
								return errSimulated
							}
						}
						return nil
					}
				}(),
			},
			wantErrMsg: "failed to list cells for status",
		},
		"Error: UpdateStatus (List TableGroups Failed)": {
			failureConfig: &testutil.FailureConfig{
				OnList: func() func(client.ObjectList) error {
					count := 0
					return func(list client.ObjectList) error {
						if _, ok := list.(*multigresv1alpha1.TableGroupList); ok {
							count++
							if count > 1 {
								return errSimulated
							}
						}
						return nil
					}
				}(),
			},
			wantErrMsg: "failed to list tablegroups for status",
		},
		"Error: Update Status Failed (API Error)": {
			failureConfig: &testutil.FailureConfig{
				OnStatusPatch: testutil.FailOnObjectName(clusterName, errSimulated),
			},
			wantErrMsg: "failed to patch status",
		},
	}

	runReconcileTest(t, tests)
}

func TestUpdateStatus_Coverage(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	cluster := &multigresv1alpha1.MultigresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
	}

	cell := &multigresv1alpha1.Cell{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cell-1",
			Namespace: "default",
			Labels:    map[string]string{"multigres.com/cluster": "test-cluster"},
		},
		Spec: multigresv1alpha1.CellSpec{
			Name: "cell-1",
		},
		Status: multigresv1alpha1.CellStatus{
			Conditions: []metav1.Condition{
				{Type: "Available", Status: metav1.ConditionTrue},
			},
			GatewayReplicas: 1,
		},
	}

	tg := &multigresv1alpha1.TableGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tg-1",
			Namespace: "default",
			Labels:    map[string]string{"multigres.com/cluster": "test-cluster"},
		},
		Spec: multigresv1alpha1.TableGroupSpec{
			DatabaseName: "db1",
		},
		Status: multigresv1alpha1.TableGroupStatus{
			ReadyShards: 1,
			TotalShards: 1,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster, cell, tg).
		WithStatusSubresource(cluster, cell, tg).
		Build()

	r := &MultigresClusterReconciler{
		Client:   fakeClient,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(100),
	}

	if err := r.updateStatus(context.Background(), cluster); err != nil {
		t.Fatalf("updateStatus failed: %v", err)
	}

	if err := fakeClient.Get(
		context.Background(),
		client.ObjectKeyFromObject(cluster),
		cluster,
	); err != nil {
		t.Fatalf("Failed to refresh cluster: %v", err)
	}

	found := false
	for _, c := range cluster.Status.Conditions {
		if c.Type == "Available" {
			found = true
			if c.Status != metav1.ConditionTrue {
				t.Errorf("Expected Available=True, got %s", c.Status)
			}
		}
	}
	if !found {
		t.Error("Available condition not found")
	}

	if s, ok := cluster.Status.Cells["cell-1"]; !ok || !s.Ready {
		t.Errorf("Expected cell-1 to be ready in status summary, got %v", s)
	}

	if s, ok := cluster.Status.Databases["db1"]; !ok || s.ReadyShards != 1 {
		t.Errorf("Expected db1 to have 1 ready shard in status summary, got %v", s)
	}

	cDegraded := &multigresv1alpha1.Cell{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cell-degraded",
			Namespace: "default",
			Labels:    map[string]string{"multigres.com/cluster": "test-cluster"},
		},
		Spec: multigresv1alpha1.CellSpec{Name: "cell-degraded"},
		Status: multigresv1alpha1.CellStatus{
			Phase: multigresv1alpha1.PhaseDegraded,
		},
	}

	fakeClient = fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster, cell, tg, cDegraded).
		WithStatusSubresource(cluster, cell, tg, cDegraded).
		Build()

	r.Client = fakeClient
	if err := r.updateStatus(context.Background(), cluster); err != nil {
		t.Fatalf("updateStatus failed: %v", err)
	}

	if err := fakeClient.Get(
		context.Background(),
		client.ObjectKeyFromObject(cluster),
		cluster,
	); err != nil {
		t.Fatalf("Failed to refresh cluster: %v", err)
	}

	if cluster.Status.Phase != multigresv1alpha1.PhaseDegraded {
		t.Errorf("Expected PhaseDegraded, got %s", cluster.Status.Phase)
	}

	cProgressing := &multigresv1alpha1.Cell{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cell-prog",
			Namespace: "default",
			Labels:    map[string]string{"multigres.com/cluster": "test-cluster"},
		},
		Spec: multigresv1alpha1.CellSpec{Name: "cell-prog"},
		Status: multigresv1alpha1.CellStatus{
			Phase: multigresv1alpha1.PhaseInitializing,
		},
	}

	fakeClient = fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster, cell, tg, cProgressing).
		WithStatusSubresource(cluster, cell, tg, cProgressing).
		Build()
	r.Client = fakeClient

	if err := r.updateStatus(context.Background(), cluster); err != nil {
		t.Fatalf("updateStatus failed: %v", err)
	}
	if err := fakeClient.Get(
		context.Background(),
		client.ObjectKeyFromObject(cluster),
		cluster,
	); err != nil {
		t.Fatalf("Failed to refresh cluster: %v", err)
	}
	if cluster.Status.Phase != multigresv1alpha1.PhaseProgressing {
		t.Errorf("Expected PhaseProgressing, got %s", cluster.Status.Phase)
	}

	tgDegraded := &multigresv1alpha1.TableGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tg-degraded",
			Namespace: "default",
			Labels:    map[string]string{"multigres.com/cluster": "test-cluster"},
		},
		Spec: multigresv1alpha1.TableGroupSpec{
			DatabaseName:   "db1",
			TableGroupName: "tg-degraded",
		},
		Status: multigresv1alpha1.TableGroupStatus{
			Phase: multigresv1alpha1.PhaseDegraded,
		},
	}

	fakeClient = fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster, cell, tg, tgDegraded).
		WithStatusSubresource(cluster, cell, tg, tgDegraded).
		Build()

	r.Client = fakeClient
	if err := r.updateStatus(context.Background(), cluster); err != nil {
		t.Fatalf("updateStatus failed: %v", err)
	}

	if err := fakeClient.Get(
		context.Background(),
		client.ObjectKeyFromObject(cluster),
		cluster,
	); err != nil {
		t.Fatalf("Failed to refresh cluster: %v", err)
	}

	if cluster.Status.Phase != multigresv1alpha1.PhaseDegraded {
		t.Errorf("Expected PhaseDegraded, got %s", cluster.Status.Phase)
	}

	tgInit := &multigresv1alpha1.TableGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tg-init",
			Namespace: "default",
			Labels:    map[string]string{"multigres.com/cluster": "test-cluster"},
		},
		Spec: multigresv1alpha1.TableGroupSpec{
			DatabaseName:   "db1",
			TableGroupName: "tg-init",
		},
		Status: multigresv1alpha1.TableGroupStatus{
			Phase: multigresv1alpha1.PhaseInitializing,
		},
	}

	fakeClient = fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster, cell, tg, tgInit).
		WithStatusSubresource(cluster, cell, tg, tgInit).
		Build()

	r.Client = fakeClient
	if err := r.updateStatus(context.Background(), cluster); err != nil {
		t.Fatalf("updateStatus failed: %v", err)
	}

	if err := fakeClient.Get(
		context.Background(),
		client.ObjectKeyFromObject(cluster),
		cluster,
	); err != nil {
		t.Fatalf("Failed to refresh cluster: %v", err)
	}

	if cluster.Status.Phase != multigresv1alpha1.PhaseProgressing {
		t.Errorf("Expected PhaseProgressing, got %s", cluster.Status.Phase)
	}
}

func TestUpdateStatus_ZeroResources(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	cluster := &multigresv1alpha1.MultigresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster).
		WithStatusSubresource(cluster).
		Build()

	r := &MultigresClusterReconciler{
		Client:   fakeClient,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(100),
	}

	if err := r.updateStatus(context.Background(), cluster); err != nil {
		t.Fatalf("updateStatus failed: %v", err)
	}

	if err := fakeClient.Get(
		context.Background(),
		client.ObjectKeyFromObject(cluster),
		cluster,
	); err != nil {
		t.Fatalf("Failed to refresh cluster: %v", err)
	}

	if cluster.Status.Phase != multigresv1alpha1.PhaseHealthy {
		t.Errorf("Expected PhaseHealthy (vacuously true), got %s", cluster.Status.Phase)
	}

	cond := meta.FindStatusCondition(cluster.Status.Conditions, "Available")
	if cond == nil {
		t.Fatal("Available condition missing")
	}
	if cond.Status != metav1.ConditionFalse {
		t.Errorf("Expected Available=False (no cells), got %s", cond.Status)
	}
}

func TestUpdateStatus_GenerationMismatch(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	cluster := &multigresv1alpha1.MultigresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-cluster",
			Namespace:  "default",
			Generation: 2,
		},
	}

	cell := &multigresv1alpha1.Cell{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "cell-1",
			Namespace:  "default",
			Labels:     map[string]string{"multigres.com/cluster": "test-cluster"},
			Generation: 2,
		},
		Spec: multigresv1alpha1.CellSpec{Name: "cell-1"},
		Status: multigresv1alpha1.CellStatus{
			ObservedGeneration: 1,
			Phase:              multigresv1alpha1.PhaseHealthy,
		},
	}

	tg := &multigresv1alpha1.TableGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "tg-1",
			Namespace:  "default",
			Labels:     map[string]string{"multigres.com/cluster": "test-cluster"},
			Generation: 2,
		},
		Spec: multigresv1alpha1.TableGroupSpec{DatabaseName: "db1"},
		Status: multigresv1alpha1.TableGroupStatus{
			ObservedGeneration: 1,
			Phase:              multigresv1alpha1.PhaseHealthy,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster, cell, tg).
		WithStatusSubresource(cluster, cell, tg).
		Build()

	r := &MultigresClusterReconciler{
		Client:   fakeClient,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(100),
	}

	if err := r.updateStatus(context.Background(), cluster); err != nil {
		t.Fatalf("updateStatus failed: %v", err)
	}

	if err := fakeClient.Get(
		context.Background(),
		client.ObjectKeyFromObject(cluster),
		cluster,
	); err != nil {
		t.Fatalf("Failed to refresh cluster: %v", err)
	}

	if cluster.Status.Phase != multigresv1alpha1.PhaseProgressing {
		t.Errorf(
			"Expected PhaseProgressing due to generation mismatch, got %s",
			cluster.Status.Phase,
		)
	}
}

func TestExtractExternalEndpoint(t *testing.T) {
	tests := []struct {
		name string
		svc  *corev1.Service
		want string
	}{
		{
			name: "nil Service",
			svc:  nil,
			want: "",
		},
		{
			name: "empty ingress list",
			svc: &corev1.Service{
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{},
					},
				},
			},
			want: "",
		},
		{
			name: "service external IP preferred",
			svc: &corev1.Service{
				Spec: corev1.ServiceSpec{
					ExternalIPs: []string{"2001:db8::10"},
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{Hostname: "a1234.elb.us-east-1.amazonaws.com"},
						},
					},
				},
			},
			want: "2001:db8::10",
		},
		{
			name: "hostname only",
			svc: &corev1.Service{
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{Hostname: "a1234.elb.us-east-1.amazonaws.com"},
						},
					},
				},
			},
			want: "a1234.elb.us-east-1.amazonaws.com",
		},
		{
			name: "IP only",
			svc: &corev1.Service{
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{IP: "203.0.113.42"},
						},
					},
				},
			},
			want: "203.0.113.42",
		},
		{
			name: "both hostname and IP - hostname preferred",
			svc: &corev1.Service{
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{
								Hostname: "a1234.elb.us-east-1.amazonaws.com",
								IP:       "203.0.113.42",
							},
						},
					},
				},
			},
			want: "a1234.elb.us-east-1.amazonaws.com",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractExternalEndpoint(tc.svc)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestComputeGatewayCondition(t *testing.T) {
	const gen int64 = 7

	tests := []struct {
		name                   string
		externalGatewayEnabled bool
		externalEndpoint       string
		aggregateReadyGateways int32
		clusterGeneration      int64
		wantNil                bool
		wantStatus             metav1.ConditionStatus
		wantReason             string
		wantMessageContains    string
	}{
		{
			name:                   "disabled - externalGateway nil equivalent",
			externalGatewayEnabled: false,
			wantNil:                true,
		},
		{
			name:                   "disabled - enabled false",
			externalGatewayEnabled: false,
			externalEndpoint:       "",
			aggregateReadyGateways: 0,
			clusterGeneration:      gen,
			wantNil:                true,
		},
		{
			name:                   "enabled, empty endpoint - AwaitingEndpoint",
			externalGatewayEnabled: true,
			externalEndpoint:       "",
			aggregateReadyGateways: 0,
			clusterGeneration:      gen,
			wantStatus:             metav1.ConditionFalse,
			wantReason:             multigresv1alpha1.ReasonAwaitingEndpoint,
			wantMessageContains:    "gateway service endpoint",
		},
		{
			name:                   "enabled, endpoint present, 0 ready gateways - NoReadyGateways",
			externalGatewayEnabled: true,
			externalEndpoint:       "a1234.elb.us-east-1.amazonaws.com",
			aggregateReadyGateways: 0,
			clusterGeneration:      gen,
			wantStatus:             metav1.ConditionFalse,
			wantReason:             multigresv1alpha1.ReasonNoReadyGateways,
			wantMessageContains:    "no multigateway pods are ready",
		},
		{
			name:                   "enabled, endpoint present, >0 ready gateways - EndpointReady",
			externalGatewayEnabled: true,
			externalEndpoint:       "a1234.elb.us-east-1.amazonaws.com",
			aggregateReadyGateways: 3,
			clusterGeneration:      gen,
			wantStatus:             metav1.ConditionTrue,
			wantReason:             multigresv1alpha1.ReasonEndpointReady,
			wantMessageContains:    "a1234.elb.us-east-1.amazonaws.com",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := computeGatewayCondition(
				tc.externalGatewayEnabled,
				tc.externalEndpoint,
				tc.aggregateReadyGateways,
				tc.clusterGeneration,
			)

			if tc.wantNil {
				assert.Nil(t, got)
				return
			}

			require.NotNil(t, got)
			assert.Equal(t, multigresv1alpha1.ConditionGatewayExternalReady, got.Type)
			assert.Equal(t, tc.wantStatus, got.Status)
			assert.Equal(t, tc.wantReason, got.Reason)
			assert.Contains(t, got.Message, tc.wantMessageContains)
			assert.Equal(t, tc.clusterGeneration, got.ObservedGeneration)
		})
	}
}

func TestUpdateStatus_GatewayServiceErrors(t *testing.T) {
	scheme := setupScheme()

	clusterName := "gw-test"
	namespace := "default"

	baseCluster := &multigresv1alpha1.MultigresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:       clusterName,
			Namespace:  namespace,
			Generation: 3,
		},
		Spec: multigresv1alpha1.MultigresClusterSpec{
			ExternalGateway: &multigresv1alpha1.ExternalGatewayConfig{Enabled: true},
		},
		Status: multigresv1alpha1.MultigresClusterStatus{},
	}

	t.Run("non-NotFound error fetching global service returns error", func(t *testing.T) {
		injectedErr := fmt.Errorf("simulated API server error")

		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(baseCluster.DeepCopy()).
			WithStatusSubresource(&multigresv1alpha1.MultigresCluster{}).
			Build()

		failClient := testutil.NewFakeClientWithFailures(cl, &testutil.FailureConfig{
			OnGet: testutil.FailOnKeyName(clusterName+"-multigateway", injectedErr),
		})

		r := &MultigresClusterReconciler{
			Client:   failClient,
			Scheme:   scheme,
			Recorder: record.NewFakeRecorder(10),
		}

		cluster := baseCluster.DeepCopy()
		err := r.updateStatus(t.Context(), cluster)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get global multigateway service for status")
	})

	t.Run("NotFound global service sets AwaitingEndpoint when enabled", func(t *testing.T) {
		// No Service object created — Get will return NotFound.
		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(baseCluster.DeepCopy()).
			WithStatusSubresource(&multigresv1alpha1.MultigresCluster{}).
			Build()

		r := &MultigresClusterReconciler{
			Client:   cl,
			Scheme:   scheme,
			Recorder: record.NewFakeRecorder(10),
		}

		cluster := baseCluster.DeepCopy()
		err := r.updateStatus(t.Context(), cluster)
		require.NoError(t, err)

		require.NotNil(t, cluster.Status.Gateway)
		assert.Empty(t, cluster.Status.Gateway.ExternalEndpoint)

		cond := meta.FindStatusCondition(
			cluster.Status.Conditions,
			multigresv1alpha1.ConditionGatewayExternalReady,
		)
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, multigresv1alpha1.ReasonAwaitingEndpoint, cond.Reason)
	})
}

func TestUpdateStatus_StaleCellGenerationIgnored(t *testing.T) {
	scheme := setupScheme()

	clusterName := "gw-stale"
	namespace := "default"

	cluster := &multigresv1alpha1.MultigresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:       clusterName,
			Namespace:  namespace,
			Generation: 5,
		},
		Spec: multigresv1alpha1.MultigresClusterSpec{
			ExternalGateway: &multigresv1alpha1.ExternalGatewayConfig{Enabled: true},
		},
		Status: multigresv1alpha1.MultigresClusterStatus{},
	}

	// Global multigateway Service with an LB ingress so we get past AwaitingEndpoint.
	gwSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName + "-multigateway",
			Namespace: namespace,
			Labels:    map[string]string{"multigres.com/cluster": clusterName},
		},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{
					{Hostname: "lb.example.com"},
				},
			},
		},
	}

	// Fresh cell: ObservedGeneration == Generation, has 2 ready gateways.
	freshCell := &multigresv1alpha1.Cell{
		ObjectMeta: metav1.ObjectMeta{
			Name:       clusterName + "-fresh",
			Namespace:  namespace,
			Generation: 4,
			Labels:     map[string]string{"multigres.com/cluster": clusterName},
		},
		Spec: multigresv1alpha1.CellSpec{Name: "fresh"},
		Status: multigresv1alpha1.CellStatus{
			ObservedGeneration:   4,
			GatewayReadyReplicas: 2,
		},
	}

	// Stale cell: ObservedGeneration < Generation, has 5 ready gateways (should be ignored).
	staleCell := &multigresv1alpha1.Cell{
		ObjectMeta: metav1.ObjectMeta{
			Name:       clusterName + "-stale",
			Namespace:  namespace,
			Generation: 10,
			Labels:     map[string]string{"multigres.com/cluster": clusterName},
		},
		Spec: multigresv1alpha1.CellSpec{Name: "stale"},
		Status: multigresv1alpha1.CellStatus{
			ObservedGeneration:   8, // stale
			GatewayReadyReplicas: 5,
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster.DeepCopy(), gwSvc, freshCell, staleCell).
		WithStatusSubresource(&multigresv1alpha1.MultigresCluster{}).
		Build()

	r := &MultigresClusterReconciler{
		Client:   cl,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(10),
	}

	clusterCopy := cluster.DeepCopy()
	err := r.updateStatus(t.Context(), clusterCopy)
	require.NoError(t, err)

	// Only the fresh cell's 2 ready gateways should count.
	// With endpoint present and aggregateReadyGateways=2, condition should be EndpointReady.
	cond := meta.FindStatusCondition(
		clusterCopy.Status.Conditions,
		multigresv1alpha1.ConditionGatewayExternalReady,
	)
	require.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionTrue, cond.Status)
	assert.Equal(t, multigresv1alpha1.ReasonEndpointReady, cond.Reason)
	assert.Contains(t, cond.Message, "lb.example.com")

	// Now verify that if we remove the fresh cell (only stale remains),
	// the aggregate is 0 and condition becomes NoReadyGateways.
	cl2 := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster.DeepCopy(), gwSvc, staleCell).
		WithStatusSubresource(&multigresv1alpha1.MultigresCluster{}).
		Build()

	r2 := &MultigresClusterReconciler{
		Client:   cl2,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(10),
	}

	clusterCopy2 := cluster.DeepCopy()
	err = r2.updateStatus(t.Context(), clusterCopy2)
	require.NoError(t, err)

	cond2 := meta.FindStatusCondition(
		clusterCopy2.Status.Conditions,
		multigresv1alpha1.ConditionGatewayExternalReady,
	)
	require.NotNil(t, cond2)
	assert.Equal(t, metav1.ConditionFalse, cond2.Status)
	assert.Equal(t, multigresv1alpha1.ReasonNoReadyGateways, cond2.Reason)
}
