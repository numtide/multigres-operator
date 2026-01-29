//go:build integration
// +build integration

package cell_test

import (
	"path/filepath"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	cellcontroller "github.com/numtide/multigres-operator/pkg/resource-handler/controller/cell"
	"github.com/numtide/multigres-operator/pkg/testutil"
	nameutil "github.com/numtide/multigres-operator/pkg/util/name"
)

func TestSetupWithManager(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	mgr := testutil.SetUpEnvtestManager(t, scheme,
		testutil.WithCRDPaths(
			filepath.Join("../../../../", "config", "crd", "bases"),
		),
	)

	if err := (&cellcontroller.CellReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr, controller.Options{
		SkipNameValidation: ptr.To(true),
	}); err != nil {
		t.Fatalf("Failed to create controller, %v", err)
	}
}

func TestCellReconciliation(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	tests := map[string]struct {
		cell            *multigresv1alpha1.Cell
		existingObjects []client.Object
		wantResources   []client.Object
		wantErr         bool
		assertFunc      func(t *testing.T, c client.Client, cell *multigresv1alpha1.Cell)
	}{
		"simple cell with default replicas": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cell",
					Namespace: "default",
					Labels:    map[string]string{"multigres.com/cluster": "test-cluster"},
				},
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone1",
					Zone: "us-west-1a",
					Images: multigresv1alpha1.CellImages{
						MultiGateway: "ghcr.io/multigres/multigres:main",
					},
					MultiGateway: multigresv1alpha1.StatelessSpec{
						Replicas: ptr.To(int32(2)),
					},
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:        "global-topo:2379",
						RootPath:       "/multigres/global",
						Implementation: "etcd2",
					},
					TopoServer: &multigresv1alpha1.LocalTopoServerSpec{
						Etcd: &multigresv1alpha1.EtcdSpec{},
					},
				},
			},
			wantResources: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "test-cell-multigateway",
						Namespace:       "default",
						Labels:          cellLabels(t, "test-cell-multigateway", "multigateway", "zone1", "us-west-1a"),
						OwnerReferences: cellOwnerRefs(t, "test-cell"),
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: ptr.To(int32(2)),
						Selector: &metav1.LabelSelector{
							MatchLabels: cellLabels(t, "test-cell-multigateway", "multigateway", "zone1", "us-west-1a"),
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: cellLabels(t, "test-cell-multigateway", "multigateway", "zone1", "us-west-1a"),
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
											tcpPort(t, "http", 15100),
											tcpPort(t, "grpc", 15170),
											tcpPort(t, "postgres", 15432),
										},
									},
								},
							},
						},
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "test-cell-multigateway",
						Namespace:       "default",
						Labels:          cellLabels(t, "test-cell-multigateway", "multigateway", "zone1", "us-west-1a"),
						OwnerReferences: cellOwnerRefs(t, "test-cell"),
					},
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeClusterIP,
						Ports: []corev1.ServicePort{
							tcpServicePort(t, "http", 15100),
							tcpServicePort(t, "grpc", 15170),
							tcpServicePort(t, "postgres", 15432),
						},
						Selector: cellLabels(t, "test-cell-multigateway", "multigateway", "zone1", "us-west-1a"),
					},
				},
			},
		},
		"cell with custom replicas": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "custom-replicas-cell",
					Namespace: "default",
					Labels:    map[string]string{"multigres.com/cluster": "test-cluster"},
				},
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone2",
					Zone: "us-west-1b",
					Images: multigresv1alpha1.CellImages{
						MultiGateway: "ghcr.io/multigres/multigres:main",
					},
					MultiGateway: multigresv1alpha1.StatelessSpec{
						Replicas: ptr.To(int32(3)),
					},
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:        "global-topo:2379",
						RootPath:       "/multigres/global",
						Implementation: "etcd2",
					},
					TopoServer: &multigresv1alpha1.LocalTopoServerSpec{
						Etcd: &multigresv1alpha1.EtcdSpec{},
					},
				},
			},
			wantResources: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "custom-replicas-cell-multigateway",
						Namespace:       "default",
						Labels:          cellLabels(t, "custom-replicas-cell-multigateway", "multigateway", "zone2", "us-west-1b"),
						OwnerReferences: cellOwnerRefs(t, "custom-replicas-cell"),
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: ptr.To(int32(3)),
						Selector: &metav1.LabelSelector{
							MatchLabels: cellLabels(t, "custom-replicas-cell-multigateway", "multigateway", "zone2", "us-west-1b"),
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: cellLabels(t, "custom-replicas-cell-multigateway", "multigateway", "zone2", "us-west-1b"),
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
											"--cell", "zone2",
										},
										Ports: []corev1.ContainerPort{
											tcpPort(t, "http", 15100),
											tcpPort(t, "grpc", 15170),
											tcpPort(t, "postgres", 15432),
										},
									},
								},
							},
						},
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "custom-replicas-cell-multigateway",
						Namespace:       "default",
						Labels:          cellLabels(t, "custom-replicas-cell-multigateway", "multigateway", "zone2", "us-west-1b"),
						OwnerReferences: cellOwnerRefs(t, "custom-replicas-cell"),
					},
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeClusterIP,
						Ports: []corev1.ServicePort{
							tcpServicePort(t, "http", 15100),
							tcpServicePort(t, "grpc", 15170),
							tcpServicePort(t, "postgres", 15432),
						},
						Selector: cellLabels(t, "custom-replicas-cell-multigateway", "multigateway", "zone2", "us-west-1b"),
					},
				},
			},
		},
		"cell with custom images": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "custom-images-cell",
					Namespace: "default",
					Labels:    map[string]string{"multigres.com/cluster": "test-cluster"},
				},
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone3",
					Zone: "us-west-1c",
					Images: multigresv1alpha1.CellImages{
						MultiGateway: "custom/multigateway:v1.0.0",
					},
					MultiGateway: multigresv1alpha1.StatelessSpec{
						Replicas: ptr.To(int32(2)),
					},
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:        "global-topo:2379",
						RootPath:       "/multigres/global",
						Implementation: "etcd2",
					},
					TopoServer: &multigresv1alpha1.LocalTopoServerSpec{
						Etcd: &multigresv1alpha1.EtcdSpec{},
					},
				},
			},
			wantResources: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "custom-images-cell-multigateway",
						Namespace:       "default",
						Labels:          cellLabels(t, "custom-images-cell-multigateway", "multigateway", "zone3", "us-west-1c"),
						OwnerReferences: cellOwnerRefs(t, "custom-images-cell"),
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: ptr.To(int32(2)),
						Selector: &metav1.LabelSelector{
							MatchLabels: cellLabels(t, "custom-images-cell-multigateway", "multigateway", "zone3", "us-west-1c"),
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: cellLabels(t, "custom-images-cell-multigateway", "multigateway", "zone3", "us-west-1c"),
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "multigateway",
										Image: "custom/multigateway:v1.0.0",
										Args: []string{
											"multigateway",
											"--http-port", "15100",
											"--grpc-port", "15170",
											"--pg-port", "15432",
											"--topo-global-server-addresses", "global-topo:2379",
											"--topo-global-root", "/multigres/global",
											"--cell", "zone3",
										},
										Ports: []corev1.ContainerPort{
											tcpPort(t, "http", 15100),
											tcpPort(t, "grpc", 15170),
											tcpPort(t, "postgres", 15432),
										},
									},
								},
							},
						},
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "custom-images-cell-multigateway",
						Namespace:       "default",
						Labels:          cellLabels(t, "custom-images-cell-multigateway", "multigateway", "zone3", "us-west-1c"),
						OwnerReferences: cellOwnerRefs(t, "custom-images-cell"),
					},
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeClusterIP,
						Ports: []corev1.ServicePort{
							tcpServicePort(t, "http", 15100),
							tcpServicePort(t, "grpc", 15170),
							tcpServicePort(t, "postgres", 15432),
						},
						Selector: cellLabels(t, "custom-images-cell-multigateway", "multigateway", "zone3", "us-west-1c"),
					},
				},
			},
		},
		"cell with affinity": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "affinity-cell",
					Namespace: "default",
					Labels:    map[string]string{"multigres.com/cluster": "test-cluster"},
				},
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone4",
					Zone: "us-west-1d",
					Images: multigresv1alpha1.CellImages{
						MultiGateway: "ghcr.io/multigres/multigres:main",
					},
					MultiGateway: multigresv1alpha1.StatelessSpec{
						Replicas: ptr.To(int32(2)),
						Affinity: &corev1.Affinity{
							NodeAffinity: &corev1.NodeAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
									NodeSelectorTerms: []corev1.NodeSelectorTerm{
										{
											MatchExpressions: []corev1.NodeSelectorRequirement{
												{
													Key:      "node-type",
													Operator: corev1.NodeSelectorOpIn,
													Values:   []string{"gateway"},
												},
											},
										},
									},
								},
							},
						},
					},
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:        "global-topo:2379",
						RootPath:       "/multigres/global",
						Implementation: "etcd2",
					},
					TopoServer: &multigresv1alpha1.LocalTopoServerSpec{
						Etcd: &multigresv1alpha1.EtcdSpec{},
					},
				},
			},
			wantResources: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "affinity-cell-multigateway",
						Namespace:       "default",
						Labels:          cellLabels(t, "affinity-cell-multigateway", "multigateway", "zone4", "us-west-1d"),
						OwnerReferences: cellOwnerRefs(t, "affinity-cell"),
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: ptr.To(int32(2)),
						Selector: &metav1.LabelSelector{
							MatchLabels: cellLabels(t, "affinity-cell-multigateway", "multigateway", "zone4", "us-west-1d"),
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: cellLabels(t, "affinity-cell-multigateway", "multigateway", "zone4", "us-west-1d"),
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
											"--cell", "zone4",
										},
										Ports: []corev1.ContainerPort{
											tcpPort(t, "http", 15100),
											tcpPort(t, "grpc", 15170),
											tcpPort(t, "postgres", 15432),
										},
									},
								},
								Affinity: &corev1.Affinity{
									NodeAffinity: &corev1.NodeAffinity{
										RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
											NodeSelectorTerms: []corev1.NodeSelectorTerm{
												{
													MatchExpressions: []corev1.NodeSelectorRequirement{
														{
															Key:      "node-type",
															Operator: corev1.NodeSelectorOpIn,
															Values:   []string{"gateway"},
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "affinity-cell-multigateway",
						Namespace:       "default",
						Labels:          cellLabels(t, "affinity-cell-multigateway", "multigateway", "zone4", "us-west-1d"),
						OwnerReferences: cellOwnerRefs(t, "affinity-cell"),
					},
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeClusterIP,
						Ports: []corev1.ServicePort{
							tcpServicePort(t, "http", 15100),
							tcpServicePort(t, "grpc", 15170),
							tcpServicePort(t, "postgres", 15432),
						},
						Selector: cellLabels(t, "affinity-cell-multigateway", "multigateway", "zone4", "us-west-1d"),
					},
				},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			mgr := testutil.SetUpEnvtestManager(t, scheme,
				testutil.WithCRDPaths(
					filepath.Join("../../../../", "config", "crd", "bases"),
				),
			)

			watcher := testutil.NewResourceWatcher(t, ctx, mgr,
				testutil.WithCmpOpts(
					testutil.IgnoreMetaRuntimeFields(),
					testutil.IgnoreServiceRuntimeFields(),
					testutil.IgnoreDeploymentRuntimeFields(),
					testutil.IgnorePodSpecDefaults(),
					testutil.IgnoreDeploymentSpecDefaults(),
				),
				testutil.WithExtraResource(&multigresv1alpha1.Cell{}),
			)
			client := mgr.GetClient()

			cellReconciler := &cellcontroller.CellReconciler{
				Client: mgr.GetClient(),
				Scheme: mgr.GetScheme(),
			}
			if err := cellReconciler.SetupWithManager(mgr, controller.Options{
				// Needed for the parallel test runs
				SkipNameValidation: ptr.To(true),
			}); err != nil {
				t.Fatalf("Failed to create controller, %v", err)
			}

			if err := client.Create(ctx, tc.cell); err != nil {
				t.Fatalf("Failed to create the initial item, %v", err)
			}

			// Patch wantResources with hashed names
			for _, obj := range tc.wantResources {
				clusterName := tc.cell.Labels["multigres.com/cluster"]

				// Replicate controller logic for naming
				hashedDeployName := nameutil.JoinWithConstraints(
					nameutil.DefaultConstraints,
					clusterName,
					string(tc.cell.Spec.Name),
					"multigateway",
				)
				hashedSvcName := nameutil.JoinWithConstraints(
					nameutil.ServiceConstraints,
					clusterName,
					string(tc.cell.Spec.Name),
					"multigateway",
				)

				// Update labels
				labels := obj.GetLabels()
				if labels != nil {
					labels["app.kubernetes.io/instance"] = clusterName // Instance is cluster name
					obj.SetLabels(labels)
				}

				// Update selector if Deployment
				if deploy, ok := obj.(*appsv1.Deployment); ok {
					obj.SetName(hashedDeployName)
					if deploy.Spec.Selector != nil {
						deploy.Spec.Selector.MatchLabels["app.kubernetes.io/instance"] = clusterName
					}
					if deploy.Spec.Template.ObjectMeta.Labels != nil {
						deploy.Spec.Template.ObjectMeta.Labels["app.kubernetes.io/instance"] = clusterName
					}
				}
				// Update selector if Service
				if svc, ok := obj.(*corev1.Service); ok {
					obj.SetName(hashedSvcName)
					if svc.Spec.Selector != nil {
						svc.Spec.Selector["app.kubernetes.io/instance"] = clusterName
					}
				}
			}

			if err := watcher.WaitForMatch(tc.wantResources...); err != nil {
				t.Errorf("Resources mismatch:\n%v", err)
			}
		})
	}
}

