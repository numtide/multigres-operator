//go:build e2e

package e2e_test

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/testutil"
	"github.com/numtide/multigres-operator/pkg/util/metadata"
	nameutil "github.com/numtide/multigres-operator/pkg/util/name"
)

func TestCellCreatesDeploymentAndService(t *testing.T) {
	t.Parallel()

	mgr, c := setUpOperator(t)
	ctx := t.Context()

	watcher := testutil.NewResourceWatcher(t, ctx, mgr,
		testutil.WithCmpOpts(
			testutil.IgnoreMetaRuntimeFields(),
			testutil.IgnoreServiceRuntimeFields(),
			testutil.IgnoreDeploymentRuntimeFields(),
			testutil.IgnorePodSpecDefaults(),
			testutil.IgnoreProbeDefaults(),
			testutil.IgnoreDeploymentSpecDefaults(),
		),
		testutil.WithExtraResource(&multigresv1alpha1.Cell{}),
	)

	cell := &multigresv1alpha1.Cell{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "e2e-cell",
			Namespace: "default",
			Labels:    map[string]string{"multigres.com/cluster": "e2e-cluster"},
		},
		Spec: multigresv1alpha1.CellSpec{
			Name: "zone1",
			Zone: "us-east-1a",
			Images: multigresv1alpha1.CellImages{
				MultiGateway: "ghcr.io/multigres/multigres:main",
			},
			MultiGateway: multigresv1alpha1.StatelessSpec{
				Replicas: ptr.To(int32(2)),
			},
			GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
				Address:        "global-topo:2379",
				RootPath:       "/multigres/global",
				Implementation: "etcd",
			},
			TopoServer: &multigresv1alpha1.LocalTopoServerSpec{
				Etcd: &multigresv1alpha1.EtcdSpec{},
			},
		},
	}

	if err := c.Create(ctx, cell); err != nil {
		t.Fatalf("Failed to create Cell: %v", err)
	}

	clusterName := "e2e-cluster"
	cellName := "zone1"
	zone := "us-east-1a"

	hashedDeployName := nameutil.JoinWithConstraints(
		nameutil.DefaultConstraints,
		clusterName, cellName, "multigateway",
	)
	hashedSvcName := nameutil.JoinWithConstraints(
		nameutil.ServiceConstraints,
		clusterName, cellName, "multigateway",
	)

	labels := map[string]string{
		"app.kubernetes.io/component":  "multigateway",
		"app.kubernetes.io/instance":   clusterName,
		"app.kubernetes.io/managed-by": "multigres-operator",
		"app.kubernetes.io/name":       "multigres",
		"app.kubernetes.io/part-of":    "multigres",
		"multigres.com/cell":           cellName,
		"multigres.com/zone":           zone,
	}
	selectorLabels := metadata.GetSelectorLabels(labels)
	ownerRefs := []metav1.OwnerReference{{
		APIVersion:         "multigres.com/v1alpha1",
		Kind:               "Cell",
		Name:               "e2e-cell",
		Controller:         ptr.To(true),
		BlockOwnerDeletion: ptr.To(true),
	}}

	wantResources := []client.Object{
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:            hashedDeployName,
				Namespace:       "default",
				Labels:          labels,
				OwnerReferences: ownerRefs,
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: ptr.To(int32(2)),
				Selector: &metav1.LabelSelector{
					MatchLabels: selectorLabels,
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: labels,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "multigateway",
								Image: "ghcr.io/multigres/multigres:main",
								Args: []string{
									"multigateway",
									"--http-port", "15100",
									"--grpc-port", "15170",
									"--pg-port", "15432",
									"--topo-global-server-addresses", "global-topo:2379",
									"--topo-global-root", "/multigres/global",
									"--cell", "zone1",
								},
								Ports: []corev1.ContainerPort{
									{Name: "http", ContainerPort: 15100, Protocol: corev1.ProtocolTCP},
									{Name: "grpc", ContainerPort: 15170, Protocol: corev1.ProtocolTCP},
									{Name: "postgres", ContainerPort: 15432, Protocol: corev1.ProtocolTCP},
								},
								StartupProbe: &corev1.Probe{
									ProbeHandler: corev1.ProbeHandler{
										HTTPGet: &corev1.HTTPGetAction{
											Path: "/ready",
											Port: intstr.FromInt32(15100),
										},
									},
									PeriodSeconds:    5,
									FailureThreshold: 30,
								},
								LivenessProbe: &corev1.Probe{
									ProbeHandler: corev1.ProbeHandler{
										HTTPGet: &corev1.HTTPGetAction{
											Path: "/live",
											Port: intstr.FromInt32(15100),
										},
									},
									PeriodSeconds: 10,
								},
								ReadinessProbe: &corev1.Probe{
									ProbeHandler: corev1.ProbeHandler{
										HTTPGet: &corev1.HTTPGetAction{
											Path: "/ready",
											Port: intstr.FromInt32(15100),
										},
									},
									PeriodSeconds: 5,
								},
							},
						},
					},
				},
			},
		},
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:            hashedSvcName,
				Namespace:       "default",
				Labels:          labels,
				OwnerReferences: ownerRefs,
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeClusterIP,
				Ports: []corev1.ServicePort{
					{Name: "http", Port: 15100, TargetPort: intstr.FromString("http"), Protocol: corev1.ProtocolTCP},
					{Name: "grpc", Port: 15170, TargetPort: intstr.FromString("grpc"), Protocol: corev1.ProtocolTCP},
					{Name: "postgres", Port: 15432, TargetPort: intstr.FromString("postgres"), Protocol: corev1.ProtocolTCP},
				},
				Selector: selectorLabels,
			},
		},
	}

	if err := watcher.WaitForMatch(wantResources...); err != nil {
		t.Errorf("Cell resources mismatch:\n%v", err)
	}
}
