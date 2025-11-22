package shard

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

func TestBuildPoolHeadlessService(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	tests := map[string]struct {
		shard     *multigresv1alpha1.Shard
		pool      multigresv1alpha1.ShardPoolSpec
		poolIndex int
		scheme    *runtime.Scheme
		want      *corev1.Service
		wantErr   bool
	}{
		"replica pool headless service": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shard",
					Namespace: "default",
					UID:       "test-uid",
				},
				Spec: multigresv1alpha1.ShardSpec{},
			},
			pool: multigresv1alpha1.ShardPoolSpec{
				Type: "replica",
			},
			poolIndex: 0,
			scheme:    scheme,
			want: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shard-replica-headless",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "test-shard-replica",
						"app.kubernetes.io/component":  PoolComponentName,
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "multigres.com/v1alpha1",
							Kind:               "Shard",
							Name:               "test-shard",
							UID:                "test-uid",
							Controller:         ptr.To(true),
							BlockOwnerDeletion: ptr.To(true),
						},
					},
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: corev1.ClusterIPNone,
					Selector: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "test-shard-replica",
						"app.kubernetes.io/component":  PoolComponentName,
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
					},
					Ports: []corev1.ServicePort{
						{
							Name:       "http",
							Port:       DefaultMultiPoolerHTTPPort,
							TargetPort: intstr.FromString("http"),
							Protocol:   corev1.ProtocolTCP,
						},
						{
							Name:       "grpc",
							Port:       DefaultMultiPoolerGRPCPort,
							TargetPort: intstr.FromString("grpc"),
							Protocol:   corev1.ProtocolTCP,
						},
						{
							Name:       "postgres",
							Port:       DefaultPostgresPort,
							TargetPort: intstr.FromString("postgres"),
							Protocol:   corev1.ProtocolTCP,
						},
					},
					PublishNotReadyAddresses: true,
				},
			},
		},
		"readOnly pool with custom cell": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "shard-001",
					Namespace: "prod",
					UID:       "prod-uid",
				},
				Spec: multigresv1alpha1.ShardSpec{},
			},
			pool: multigresv1alpha1.ShardPoolSpec{
				Type: "readOnly",
				Cell: "zone-east",
			},
			poolIndex: 1,
			scheme:    scheme,
			want: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "shard-001-readOnly-headless",
					Namespace: "prod",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "shard-001-readOnly",
						"app.kubernetes.io/component":  PoolComponentName,
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "multigres.com/v1alpha1",
							Kind:               "Shard",
							Name:               "shard-001",
							UID:                "prod-uid",
							Controller:         ptr.To(true),
							BlockOwnerDeletion: ptr.To(true),
						},
					},
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: corev1.ClusterIPNone,
					Selector: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "shard-001-readOnly",
						"app.kubernetes.io/component":  PoolComponentName,
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
					},
					Ports: []corev1.ServicePort{
						{
							Name:       "http",
							Port:       DefaultMultiPoolerHTTPPort,
							TargetPort: intstr.FromString("http"),
							Protocol:   corev1.ProtocolTCP,
						},
						{
							Name:       "grpc",
							Port:       DefaultMultiPoolerGRPCPort,
							TargetPort: intstr.FromString("grpc"),
							Protocol:   corev1.ProtocolTCP,
						},
						{
							Name:       "postgres",
							Port:       DefaultPostgresPort,
							TargetPort: intstr.FromString("postgres"),
							Protocol:   corev1.ProtocolTCP,
						},
					},
					PublishNotReadyAddresses: true,
				},
			},
		},
		"pool without type uses index in name": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "shard-002",
					Namespace: "default",
					UID:       "uid-002",
				},
				Spec: multigresv1alpha1.ShardSpec{},
			},
			pool:      multigresv1alpha1.ShardPoolSpec{},
			poolIndex: 3,
			scheme:    scheme,
			want: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "shard-002-pool-3-headless",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "shard-002-pool-3",
						"app.kubernetes.io/component":  PoolComponentName,
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "multigres.com/v1alpha1",
							Kind:               "Shard",
							Name:               "shard-002",
							UID:                "uid-002",
							Controller:         ptr.To(true),
							BlockOwnerDeletion: ptr.To(true),
						},
					},
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: corev1.ClusterIPNone,
					Selector: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "shard-002-pool-3",
						"app.kubernetes.io/component":  PoolComponentName,
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
					},
					Ports: []corev1.ServicePort{
						{
							Name:       "http",
							Port:       DefaultMultiPoolerHTTPPort,
							TargetPort: intstr.FromString("http"),
							Protocol:   corev1.ProtocolTCP,
						},
						{
							Name:       "grpc",
							Port:       DefaultMultiPoolerGRPCPort,
							TargetPort: intstr.FromString("grpc"),
							Protocol:   corev1.ProtocolTCP,
						},
						{
							Name:       "postgres",
							Port:       DefaultPostgresPort,
							TargetPort: intstr.FromString("postgres"),
							Protocol:   corev1.ProtocolTCP,
						},
					},
					PublishNotReadyAddresses: true,
				},
			},
		},
		"invalid scheme - should error": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shard",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.ShardSpec{},
			},
			pool: multigresv1alpha1.ShardPoolSpec{
				Type: "replica",
			},
			poolIndex: 0,
			scheme:    runtime.NewScheme(), // empty scheme
			wantErr:   true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := BuildPoolHeadlessService(tc.shard, tc.pool, tc.poolIndex, tc.scheme)

			if (err != nil) != tc.wantErr {
				t.Errorf("BuildPoolHeadlessService() error = %v, wantErr %v", err, tc.wantErr)
				return
			}

			if tc.wantErr {
				return
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("BuildPoolHeadlessService() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestBuildPoolName(t *testing.T) {
	tests := map[string]struct {
		shardName string
		pool      multigresv1alpha1.ShardPoolSpec
		poolIndex int
		want      string
	}{
		"with pool type": {
			shardName: "shard-001",
			pool: multigresv1alpha1.ShardPoolSpec{
				Type: "replica",
			},
			poolIndex: 0,
			want:      "shard-001-replica",
		},
		"with readOnly type": {
			shardName: "test-shard",
			pool: multigresv1alpha1.ShardPoolSpec{
				Type: "readOnly",
			},
			poolIndex: 1,
			want:      "test-shard-readOnly",
		},
		"without type uses index": {
			shardName: "shard-002",
			pool:      multigresv1alpha1.ShardPoolSpec{},
			poolIndex: 2,
			want:      "shard-002-pool-2",
		},
		"empty type uses index": {
			shardName: "my-shard",
			pool: multigresv1alpha1.ShardPoolSpec{
				Type: "",
			},
			poolIndex: 5,
			want:      "my-shard-pool-5",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := buildPoolName(tc.shardName, tc.pool, tc.poolIndex)

			if got != tc.want {
				t.Errorf("buildPoolName() = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestBuildPoolLabels(t *testing.T) {
	tests := map[string]struct {
		shard    *multigresv1alpha1.Shard
		pool     multigresv1alpha1.ShardPoolSpec
		poolName string
		want     map[string]string
	}{
		"with custom cell": {
			shard: &multigresv1alpha1.Shard{},
			pool: multigresv1alpha1.ShardPoolSpec{
				Cell: "zone-west",
			},
			poolName: "test-shard-replica",
			want: map[string]string{
				"app.kubernetes.io/name":       "multigres",
				"app.kubernetes.io/instance":   "test-shard-replica",
				"app.kubernetes.io/component":  PoolComponentName,
				"app.kubernetes.io/part-of":    "multigres",
				"app.kubernetes.io/managed-by": "multigres-operator",
			},
		},
		"without cell uses default": {
			shard:    &multigresv1alpha1.Shard{},
			pool:     multigresv1alpha1.ShardPoolSpec{},
			poolName: "test-shard-pool-0",
			want: map[string]string{
				"app.kubernetes.io/name":       "multigres",
				"app.kubernetes.io/instance":   "test-shard-pool-0",
				"app.kubernetes.io/component":  PoolComponentName,
				"app.kubernetes.io/part-of":    "multigres",
				"app.kubernetes.io/managed-by": "multigres-operator",
			},
		},
		"with empty cell uses default": {
			shard: &multigresv1alpha1.Shard{},
			pool: multigresv1alpha1.ShardPoolSpec{
				Cell: "",
			},
			poolName: "my-shard-readOnly",
			want: map[string]string{
				"app.kubernetes.io/name":       "multigres",
				"app.kubernetes.io/instance":   "my-shard-readOnly",
				"app.kubernetes.io/component":  PoolComponentName,
				"app.kubernetes.io/part-of":    "multigres",
				"app.kubernetes.io/managed-by": "multigres-operator",
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := buildPoolLabels(tc.shard, tc.pool, tc.poolName)

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("buildPoolLabels() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
