//go:build integration
// +build integration

package webhook_test

import (
	"strings"
	"testing"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestCEL_MultigresCluster(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		cluster     *multigresv1alpha1.MultigresCluster
		expectError string
	}{
		{
			name: "Invalid Cell: Both Spec and Overrides",
			cluster: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cel-cell-conflict",
					Namespace: testNamespace,
				},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					Cells: []multigresv1alpha1.CellConfig{
						{
							Name: "invalid-cell",
							Spec: &multigresv1alpha1.CellInlineSpec{
								MultiGateway: multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(1))},
							},
							Overrides: &multigresv1alpha1.CellOverrides{
								MultiGateway: &multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(2))},
							},
						},
					},
				},
			},
			expectError: "cannot specify both 'spec' and 'overrides'",
		},
		{
			name: "Invalid Cell: Both Spec and CellTemplate",
			cluster: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cel-cell-template-conflict",
					Namespace: testNamespace,
				},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					Cells: []multigresv1alpha1.CellConfig{
						{
							Name:         "invalid-cell-template",
							CellTemplate: "some-template",
							Spec: &multigresv1alpha1.CellInlineSpec{
								MultiGateway: multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(1))},
							},
						},
					},
				},
			},
			expectError: "cannot specify both 'spec' and 'cellTemplate'",
		},
		{
			name: "Invalid Cell: Both Zone and Region",
			cluster: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cel-cell-location-conflict",
					Namespace: testNamespace,
				},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					Cells: []multigresv1alpha1.CellConfig{
						{
							Name:   "invalid-location",
							Zone:   "us-east-1a",
							Region: "us-east-1",
						},
					},
				},
			},
			expectError: "must specify either 'zone' or 'region', but not both",
		},
		{
			name: "Invalid Shard: Both Spec and Overrides",
			cluster: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cel-shard-conflict",
					Namespace: testNamespace,
				},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					Databases: []multigresv1alpha1.DatabaseConfig{
						{
							Name:    "postgres",
							Default: true,
							TableGroups: []multigresv1alpha1.TableGroupConfig{
								{
									Name:    "default",
									Default: true,
									Shards: []multigresv1alpha1.ShardConfig{
										{
											Name: "0",
											Spec: &multigresv1alpha1.ShardInlineSpec{},
											Overrides: &multigresv1alpha1.ShardOverrides{
												MultiOrch: &multigresv1alpha1.MultiOrchSpec{
													StatelessSpec: multigresv1alpha1.StatelessSpec{
														Replicas: ptr.To(int32(2)),
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
			expectError: "cannot specify both 'spec' and 'overrides'",
		},
		{
			name: "Invalid Shard: Both Spec and ShardTemplate",
			cluster: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cel-shard-template-conflict",
					Namespace: testNamespace,
				},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					Databases: []multigresv1alpha1.DatabaseConfig{
						{
							Name:    "postgres",
							Default: true,
							TableGroups: []multigresv1alpha1.TableGroupConfig{
								{
									Name:    "default",
									Default: true,
									Shards: []multigresv1alpha1.ShardConfig{
										{
											Name:          "0",
											ShardTemplate: "some-template",
											Spec:          &multigresv1alpha1.ShardInlineSpec{},
										},
									},
								},
							},
						},
					},
				},
			},
			expectError: "cannot specify both 'spec' and 'shardTemplate'",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := k8sClient.Create(ctx, tc.cluster)
			if err == nil {
				t.Fatal("Expected error, got nil")
			}
			// In envtest, CEL errors usually appear in the error string
			if tc.expectError != "" {
				if !strings.Contains(err.Error(), tc.expectError) {
					t.Errorf("Expected error message to contain %q, got %q", tc.expectError, err.Error())
				}
			}
		})
	}
}

func TestCEL_TopoServer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		cluster     *multigresv1alpha1.MultigresCluster
		expectError string
	}{
		{
			name: "Invalid GlobalTopoServer: Both Etcd and External",
			cluster: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cel-toposerver-conflict",
					Namespace: testNamespace,
				},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					GlobalTopoServer: &multigresv1alpha1.GlobalTopoServerSpec{
						Etcd:     &multigresv1alpha1.EtcdSpec{Replicas: ptr.To(int32(1))},
						External: &multigresv1alpha1.ExternalTopoServerSpec{Endpoints: []multigresv1alpha1.EndpointUrl{"http://etcd:2379"}},
					},
					Cells: []multigresv1alpha1.CellConfig{}, // Empty for simplicity
				},
			},
			expectError: "must specify exactly one of 'etcd', 'external', or 'templateRef'",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := k8sClient.Create(ctx, tc.cluster)
			if err == nil {
				t.Fatal("Expected error, got nil")
			}
			if tc.expectError != "" {
				if !strings.Contains(err.Error(), tc.expectError) {
					t.Errorf("Expected error message to contain %q, got %q", tc.expectError, err.Error())
				}
			}
		})
	}
}

