//go:build integration
// +build integration

package multigrescluster_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/testutil"
	"github.com/numtide/multigres-operator/pkg/util/metadata"
)

const pollInterval = 200 * time.Millisecond

func TestExternalGateway_EnableDisableLifecycle(t *testing.T) {
	t.Parallel()

	const clusterName = "gw-lifecycle"

	k8sClient, watcher := setupIntegration(t)

	// Step 1: Create cluster with externalGateway enabled and annotations.
	cluster := &multigresv1alpha1.MultigresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: testNamespace,
		},
		Spec: multigresv1alpha1.MultigresClusterSpec{
			TemplateDefaults: multigresv1alpha1.TemplateDefaults{
				CoreTemplate:  "default",
				CellTemplate:  "default",
				ShardTemplate: "default",
			},
			Cells: []multigresv1alpha1.CellConfig{
				{Name: "zone-a", Zone: "us-east-1a"},
			},
			ExternalGateway: &multigresv1alpha1.ExternalGatewayConfig{
				Enabled: true,
				Annotations: map[string]string{
					"service.beta.kubernetes.io/aws-load-balancer-scheme": "internet-facing",
				},
			},
		},
	}

	require.NoError(t, k8sClient.Create(t.Context(), cluster))

	// Add extra comparison options for LoadBalancer Service runtime fields.
	watcher.SetCmpOpts(
		testutil.IgnoreMetaRuntimeFields(),
		testutil.IgnoreServiceRuntimeFields(),
		cmpopts.IgnoreFields(corev1.ServiceSpec{},
			"ExternalTrafficPolicy",
			"AllocateLoadBalancerNodePorts",
		),
		cmpopts.IgnoreFields(corev1.ServicePort{}, "NodePort"),
	)

	// Step 2: Verify the global Service is created as LoadBalancer with annotations.
	gwLabels := clusterLabels(t, clusterName, "multigateway", "")
	expectedGwSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName + "-multigateway",
			Namespace: testNamespace,
			Labels:    gwLabels,
			Annotations: map[string]string{
				"service.beta.kubernetes.io/aws-load-balancer-scheme": "internet-facing",
			},
			OwnerReferences: clusterOwnerRefs(t, clusterName),
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				metadata.LabelAppComponent: metadata.ComponentMultiGateway,
				metadata.LabelAppInstance:  clusterName,
			},
			Type: corev1.ServiceTypeLoadBalancer,
			Ports: []corev1.ServicePort{
				{
					Name:       "postgres",
					Port:       15432,
					TargetPort: intstr.FromString("postgres"),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	require.NoError(t, watcher.WaitForMatch(expectedGwSvc),
		"global multigateway Service should be LoadBalancer with annotations")

	// Step 3: Verify initial condition is AwaitingLoadBalancer (no ingress yet).
	// The cluster status should have GatewayExternalReady=False/AwaitingLoadBalancer.
	assert.Eventually(t, func() bool {
		var mgc multigresv1alpha1.MultigresCluster
		if err := k8sClient.Get(t.Context(), client.ObjectKeyFromObject(cluster), &mgc); err != nil {
			return false
		}
		cond := meta.FindStatusCondition(mgc.Status.Conditions, multigresv1alpha1.ConditionGatewayExternalReady)
		return cond != nil &&
			cond.Status == metav1.ConditionFalse &&
			cond.Reason == multigresv1alpha1.ReasonAwaitingLoadBalancer
	}, testTimeout, pollInterval, "condition should be False/AwaitingLoadBalancer before LB ingress")

	// Step 4: Simulate cloud controller populating LB ingress on the Service.
	var gwSvc corev1.Service
	require.NoError(t, k8sClient.Get(t.Context(), client.ObjectKey{
		Name:      clusterName + "-multigateway",
		Namespace: testNamespace,
	}, &gwSvc))

	gwSvc.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{
		{Hostname: "a1234.elb.us-east-1.amazonaws.com"},
	}
	require.NoError(t, k8sClient.Status().Update(t.Context(), &gwSvc),
		"simulating cloud controller LB ingress population")

	// Step 5: Verify condition transitions to NoReadyGateways (endpoint assigned, 0 ready gateways).
	// No Cell CRs have gatewayReadyReplicas > 0 yet, so aggregate is 0.
	assert.Eventually(t, func() bool {
		var mgc multigresv1alpha1.MultigresCluster
		if err := k8sClient.Get(t.Context(), client.ObjectKeyFromObject(cluster), &mgc); err != nil {
			return false
		}
		cond := meta.FindStatusCondition(mgc.Status.Conditions, multigresv1alpha1.ConditionGatewayExternalReady)
		if cond == nil {
			return false
		}
		endpointPopulated := mgc.Status.Gateway != nil && mgc.Status.Gateway.ExternalEndpoint != ""
		return endpointPopulated &&
			cond.Status == metav1.ConditionFalse &&
			cond.Reason == multigresv1alpha1.ReasonNoReadyGateways
	}, testTimeout, pollInterval, "condition should be False/NoReadyGateways after LB ingress assigned")

	// Step 6: Simulate Cell reporting ready gateways by updating Cell status.
	var cellList multigresv1alpha1.CellList
	require.NoError(t, k8sClient.List(t.Context(), &cellList,
		client.InNamespace(testNamespace),
		client.MatchingLabels{"multigres.com/cluster": clusterName},
	))
	require.NotEmpty(t, cellList.Items, "expected at least one Cell CR")

	cell := &cellList.Items[0]
	cell.Status.GatewayReadyReplicas = 1
	cell.Status.GatewayReplicas = 1
	cell.Status.ObservedGeneration = cell.Generation
	require.NoError(t, k8sClient.Status().Update(t.Context(), cell),
		"simulating Cell reporting ready gateway replicas")

	// Step 7: Verify condition transitions to EndpointReady.
	assert.Eventually(t, func() bool {
		var mgc multigresv1alpha1.MultigresCluster
		if err := k8sClient.Get(t.Context(), client.ObjectKeyFromObject(cluster), &mgc); err != nil {
			return false
		}
		cond := meta.FindStatusCondition(mgc.Status.Conditions, multigresv1alpha1.ConditionGatewayExternalReady)
		return cond != nil &&
			cond.Status == metav1.ConditionTrue &&
			cond.Reason == multigresv1alpha1.ReasonEndpointReady &&
			mgc.Status.Gateway != nil &&
			mgc.Status.Gateway.ExternalEndpoint == "a1234.elb.us-east-1.amazonaws.com"
	}, testTimeout, pollInterval, "condition should be True/EndpointReady with endpoint in status")

	// Verify observedGeneration is set on the condition.
	var mgcCheck multigresv1alpha1.MultigresCluster
	require.NoError(t, k8sClient.Get(t.Context(), client.ObjectKeyFromObject(cluster), &mgcCheck))
	cond := meta.FindStatusCondition(mgcCheck.Status.Conditions, multigresv1alpha1.ConditionGatewayExternalReady)
	require.NotNil(t, cond)
	assert.Equal(t, mgcCheck.Generation, cond.ObservedGeneration,
		"observedGeneration should match cluster generation")

	// Step 8: Disable external gateway and verify reversion.
	require.NoError(t, k8sClient.Get(t.Context(), client.ObjectKeyFromObject(cluster), cluster))
	cluster.Spec.ExternalGateway = &multigresv1alpha1.ExternalGatewayConfig{
		Enabled: false,
	}
	require.NoError(t, k8sClient.Update(t.Context(), cluster),
		"disabling external gateway")

	// Step 9: Verify Service reverts to ClusterIP with no gateway annotations.
	expectedClusterIPSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            clusterName + "-multigateway",
			Namespace:       testNamespace,
			Labels:          gwLabels,
			OwnerReferences: clusterOwnerRefs(t, clusterName),
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				metadata.LabelAppComponent: metadata.ComponentMultiGateway,
				metadata.LabelAppInstance:  clusterName,
			},
			Type: corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				{
					Name:       "postgres",
					Port:       15432,
					TargetPort: intstr.FromString("postgres"),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	require.NoError(t, watcher.WaitForMatch(expectedClusterIPSvc),
		"global multigateway Service should revert to ClusterIP after disabling")

	// Step 10: Verify gateway status is nil and condition is removed.
	assert.Eventually(t, func() bool {
		var mgc multigresv1alpha1.MultigresCluster
		if err := k8sClient.Get(t.Context(), client.ObjectKeyFromObject(cluster), &mgc); err != nil {
			return false
		}
		cond := meta.FindStatusCondition(mgc.Status.Conditions, multigresv1alpha1.ConditionGatewayExternalReady)
		return mgc.Status.Gateway == nil && cond == nil
	}, testTimeout, pollInterval, "gateway status should be nil and condition removed after disabling")
}

func TestExternalGateway_NoReadyGatewaysTransition(t *testing.T) {
	t.Parallel()

	const clusterName = "gw-no-ready"

	k8sClient, _ := setupIntegration(t)

	// Step 1: Create cluster with externalGateway enabled.
	cluster := &multigresv1alpha1.MultigresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: testNamespace,
		},
		Spec: multigresv1alpha1.MultigresClusterSpec{
			TemplateDefaults: multigresv1alpha1.TemplateDefaults{
				CoreTemplate:  "default",
				CellTemplate:  "default",
				ShardTemplate: "default",
			},
			Cells: []multigresv1alpha1.CellConfig{
				{Name: "zone-a", Zone: "us-east-1a"},
			},
			ExternalGateway: &multigresv1alpha1.ExternalGatewayConfig{
				Enabled: true,
			},
		},
	}

	require.NoError(t, k8sClient.Create(t.Context(), cluster))

	// Step 2: Wait for initial AwaitingLoadBalancer condition (no LB ingress yet).
	assert.Eventually(t, func() bool {
		var mgc multigresv1alpha1.MultigresCluster
		if err := k8sClient.Get(t.Context(), client.ObjectKeyFromObject(cluster), &mgc); err != nil {
			return false
		}
		cond := meta.FindStatusCondition(mgc.Status.Conditions, multigresv1alpha1.ConditionGatewayExternalReady)
		return cond != nil &&
			cond.Status == metav1.ConditionFalse &&
			cond.Reason == multigresv1alpha1.ReasonAwaitingLoadBalancer
	}, testTimeout, pollInterval, "condition should be False/AwaitingLoadBalancer initially")

	// Step 3: Simulate cloud controller populating LB ingress.
	var gwSvc corev1.Service
	require.NoError(t, k8sClient.Get(t.Context(), client.ObjectKey{
		Name:      clusterName + "-multigateway",
		Namespace: testNamespace,
	}, &gwSvc))

	gwSvc.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{
		{Hostname: "no-ready-gw.elb.us-east-1.amazonaws.com"},
	}
	require.NoError(t, k8sClient.Status().Update(t.Context(), &gwSvc))

	// Step 4: Verify condition is False/NoReadyGateways — endpoint assigned but
	// zero ready gateways across all Cells.
	assert.Eventually(t, func() bool {
		var mgc multigresv1alpha1.MultigresCluster
		if err := k8sClient.Get(t.Context(), client.ObjectKeyFromObject(cluster), &mgc); err != nil {
			return false
		}
		cond := meta.FindStatusCondition(mgc.Status.Conditions, multigresv1alpha1.ConditionGatewayExternalReady)
		if cond == nil {
			return false
		}
		return cond.Status == metav1.ConditionFalse &&
			cond.Reason == multigresv1alpha1.ReasonNoReadyGateways &&
			strings.Contains(cond.Message, "no multigateway pods are ready") &&
			mgc.Status.Gateway != nil &&
			mgc.Status.Gateway.ExternalEndpoint == "no-ready-gw.elb.us-east-1.amazonaws.com"
	}, testTimeout, pollInterval, "condition should be False/NoReadyGateways with endpoint populated")

	// Step 5: Simulate Cell reporting gatewayReadyReplicas > 0.
	var cellList multigresv1alpha1.CellList
	require.NoError(t, k8sClient.List(t.Context(), &cellList,
		client.InNamespace(testNamespace),
		client.MatchingLabels{"multigres.com/cluster": clusterName},
	))
	require.NotEmpty(t, cellList.Items, "expected at least one Cell CR")

	cell := &cellList.Items[0]
	cell.Status.GatewayReadyReplicas = 2
	cell.Status.GatewayReplicas = 2
	cell.Status.ObservedGeneration = cell.Generation
	require.NoError(t, k8sClient.Status().Update(t.Context(), cell))

	// Step 6: Verify condition transitions to True/EndpointReady.
	assert.Eventually(t, func() bool {
		var mgc multigresv1alpha1.MultigresCluster
		if err := k8sClient.Get(t.Context(), client.ObjectKeyFromObject(cluster), &mgc); err != nil {
			return false
		}
		cond := meta.FindStatusCondition(mgc.Status.Conditions, multigresv1alpha1.ConditionGatewayExternalReady)
		return cond != nil &&
			cond.Status == metav1.ConditionTrue &&
			cond.Reason == multigresv1alpha1.ReasonEndpointReady &&
			strings.Contains(cond.Message, "is serving traffic") &&
			mgc.Status.Gateway != nil &&
			mgc.Status.Gateway.ExternalEndpoint == "no-ready-gw.elb.us-east-1.amazonaws.com"
	}, testTimeout, pollInterval, "condition should transition to True/EndpointReady after Cell reports ready gateways")

	// Verify observedGeneration is set correctly.
	var mgcCheck multigresv1alpha1.MultigresCluster
	require.NoError(t, k8sClient.Get(t.Context(), client.ObjectKeyFromObject(cluster), &mgcCheck))
	cond := meta.FindStatusCondition(mgcCheck.Status.Conditions, multigresv1alpha1.ConditionGatewayExternalReady)
	require.NotNil(t, cond)
	assert.Equal(t, mgcCheck.Generation, cond.ObservedGeneration,
		"observedGeneration should match cluster generation")
}
