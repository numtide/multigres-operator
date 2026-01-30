package resolver

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/testutil"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestResolver_PopulateClusterDefaults(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	// Fixture: An implicit "default" ShardTemplate exists in the namespace
	shardTplDefault := &multigresv1alpha1.ShardTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default",
			Namespace: "default",
		},
	}

	tests := map[string]struct {
		input   *multigresv1alpha1.MultigresCluster
		objects []client.Object
		want    *multigresv1alpha1.MultigresCluster
	}{
		"Empty Cluster: Applies All System Defaults": {
			input: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
			},
			want: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					Images: multigresv1alpha1.ClusterImages{
						Postgres:        DefaultPostgresImage,
						MultiAdmin:      DefaultMultiAdminImage,
						MultiAdminWeb:   DefaultMultiAdminWebImage,
						MultiOrch:       DefaultMultiOrchImage,
						MultiPooler:     DefaultMultiPoolerImage,
						MultiGateway:    DefaultMultiGatewayImage,
						ImagePullPolicy: DefaultImagePullPolicy,
					},
					TemplateDefaults: multigresv1alpha1.TemplateDefaults{
						CoreTemplate:  "",
						CellTemplate:  "",
						ShardTemplate: "",
					},
					Databases: []multigresv1alpha1.DatabaseConfig{
						{
							Name:    DefaultSystemDatabaseName,
							Default: true,
							TableGroups: []multigresv1alpha1.TableGroupConfig{
								{
									Name:    DefaultSystemTableGroupName,
									Default: true,
									Shards: []multigresv1alpha1.ShardConfig{
										{Name: "0-inf"},
									},
								},
							},
						},
					},
				},
			},
		},
		"Existing Database but No TableGroups: Inject TG and Shard": {
			input: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					Databases: []multigresv1alpha1.DatabaseConfig{
						{Name: "custom-db"},
					},
				},
			},
			want: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					Images: multigresv1alpha1.ClusterImages{
						Postgres:        DefaultPostgresImage,
						MultiAdmin:      DefaultMultiAdminImage,
						MultiAdminWeb:   DefaultMultiAdminWebImage,
						MultiOrch:       DefaultMultiOrchImage,
						MultiPooler:     DefaultMultiPoolerImage,
						MultiGateway:    DefaultMultiGatewayImage,
						ImagePullPolicy: DefaultImagePullPolicy,
					},
					TemplateDefaults: multigresv1alpha1.TemplateDefaults{
						CoreTemplate:  "",
						CellTemplate:  "",
						ShardTemplate: "",
					},
					Databases: []multigresv1alpha1.DatabaseConfig{
						{
							Name: "custom-db",
							TableGroups: []multigresv1alpha1.TableGroupConfig{
								{
									Name:    DefaultSystemTableGroupName,
									Default: true,
									Shards: []multigresv1alpha1.ShardConfig{
										{Name: "0-inf"},
									},
								},
							},
						},
					},
				},
			},
		},
		"Existing TableGroup but No Shards: Inject Shard 0 with Default Cells": {
			input: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					Cells: []multigresv1alpha1.CellConfig{
						{Name: "zone-a"},
						{Name: "zone-b"},
					},
					Databases: []multigresv1alpha1.DatabaseConfig{
						{
							Name: "custom-db",
							TableGroups: []multigresv1alpha1.TableGroupConfig{
								{Name: "my-tg"},
							},
						},
					},
				},
			},
			want: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					Images: multigresv1alpha1.ClusterImages{
						Postgres:        DefaultPostgresImage,
						MultiAdmin:      DefaultMultiAdminImage,
						MultiAdminWeb:   DefaultMultiAdminWebImage,
						MultiOrch:       DefaultMultiOrchImage,
						MultiPooler:     DefaultMultiPoolerImage,
						MultiGateway:    DefaultMultiGatewayImage,
						ImagePullPolicy: DefaultImagePullPolicy,
					},
					TemplateDefaults: multigresv1alpha1.TemplateDefaults{
						CoreTemplate:  "",
						CellTemplate:  "",
						ShardTemplate: "",
					},
					Cells: []multigresv1alpha1.CellConfig{
						{Name: "zone-a"},
						{Name: "zone-b"},
					},
					Databases: []multigresv1alpha1.DatabaseConfig{
						{
							Name: "custom-db",
							TableGroups: []multigresv1alpha1.TableGroupConfig{
								{
									Name: "my-tg",
									Shards: []multigresv1alpha1.ShardConfig{
										{
											Name: "0-inf",
											Spec: &multigresv1alpha1.ShardInlineSpec{
												MultiOrch: multigresv1alpha1.MultiOrchSpec{},
												Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
													"default": {
														Type: "readWrite",
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
		},
		// COVERAGE: Implicit Default ShardTemplate exists -> Shard 0 created, but defaults NOT injected.
		// This hits the `else { shouldInjectDefaults = false }` branch.
		"Implicit Default ShardTemplate Exists -> Shard 0 Created Empty": {
			input: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					Cells: []multigresv1alpha1.CellConfig{{Name: "zone-a"}},
					Databases: []multigresv1alpha1.DatabaseConfig{
						{
							Name: "db",
							TableGroups: []multigresv1alpha1.TableGroupConfig{
								{Name: "tg"},
							},
						},
					},
				},
			},
			objects: []client.Object{shardTplDefault}, // "default" exists!
			want: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					Images: multigresv1alpha1.ClusterImages{
						Postgres:        DefaultPostgresImage,
						MultiAdmin:      DefaultMultiAdminImage,
						MultiAdminWeb:   DefaultMultiAdminWebImage,
						MultiOrch:       DefaultMultiOrchImage,
						MultiPooler:     DefaultMultiPoolerImage,
						MultiGateway:    DefaultMultiGatewayImage,
						ImagePullPolicy: DefaultImagePullPolicy,
					},
					TemplateDefaults: multigresv1alpha1.TemplateDefaults{
						CoreTemplate:  "",
						CellTemplate:  "",
						ShardTemplate: "",
					},
					Cells: []multigresv1alpha1.CellConfig{{Name: "zone-a"}},
					Databases: []multigresv1alpha1.DatabaseConfig{
						{
							Name: "db",
							TableGroups: []multigresv1alpha1.TableGroupConfig{
								{
									Name: "tg",
									Shards: []multigresv1alpha1.ShardConfig{
										{
											Name: "0-inf",
											Spec: &multigresv1alpha1.ShardInlineSpec{
												MultiOrch: multigresv1alpha1.MultiOrchSpec{},
												// KEY: Pools is empty because implicit template takes over
												Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{},
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
		"Existing Shards: Do Not Inject": {
			input: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					Databases: []multigresv1alpha1.DatabaseConfig{
						{
							Name: "custom-db",
							TableGroups: []multigresv1alpha1.TableGroupConfig{
								{
									Name: "my-tg",
									Shards: []multigresv1alpha1.ShardConfig{
										{Name: "custom-shard"},
									},
								},
							},
						},
					},
				},
			},
			want: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					Images: multigresv1alpha1.ClusterImages{
						Postgres:        DefaultPostgresImage,
						MultiAdmin:      DefaultMultiAdminImage,
						MultiAdminWeb:   DefaultMultiAdminWebImage,
						MultiOrch:       DefaultMultiOrchImage,
						MultiPooler:     DefaultMultiPoolerImage,
						MultiGateway:    DefaultMultiGatewayImage,
						ImagePullPolicy: DefaultImagePullPolicy,
					},
					TemplateDefaults: multigresv1alpha1.TemplateDefaults{
						CoreTemplate:  "",
						CellTemplate:  "",
						ShardTemplate: "",
					},
					Databases: []multigresv1alpha1.DatabaseConfig{
						{
							Name: "custom-db",
							TableGroups: []multigresv1alpha1.TableGroupConfig{
								{
									Name: "my-tg",
									Shards: []multigresv1alpha1.ShardConfig{
										{Name: "custom-shard"},
									},
								},
							},
						},
					},
				},
			},
		},
		"Pre-populated Fields: Preserves Values": {
			input: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					Images: multigresv1alpha1.ClusterImages{
						Postgres:        "custom/postgres:16",
						MultiAdmin:      "custom/admin:1",
						MultiAdminWeb:   "custom/web:1",
						MultiOrch:       "custom/orch:1",
						MultiPooler:     "custom/pooler:1",
						MultiGateway:    "custom/gateway:1",
						ImagePullPolicy: "Always",
					},
					TemplateDefaults: multigresv1alpha1.TemplateDefaults{
						CoreTemplate:  "custom-core",
						CellTemplate:  "custom-cell",
						ShardTemplate: "custom-shard",
					},
					Databases: []multigresv1alpha1.DatabaseConfig{
						{
							Name: "custom-db",
							TableGroups: []multigresv1alpha1.TableGroupConfig{
								{
									Name: "custom-tg",
									Shards: []multigresv1alpha1.ShardConfig{
										{Name: "custom-0"},
									},
								},
							},
						},
					},
				},
			},
			want: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					Images: multigresv1alpha1.ClusterImages{
						Postgres:        "custom/postgres:16",
						MultiAdmin:      "custom/admin:1",
						MultiAdminWeb:   "custom/web:1",
						MultiOrch:       "custom/orch:1",
						MultiPooler:     "custom/pooler:1",
						MultiGateway:    "custom/gateway:1",
						ImagePullPolicy: "Always",
					},
					TemplateDefaults: multigresv1alpha1.TemplateDefaults{
						CoreTemplate:  "custom-core",
						CellTemplate:  "custom-cell",
						ShardTemplate: "custom-shard",
					},
					Databases: []multigresv1alpha1.DatabaseConfig{
						{
							Name: "custom-db",
							TableGroups: []multigresv1alpha1.TableGroupConfig{
								{
									Name: "custom-tg",
									Shards: []multigresv1alpha1.ShardConfig{
										{Name: "custom-0"},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := NewResolver(
				fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.objects...).Build(),
				"default",
				multigresv1alpha1.TemplateDefaults{},
			)

			got := tc.input.DeepCopy()
			if err := r.PopulateClusterDefaults(t.Context(), got); err != nil {
				t.Fatalf("PopulateClusterDefaults failed: %v", err)
			}

			if diff := cmp.Diff(tc.want, got, cmpopts.IgnoreUnexported(resource.Quantity{}), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestResolver_PopulateClusterDefaults_ClientError(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	errSim := errors.New("simulated error")
	mc := testutil.NewFakeClientWithFailures(fake.NewClientBuilder().WithScheme(scheme).Build(),
		&testutil.FailureConfig{
			OnGet: func(_ client.ObjectKey) error { return errSim },
		})

	r := NewResolver(mc, "default", multigresv1alpha1.TemplateDefaults{})
	// This input triggers the "check implicit shard template" path because ShardTemplate is empty
	input := &multigresv1alpha1.MultigresCluster{
		Spec: multigresv1alpha1.MultigresClusterSpec{
			Databases: []multigresv1alpha1.DatabaseConfig{{Name: "db"}},
		},
	}

	err := r.PopulateClusterDefaults(t.Context(), input)
	if err == nil || !errors.Is(err, errSim) {
		t.Errorf("Expected simulated error, got %v", err)
	}
}

func TestResolver_ResolveGlobalTopo(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	coreTpl, _, _, ns := setupFixtures(t)

	tests := map[string]struct {
		cluster *multigresv1alpha1.MultigresCluster
		objects []client.Object
		want    *multigresv1alpha1.GlobalTopoServerSpec
		wantErr bool
	}{
		"Inline Takes Precedence": {
			cluster: &multigresv1alpha1.MultigresCluster{
				Spec: multigresv1alpha1.MultigresClusterSpec{
					GlobalTopoServer: &multigresv1alpha1.GlobalTopoServerSpec{
						Etcd: &multigresv1alpha1.EtcdSpec{Image: "inline"},
					},
				},
			},
			want: &multigresv1alpha1.GlobalTopoServerSpec{
				Etcd: &multigresv1alpha1.EtcdSpec{
					Image:     "inline",
					Replicas:  ptr.To(DefaultEtcdReplicas),
					Resources: DefaultResourcesEtcd(),
					Storage:   multigresv1alpha1.StorageSpec{Size: DefaultEtcdStorageSize},
				},
			},
		},
		"Template Reference": {
			cluster: &multigresv1alpha1.MultigresCluster{
				Spec: multigresv1alpha1.MultigresClusterSpec{
					GlobalTopoServer: &multigresv1alpha1.GlobalTopoServerSpec{
						TemplateRef: "default",
					},
				},
			},
			objects: []client.Object{coreTpl},
			want: &multigresv1alpha1.GlobalTopoServerSpec{
				Etcd: &multigresv1alpha1.EtcdSpec{
					Image:     "core-default", // From fixture
					Replicas:  ptr.To(DefaultEtcdReplicas),
					Resources: DefaultResourcesEtcd(),
					Storage:   multigresv1alpha1.StorageSpec{Size: DefaultEtcdStorageSize},
				},
			},
		},
		// COVERAGE: Explicit Override of ALL fields to ensure merge logic branches are hit
		"Template Reference + Full Inline Override": {
			cluster: &multigresv1alpha1.MultigresCluster{
				Spec: multigresv1alpha1.MultigresClusterSpec{
					GlobalTopoServer: &multigresv1alpha1.GlobalTopoServerSpec{
						TemplateRef: "default",
						Etcd: &multigresv1alpha1.EtcdSpec{
							Image:    "override-image",
							Replicas: ptr.To(int32(99)),
							RootPath: "/override/path",
							Storage:  multigresv1alpha1.StorageSpec{Size: "99Gi"},
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("99Gi"),
								},
							},
						},
					},
				},
			},
			objects: []client.Object{coreTpl},
			want: &multigresv1alpha1.GlobalTopoServerSpec{
				Etcd: &multigresv1alpha1.EtcdSpec{
					Image:    "override-image",
					Replicas: ptr.To(int32(99)),
					RootPath: "/override/path",
					Storage:  multigresv1alpha1.StorageSpec{Size: "99Gi"},
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("99Gi"),
						},
					},
				},
			},
		},
		"Cluster Default Template": {
			cluster: &multigresv1alpha1.MultigresCluster{
				Spec: multigresv1alpha1.MultigresClusterSpec{
					TemplateDefaults: multigresv1alpha1.TemplateDefaults{
						CoreTemplate: "default",
					},
				},
			},
			objects: []client.Object{coreTpl},
			want: &multigresv1alpha1.GlobalTopoServerSpec{
				Etcd: &multigresv1alpha1.EtcdSpec{
					Image:     "core-default",
					Replicas:  ptr.To(DefaultEtcdReplicas),
					Resources: DefaultResourcesEtcd(),
					Storage:   multigresv1alpha1.StorageSpec{Size: DefaultEtcdStorageSize},
				},
			},
		},
		"Fallback (Implicit Default Not Found) -> Default Etcd": {
			cluster: &multigresv1alpha1.MultigresCluster{},
			objects: nil,
			want: &multigresv1alpha1.GlobalTopoServerSpec{
				Etcd: &multigresv1alpha1.EtcdSpec{
					Image:     DefaultEtcdImage,
					Replicas:  ptr.To(DefaultEtcdReplicas),
					Resources: DefaultResourcesEtcd(),
					Storage:   multigresv1alpha1.StorageSpec{Size: DefaultEtcdStorageSize},
				},
			},
		},
		"Missing Explicit Template -> Error": {
			cluster: &multigresv1alpha1.MultigresCluster{
				Spec: multigresv1alpha1.MultigresClusterSpec{
					GlobalTopoServer: &multigresv1alpha1.GlobalTopoServerSpec{
						TemplateRef: "missing",
					},
				},
			},
			wantErr: true,
		},
		"External Spec": {
			cluster: &multigresv1alpha1.MultigresCluster{
				Spec: multigresv1alpha1.MultigresClusterSpec{
					GlobalTopoServer: &multigresv1alpha1.GlobalTopoServerSpec{
						External: &multigresv1alpha1.ExternalTopoServerSpec{
							Endpoints: []multigresv1alpha1.EndpointUrl{"https://1.2.3.4:2379"},
						},
						Etcd: &multigresv1alpha1.EtcdSpec{Image: "ignored"},
					},
				},
			},
			want: &multigresv1alpha1.GlobalTopoServerSpec{
				External: &multigresv1alpha1.ExternalTopoServerSpec{
					Endpoints: []multigresv1alpha1.EndpointUrl{"https://1.2.3.4:2379"},
				},
				Etcd: nil, // Explicitly nilled out
			},
		},
		"CoreTemplate with GlobalTopo": {
			cluster: &multigresv1alpha1.MultigresCluster{
				Spec: multigresv1alpha1.MultigresClusterSpec{
					GlobalTopoServer: &multigresv1alpha1.GlobalTopoServerSpec{
						TemplateRef: "with-topo",
					},
				},
			},
			objects: []client.Object{
				&multigresv1alpha1.CoreTemplate{
					ObjectMeta: metav1.ObjectMeta{Name: "with-topo", Namespace: "default"},
					Spec: multigresv1alpha1.CoreTemplateSpec{
						GlobalTopoServer: &multigresv1alpha1.TopoServerSpec{
							Etcd: &multigresv1alpha1.EtcdSpec{Image: "core-image"},
						},
					},
				},
			},
			want: &multigresv1alpha1.GlobalTopoServerSpec{
				Etcd: &multigresv1alpha1.EtcdSpec{
					Image:     "core-image",
					Replicas:  ptr.To(DefaultEtcdReplicas), // Defaults applied
					Resources: DefaultResourcesEtcd(),
					Storage:   multigresv1alpha1.StorageSpec{Size: DefaultEtcdStorageSize},
				},
			},
		},
		"Template External -> Inline Etcd Override": {
			cluster: &multigresv1alpha1.MultigresCluster{
				Spec: multigresv1alpha1.MultigresClusterSpec{
					GlobalTopoServer: &multigresv1alpha1.GlobalTopoServerSpec{
						TemplateRef: "topo-external",
						Etcd:        &multigresv1alpha1.EtcdSpec{Image: "new-etcd"},
					},
				},
			},
			objects: []client.Object{
				&multigresv1alpha1.CoreTemplate{
					ObjectMeta: metav1.ObjectMeta{Name: "topo-external", Namespace: "default"},
					Spec: multigresv1alpha1.CoreTemplateSpec{
						GlobalTopoServer: &multigresv1alpha1.TopoServerSpec{
							// Template defines external, so Etcd is nil
							// Note: TopoServerSpec in CoreTemplate only has Etcd field in current definition?
							// Let's check TopoServerSpec definition again.
							// It says: type TopoServerSpec struct { Etcd *EtcdSpec }
							// It DOES NOT have External.
							// Wait, if it only has Etcd, how can it be nil? It's a pointer.
							// Use case: User creates CoreTemplate with empty spec? Or nil TopoServer?
							Etcd: nil,
						},
					},
				},
			},
			want: &multigresv1alpha1.GlobalTopoServerSpec{
				Etcd: &multigresv1alpha1.EtcdSpec{
					Image:     "new-etcd",
					Replicas:  ptr.To(DefaultEtcdReplicas),
					Resources: DefaultResourcesEtcd(),
					Storage:   multigresv1alpha1.StorageSpec{Size: DefaultEtcdStorageSize},
				},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.objects...).Build()
			r := NewResolver(c, ns, tc.cluster.Spec.TemplateDefaults)

			got, err := r.ResolveGlobalTopo(t.Context(), tc.cluster)
			if tc.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if diff := cmp.Diff(tc.want, got, cmpopts.IgnoreUnexported(resource.Quantity{}), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestResolver_ResolveMultiAdmin(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	coreTpl, _, _, ns := setupFixtures(t)

	tests := map[string]struct {
		cluster *multigresv1alpha1.MultigresCluster
		objects []client.Object
		want    *multigresv1alpha1.StatelessSpec
		wantErr bool
	}{
		"Inline": {
			cluster: &multigresv1alpha1.MultigresCluster{
				Spec: multigresv1alpha1.MultigresClusterSpec{
					MultiAdmin: &multigresv1alpha1.MultiAdminConfig{
						Spec: &multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(10))},
					},
				},
			},
			want: &multigresv1alpha1.StatelessSpec{
				Replicas:  ptr.To(int32(10)),
				Resources: DefaultResourcesAdmin(),
			},
		},
		"Template": {
			cluster: &multigresv1alpha1.MultigresCluster{
				Spec: multigresv1alpha1.MultigresClusterSpec{
					MultiAdmin: &multigresv1alpha1.MultiAdminConfig{
						TemplateRef: "default",
					},
				},
			},
			objects: []client.Object{coreTpl},
			want: &multigresv1alpha1.StatelessSpec{
				// No Image, uses global
				Replicas:  ptr.To(DefaultAdminReplicas),
				Resources: DefaultResourcesAdmin(),
			},
		},
		"Missing Template": {
			cluster: &multigresv1alpha1.MultigresCluster{
				Spec: multigresv1alpha1.MultigresClusterSpec{
					MultiAdmin: &multigresv1alpha1.MultiAdminConfig{TemplateRef: "gone"},
				},
			},
			wantErr: true,
		},
		"Fallback (No Spec, No Template) -> Defaults": {
			cluster: &multigresv1alpha1.MultigresCluster{
				Spec: multigresv1alpha1.MultigresClusterSpec{
					MultiAdmin: &multigresv1alpha1.MultiAdminConfig{}, // Empty
				},
			},
			objects: nil, // No core template found
			want: &multigresv1alpha1.StatelessSpec{
				Replicas:  ptr.To(DefaultAdminReplicas),
				Resources: DefaultResourcesAdmin(),
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.objects...).Build()
			r := NewResolver(c, ns, tc.cluster.Spec.TemplateDefaults)

			got, err := r.ResolveMultiAdmin(t.Context(), tc.cluster)
			if tc.wantErr {
				if err == nil {
					t.Error("Expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if diff := cmp.Diff(tc.want, got, cmpopts.IgnoreUnexported(resource.Quantity{}), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Diff (-want +got):\n%s", diff)
			}
		})
	}
}

// TestResolver_ResolveCoreTemplate hits specific error branches for Implicit vs Explicit missing.
func TestResolver_ResolveCoreTemplate(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	r := NewResolver(
		fake.NewClientBuilder().WithScheme(scheme).Build(),
		"default",
		multigresv1alpha1.TemplateDefaults{},
	)

	// 1. Implicit Fallback ("default" or "") -> Not Found -> Returns nil, nil (No Error)
	// This covers: "if isImplicitFallback { return ... nil }"
	t.Run("Implicit Fallback Missing", func(t *testing.T) {
		tpl, err := r.ResolveCoreTemplate(t.Context(), "")
		if err != nil {
			t.Errorf("Expected nil error for implicit missing, got %v", err)
		}
		if tpl.Name != "" { // Empty struct
			t.Errorf("Expected empty template, got %v", tpl)
		}
	})

	// 2. Explicit Template ("custom") -> Not Found -> Returns Error
	// This covers: "return nil, fmt.Errorf(...)"
	t.Run("Explicit Template Missing", func(t *testing.T) {
		_, err := r.ResolveCoreTemplate(t.Context(), "missing-custom")
		if err == nil {
			t.Error("Expected error for explicit missing template")
		}
	})
}

func TestResolver_ClientErrors_Core(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	errSim := testutil.ErrInjected
	baseClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	failConfig := &testutil.FailureConfig{
		OnGet: func(_ client.ObjectKey) error { return errSim },
	}
	c := testutil.NewFakeClientWithFailures(baseClient, failConfig)

	r := NewResolver(c, "default", multigresv1alpha1.TemplateDefaults{})

	_, err := r.ResolveCoreTemplate(t.Context(), "any")
	if err == nil || !errors.Is(err, errSim) {
		t.Errorf("Error mismatch: got %v, want %v", err, errSim)
	}
}

func TestResolver_ResolveMultiAdminWeb(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	coreTpl, _, _, ns := setupFixtures(t)

	tests := map[string]struct {
		cluster *multigresv1alpha1.MultigresCluster
		objects []client.Object
		want    *multigresv1alpha1.StatelessSpec
		wantErr bool
	}{
		"Inline": {
			cluster: &multigresv1alpha1.MultigresCluster{
				Spec: multigresv1alpha1.MultigresClusterSpec{
					MultiAdminWeb: &multigresv1alpha1.MultiAdminWebConfig{
						Spec: &multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(10))},
					},
				},
			},
			want: &multigresv1alpha1.StatelessSpec{
				Replicas:  ptr.To(int32(10)),
				Resources: DefaultResourcesAdminWeb(),
			},
		},
		"Template": {
			cluster: &multigresv1alpha1.MultigresCluster{
				Spec: multigresv1alpha1.MultigresClusterSpec{
					MultiAdminWeb: &multigresv1alpha1.MultiAdminWebConfig{
						TemplateRef: "default",
					},
				},
			},
			objects: []client.Object{coreTpl},
			want: &multigresv1alpha1.StatelessSpec{
				Replicas:  ptr.To(DefaultMultiAdminWebReplicas),
				Resources: DefaultResourcesAdminWeb(),
			},
		},
		"Fallback (No Spec, No Template) -> Defaults": {
			cluster: &multigresv1alpha1.MultigresCluster{
				Spec: multigresv1alpha1.MultigresClusterSpec{
					MultiAdminWeb: &multigresv1alpha1.MultiAdminWebConfig{},
				},
			},
			objects: nil,
			want: &multigresv1alpha1.StatelessSpec{
				Replicas:  ptr.To(DefaultMultiAdminWebReplicas),
				Resources: DefaultResourcesAdminWeb(),
			},
		},
		"Explicit Missing Template -> Error": {
			cluster: &multigresv1alpha1.MultigresCluster{
				Spec: multigresv1alpha1.MultigresClusterSpec{
					MultiAdminWeb: &multigresv1alpha1.MultiAdminWebConfig{
						TemplateRef: "missing",
					},
				},
			},
			wantErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.objects...).Build()
			r := NewResolver(c, ns, tc.cluster.Spec.TemplateDefaults)

			got, err := r.ResolveMultiAdminWeb(t.Context(), tc.cluster)
			if tc.wantErr {
				if err == nil {
					t.Error("Expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if diff := cmp.Diff(tc.want, got, cmpopts.IgnoreUnexported(resource.Quantity{}), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Diff (-want +got):\n%s", diff)
			}
		})
	}
}
