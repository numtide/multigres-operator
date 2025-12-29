//go:build integration
// +build integration

package toposerver_test

import (
	"path/filepath"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	toposervercontroller "github.com/numtide/multigres-operator/pkg/resource-handler/controller/toposerver"
	"github.com/numtide/multigres-operator/pkg/testutil"
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

	if err := (&toposervercontroller.TopoServerReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr, controller.Options{
		SkipNameValidation: ptr.To(true),
	}); err != nil {
		t.Fatalf("Failed to create controller, %v", err)
	}
}

func TestTopoServerReconciliation(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	tests := map[string]struct {
		toposerver      *multigresv1alpha1.TopoServer
		existingObjects []client.Object
		failureConfig   *testutil.FailureConfig
		wantResources   []client.Object
		wantErr         bool
		wantRequeue     bool
		assertFunc      func(t *testing.T, c client.Client, toposerver *multigresv1alpha1.TopoServer)
	}{
		"simple toposerver input": {
			toposerver: &multigresv1alpha1.TopoServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-toposerver",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.TopoServerSpec{
					Etcd: &multigresv1alpha1.EtcdSpec{},
				},
			},
			wantResources: []client.Object{
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "test-toposerver",
						Namespace:       "default",
						Labels:          toposerverLabels(t, "test-toposerver"),
						OwnerReferences: toposerverOwnerRefs(t, "test-toposerver"),
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas:    ptr.To(int32(3)),
						ServiceName: "test-toposerver-headless",
						Selector: &metav1.LabelSelector{
							MatchLabels: toposerverLabels(t, "test-toposerver"),
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: toposerverLabels(t, "test-toposerver"),
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "etcd",
										Image: "gcr.io/etcd-development/etcd:v3.5.9",
										Ports: []corev1.ContainerPort{
											tcpPort(t, "client", 2379),
											tcpPort(t, "peer", 2380),
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
											{Name: "ETCD_ADVERTISE_CLIENT_URLS", Value: "http://$(POD_NAME).test-toposerver-headless.$(POD_NAMESPACE).svc.cluster.local:2379"},
											{Name: "ETCD_INITIAL_ADVERTISE_PEER_URLS", Value: "http://$(POD_NAME).test-toposerver-headless.$(POD_NAMESPACE).svc.cluster.local:2380"},
											{Name: "ETCD_INITIAL_CLUSTER_STATE", Value: "new"},
											{Name: "ETCD_INITIAL_CLUSTER_TOKEN", Value: "test-toposerver"},
											{Name: "ETCD_INITIAL_CLUSTER", Value: "test-toposerver-0=http://test-toposerver-0.test-toposerver-headless.default.svc.cluster.local:2380,test-toposerver-1=http://test-toposerver-1.test-toposerver-headless.default.svc.cluster.local:2380,test-toposerver-2=http://test-toposerver-2.test-toposerver-headless.default.svc.cluster.local:2380"},
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
						Name:            "test-toposerver",
						Namespace:       "default",
						Labels:          toposerverLabels(t, "test-toposerver"),
						OwnerReferences: toposerverOwnerRefs(t, "test-toposerver"),
					},
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeClusterIP,
						Ports: []corev1.ServicePort{
							tcpServicePort(t, "client", 2379),
						},
						Selector: toposerverLabels(t, "test-toposerver"),
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "test-toposerver-headless",
						Namespace:       "default",
						Labels:          toposerverLabels(t, "test-toposerver"),
						OwnerReferences: toposerverOwnerRefs(t, "test-toposerver"),
					},
					Spec: corev1.ServiceSpec{
						Type:      corev1.ServiceTypeClusterIP,
						ClusterIP: corev1.ClusterIPNone,
						Ports: []corev1.ServicePort{
							tcpServicePort(t, "client", 2379),
							tcpServicePort(t, "peer", 2380),
						},
						Selector:                 toposerverLabels(t, "test-toposerver"),
						PublishNotReadyAddresses: true,
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
					testutil.IgnoreStatefulSetRuntimeFields(),
					testutil.IgnorePodSpecDefaults(),
					testutil.IgnoreStatefulSetSpecDefaults(),
				),
				testutil.WithExtraResource(&multigresv1alpha1.TopoServer{}),
			)
			client := mgr.GetClient()

			toposerverReconciler := &toposervercontroller.TopoServerReconciler{
				Client: mgr.GetClient(),
				Scheme: mgr.GetScheme(),
			}
			if err := toposerverReconciler.SetupWithManager(mgr, controller.Options{
				// Needed for the parallel test runs
				SkipNameValidation: ptr.To(true),
			}); err != nil {
				t.Fatalf("Failed to create controller, %v", err)
			}

			if err := client.Create(ctx, tc.toposerver); err != nil {
				t.Fatalf("Failed to create the initial item, %v", err)
			}

			if err := watcher.WaitForMatch(tc.wantResources...); err != nil {
				t.Errorf("Resources mismatch:\n%v", err)
			}
		})
	}

}

// Test helpers

// toposerverLabels returns standard labels for toposerver resources in tests
func toposerverLabels(t testing.TB, instanceName string) map[string]string {
	t.Helper()
	return map[string]string{
		"app.kubernetes.io/component":  "toposerver",
		"app.kubernetes.io/instance":   instanceName,
		"app.kubernetes.io/managed-by": "multigres-operator",
		"app.kubernetes.io/name":       "multigres",
		"app.kubernetes.io/part-of":    "multigres",
	}
}

// toposerverOwnerRefs returns owner references for a TopoServer resource
func toposerverOwnerRefs(t testing.TB, toposerverName string) []metav1.OwnerReference {
	t.Helper()
	return []metav1.OwnerReference{{
		APIVersion:         "multigres.com/v1alpha1",
		Kind:               "TopoServer",
		Name:               toposerverName,
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
