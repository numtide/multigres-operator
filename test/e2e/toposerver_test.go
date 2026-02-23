//go:build e2e

package e2e_test

import (
	"fmt"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/testutil"
	"github.com/numtide/multigres-operator/pkg/util/metadata"
)

func TestTopoServerCreatesStatefulSetAndServices(t *testing.T) {
	t.Parallel()

	mgr, c, ns := setUpOperator(t)
	ctx := t.Context()

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

	topoServer := &multigresv1alpha1.TopoServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "e2e-topo",
			Namespace: ns,
			Labels:    map[string]string{"multigres.com/cluster": "e2e-cluster"},
		},
		Spec: multigresv1alpha1.TopoServerSpec{
			Etcd: &multigresv1alpha1.EtcdSpec{},
		},
	}

	if err := c.Create(ctx, topoServer); err != nil {
		t.Fatalf("Failed to create TopoServer: %v", err)
	}

	clusterName := "e2e-cluster"
	labels := map[string]string{
		"app.kubernetes.io/component":  "toposerver",
		"app.kubernetes.io/instance":   clusterName,
		"app.kubernetes.io/managed-by": "multigres-operator",
		"app.kubernetes.io/name":       "multigres",
		"app.kubernetes.io/part-of":    "multigres",
		"multigres.com/cluster":        clusterName,
	}
	selectorLabels := metadata.GetSelectorLabels(labels)
	ownerRefs := []metav1.OwnerReference{{
		APIVersion:         "multigres.com/v1alpha1",
		Kind:               "TopoServer",
		Name:               "e2e-topo",
		Controller:         ptr.To(true),
		BlockOwnerDeletion: ptr.To(true),
	}}

	wantResources := []client.Object{
		&appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:            "e2e-topo",
				Namespace:       ns,
				Labels:          labels,
				OwnerReferences: ownerRefs,
			},
			Spec: appsv1.StatefulSetSpec{
				Replicas:    ptr.To(int32(3)),
				ServiceName: "e2e-topo-headless",
				Selector: &metav1.LabelSelector{
					MatchLabels: selectorLabels,
				},
				PersistentVolumeClaimRetentionPolicy: &appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy{
					WhenDeleted: appsv1.RetainPersistentVolumeClaimRetentionPolicyType,
					WhenScaled:  appsv1.RetainPersistentVolumeClaimRetentionPolicyType,
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: labels,
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
									{Name: "ETCD_ADVERTISE_CLIENT_URLS", Value: "http://$(POD_NAME).e2e-topo-headless.$(POD_NAMESPACE).svc.cluster.local:2379"},
									{Name: "ETCD_INITIAL_ADVERTISE_PEER_URLS", Value: "http://$(POD_NAME).e2e-topo-headless.$(POD_NAMESPACE).svc.cluster.local:2380"},
									{Name: "ETCD_INITIAL_CLUSTER_STATE", Value: "new"},
									{Name: "ETCD_INITIAL_CLUSTER_TOKEN", Value: "e2e-topo"},
									{Name: "ETCD_INITIAL_CLUSTER", Value: fmt.Sprintf(
									"e2e-topo-0=http://e2e-topo-0.e2e-topo-headless.%[1]s.svc.cluster.local:2380,"+
										"e2e-topo-1=http://e2e-topo-1.e2e-topo-headless.%[1]s.svc.cluster.local:2380,"+
										"e2e-topo-2=http://e2e-topo-2.e2e-topo-headless.%[1]s.svc.cluster.local:2380", ns)},
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
				Name:            "e2e-topo",
				Namespace:       ns,
				Labels:          labels,
				OwnerReferences: ownerRefs,
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeClusterIP,
				Ports: []corev1.ServicePort{
					{Name: "client", Port: 2379, TargetPort: intstr.FromString("client"), Protocol: corev1.ProtocolTCP},
				},
				Selector: selectorLabels,
			},
		},
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:            "e2e-topo-headless",
				Namespace:       ns,
				Labels:          labels,
				OwnerReferences: ownerRefs,
			},
			Spec: corev1.ServiceSpec{
				Type:      corev1.ServiceTypeClusterIP,
				ClusterIP: corev1.ClusterIPNone,
				Ports: []corev1.ServicePort{
					{Name: "client", Port: 2379, TargetPort: intstr.FromString("client"), Protocol: corev1.ProtocolTCP},
					{Name: "peer", Port: 2380, TargetPort: intstr.FromString("peer"), Protocol: corev1.ProtocolTCP},
				},
				Selector:                 selectorLabels,
				PublishNotReadyAddresses: true,
			},
		},
	}

	if err := watcher.WaitForMatch(wantResources...); err != nil {
		t.Errorf("TopoServer resources mismatch:\n%v", err)
	}
}
