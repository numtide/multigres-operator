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
		shard    *multigresv1alpha1.Shard
		poolName string
		poolSpec multigresv1alpha1.ShardPoolSpec
		scheme   *runtime.Scheme
		want     *corev1.Service
		wantErr  bool
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
			poolName: "primary",
			poolSpec: multigresv1alpha1.ShardPoolSpec{
				Type: "replica",
			},
			want: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shard-pool-primary-headless",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "test-shard-pool-primary",
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
						"app.kubernetes.io/instance":   "test-shard-pool-primary",
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
			poolName: "ro",
			poolSpec: multigresv1alpha1.ShardPoolSpec{
				Type: "readonly",
				Cell: "zone-east",
			},
			want: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "shard-001-pool-ro-headless",
					Namespace: "prod",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "shard-001-pool-ro",
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
						"app.kubernetes.io/instance":   "shard-001-pool-ro",
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
			poolName: "primary",
			poolSpec: multigresv1alpha1.ShardPoolSpec{},
			want: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "shard-002-pool-primary-headless",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "shard-002-pool-primary",
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
						"app.kubernetes.io/instance":   "shard-002-pool-primary",
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
			poolSpec: multigresv1alpha1.ShardPoolSpec{
				Type: "replica",
			},
			poolName: "primary",
			scheme:   runtime.NewScheme(), // empty scheme
			wantErr:  true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			testScheme := scheme
			if tc.scheme != nil {
				testScheme = tc.scheme
			}
			got, err := BuildPoolHeadlessService(tc.shard, tc.poolName, tc.poolSpec, testScheme)

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

func TestBuildPoolLabels(t *testing.T) {
	tests := map[string]struct {
		shard    *multigresv1alpha1.Shard
		poolName string
		want     map[string]string
	}{
		"primary pool": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{Name: "test-shard"},
				Spec: multigresv1alpha1.ShardSpec{
					Pools: map[string]multigresv1alpha1.ShardPoolSpec{
						"primary": {Cell: "zone-west"},
					},
				},
			},
			poolName: "primary",
			want: map[string]string{
				"app.kubernetes.io/name":       "multigres",
				"app.kubernetes.io/instance":   "test-shard-pool-primary",
				"app.kubernetes.io/component":  PoolComponentName,
				"app.kubernetes.io/part-of":    "multigres",
				"app.kubernetes.io/managed-by": "multigres-operator",
			},
		},
		"replica pool": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{Name: "my-shard"},
				Spec: multigresv1alpha1.ShardSpec{
					Pools: map[string]multigresv1alpha1.ShardPoolSpec{
						"replica": {},
					},
				},
			},
			poolName: "replica",
			want: map[string]string{
				"app.kubernetes.io/name":       "multigres",
				"app.kubernetes.io/instance":   "my-shard-pool-replica",
				"app.kubernetes.io/component":  PoolComponentName,
				"app.kubernetes.io/part-of":    "multigres",
				"app.kubernetes.io/managed-by": "multigres-operator",
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// Getting the pool details by getting the definitions based on
			// poolName provided.
			poolSpec := tc.shard.Spec.Pools[tc.poolName]
			got := buildPoolLabels(tc.shard, tc.poolName, poolSpec)

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("buildPoolLabels() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
