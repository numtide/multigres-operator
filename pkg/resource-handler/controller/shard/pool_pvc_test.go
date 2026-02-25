package shard

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

func TestBuildPoolDataPVC_BasicStructure(t *testing.T) {
	shard := newTestShard()
	pool := newTestPoolSpec()

	pvc, err := BuildPoolDataPVC(shard, "main", "z1", pool, 0, testScheme())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pvc.Namespace != "default" {
		t.Errorf("namespace = %q, want %q", pvc.Namespace, "default")
	}

	if len(pvc.OwnerReferences) != 1 {
		t.Fatalf("expected 1 owner reference, got %d", len(pvc.OwnerReferences))
	}
	if pvc.OwnerReferences[0].Name != "test-shard" {
		t.Errorf("owner name = %q, want %q", pvc.OwnerReferences[0].Name, "test-shard")
	}

	expectedLabels := map[string]string{
		"app.kubernetes.io/component": PoolComponentName,
		"multigres.com/cluster":       "test-cluster",
		"multigres.com/cell":          "z1",
		"multigres.com/pool":          "main",
		"multigres.com/shard":         "0-inf",
	}
	for k, want := range expectedLabels {
		if got := pvc.Labels[k]; got != want {
			t.Errorf("label %q = %q, want %q", k, got, want)
		}
	}
}

func TestBuildPoolDataPVC_StorageDefaults(t *testing.T) {
	pool := multigresv1alpha1.PoolSpec{
		Storage: multigresv1alpha1.StorageSpec{},
	}

	pvc, err := BuildPoolDataPVC(newTestShard(), "main", "z1", pool, 0, testScheme())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Default size
	got := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
	want := resource.MustParse(DefaultDataVolumeSize)
	if got.Cmp(want) != 0 {
		t.Errorf("storage size = %s, want %s", got.String(), want.String())
	}

	// Default access mode
	if len(pvc.Spec.AccessModes) != 1 || pvc.Spec.AccessModes[0] != corev1.ReadWriteOnce {
		t.Errorf("access modes = %v, want [ReadWriteOnce]", pvc.Spec.AccessModes)
	}

	// No storage class by default
	if pvc.Spec.StorageClassName != nil {
		t.Errorf("storage class = %q, want nil", *pvc.Spec.StorageClassName)
	}
}

func TestBuildPoolDataPVC_CustomStorage(t *testing.T) {
	pool := multigresv1alpha1.PoolSpec{
		Storage: multigresv1alpha1.StorageSpec{
			Class:       "fast-ssd",
			Size:        "50Gi",
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
		},
	}

	pvc, err := BuildPoolDataPVC(newTestShard(), "main", "z1", pool, 0, testScheme())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Custom size
	got := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
	want := resource.MustParse("50Gi")
	if got.Cmp(want) != 0 {
		t.Errorf("storage size = %s, want %s", got.String(), want.String())
	}

	// Custom access mode
	if len(pvc.Spec.AccessModes) != 1 || pvc.Spec.AccessModes[0] != corev1.ReadWriteMany {
		t.Errorf("access modes = %v, want [ReadWriteMany]", pvc.Spec.AccessModes)
	}

	// Custom storage class
	if pvc.Spec.StorageClassName == nil || *pvc.Spec.StorageClassName != "fast-ssd" {
		t.Errorf("storage class = %v, want %q", pvc.Spec.StorageClassName, "fast-ssd")
	}
}

func TestBuildPoolDataPVC_NameConsistency(t *testing.T) {
	shard := newTestShard()
	pool := newTestPoolSpec()

	pvc, _ := BuildPoolDataPVC(shard, "main", "z1", pool, 0, testScheme())
	pod, _ := BuildPoolPod(shard, "main", "z1", pool, 0, testScheme())

	// Find the data volume in the pod
	var dataPVCRef string
	for _, v := range pod.Spec.Volumes {
		if v.Name == DataVolumeName && v.PersistentVolumeClaim != nil {
			dataPVCRef = v.PersistentVolumeClaim.ClaimName
		}
	}

	if dataPVCRef != pvc.Name {
		t.Errorf("pod references PVC %q but BuildPoolDataPVC created %q", dataPVCRef, pvc.Name)
	}
}

func TestBuildPoolDataPVCName_MatchesPodReference(t *testing.T) {
	tests := []struct {
		name     string
		shard    *multigresv1alpha1.Shard
		poolName string
		cellName string
		index    int
	}{
		{"index 0", newTestShard(), "main", "z1", 0},
		{"index 5", newTestShard(), "main", "z1", 5},
		{
			"long names",
			func() *multigresv1alpha1.Shard {
				s := newTestShard()
				s.Labels["multigres.com/cluster"] = "long-cluster"
				return s
			}(),
			"replica-pool", "us-east-1a", 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pvcName := BuildPoolDataPVCName(tt.shard, tt.poolName, tt.cellName, tt.index)
			pod, _ := BuildPoolPod(
				tt.shard,
				tt.poolName,
				tt.cellName,
				newTestPoolSpec(),
				tt.index,
				testScheme(),
			)

			var podPVCRef string
			for _, v := range pod.Spec.Volumes {
				if v.Name == DataVolumeName && v.PersistentVolumeClaim != nil {
					podPVCRef = v.PersistentVolumeClaim.ClaimName
				}
			}

			if pvcName != podPVCRef {
				t.Errorf("BuildPoolDataPVCName() = %q, pod references %q", pvcName, podPVCRef)
			}
		})
	}
}

func TestBuildPoolPodDisruptionBudget(t *testing.T) {
	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-shard",
			Namespace: "default",
			UID:       "test-uid",
			Labels:    map[string]string{"multigres.com/cluster": "test-cluster"},
		},
		Spec: multigresv1alpha1.ShardSpec{
			DatabaseName:   "postgres",
			TableGroupName: "default",
			ShardName:      "0-inf",
		},
	}

	pdb, err := BuildPoolPodDisruptionBudget(shard, "main", "z1", testScheme())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pdb.Namespace != "default" {
		t.Errorf("namespace = %q, want %q", pdb.Namespace, "default")
	}

	// maxUnavailable should be 1
	if pdb.Spec.MaxUnavailable == nil || pdb.Spec.MaxUnavailable.IntValue() != 1 {
		t.Errorf("maxUnavailable = %v, want 1", pdb.Spec.MaxUnavailable)
	}

	// Selector should match pool labels
	if pdb.Spec.Selector == nil {
		t.Fatal("selector is nil")
	}
	sel := pdb.Spec.Selector.MatchLabels
	if sel["multigres.com/cell"] != "z1" {
		t.Errorf("selector cell = %q, want %q", sel["multigres.com/cell"], "z1")
	}
	if sel["app.kubernetes.io/component"] != PoolComponentName {
		t.Errorf(
			"selector component = %q, want %q",
			sel["app.kubernetes.io/component"],
			PoolComponentName,
		)
	}

	// Owner reference
	if len(pdb.OwnerReferences) != 1 {
		t.Fatalf("expected 1 owner reference, got %d", len(pdb.OwnerReferences))
	}
}
