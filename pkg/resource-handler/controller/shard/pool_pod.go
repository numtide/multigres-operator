package shard

import (
	"encoding/hex"
	"fmt"
	"hash"
	"hash/fnv"
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	nameutil "github.com/numtide/multigres-operator/pkg/util/name"
)

const (
	// PoolPodFinalizer prevents Kubernetes from removing pods before the
	// operator can clean up etcd topology entries.
	PoolPodFinalizer = "multigres.com/pool-pod-protection"

	// ShardFinalizer ensures the operator cleans up child resources (Pods, PVCs)
	// that have their own finalizers before the Shard resource is removed.
	ShardFinalizer = "multigres.com/shard-resource-protection"

	// AnnotationSpecHash stores the FNV-1a hash of operator-managed pod spec
	// fields, enabling O(1) drift detection without deep comparison.
	AnnotationSpecHash = "multigres.com/spec-hash"

	// defaultTerminationGracePeriod gives multipooler time to gracefully close
	// connections and set NOT_SERVING in etcd before SIGKILL.
	defaultTerminationGracePeriod int64 = 30

	// DefaultPoolReplicas is the default number of replicas for a pool cell if not specified.
	DefaultPoolReplicas int32 = 1
)

// BuildPoolPodName constructs the deterministic name for a pool pod at the
// given index. The base name is generated using PodConstraints (60 chars) and
// the index is appended as a suffix.
func BuildPoolPodName(shard *multigresv1alpha1.Shard, poolName, cellName string, index int) string {
	clusterName := shard.Labels["multigres.com/cluster"]
	baseName := nameutil.JoinWithConstraints(
		nameutil.PodConstraints,
		clusterName,
		string(shard.Spec.DatabaseName),
		string(shard.Spec.TableGroupName),
		string(shard.Spec.ShardName),
		"pool",
		poolName,
		cellName,
	)
	return fmt.Sprintf("%s-%d", baseName, index)
}

// BuildPoolPod creates a Pod for a shard pool in a specific cell at the given
// replica index. It reuses the existing container and volume builders from
// containers.go and sets operator-specific metadata (finalizer, spec-hash).
func BuildPoolPod(
	shard *multigresv1alpha1.Shard,
	poolName string,
	cellName string,
	poolSpec multigresv1alpha1.PoolSpec,
	index int,
	scheme *runtime.Scheme,
) (*corev1.Pod, error) {
	podName := BuildPoolPodName(shard, poolName, cellName, index)
	labels := buildPoolLabelsWithCell(shard, poolName, cellName, poolSpec)

	// Construct volumes: reuse shared volumes and prepend the per-pod data PVC.
	dataPVCName := BuildPoolDataPVCName(shard, poolName, cellName, index)
	volumes := buildPoolVolumes(shard, cellName)
	volumes = append([]corev1.Volume{{
		Name: DataVolumeName,
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: dataPVCName,
			},
		},
	}}, volumes...)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: shard.Namespace,
			Labels:    labels,
			Annotations: map[string]string{
				AnnotationSpecHash: "", // placeholder, computed below
			},
			Finalizers: []string{PoolPodFinalizer},
		},
		Spec: corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{
				FSGroup: ptr.To(int64(999)), // postgres group in postgres:17 image
			},
			TerminationGracePeriodSeconds: ptr.To(defaultTerminationGracePeriod),
			Containers: []corev1.Container{
				buildMultiPoolerSidecar(shard, poolSpec, poolName, cellName),
				buildPgctldContainer(shard, poolSpec),
			},
			Volumes:      volumes,
			Affinity:     poolSpec.Affinity,
			NodeSelector: shard.Spec.CellTopologyLabels[multigresv1alpha1.CellName(cellName)],
			// Hostname is set to the pod name for DNS resolution via headless service.
			Hostname:  podName,
			Subdomain: buildHeadlessServiceName(shard, poolName, cellName),
		},
	}

	pod.Annotations[AnnotationSpecHash] = ComputeSpecHash(pod)

	if err := ctrl.SetControllerReference(shard, pod, scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference: %w", err)
	}

	return pod, nil
}

// buildHeadlessServiceName constructs the headless service name for DNS
// resolution. Matches the naming used by pool_service.go.
func buildHeadlessServiceName(shard *multigresv1alpha1.Shard, poolName, cellName string) string {
	clusterName := shard.Labels["multigres.com/cluster"]
	return nameutil.JoinWithConstraints(
		nameutil.ServiceConstraints,
		clusterName,
		string(shard.Spec.DatabaseName),
		string(shard.Spec.TableGroupName),
		string(shard.Spec.ShardName),
		"pool",
		poolName,
		cellName,
		"headless",
	)
}

// ComputeSpecHash produces a deterministic FNV-1a hex string over the operator-
// managed pod spec fields that should trigger a rolling update when changed.
//
// Fields included: images, commands, args, env vars, resources, volume mounts,
// container security contexts, pod affinity, and node selector.
func ComputeSpecHash(pod *corev1.Pod) string {
	h := fnv.New32a()
	spec := &pod.Spec

	hashContainers(h, spec.InitContainers)
	hashContainers(h, spec.Containers)

	if spec.Affinity != nil {
		fmt.Fprintf(h, "%v", spec.Affinity)
	}

	if spec.NodeSelector != nil {
		keys := sortedKeys(spec.NodeSelector)
		for _, k := range keys {
			fmt.Fprintf(h, "%s=%s", k, spec.NodeSelector[k])
		}
	}

	if spec.TerminationGracePeriodSeconds != nil {
		fmt.Fprintf(h, "tgp=%d", *spec.TerminationGracePeriodSeconds)
	}

	return hex.EncodeToString(h.Sum(nil))
}

func hashContainers(h hash.Hash32, containers []corev1.Container) {
	for _, c := range containers {
		fmt.Fprintf(h, "name=%s", c.Name)
		fmt.Fprintf(h, "image=%s", c.Image)
		for _, cmd := range c.Command {
			fmt.Fprintf(h, "cmd=%s", cmd)
		}
		for _, arg := range c.Args {
			fmt.Fprintf(h, "arg=%s", arg)
		}
		for _, e := range c.Env {
			fmt.Fprintf(h, "env=%s=%s", e.Name, e.Value)
		}
		fmt.Fprintf(h, "res=%v", c.Resources)
		for _, vm := range c.VolumeMounts {
			fmt.Fprintf(h, "vm=%s:%s", vm.Name, vm.MountPath)
		}
		if c.SecurityContext != nil {
			fmt.Fprintf(h, "sc=%v", c.SecurityContext)
		}
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
