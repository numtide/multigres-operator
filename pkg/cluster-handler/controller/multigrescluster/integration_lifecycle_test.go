//go:build integration
// +build integration

package multigrescluster_test

import (
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/resolver"
	nameutil "github.com/numtide/multigres-operator/pkg/util/name"
)

func TestMultigresCluster_Lifecycle(t *testing.T) {
	t.Parallel()

	t.Run("TableGroup Long Name Hashing", func(t *testing.T) {
		t.Parallel()
		k8sClient, _ := setupIntegration(t)
		// Name length math: 25 (cluster) + 8 (db) + 25 (tg) + 2 (hyphens) = 60 chars.
		longClusterName := "valid-cluster-name-123456" // 25 chars
		longTGName := "valid-tg-name-12345678901"      // 25 chars
		// Wait, we need > 63 chars to trigger hashing for labels.
		// Let's use max allowed: 25 (cluster) + 30 (db) + 25 (tg) + 2 = 82 chars.

		cluster := &multigresv1alpha1.MultigresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: longClusterName, Namespace: testNamespace},
			Spec: multigresv1alpha1.MultigresClusterSpec{
				Databases: []multigresv1alpha1.DatabaseConfig{
					{
						Name:    "postgres",
						Default: true,
						TableGroups: []multigresv1alpha1.TableGroupConfig{
							// Must provide the mandatory default one to pass "system catalog" validation
							{Name: "default", Default: true},
							// And the one that triggers length limit (Default: false to avoid "must be named default" rule)
							{Name: multigresv1alpha1.TableGroupName(longTGName), Default: false},
						},
					},
				},
			},
		}

		if err := k8sClient.Create(t.Context(), cluster); err != nil {
			t.Fatalf("Failed to create cluster: %v", err)
		}

		// Verify the TableGroup DOES exist (hashed)
		time.Sleep(2 * time.Second)
		tgName := nameutil.JoinWithConstraints(
			nameutil.DefaultConstraints,
			longClusterName,
			"postgres",
			longTGName,
		)
		err := k8sClient.Get(t.Context(), client.ObjectKey{Name: tgName, Namespace: testNamespace}, &multigresv1alpha1.TableGroup{})
		if err != nil {
			t.Errorf("Expected TableGroup %s to be created using hashing, but got error: %v", tgName, err)
		}

		// Ensure Cluster exists
		fetchedCluster := &multigresv1alpha1.MultigresCluster{}
		if err := k8sClient.Get(t.Context(), client.ObjectKeyFromObject(cluster), fetchedCluster); err != nil {
			t.Error("Cluster should exist")
		}
	})

	t.Run("Annotation Limit (Bombing)", func(t *testing.T) {
		t.Parallel()
		k8sClient, watcher := setupIntegration(t)
		// 250 chars is near limit (256). If controller appends to this value, it might fail.
		longAnnotation := strings.Repeat("a", 250)
		cluster := &multigresv1alpha1.MultigresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "short-annot-bomb", Namespace: testNamespace},
			Spec: multigresv1alpha1.MultigresClusterSpec{
				MultiAdmin: &multigresv1alpha1.MultiAdminConfig{
					Spec: &multigresv1alpha1.StatelessSpec{
						PodAnnotations: map[string]string{"heavy-annotation": longAnnotation},
					},
				},
			},
		}
		if err := k8sClient.Create(t.Context(), cluster); err != nil {
			t.Fatalf("Failed to create cluster: %v", err)
		}

		// Verify MultiAdmin Deployment created successfully WITH annotation
		deploy := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:            "short-annot-bomb-multiadmin",
				Namespace:       testNamespace,
				Labels:          clusterLabels(t, "short-annot-bomb", "multiadmin", ""),
				OwnerReferences: clusterOwnerRefs(t, "short-annot-bomb"),
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: ptr.To(resolver.DefaultAdminReplicas),
				Selector: &metav1.LabelSelector{
					MatchLabels: clusterLabels(t, "short-annot-bomb", "multiadmin", ""),
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels:      clusterLabels(t, "short-annot-bomb", "multiadmin", ""),
						Annotations: map[string]string{"heavy-annotation": longAnnotation},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "multiadmin",
								Image: resolver.DefaultMultiAdminImage,
								Command: []string{
									"/multigres/bin/multiadmin",
								},
								Args: []string{
									"--http-port=18000",
									"--grpc-port=18070",
									"--topo-global-server-addresses=short-annot-bomb-global-topo." + testNamespace + ".svc:2379",
									"--topo-global-root=/multigres/global",
									"--service-map=grpc-multiadmin",
									"--pprof-http=true",
								},
								Ports: []corev1.ContainerPort{
									{
										Name:          "http",
										ContainerPort: 18000,
										Protocol:      corev1.ProtocolTCP,
									},
									{
										Name:          "grpc",
										ContainerPort: 18070,
										Protocol:      corev1.ProtocolTCP,
									},
								},
								Resources: resolver.DefaultResourcesAdmin(),
								LivenessProbe: &corev1.Probe{
									ProbeHandler: corev1.ProbeHandler{
										HTTPGet: &corev1.HTTPGetAction{
											Path:   "/live",
											Port:   intstr.FromInt(18000),
											Scheme: corev1.URISchemeHTTP,
										},
									},
									InitialDelaySeconds: 10,
									TimeoutSeconds:      1,
									PeriodSeconds:       10,
									SuccessThreshold:    1,
									FailureThreshold:    3,
								},
								ReadinessProbe: &corev1.Probe{
									ProbeHandler: corev1.ProbeHandler{
										HTTPGet: &corev1.HTTPGetAction{
											Path:   "/ready",
											Port:   intstr.FromInt(18000),
											Scheme: corev1.URISchemeHTTP,
										},
									},
									InitialDelaySeconds: 5,
									TimeoutSeconds:      1,
									PeriodSeconds:       5,
									SuccessThreshold:    1,
									FailureThreshold:    3,
								},
							},
						},
					},
				},
			},
		}
		if err := watcher.WaitForMatch(deploy); err != nil {
			t.Errorf("MultiAdmin deployment failed to create with massive annotation: %v", err)
		}
	})

	t.Run("Cell Renaming (Zombie Cell)", func(t *testing.T) {
		t.Parallel()
		k8sClient, watcher := setupIntegration(t)
		cluster := &multigresv1alpha1.MultigresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "zombie-test", Namespace: testNamespace},
			Spec: multigresv1alpha1.MultigresClusterSpec{
				Cells: []multigresv1alpha1.CellConfig{{Name: "zone-a", Zone: "us-east-1a"}},
			},
		}
		if err := k8sClient.Create(t.Context(), cluster); err != nil {
			t.Fatalf("Failed to create cluster: %v", err)
		}

		// Wait for zone-a
		zoneA := &multigresv1alpha1.Cell{
			ObjectMeta: metav1.ObjectMeta{
				Name: nameutil.JoinWithConstraints(
					nameutil.DefaultConstraints,
					"zombie-test",
					"zone-a",
				),
				Namespace:       testNamespace,
				Labels:          clusterLabels(t, "zombie-test", "", "zone-a"),
				OwnerReferences: clusterOwnerRefs(t, "zombie-test"),
			},
			Spec: multigresv1alpha1.CellSpec{
				Name: "zone-a",
				Zone: "us-east-1a",
				Images: multigresv1alpha1.CellImages{
					MultiGateway:    resolver.DefaultMultiGatewayImage,
					ImagePullPolicy: resolver.DefaultImagePullPolicy,
				},
				MultiGateway: multigresv1alpha1.StatelessSpec{
					Replicas:  ptr.To(int32(1)),
					Resources: resolver.DefaultResourcesGateway(), // FIX: Expect defaults
				},
				AllCells: []multigresv1alpha1.CellName{"zone-a"},
				GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
					Address:        "zombie-test-global-topo.default.svc:2379",
					RootPath:       "/multigres/global",
					Implementation: "etcd2",
				},
				TopologyReconciliation: multigresv1alpha1.TopologyReconciliation{
					RegisterCell: true,
					PrunePoolers: true,
				},
			},
		}
		if err := watcher.WaitForMatch(zoneA); err != nil {
			t.Fatalf("Failed to wait for initial cell: %v", err)
		}

		// Rename Cell
		if err := k8sClient.Get(t.Context(), client.ObjectKeyFromObject(cluster), cluster); err != nil {
			t.Fatal(err)
		}
		cluster.Spec.Cells = []multigresv1alpha1.CellConfig{{Name: "zone-b", Zone: "us-east-1b"}}
		if err := k8sClient.Update(t.Context(), cluster); err != nil {
			t.Fatalf("Failed to update cluster: %v", err)
		}

		// Wait for zone-b creation
		zoneB := &multigresv1alpha1.Cell{
			ObjectMeta: metav1.ObjectMeta{
				Name: nameutil.JoinWithConstraints(
					nameutil.DefaultConstraints,
					"zombie-test",
					"zone-b",
				),
				Namespace:       testNamespace,
				Labels:          clusterLabels(t, "zombie-test", "", "zone-b"),
				OwnerReferences: clusterOwnerRefs(t, "zombie-test"),
			},
			Spec: multigresv1alpha1.CellSpec{
				Name: "zone-b",
				Zone: "us-east-1b",
				Images: multigresv1alpha1.CellImages{
					MultiGateway:    resolver.DefaultMultiGatewayImage,
					ImagePullPolicy: resolver.DefaultImagePullPolicy,
				},
				MultiGateway: multigresv1alpha1.StatelessSpec{
					Replicas:  ptr.To(int32(1)),
					Resources: resolver.DefaultResourcesGateway(), // FIX: Expect defaults
				},
				AllCells: []multigresv1alpha1.CellName{"zone-b"},
				GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
					Address:        "zombie-test-global-topo.default.svc:2379",
					RootPath:       "/multigres/global",
					Implementation: "etcd2",
				},
				TopologyReconciliation: multigresv1alpha1.TopologyReconciliation{
					RegisterCell: true,
					PrunePoolers: true,
				},
			},
		}
		if err := watcher.WaitForMatch(zoneB); err != nil {
			t.Fatalf("Failed to wait for new cell: %v", err)
		}

		// Verify zone-a deletion (Zombie Check)
		deleted := false
		for i := 0; i < 20; i++ {
			err := k8sClient.Get(t.Context(), client.ObjectKeyFromObject(zoneA), &multigresv1alpha1.Cell{})
			if apierrors.IsNotFound(err) {
				deleted = true
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		if !deleted {
			t.Error("Zombie Cell 'zone-a' was not deleted after rename")
		}
	})

	t.Run("Mutability (Image Update)", func(t *testing.T) {
		t.Parallel()
		k8sClient, watcher := setupIntegration(t)
		cluster := &multigresv1alpha1.MultigresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "mut-test", Namespace: testNamespace},
			Spec: multigresv1alpha1.MultigresClusterSpec{
				Images: multigresv1alpha1.ClusterImages{MultiAdmin: "admin:v1"},
			},
		}
		if err := k8sClient.Create(t.Context(), cluster); err != nil {
			t.Fatalf("Failed to create cluster: %v", err)
		}

		// Wait for v1
		deploy := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{

				Name:            "mut-test-multiadmin",
				Namespace:       testNamespace,
				Labels:          clusterLabels(t, "mut-test", "multiadmin", ""),
				OwnerReferences: clusterOwnerRefs(t, "mut-test"),
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: ptr.To(resolver.DefaultAdminReplicas),
				Selector: &metav1.LabelSelector{
					MatchLabels: clusterLabels(t, "mut-test", "multiadmin", ""),
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: clusterLabels(t, "mut-test", "multiadmin", ""),
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name:  "multiadmin",
							Image: "admin:v1",
							Command: []string{
								"/multigres/bin/multiadmin",
							},
							Args: []string{
								"--http-port=18000",
								"--grpc-port=18070",
								"--topo-global-server-addresses=mut-test-global-topo." + testNamespace + ".svc:2379",
								"--topo-global-root=/multigres/global",
								"--service-map=grpc-multiadmin",
								"--pprof-http=true",
							},
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: 18000,
									Protocol:      corev1.ProtocolTCP,
								},
								{
									Name:          "grpc",
									ContainerPort: 18070,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							Resources: resolver.DefaultResourcesAdmin(),
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path:   "/live",
										Port:   intstr.FromInt(18000),
										Scheme: corev1.URISchemeHTTP,
									},
								},
								InitialDelaySeconds: 10,
								TimeoutSeconds:      1,
								PeriodSeconds:       10,
								SuccessThreshold:    1,
								FailureThreshold:    3,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path:   "/ready",
										Port:   intstr.FromInt(18000),
										Scheme: corev1.URISchemeHTTP,
									},
								},
								InitialDelaySeconds: 5,
								TimeoutSeconds:      1,
								PeriodSeconds:       5,
								SuccessThreshold:    1,
								FailureThreshold:    3,
							},
						}},
					},
				},
			},
		}
		if err := watcher.WaitForMatch(deploy); err != nil {
			t.Fatalf("Failed to wait for initial deployment v1: %v", err)
		}

		// Update Image
		if err := k8sClient.Get(t.Context(), client.ObjectKeyFromObject(cluster), cluster); err != nil {
			t.Fatal(err)
		}
		cluster.Spec.Images.MultiAdmin = "admin:v2"
		if err := k8sClient.Update(t.Context(), cluster); err != nil {
			t.Fatalf("Failed to update cluster: %v", err)
		}

		// Verify v2
		deployV2 := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:            "mut-test-multiadmin",
				Namespace:       testNamespace,
				Labels:          clusterLabels(t, "mut-test", "multiadmin", ""),
				OwnerReferences: clusterOwnerRefs(t, "mut-test"),
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: ptr.To(resolver.DefaultAdminReplicas),
				Selector: &metav1.LabelSelector{
					MatchLabels: clusterLabels(t, "mut-test", "multiadmin", ""),
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: clusterLabels(t, "mut-test", "multiadmin", ""),
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name:  "multiadmin",
							Image: "admin:v2",
							Command: []string{
								"/multigres/bin/multiadmin",
							},
							Args: []string{
								"--http-port=18000",
								"--grpc-port=18070",
								"--topo-global-server-addresses=mut-test-global-topo." + testNamespace + ".svc:2379",
								"--topo-global-root=/multigres/global",
								"--service-map=grpc-multiadmin",
								"--pprof-http=true",
							},
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: 18000,
									Protocol:      corev1.ProtocolTCP,
								},
								{
									Name:          "grpc",
									ContainerPort: 18070,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							Resources: resolver.DefaultResourcesAdmin(),
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path:   "/live",
										Port:   intstr.FromInt(18000),
										Scheme: corev1.URISchemeHTTP,
									},
								},
								InitialDelaySeconds: 10,
								TimeoutSeconds:      1,
								PeriodSeconds:       10,
								SuccessThreshold:    1,
								FailureThreshold:    3,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path:   "/ready",
										Port:   intstr.FromInt(18000),
										Scheme: corev1.URISchemeHTTP,
									},
								},
								InitialDelaySeconds: 5,
								TimeoutSeconds:      1,
								PeriodSeconds:       5,
								SuccessThreshold:    1,
								FailureThreshold:    3,
							},
						}},
					},
				},
			},
		}
		if err := watcher.WaitForMatch(deployV2); err != nil {
			t.Errorf("Deployment failed to update to v2: %v", err)
		}
	})
}
