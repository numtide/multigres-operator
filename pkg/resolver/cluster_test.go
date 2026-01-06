package resolver

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestResolver_PopulateClusterDefaults(t *testing.T) {
	t.Parallel()

	r := NewResolver(
		fake.NewClientBuilder().Build(),
		"default",
		multigresv1alpha1.TemplateDefaults{},
	)

	tests := map[string]struct {
		input *multigresv1alpha1.MultigresCluster
		want  *multigresv1alpha1.MultigresCluster
	}{
		"Empty Cluster: Applies All System Defaults": {
			input: &multigresv1alpha1.MultigresCluster{},
			want: &multigresv1alpha1.MultigresCluster{
				Spec: multigresv1alpha1.MultigresClusterSpec{
					Images: multigresv1alpha1.ClusterImages{
						Postgres:        DefaultPostgresImage,
						MultiAdmin:      DefaultMultiAdminImage,
						MultiOrch:       DefaultMultiOrchImage,
						MultiPooler:     DefaultMultiPoolerImage,
						MultiGateway:    DefaultMultiGatewayImage,
						ImagePullPolicy: DefaultImagePullPolicy,
					},
					TemplateDefaults: multigresv1alpha1.TemplateDefaults{
						CoreTemplate:  FallbackCoreTemplate,
						CellTemplate:  FallbackCellTemplate,
						ShardTemplate: FallbackShardTemplate,
					},
					// Expect Smart Defaulting: System Catalog with Shard 0
					Databases: []multigresv1alpha1.DatabaseConfig{
						{
							Name:    DefaultSystemDatabaseName,
							Default: true,
							TableGroups: []multigresv1alpha1.TableGroupConfig{
								{
									Name:    DefaultSystemTableGroupName,
									Default: true,
									Shards: []multigresv1alpha1.ShardConfig{
										{Name: "0"},
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
				Spec: multigresv1alpha1.MultigresClusterSpec{
					Databases: []multigresv1alpha1.DatabaseConfig{
						{Name: "custom-db"},
					},
				},
			},
			want: &multigresv1alpha1.MultigresCluster{
				Spec: multigresv1alpha1.MultigresClusterSpec{
					Images: multigresv1alpha1.ClusterImages{
						Postgres:        DefaultPostgresImage,
						MultiAdmin:      DefaultMultiAdminImage,
						MultiOrch:       DefaultMultiOrchImage,
						MultiPooler:     DefaultMultiPoolerImage,
						MultiGateway:    DefaultMultiGatewayImage,
						ImagePullPolicy: DefaultImagePullPolicy,
					},
					TemplateDefaults: multigresv1alpha1.TemplateDefaults{
						CoreTemplate:  FallbackCoreTemplate,
						CellTemplate:  FallbackCellTemplate,
						ShardTemplate: FallbackShardTemplate,
					},
					Databases: []multigresv1alpha1.DatabaseConfig{
						{
							Name: "custom-db",
							TableGroups: []multigresv1alpha1.TableGroupConfig{
								{
									Name:    DefaultSystemTableGroupName,
									Default: true,
									Shards: []multigresv1alpha1.ShardConfig{
										{Name: "0"},
									},
								},
							},
						},
					},
				},
			},
		},
		"Existing TableGroup but No Shards: Inject Shard 0": {
			input: &multigresv1alpha1.MultigresCluster{
				Spec: multigresv1alpha1.MultigresClusterSpec{
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
				Spec: multigresv1alpha1.MultigresClusterSpec{
					Images: multigresv1alpha1.ClusterImages{
						Postgres:        DefaultPostgresImage,
						MultiAdmin:      DefaultMultiAdminImage,
						MultiOrch:       DefaultMultiOrchImage,
						MultiPooler:     DefaultMultiPoolerImage,
						MultiGateway:    DefaultMultiGatewayImage,
						ImagePullPolicy: DefaultImagePullPolicy,
					},
					TemplateDefaults: multigresv1alpha1.TemplateDefaults{
						CoreTemplate:  FallbackCoreTemplate,
						CellTemplate:  FallbackCellTemplate,
						ShardTemplate: FallbackShardTemplate,
					},
					Databases: []multigresv1alpha1.DatabaseConfig{
						{
							Name: "custom-db",
							TableGroups: []multigresv1alpha1.TableGroupConfig{
								{
									Name: "my-tg",
									Shards: []multigresv1alpha1.ShardConfig{
										{Name: "0"},
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
				Spec: multigresv1alpha1.MultigresClusterSpec{
					Images: multigresv1alpha1.ClusterImages{
						Postgres:        DefaultPostgresImage,
						MultiAdmin:      DefaultMultiAdminImage,
						MultiOrch:       DefaultMultiOrchImage,
						MultiPooler:     DefaultMultiPoolerImage,
						MultiGateway:    DefaultMultiGatewayImage,
						ImagePullPolicy: DefaultImagePullPolicy,
					},
					TemplateDefaults: multigresv1alpha1.TemplateDefaults{
						CoreTemplate:  FallbackCoreTemplate,
						CellTemplate:  FallbackCellTemplate,
						ShardTemplate: FallbackShardTemplate,
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
		// ADDED: This covers the "else" branches where we skip defaulting
		"Pre-populated Fields: Preserves Values": {
			input: &multigresv1alpha1.MultigresCluster{
				Spec: multigresv1alpha1.MultigresClusterSpec{
					Images: multigresv1alpha1.ClusterImages{
						Postgres:        "custom/postgres:16",
						MultiAdmin:      "custom/admin:1",
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
				Spec: multigresv1alpha1.MultigresClusterSpec{
					Images: multigresv1alpha1.ClusterImages{
						Postgres:        "custom/postgres:16",
						MultiAdmin:      "custom/admin:1",
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
			got := tc.input.DeepCopy()
			r.PopulateClusterDefaults(got)

			if diff := cmp.Diff(tc.want, got, cmpopts.IgnoreUnexported(resource.Quantity{}), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Cluster defaults mismatch (-want +got):\n%s", diff)
			}
		})
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
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerSpec{
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
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerSpec{
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
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerSpec{
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
					MultiAdmin: multigresv1alpha1.MultiAdminConfig{
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
					MultiAdmin: multigresv1alpha1.MultiAdminConfig{
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
					MultiAdmin: multigresv1alpha1.MultiAdminConfig{TemplateRef: "gone"},
				},
			},
			wantErr: true,
		},
		// ADDED: This covers the "else" fallback path in ResolveMultiAdmin
		"Fallback (No Spec, No Template) -> Defaults": {
			cluster: &multigresv1alpha1.MultigresCluster{
				Spec: multigresv1alpha1.MultigresClusterSpec{
					MultiAdmin: multigresv1alpha1.MultiAdminConfig{}, // Empty
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

func TestResolver_ClientErrors_Core(t *testing.T) {
	t.Parallel()
	errSimulated := errors.New("simulated database connection error")
	mc := &mockClient{failGet: true, err: errSimulated}
	r := NewResolver(mc, "default", multigresv1alpha1.TemplateDefaults{})

	_, err := r.ResolveCoreTemplate(t.Context(), "any")
	if err == nil ||
		err.Error() != "failed to get CoreTemplate: simulated database connection error" {
		t.Errorf("Error mismatch: got %v, want simulated error", err)
	}
}