// Test helpers

// cellLabels returns standard labels for cell resources in tests
func cellLabels(t testing.TB, instanceName, component, cellName, zone string) map[string]string {
	t.Helper()
	return map[string]string{
		"app.kubernetes.io/component":  component,
		"app.kubernetes.io/instance":   "test-cluster", // Use literal cluster name for instance label
		"app.kubernetes.io/managed-by": "multigres-operator",
		"app.kubernetes.io/name":       "multigres",
		"app.kubernetes.io/part-of":    "multigres",
		"multigres.com/cell":           cellName,
		"multigres.com/zone":           zone,
	}
}

// cellOwnerRefs returns owner references for a Cell resource
func cellOwnerRefs(t testing.TB, cellName string) []metav1.OwnerReference {
	t.Helper()
	return []metav1.OwnerReference{{
		APIVersion:         "multigres.com/v1alpha1",
		Kind:               "Cell",
		Name:               cellName,
		Controller:         ptr.To(true),
		BlockOwnerDeletion: ptr.To(true),
	}}
}

// tcpPort creates a simple TCP container port
func tcpPort(t testing.TB, name string, port int32) corev1.ContainerPort {
	t.Helper()
	return corev1.ContainerPort{Name: name, ContainerPort: port, Protocol: corev1.ProtocolTCP}
}

// tcpServicePort creates a TCP service port with named target
func tcpServicePort(t testing.TB, name string, port int32) corev1.ServicePort {
	t.Helper()
	return corev1.ServicePort{Name: name, Port: port, TargetPort: intstr.FromString(name), Protocol: corev1.ProtocolTCP}
}
