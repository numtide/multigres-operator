//go:build integration
// +build integration

package etcd_test

import (
	"testing"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/resource-handler/controller/testutil"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	etcdcontroller "github.com/numtide/multigres-operator/pkg/resource-handler/controller/etcd"
)

func TestSetupWithManager(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	cfg := testutil.SetUpEnvtest(t)
	mgr := testutil.SetUpManager(t, cfg, scheme)
	testutil.StartManager(t, mgr)

	if err := (&etcdcontroller.EtcdReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		t.Fatalf("Failed to create controller, %v", err)
	}
}

func TestEtcdReconciliation(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	tests := map[string]struct {
		etcd            *multigresv1alpha1.Etcd
		existingObjects []client.Object
		failureConfig   *testutil.FailureConfig
		wantResources   []client.Object
		// TODO: If wantErr is false but failureConfig is set, assertions may fail
		// due to failure injection. This should be addressed when we need to test
		// partial failures that don't prevent reconciliation success.
		wantErr     bool
		wantRequeue bool
		assertFunc  func(t *testing.T, c client.Client, etcd *multigresv1alpha1.Etcd)
	}{
		"something": {
			etcd: &multigresv1alpha1.Etcd{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-etcd",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.EtcdSpec{},
			},
			wantResources: []client.Object{
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-etcd",
						Namespace: "default",
						Labels: map[string]string{
							"app.kubernetes.io/component":  "etcd",
							"app.kubernetes.io/instance":   "test-etcd",
							"app.kubernetes.io/managed-by": "multigres-operator",
							"app.kubernetes.io/name":       "multigres",
							"app.kubernetes.io/part-of":    "multigres",
							"multigres.com/cell":           "multigres-global-topo",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion:         "multigres.com/v1alpha1",
								Kind:               "Etcd",
								Name:               "test-etcd",
								Controller:         ptr.To(true),
								BlockOwnerDeletion: ptr.To(true),
							},
						},
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas:    ptr.To(int32(3)),
						ServiceName: "test-etcd-headless",
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"app.kubernetes.io/component":  "etcd",
								"app.kubernetes.io/instance":   "test-etcd",
								"app.kubernetes.io/managed-by": "multigres-operator",
								"app.kubernetes.io/name":       "multigres",
								"app.kubernetes.io/part-of":    "multigres",
								"multigres.com/cell":           "multigres-global-topo",
							},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									"app.kubernetes.io/component":  "etcd",
									"app.kubernetes.io/instance":   "test-etcd",
									"app.kubernetes.io/managed-by": "multigres-operator",
									"app.kubernetes.io/name":       "multigres",
									"app.kubernetes.io/part-of":    "multigres",
									"multigres.com/cell":           "multigres-global-topo",
								},
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "etcd",
										Image: "gcr.io/etcd-development/etcd:v3.5.9",
										Ports: []corev1.ContainerPort{
											{Name: "client", ContainerPort: 2379, Protocol: corev1.ProtocolTCP},
											{Name: "peer", ContainerPort: 2380, Protocol: corev1.ProtocolTCP},
										},
										Env: []corev1.EnvVar{
											{
												Name: "POD_NAME",
												ValueFrom: &corev1.EnvVarSource{
													FieldRef: &corev1.ObjectFieldSelector{
														APIVersion: "v1",
														FieldPath:  "metadata.name",
													},
												},
											},
											{
												Name: "POD_NAMESPACE",
												ValueFrom: &corev1.EnvVarSource{
													FieldRef: &corev1.ObjectFieldSelector{
														APIVersion: "v1",
														FieldPath:  "metadata.namespace",
													},
												},
											},
											{Name: "ETCD_NAME", Value: "$(POD_NAME)"},
											{Name: "ETCD_DATA_DIR", Value: "/var/lib/etcd"},
											{Name: "ETCD_LISTEN_CLIENT_URLS", Value: "http://0.0.0.0:2379"},
											{Name: "ETCD_LISTEN_PEER_URLS", Value: "http://0.0.0.0:2380"},
											{Name: "ETCD_ADVERTISE_CLIENT_URLS", Value: "http://$(POD_NAME).test-etcd-headless.$(POD_NAMESPACE).svc.cluster.local:2379"},
											{Name: "ETCD_INITIAL_ADVERTISE_PEER_URLS", Value: "http://$(POD_NAME).test-etcd-headless.$(POD_NAMESPACE).svc.cluster.local:2380"},
											{Name: "ETCD_INITIAL_CLUSTER_STATE", Value: "new"},
											{Name: "ETCD_INITIAL_CLUSTER_TOKEN", Value: "test-etcd"},
											{Name: "ETCD_INITIAL_CLUSTER", Value: "test-etcd-0=http://test-etcd-0.test-etcd-headless.default.svc.cluster.local:2380,test-etcd-1=http://test-etcd-1.test-etcd-headless.default.svc.cluster.local:2380,test-etcd-2=http://test-etcd-2.test-etcd-headless.default.svc.cluster.local:2380"},
										},
										VolumeMounts: []corev1.VolumeMount{
											{Name: "data", MountPath: "/var/lib/etcd"},
										},
									},
								},
							},
						},
						VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
							{
								ObjectMeta: metav1.ObjectMeta{Name: "data"},
								Spec: corev1.PersistentVolumeClaimSpec{
									AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
									Resources: corev1.VolumeResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceStorage: resource.MustParse("10Gi"),
										},
									},
									VolumeMode: ptr.To(corev1.PersistentVolumeFilesystem),
								},
								Status: corev1.PersistentVolumeClaimStatus{
									Phase: corev1.ClaimPending,
								},
							},
						},
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-etcd",
						Namespace: "default",
						Labels: map[string]string{
							"app.kubernetes.io/component":  "etcd",
							"app.kubernetes.io/instance":   "test-etcd",
							"app.kubernetes.io/managed-by": "multigres-operator",
							"app.kubernetes.io/name":       "multigres",
							"app.kubernetes.io/part-of":    "multigres",
							"multigres.com/cell":           "multigres-global-topo",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion:         "multigres.com/v1alpha1",
								Kind:               "Etcd",
								Name:               "test-etcd",
								Controller:         ptr.To(true),
								BlockOwnerDeletion: ptr.To(true),
							},
						},
					},
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeClusterIP,
						Ports: []corev1.ServicePort{
							{Name: "client", Port: 2379, TargetPort: intstr.FromString("client"), Protocol: corev1.ProtocolTCP},
						},
						Selector: map[string]string{
							"app.kubernetes.io/component":  "etcd",
							"app.kubernetes.io/instance":   "test-etcd",
							"app.kubernetes.io/managed-by": "multigres-operator",
							"app.kubernetes.io/name":       "multigres",
							"app.kubernetes.io/part-of":    "multigres",
							"multigres.com/cell":           "multigres-global-topo",
						},
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-etcd-headless",
						Namespace: "default",
						Labels: map[string]string{
							"app.kubernetes.io/component":  "etcd",
							"app.kubernetes.io/instance":   "test-etcd",
							"app.kubernetes.io/managed-by": "multigres-operator",
							"app.kubernetes.io/name":       "multigres",
							"app.kubernetes.io/part-of":    "multigres",
							"multigres.com/cell":           "multigres-global-topo",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion:         "multigres.com/v1alpha1",
								Kind:               "Etcd",
								Name:               "test-etcd",
								Controller:         ptr.To(true),
								BlockOwnerDeletion: ptr.To(true),
							},
						},
					},
					Spec: corev1.ServiceSpec{
						Type:      corev1.ServiceTypeClusterIP,
						ClusterIP: corev1.ClusterIPNone,
						Ports: []corev1.ServicePort{
							{Name: "client", Port: 2379, TargetPort: intstr.FromString("client"), Protocol: corev1.ProtocolTCP},
							{Name: "peer", Port: 2380, TargetPort: intstr.FromString("peer"), Protocol: corev1.ProtocolTCP},
						},
						Selector: map[string]string{
							"app.kubernetes.io/component":  "etcd",
							"app.kubernetes.io/instance":   "test-etcd",
							"app.kubernetes.io/managed-by": "multigres-operator",
							"app.kubernetes.io/name":       "multigres",
							"app.kubernetes.io/part-of":    "multigres",
							"multigres.com/cell":           "multigres-global-topo",
						},
						PublishNotReadyAddresses: true,
					},
				},
			},
		},
		// "another test": {
		// 	etcd: &multigresv1alpha1.Etcd{
		// 		ObjectMeta: metav1.ObjectMeta{
		// 			Name:      "test-etcd",
		// 			Namespace: "default",
		// 		},
		// 		Spec: multigresv1alpha1.EtcdSpec{},
		// 	},
		// },
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			mgr := testutil.SetUpEnvtestManager(t, scheme)

			watcher := testutil.NewResourceWatcher(t, ctx, mgr,
				testutil.WithCmpOpts(
					testutil.IgnoreKubernetesMetadata(),
					testutil.IgnoreServiceRuntimeFields(),
					testutil.IgnoreStatefulSetRuntimeFields(),
					testutil.IgnorePodSpecDefaults(),
					testutil.IgnoreStatefulSetSpecDefaults(),
				),
				testutil.WithExtraResource(&multigresv1alpha1.Etcd{}),
			)
			client := mgr.GetClient()

			etcdReconciler := &etcdcontroller.EtcdReconciler{
				Client: mgr.GetClient(),
				Scheme: mgr.GetScheme(),
			}
			if err := etcdReconciler.SetupWithManager(mgr, controller.Options{
				// Needed for the parallel test runs
				SkipNameValidation: ptr.To(true),
			}); err != nil {
				t.Fatalf("Failed to create controller, %v", err)
			}

			if err := client.Create(ctx, tc.etcd); err != nil {
				t.Fatalf("Failed to create the initial item, %v", err)
			}

			if err := watcher.WaitForMatch(tc.wantResources...); err != nil {
				t.Errorf("Resources mismatch:\n%v", err)
			}
		})
	}

}