func TestCEL_Limits(t *testing.T) {
	t.Parallel()

	// Helper to create a long string
	longString := strings.Repeat("a", 64)
	veryLongString := strings.Repeat("a", 257)

	tests := []struct {
		name        string
		cluster     *multigresv1alpha1.MultigresCluster
		expectError string
	}{
		{
			name: "Invalid Annotation Key Length",
			cluster: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cel-annotation-key-limit",
					Namespace: testNamespace,
				},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					MultiAdmin: &multigresv1alpha1.MultiAdminConfig{
						Spec: &multigresv1alpha1.StatelessSpec{
							PodAnnotations: map[string]string{
								longString: "value",
							},
						},
					},
				},
			},
			expectError: "annotation keys must be <64 chars",
		},
		{
			name: "Invalid Annotation Value Length",
			cluster: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cel-annotation-value-limit",
					Namespace: testNamespace,
				},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					MultiAdmin: &multigresv1alpha1.MultiAdminConfig{
						Spec: &multigresv1alpha1.StatelessSpec{
							PodAnnotations: map[string]string{
								"key": veryLongString,
							},
						},
					},
				},
			},
			expectError: "values <256 chars",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := k8sClient.Create(ctx, tc.cluster)
			if err == nil {
				t.Fatal("Expected error, got nil")
			}
			if tc.expectError != "" {
				if !strings.Contains(err.Error(), tc.expectError) {
					t.Errorf("Expected error message to contain %q, got %q", tc.expectError, err.Error())
				}
			}
		})
	}
}

func TestCEL_Database(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		cluster     *multigresv1alpha1.MultigresCluster
		expectError string
	}{
		{
			name: "Invalid Database Name",
			cluster: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cel-db-name",
					Namespace: testNamespace,
				},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					Databases: []multigresv1alpha1.DatabaseConfig{
						{
							Name:    "my-app-db", // Must be 'postgres'
							Default: true,
							TableGroups: []multigresv1alpha1.TableGroupConfig{
								{Name: "default", Default: true},
							},
						},
					},
				},
			},
			expectError: "only the single system database named 'postgres'",
		},
		{
			name: "Invalid TableGroup: Default Group Wrong Name",
			cluster: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cel-tg-name",
					Namespace: testNamespace,
				},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					Databases: []multigresv1alpha1.DatabaseConfig{
						{
							Name:    "postgres",
							Default: true,
							TableGroups: []multigresv1alpha1.TableGroupConfig{
								{Name: "main", Default: true}, // Must be 'default'
							},
						},
					},
				},
			},
			expectError: "the default tablegroup must be named 'default'",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := k8sClient.Create(ctx, tc.cluster)
			if err == nil {
				t.Fatal("Expected error, got nil")
			}
			if tc.expectError != "" {
				if !strings.Contains(err.Error(), tc.expectError) {
					t.Errorf("Expected error message to contain %q, got %q", tc.expectError, err.Error())
				}
			}
		})
	}
}

func TestCEL_MultiAdmin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		cluster     *multigresv1alpha1.MultigresCluster
		expectError string
	}{
		{
			name: "Invalid MultiAdmin: Both Spec and Template",
			cluster: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cel-multiadmin-conflict",
					Namespace: testNamespace,
				},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					MultiAdmin: &multigresv1alpha1.MultiAdminConfig{
						Spec:        &multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(1))},
						TemplateRef: "some-template",
					},
				},
			},
			expectError: "cannot specify both 'spec' and 'templateRef'",
		},
		{
			name: "Invalid MultiAdmin: None",
			cluster: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cel-multiadmin-empty",
					Namespace: testNamespace,
				},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					MultiAdmin: &multigresv1alpha1.MultiAdminConfig{}, // Both empty
				},
			},
			expectError: "must specify either 'spec' or 'templateRef'",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := k8sClient.Create(ctx, tc.cluster)
			if err == nil {
				t.Fatal("Expected error, got nil")
			}
			if tc.expectError != "" {
				if !strings.Contains(err.Error(), tc.expectError) {
					t.Errorf("Expected error message to contain %q, got %q", tc.expectError, err.Error())
				}
			}
		})
	}
}
