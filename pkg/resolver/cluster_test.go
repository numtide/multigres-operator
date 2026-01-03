package resolver

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
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
		"Empty Cluster: Applies All Static Defaults": {
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
					// Expect Smart Defaulting: System Catalog
					Databases: []multigresv1alpha1.DatabaseConfig{
						{
							Name:    DefaultSystemDatabaseName,
							Default: true,
							TableGroups: []multigresv1alpha1.TableGroupConfig{
								{
									Name:    DefaultSystemTableGroupName,
									Default: true,
								},
							},
						},
					},
				},
			},
		},
		"Preserve Existing Values: Does Not Overwrite": {
			input: &multigresv1alpha1.MultigresCluster{
				Spec: multigresv1alpha1.MultigresClusterSpec{
					Images: multigresv1alpha1.ClusterImages{
						Postgres:        "custom-postgres",
						MultiAdmin:      "custom-admin",
						MultiOrch:       "custom-orch",
						MultiPooler:     "custom-pooler",
						MultiGateway:    "custom-gateway",
						ImagePullPolicy: corev1.PullAlways,
					},
					TemplateDefaults: multigresv1alpha1.TemplateDefaults{
						CoreTemplate:  "custom-core",
						CellTemplate:  "custom-cell",
						ShardTemplate: "custom-shard",
					},
					Databases: []multigresv1alpha1.DatabaseConfig{
						{
							Name:        "existing-db",
							Default:     true,
							TableGroups: []multigresv1alpha1.TableGroupConfig{{Name: "tg1"}},
						},
					},
				},
			},
			want: &multigresv1alpha1.MultigresCluster{
				Spec: multigresv1alpha1.MultigresClusterSpec{
					Images: multigresv1alpha1.ClusterImages{
						Postgres:        "custom-postgres",
						MultiAdmin:      "custom-admin",
						MultiOrch:       "custom-orch",
						MultiPooler:     "custom-pooler",
						MultiGateway:    "custom-gateway",
						ImagePullPolicy: corev1.PullAlways,
					},
					TemplateDefaults: multigresv1alpha1.TemplateDefaults{
						CoreTemplate:  "custom-core",
						CellTemplate:  "custom-cell",
						ShardTemplate: "custom-shard",
					},
					Databases: []multigresv1alpha1.DatabaseConfig{
						{
							Name:        "existing-db",
							Default:     true,
							TableGroups: []multigresv1alpha1.TableGroupConfig{{Name: "tg1"}},
						},
					},
				},
			},
		},
		"Inline Configs: Deep Defaulting (Etcd & Admin)": {
			input: &multigresv1alpha1.MultigresCluster{
				Spec: multigresv1alpha1.MultigresClusterSpec{
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerSpec{
						Etcd: &multigresv1alpha1.EtcdSpec{},
					},
					MultiAdmin: multigresv1alpha1.MultiAdminConfig{
						Spec: &multigresv1alpha1.StatelessSpec{},
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
					// Expect Smart Defaulting for DBs
					Databases: []multigresv1alpha1.DatabaseConfig{
						{
							Name:    DefaultSystemDatabaseName,
							Default: true,
							TableGroups: []multigresv1alpha1.TableGroupConfig{
								{
									Name:    DefaultSystemTableGroupName,
									Default: true,
								},
							},
						},
					},
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerSpec{
						Etcd: &multigresv1alpha1.EtcdSpec{
							Image:     DefaultEtcdImage,
							Replicas:  ptr.To(DefaultEtcdReplicas),
							Resources: DefaultResourcesEtcd(),
							Storage:   multigresv1alpha1.StorageSpec{Size: DefaultEtcdStorageSize},
						},
					},
					MultiAdmin: multigresv1alpha1.MultiAdminConfig{
						Spec: &multigresv1alpha1.StatelessSpec{
							Replicas:  ptr.To(DefaultAdminReplicas),
							Resources: DefaultResourcesAdmin(),
						},
					},
				},
			},
		},
		"Inline Configs: Deep Defaulting (Cells)": {
			input: &multigresv1alpha1.MultigresCluster{
				Spec: multigresv1alpha1.MultigresClusterSpec{
					Cells: []multigresv1alpha1.CellConfig{
						{
							Name: "cell-1",
							Spec: &multigresv1alpha1.CellInlineSpec{
								MultiGateway: multigresv1alpha1.StatelessSpec{},
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
					// Expect Smart Defaulting for DBs
					Databases: []multigresv1alpha1.DatabaseConfig{
						{
							Name:    DefaultSystemDatabaseName,
							Default: true,
							TableGroups: []multigresv1alpha1.TableGroupConfig{
								{
									Name:    DefaultSystemTableGroupName,
									Default: true,
								},
							},
						},
					},
					Cells: []multigresv1alpha1.CellConfig{
						{
							Name: "cell-1",
							Spec: &multigresv1alpha1.CellInlineSpec{
								MultiGateway: multigresv1alpha1.StatelessSpec{
									Replicas:  ptr.To(int32(1)),
									Resources: corev1.ResourceRequirements{},
								},
							},
						},
					},
				},
			},
		},
		"Inline Configs: Partial Overrides Preserved": {
			input: &multigresv1alpha1.MultigresCluster{
				Spec: multigresv1alpha1.MultigresClusterSpec{
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerSpec{
						Etcd: &multigresv1alpha1.EtcdSpec{
							Image: "custom-etcd",
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
					// Expect Smart Defaulting for DBs
					Databases: []multigresv1alpha1.DatabaseConfig{
						{
							Name:    DefaultSystemDatabaseName,
							Default: true,
							TableGroups: []multigresv1alpha1.TableGroupConfig{
								{
									Name:    DefaultSystemTableGroupName,
									Default: true,
								},
							},
						},
					},
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerSpec{
						Etcd: &multigresv1alpha1.EtcdSpec{
							Image:     "custom-etcd",
							Replicas:  ptr.To(DefaultEtcdReplicas),
							Resources: DefaultResourcesEtcd(),
							Storage:   multigresv1alpha1.StorageSpec{Size: DefaultEtcdStorageSize},
						},
					},
				},
			},
		},
		"Smart Defaulting: Inject TableGroup when DB exists": {
			input: &multigresv1alpha1.MultigresCluster{
				Spec: multigresv1alpha1.MultigresClusterSpec{
					Databases: []multigresv1alpha1.DatabaseConfig{
						{Name: "postgres", Default: true}, // No TableGroups
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
							Name:    "postgres",
							Default: true,
							TableGroups: []multigresv1alpha1.TableGroupConfig{
								{
									Name:    DefaultSystemTableGroupName,
									Default: true,
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

func TestResolver_ResolveCoreTemplate(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	coreTpl, _, _, ns := setupFixtures(t)
	customCore := coreTpl.DeepCopy()
	customCore.Name = "custom-core"

	tests := map[string]struct {
		existingObjects []client.Object
		defaults        multigresv1alpha1.TemplateDefaults
		reqName         string
		wantErr         bool
		errContains     string
		wantFound       bool
		wantResName     string
	}{
		"Explicit Found": {
			existingObjects: []client.Object{customCore},
			reqName:         "custom-core",
			wantFound:       true,
			wantResName:     "custom-core",
		},
		"Explicit Not Found (Error)": {
			existingObjects: []client.Object{},
			reqName:         "missing-core",
			wantErr:         true,
			errContains:     "referenced CoreTemplate 'missing-core' not found",
		},
		"Default from Config Found": {
			existingObjects: []client.Object{customCore},
			defaults:        multigresv1alpha1.TemplateDefaults{CoreTemplate: "custom-core"},
			reqName:         "",
			wantFound:       true,
			wantResName:     "custom-core",
		},
		"Default from Config Not Found (Error)": {
			existingObjects: []client.Object{},
			defaults:        multigresv1alpha1.TemplateDefaults{CoreTemplate: "missing"},
			reqName:         "",
			wantErr:         true,
			errContains:     "referenced CoreTemplate 'missing' not found",
		},
		"Implicit Fallback Found": {
			existingObjects: []client.Object{coreTpl},
			defaults:        multigresv1alpha1.TemplateDefaults{},
			reqName:         "",
			wantFound:       true,
			wantResName:     "default",
		},
		"Implicit Fallback Not Found (Safe Empty Return)": {
			existingObjects: []client.Object{},
			defaults:        multigresv1alpha1.TemplateDefaults{},
			reqName:         "",
			wantFound:       false,
			wantErr:         false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.existingObjects...).
				Build()
			r := NewResolver(c, ns, tc.defaults)

			res, err := r.ResolveCoreTemplate(t.Context(), tc.reqName)
			if tc.wantErr {
				if err == nil {
					t.Fatal("Expected error, got nil")
				}
				if tc.errContains != "" && !strings.Contains(err.Error(), tc.errContains) {
					t.Errorf(
						"Error message mismatch: got %q, want substring %q",
						err.Error(),
						tc.errContains,
					)
				}
				return
			} else if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if !tc.wantFound {
				if res == nil {
					t.Fatal(
						"Expected non-nil result structure even for not-found implicit fallback",
					)
				}
				if res.GetName() != "" {
					t.Errorf("Expected empty result, got object with name %q", res.GetName())
				}
				return
			}

			if got, want := res.GetName(), tc.wantResName; got != want {
				t.Errorf("Result name mismatch: got %q, want %q", got, want)
			}
		})
	}
}

func TestResolveGlobalTopo(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		spec *multigresv1alpha1.GlobalTopoServerSpec
		tpl  *multigresv1alpha1.CoreTemplate
		want *multigresv1alpha1.GlobalTopoServerSpec
	}{
		"Inline Priority (Etcd)": {
			spec: &multigresv1alpha1.GlobalTopoServerSpec{
				Etcd: &multigresv1alpha1.EtcdSpec{Image: "inline"},
			},
			tpl: nil,
			want: &multigresv1alpha1.GlobalTopoServerSpec{
				Etcd: &multigresv1alpha1.EtcdSpec{
					Image:     "inline",
					Replicas:  ptr.To(DefaultEtcdReplicas),
					Resources: DefaultResourcesEtcd(),
					Storage:   multigresv1alpha1.StorageSpec{Size: DefaultEtcdStorageSize},
				},
			},
		},
		"Inline Priority (External)": {
			spec: &multigresv1alpha1.GlobalTopoServerSpec{
				External: &multigresv1alpha1.ExternalTopoServerSpec{
					Endpoints: []multigresv1alpha1.EndpointUrl{"http://foo"},
				},
			},
			tpl: nil,
			want: &multigresv1alpha1.GlobalTopoServerSpec{
				External: &multigresv1alpha1.ExternalTopoServerSpec{
					Endpoints: []multigresv1alpha1.EndpointUrl{"http://foo"},
				},
			},
		},
		"Template Fallback": {
			spec: &multigresv1alpha1.GlobalTopoServerSpec{},
			tpl: &multigresv1alpha1.CoreTemplate{
				Spec: multigresv1alpha1.CoreTemplateSpec{
					GlobalTopoServer: &multigresv1alpha1.TopoServerSpec{
						Etcd: &multigresv1alpha1.EtcdSpec{Image: "template"},
					},
				},
			},
			want: &multigresv1alpha1.GlobalTopoServerSpec{
				Etcd: &multigresv1alpha1.EtcdSpec{
					Image:     "template",
					Replicas:  ptr.To(DefaultEtcdReplicas),
					Resources: DefaultResourcesEtcd(),
					Storage:   multigresv1alpha1.StorageSpec{Size: DefaultEtcdStorageSize},
				},
			},
		},
		"Template Found but Nil Content": {
			spec: &multigresv1alpha1.GlobalTopoServerSpec{},
			tpl: &multigresv1alpha1.CoreTemplate{
				Spec: multigresv1alpha1.CoreTemplateSpec{
					GlobalTopoServer: nil,
				},
			},
			want: &multigresv1alpha1.GlobalTopoServerSpec{
				Etcd: &multigresv1alpha1.EtcdSpec{
					Image:     DefaultEtcdImage,
					Replicas:  ptr.To(DefaultEtcdReplicas),
					Resources: DefaultResourcesEtcd(),
					Storage:   multigresv1alpha1.StorageSpec{Size: DefaultEtcdStorageSize},
				},
			},
		},
		"No-op (Nil Template)": {
			spec: &multigresv1alpha1.GlobalTopoServerSpec{},
			tpl:  nil,
			want: &multigresv1alpha1.GlobalTopoServerSpec{
				Etcd: &multigresv1alpha1.EtcdSpec{
					Image:     DefaultEtcdImage,
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
			got := ResolveGlobalTopo(tc.spec, tc.tpl)
			if diff := cmp.Diff(tc.want, got, cmpopts.IgnoreUnexported(resource.Quantity{}), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("ResolveGlobalTopo mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestResolveMultiAdmin(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		spec *multigresv1alpha1.MultiAdminConfig
		tpl  *multigresv1alpha1.CoreTemplate
		want *multigresv1alpha1.StatelessSpec
	}{
		"Inline Priority": {
			spec: &multigresv1alpha1.MultiAdminConfig{
				Spec: &multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(5))},
			},
			tpl: nil,
			want: &multigresv1alpha1.StatelessSpec{
				Replicas:  ptr.To(int32(5)),
				Resources: DefaultResourcesAdmin(),
			},
		},
		"Template Fallback": {
			spec: &multigresv1alpha1.MultiAdminConfig{},
			tpl: &multigresv1alpha1.CoreTemplate{
				Spec: multigresv1alpha1.CoreTemplateSpec{
					MultiAdmin: &multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(3))},
				},
			},
			want: &multigresv1alpha1.StatelessSpec{
				Replicas:  ptr.To(int32(3)),
				Resources: DefaultResourcesAdmin(),
			},
		},
		"Template Found but Nil Content": {
			spec: &multigresv1alpha1.MultiAdminConfig{},
			tpl: &multigresv1alpha1.CoreTemplate{
				Spec: multigresv1alpha1.CoreTemplateSpec{
					MultiAdmin: nil,
				},
			},
			want: &multigresv1alpha1.StatelessSpec{
				Replicas:  ptr.To(DefaultAdminReplicas),
				Resources: DefaultResourcesAdmin(),
			},
		},
		"No-op (Nil Template)": {
			spec: &multigresv1alpha1.MultiAdminConfig{},
			tpl:  nil,
			want: &multigresv1alpha1.StatelessSpec{
				Replicas:  ptr.To(DefaultAdminReplicas),
				Resources: DefaultResourcesAdmin(),
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := ResolveMultiAdmin(tc.spec, tc.tpl)
			if diff := cmp.Diff(tc.want, got, cmpopts.IgnoreUnexported(resource.Quantity{}), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("ResolveMultiAdmin mismatch (-want +got):\n%s", diff)
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
