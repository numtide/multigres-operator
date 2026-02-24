package shard

import (
	"context"
	"crypto/x509"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/cert"
	"github.com/numtide/multigres-operator/pkg/monitoring"
	"github.com/numtide/multigres-operator/pkg/util/metadata"
	"github.com/numtide/multigres-operator/pkg/util/name"
)

// ShardReconciler reconciles a Shard object.
type ShardReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	// APIReader is an uncached client that reads directly from the API server.
	// The default cached client (r.Get) only sees Secrets labeled with
	// "app.kubernetes.io/managed-by: multigres-operator" due to the informer
	// cache's label filter. External Secrets (e.g., cert-manager) lack this
	// label, so we need APIReader to validate user-provided pgBackRest TLS Secrets.
	APIReader client.Reader
}

// Reconcile handles Shard resource reconciliation.
func (r *ShardReconciler) Reconcile(
	ctx context.Context,
	req ctrl.Request,
) (ctrl.Result, error) {
	start := time.Now()
	ctx, span := monitoring.StartReconcileSpan(
		ctx,
		"Shard.Reconcile",
		req.Name,
		req.Namespace,
		"Shard",
	)
	defer span.End()
	ctx = monitoring.EnrichLoggerWithTrace(ctx)

	logger := log.FromContext(ctx)
	logger.V(1).Info("reconcile started")

	// Fetch the Shard instance
	shard := &multigresv1alpha1.Shard{}
	if err := r.Get(ctx, req.NamespacedName, shard); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Shard resource not found, ignoring")
			return ctrl.Result{}, nil
		}
		monitoring.RecordSpanError(span, err)
		logger.Error(err, "Failed to get Shard")
		return ctrl.Result{}, err
	}

	// If being deleted, let Kubernetes GC handle cleanup
	if !shard.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	// Reconcile pg_hba ConfigMap first (required by all pools before StatefulSets start)
	if err := r.reconcilePgHbaConfigMap(ctx, shard); err != nil {
		monitoring.RecordSpanError(span, err)
		logger.Error(err, "Failed to reconcile pg_hba ConfigMap")
		r.Recorder.Eventf(shard, "Warning", "ConfigError", "Failed to generate pg_hba: %v", err)
		return ctrl.Result{}, err
	}

	// Reconcile postgres password Secret (required by pgctld and multipooler)
	if err := r.reconcilePostgresPasswordSecret(ctx, shard); err != nil {
		monitoring.RecordSpanError(span, err)
		logger.Error(err, "Failed to reconcile postgres password Secret")
		r.Recorder.Eventf(
			shard,
			"Warning",
			"ConfigError",
			"Failed to generate postgres password Secret: %v",
			err,
		)
		return ctrl.Result{}, err
	}

	// Reconcile pgBackRest TLS certificates (required for inter-node backup communication)
	if err := r.reconcilePgBackRestCerts(ctx, shard); err != nil {
		monitoring.RecordSpanError(span, err)
		logger.Error(err, "Failed to reconcile pgBackRest TLS certificates")
		r.Recorder.Eventf(
			shard,
			"Warning",
			"CertError",
			"Failed to reconcile pgBackRest TLS certificates: %v",
			err,
		)
		return ctrl.Result{}, err
	}

	// Reconcile MultiOrch - one Deployment and Service per cell
	{
		ctx, childSpan := monitoring.StartChildSpan(ctx, "Shard.ReconcileMultiOrch")
		multiOrchCells, err := getMultiOrchCells(shard)
		if err != nil {
			monitoring.RecordSpanError(childSpan, err)
			childSpan.End()
			logger.Error(err, "Failed to determine MultiOrch cells")
			r.Recorder.Eventf(
				shard,
				"Warning",
				"ConfigError",
				"Failed to determine MultiOrch cells: %v",
				err,
			)
			return ctrl.Result{}, err
		}

		for _, cell := range multiOrchCells {
			cellName := string(cell)

			// Reconcile MultiOrch Deployment for this cell
			if err := r.reconcileMultiOrchDeployment(ctx, shard, cellName); err != nil {
				monitoring.RecordSpanError(childSpan, err)
				childSpan.End()
				logger.Error(err, "Failed to reconcile MultiOrch Deployment", "cell", cellName)
				r.Recorder.Eventf(
					shard,
					"Warning",
					"FailedApply",
					"Failed to supply MultiOrch Deployment for cell %s: %v",
					cellName,
					err,
				)
				return ctrl.Result{}, err
			}

			// Reconcile MultiOrch Service for this cell
			if err := r.reconcileMultiOrchService(ctx, shard, cellName); err != nil {
				monitoring.RecordSpanError(childSpan, err)
				childSpan.End()
				logger.Error(err, "Failed to reconcile MultiOrch Service", "cell", cellName)
				r.Recorder.Eventf(
					shard,
					"Warning",
					"FailedApply",
					"Failed to supply MultiOrch Service for cell %s: %v",
					cellName,
					err,
				)
				return ctrl.Result{}, err
			}
		}
		childSpan.End()
	}

	// Reconcile Shared Backup PVCs (one per cell)
	{
		ctx, childSpan := monitoring.StartChildSpan(ctx, "Shard.ReconcileBackupPVCs")
		// Determine all cells where pools are running (or multiorch)
		// We can reuse getMultiOrchCells logic or just iterate pools?
		// getMultiOrchCells returns explicit MultiOrch cells OR union of pool cells.
		// This serves as a good proxy for "active cells".
		cells, err := getMultiOrchCells(shard)
		if err != nil {
			// If we can't determine cells, we can't create PVCs.
			// But getMultiOrchCells errors if NO cells found.
			// Try to proceed if possible? No, consume error.
			monitoring.RecordSpanError(childSpan, err)
			childSpan.End()
			return ctrl.Result{}, err
		}

		for _, cell := range cells {
			cellName := string(cell)
			if err := r.reconcileSharedBackupPVC(ctx, shard, cellName); err != nil {
				monitoring.RecordSpanError(childSpan, err)
				childSpan.End()
				logger.Error(err, "Failed to reconcile shared backup PVC", "cell", cellName)
				r.Recorder.Eventf(
					shard,
					"Warning",
					"FailedApply",
					"Failed to reconcile shared backup PVC for cell %s: %v",
					cellName,
					err,
				)
				return ctrl.Result{}, err
			}
		}
		childSpan.End()
	}

	{
		ctx, childSpan := monitoring.StartChildSpan(ctx, "Shard.ReconcilePools")
		for poolName, pool := range shard.Spec.Pools {
			if err := r.reconcilePool(ctx, shard, string(poolName), pool); err != nil {
				monitoring.RecordSpanError(childSpan, err)
				childSpan.End()
				logger.Error(err, "Failed to reconcile pool", "poolName", poolName)
				r.Recorder.Eventf(
					shard,
					"Warning",
					"FailedApply",
					"Failed to reconcile pool %s: %v",
					poolName,
					err,
				)
				return ctrl.Result{}, err
			}
		}
		childSpan.End()
	}

	// Update status
	{
		_, childSpan := monitoring.StartChildSpan(ctx, "Shard.UpdateStatus")
		if err := r.updateStatus(ctx, shard); err != nil {
			monitoring.RecordSpanError(childSpan, err)
			childSpan.End()
			logger.Error(err, "Failed to update status")
			r.Recorder.Eventf(shard, "Warning", "StatusError", "Failed to update status: %v", err)
			return ctrl.Result{}, err
		}
		childSpan.End()
	}

	logger.V(1).Info("reconcile complete", "duration", time.Since(start).String())
	r.Recorder.Event(shard, "Normal", "Synced", "Successfully reconciled Shard")
	return ctrl.Result{}, nil
}

// reconcileMultiOrchDeployment creates or updates the MultiOrch Deployment for a specific cell.
func (r *ShardReconciler) reconcileMultiOrchDeployment(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
	cellName string,
) error {
	desired, err := BuildMultiOrchDeployment(shard, cellName, r.Scheme)
	if err != nil {
		return fmt.Errorf("failed to build MultiOrch Deployment: %w", err)
	}

	// Server Side Apply
	desired.SetGroupVersionKind(appsv1.SchemeGroupVersion.WithKind("Deployment"))
	if err := r.Patch(
		ctx,
		desired,
		client.Apply,
		client.ForceOwnership,
		client.FieldOwner("multigres-operator"),
	); err != nil {
		return fmt.Errorf("failed to apply MultiOrch Deployment: %w", err)
	}

	r.Recorder.Eventf(
		shard,
		"Normal",
		"Applied",
		"Applied %s %s",
		desired.GroupVersionKind().Kind,
		desired.Name,
	)

	return nil
}

// reconcilePgHbaConfigMap creates or updates the pg_hba ConfigMap for a shard.
// This ConfigMap is shared across all pools and contains the authentication template.
func (r *ShardReconciler) reconcilePgHbaConfigMap(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
) error {
	desired, err := BuildPgHbaConfigMap(shard, r.Scheme)
	if err != nil {
		return fmt.Errorf("failed to build pg_hba ConfigMap: %w", err)
	}

	// Server Side Apply
	desired.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ConfigMap"))
	if err := r.Patch(
		ctx,
		desired,
		client.Apply,
		client.ForceOwnership,
		client.FieldOwner("multigres-operator"),
	); err != nil {
		return fmt.Errorf("failed to apply pg_hba ConfigMap: %w", err)
	}

	r.Recorder.Eventf(
		shard,
		"Normal",
		"Applied",
		"Applied %s %s",
		desired.GroupVersionKind().Kind,
		desired.Name,
	)

	return nil
}

// reconcilePostgresPasswordSecret creates or updates the postgres password Secret for a shard.
// This Secret is shared across all pools and provides credentials to pgctld and multipooler.
func (r *ShardReconciler) reconcilePostgresPasswordSecret(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
) error {
	desired, err := BuildPostgresPasswordSecret(shard, r.Scheme)
	if err != nil {
		return fmt.Errorf("failed to build postgres password Secret: %w", err)
	}

	// Server Side Apply
	desired.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Secret"))
	if err := r.Patch(
		ctx,
		desired,
		client.Apply,
		client.ForceOwnership,
		client.FieldOwner("multigres-operator"),
	); err != nil {
		return fmt.Errorf("failed to apply postgres password Secret: %w", err)
	}

	r.Recorder.Eventf(
		shard,
		"Normal",
		"Applied",
		"Applied %s %s",
		desired.GroupVersionKind().Kind,
		desired.Name,
	)

	return nil
}

// reconcilePgBackRestCerts ensures pgBackRest TLS certificates are available.
// For user-provided certs, validates the Secret exists and has the required keys
// using an uncached API reader (the informer cache filters by managed-by label).
// For auto-generated certs, uses pkg/cert to create and rotate CA + server Secrets.
func (r *ShardReconciler) reconcilePgBackRestCerts(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
) error {
	if shard.Spec.Backup == nil {
		return nil
	}

	// User-provided Secret: validate via uncached API reader.
	// We use APIReader instead of the cached client because the informer cache
	// only stores operator-labeled Secrets, making external Secrets (e.g.,
	// cert-manager) invisible to the cached r.Get().
	if shard.Spec.Backup.PgBackRestTLS != nil &&
		shard.Spec.Backup.PgBackRestTLS.SecretName != "" {
		secretName := shard.Spec.Backup.PgBackRestTLS.SecretName
		secret := &corev1.Secret{}
		if err := r.APIReader.Get(ctx, types.NamespacedName{
			Name:      secretName,
			Namespace: shard.Namespace,
		}, secret); err != nil {
			return fmt.Errorf("pgbackrest TLS secret %q not found: %w", secretName, err)
		}
		for _, key := range []string{"ca.crt", "tls.crt", "tls.key"} {
			if _, ok := secret.Data[key]; !ok {
				return fmt.Errorf(
					"pgbackrest TLS secret %q missing required key %q",
					secretName,
					key,
				)
			}
		}
		return nil
	}

	// Auto-generate: use pkg/cert to create CA + server cert Secrets.
	clusterName := shard.Labels[metadata.LabelMultigresCluster]
	rotator := cert.NewManager(r.Client, r.Recorder, cert.Options{
		Namespace:        shard.Namespace,
		CASecretName:     shard.Name + "-pgbackrest-ca",
		ServerSecretName: shard.Name + "-pgbackrest-tls",
		ServiceName:      "pgbackrest",
		ExtKeyUsages: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth,
		},
		Organization:  "Multigres",
		Owner:         shard,
		ComponentName: "pgbackrest",
		Labels:        metadata.BuildStandardLabels(clusterName, "pgbackrest-tls"),
	})
	return rotator.Bootstrap(ctx)
}

// reconcileMultiOrchService creates or updates the MultiOrch Service for a specific cell.
func (r *ShardReconciler) reconcileMultiOrchService(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
	cellName string,
) error {
	desired, err := BuildMultiOrchService(shard, cellName, r.Scheme)
	if err != nil {
		return fmt.Errorf("failed to build MultiOrch Service: %w", err)
	}

	// Server Side Apply
	desired.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Service"))
	if err := r.Patch(
		ctx,
		desired,
		client.Apply,
		client.ForceOwnership,
		client.FieldOwner("multigres-operator"),
	); err != nil {
		return fmt.Errorf("failed to apply MultiOrch Service: %w", err)
	}

	r.Recorder.Eventf(
		shard,
		"Normal",
		"Applied",
		"Applied %s %s",
		desired.GroupVersionKind().Kind,
		desired.Name,
	)

	return nil
}

// reconcilePool creates or updates the StatefulSet and headless Service for a pool.
// For pools spanning multiple cells, this creates one StatefulSet per cell.
func (r *ShardReconciler) reconcilePool(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
	poolName string,
	poolSpec multigresv1alpha1.PoolSpec,
) error {
	// Pools must have cells specified
	if len(poolSpec.Cells) == 0 {
		return fmt.Errorf(
			"pool %s has no cells specified - cannot deploy without cell information",
			poolName,
		)
	}

	// Create one StatefulSet per cell
	// TODO(#91): Pool.Cells may contain duplicates - add +listType=set validation at API level
	for _, cell := range poolSpec.Cells {
		cellName := string(cell)

		// Reconcile pool Pods and PVCs for this cell
		if err := r.reconcilePoolPods(ctx, shard, poolName, cellName, poolSpec); err != nil {
			return fmt.Errorf("failed to reconcile pool pods for cell %s: %w", cellName, err)
		}

		// Reconcile pool PDB for this cell
		if err := r.reconcilePoolPDB(ctx, shard, poolName, cellName); err != nil {
			return fmt.Errorf("failed to reconcile pool PDB for cell %s: %w", cellName, err)
		}

		// Reconcile pool headless Service for this cell
		if err := r.reconcilePoolHeadlessService(
			ctx,
			shard,
			poolName,
			cellName,
			poolSpec,
		); err != nil {
			return fmt.Errorf(
				"failed to reconcile pool headless Service for cell %s: %w",
				cellName,
				err,
			)
		}
	}

	return nil
}

// reconcilePoolPods ensures all missing pods and PVCs for a pool in a specific cell exist.
// It also detects configuration drift and updates the drift metric.
func (r *ShardReconciler) reconcilePoolPods(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
	poolName string,
	cellName string,
	poolSpec multigresv1alpha1.PoolSpec,
) error {
	logger := log.FromContext(ctx)

	// Determine replicas
	replicas := DefaultPoolReplicas
	if poolSpec.ReplicasPerCell != nil {
		replicas = *poolSpec.ReplicasPerCell
	}

	// 1. List existing pods and PVCs for this pool
	labels := buildPoolLabelsWithCell(shard, poolName, cellName, poolSpec)
	selector := metadata.GetSelectorLabels(labels)

	podList := &corev1.PodList{}
	if err := r.List(ctx, podList, client.InNamespace(shard.Namespace), client.MatchingLabels(selector)); err != nil {
		return fmt.Errorf("failed to list pods for pool %s cell %s: %w", poolName, cellName, err)
	}

	pvcList := &corev1.PersistentVolumeClaimList{}
	if err := r.List(ctx, pvcList, client.InNamespace(shard.Namespace), client.MatchingLabels(selector)); err != nil {
		return fmt.Errorf("failed to list PVCs for pool %s cell %s: %w", poolName, cellName, err)
	}

	// Index existing resources
	existingPods := make(map[string]*corev1.Pod)
	for i := range podList.Items {
		pod := &podList.Items[i]
		existingPods[pod.Name] = pod
	}

	existingPVCs := make(map[string]*corev1.PersistentVolumeClaim)
	for i := range pvcList.Items {
		pvc := &pvcList.Items[i]
		existingPVCs[pvc.Name] = pvc
	}

	// 2. Create missing PVCs and Pods
	var driftedCount int
	for i := int32(0); i < replicas; i++ {
		podName := BuildPoolPodName(shard, poolName, cellName, int(i))
		pvcName := BuildPoolDataPVCName(shard, poolName, cellName, int(i))

		// Create PVC if missing
		if _, exists := existingPVCs[pvcName]; !exists {
			desiredPVC, err := BuildPoolDataPVC(shard, poolName, cellName, poolSpec, int(i), r.Scheme)
			if err != nil {
				return fmt.Errorf("failed to build PVC %s: %w", pvcName, err)
			}
			if err := r.Create(ctx, desiredPVC); err != nil && !errors.IsAlreadyExists(err) {
				return fmt.Errorf("failed to create PVC %s: %w", pvcName, err)
			}
			logger.Info("Created missing pool PVC", "pvc", pvcName)
		}

		// Create Pod if missing
		if pod, exists := existingPods[podName]; !exists {
			desiredPod, err := BuildPoolPod(shard, poolName, cellName, poolSpec, int(i), r.Scheme)
			if err != nil {
				return fmt.Errorf("failed to build pod %s: %w", podName, err)
			}
			// We use r.Create instead of SSA (r.Patch with client.Apply) because most pod
			// spec fields (containers, volumes, securityContext, nodeSelector) are immutable
			// after creation. SSA would error on any configuration change. Instead, we detect
			// drift via the spec-hash annotation and handle recreation in the rolling update
			// logic (see Commit 6).
			if err := r.Create(ctx, desiredPod); err != nil && !errors.IsAlreadyExists(err) {
				return fmt.Errorf("failed to create pod %s: %w", podName, err)
			}
			logger.Info("Created missing pool pod", "pod", podName)
			r.Recorder.Eventf(shard, "Normal", "PodCreated", "Created pod %s for pool %s", podName, poolName)
		} else {
			// Pod exists. Check if it's drifted.
			if podNeedsUpdate(pod, shard, poolName, cellName, poolSpec, int(i), r.Scheme) {
				driftedCount++
			}
		}
	}

	// 3. Handle scale-down and pod cleanup
	var extraPods []*corev1.Pod
	var readyForDeletion []*corev1.Pod

	for _, pod := range existingPods {
		drainState := pod.Annotations[metadata.AnnotationDrainState]

		if drainState == metadata.DrainStateReadyForDeletion {
			readyForDeletion = append(readyForDeletion, pod)
			continue
		}

		lastDash := strings.LastIndex(pod.Name, "-")
		if lastDash == -1 {
			continue
		}
		index, err := strconv.Atoi(pod.Name[lastDash+1:])
		if err != nil {
			logger.Error(err, "Failed to parse pod index", "podName", pod.Name)
			continue
		}

		// If the pod's index is >= desired replicas, it's an extra pod.
		if int32(index) >= replicas {
			extraPods = append(extraPods, pod)
		}
	}

	// 3a. Cleanup pods ready for deletion
	for _, pod := range readyForDeletion {
		if err := r.cleanupDrainedPod(ctx, shard, pod, poolName, poolSpec); err != nil {
			return fmt.Errorf("failed to cleanup drained pod %s: %w", pod.Name, err)
		}
	}

	// Track if we delete or drain a pod in this loop, so we only do 1 at a time.
	actionTaken := false

	// 3b. Handle DRAINED pod replacements
	for _, pod := range existingPods {
		if actionTaken {
			break
		}

		role := ""
		if shard.Status.PodRoles != nil {
			role = shard.Status.PodRoles[pod.Name]
		}
		state := pod.Annotations[metadata.AnnotationDrainState]
		fmt.Printf("DEBUG: Pod %s has role '%s', drain state '%s'\n", pod.Name, role, state)

		if shard.Status.PodRoles != nil && shard.Status.PodRoles[pod.Name] == "DRAINED" && pod.Annotations[metadata.AnnotationDrainState] == "" {
			if pod.Annotations == nil {
				pod.Annotations = make(map[string]string)
			}
			pod.Annotations[metadata.AnnotationDrainState] = metadata.DrainStateRequested
			if err := r.Update(ctx, pod); err != nil {
				return fmt.Errorf("failed to request drain for DRAINED pod %s: %w", pod.Name, err)
			}
			logger.Info("Requested drain for DRAINED pod", "pod", pod.Name)
			r.Recorder.Eventf(shard, "Warning", "PodReplaced", "Replacing DRAINED pod %s", pod.Name)
			actionTaken = true
		}
	}

	// 3c. Initiate scale-down for extra pods
	if !actionTaken && len(extraPods) > 0 {
		podToDrain := r.selectPodToDrain(extraPods, shard)
		if podToDrain != nil {
			drainState := podToDrain.Annotations[metadata.AnnotationDrainState]
			if drainState == "" {
				if podToDrain.Annotations == nil {
					podToDrain.Annotations = make(map[string]string)
				}
				podToDrain.Annotations[metadata.AnnotationDrainState] = metadata.DrainStateRequested

				if err := r.Update(ctx, podToDrain); err != nil {
					return fmt.Errorf("failed to request drain for pod %s: %w", podToDrain.Name, err)
				}
				logger.Info("Requested drain for extra pod", "pod", podToDrain.Name)
				r.Recorder.Eventf(shard, "Normal", "DrainStarted", "Initiated drain for extra pod %s", podToDrain.Name)
				actionTaken = true
			}
		}
	}

	// 3d. Rolling Updates
	inProgress := false
	if driftedCount > 0 {
		inProgress = true
	}
	clusterName := shard.Labels[metadata.LabelMultigresCluster]
	monitoring.SetRollingUpdateInProgress(clusterName, shard.Name, string(poolName), string(cellName), shard.Namespace, inProgress)

	if inProgress {
		// Only set condition if not already set or if message needs update
		msg := fmt.Sprintf("%d pods need update in pool %s", driftedCount, poolName)
		meta.SetStatusCondition(&shard.Status.Conditions, metav1.Condition{
			Type:    "RollingUpdate",
			Status:  metav1.ConditionTrue,
			Reason:  "PodsDrifted",
			Message: msg,
		})
	} else {
		msg := fmt.Sprintf("All pods up to date in pool %s", poolName)
		meta.SetStatusCondition(&shard.Status.Conditions, metav1.Condition{
			Type:    "RollingUpdate",
			Status:  metav1.ConditionFalse,
			Reason:  "PodsUpToDate",
			Message: msg,
		})
	}

	if !actionTaken && driftedCount > 0 {
		var waitPrimary *corev1.Pod // The primary pod needing an update, if it's the only one left.

		for _, pod := range existingPods {
			if podNeedsUpdate(pod, shard, poolName, cellName, poolSpec, resolvePodIndex(pod.Name), r.Scheme) {
				isPrimary := shard.Status.PodRoles != nil && shard.Status.PodRoles[pod.Name] == "PRIMARY"

				if !isPrimary {
					// Delete replica pod immediately to trigger recreation on next reconcile
					if err := r.Delete(ctx, pod); err != nil {
						return fmt.Errorf("failed to delete sub-spec pod %s: %w", pod.Name, err)
					}
					logger.Info("Deleted sub-spec replica pod for rolling update", "pod", pod.Name)
					r.Recorder.Eventf(shard, "Normal", "PodUpdated", "Deleted replica pod %s for rolling update", pod.Name)
					actionTaken = true
					break // Only do 1 per reconcile loop
				} else {
					waitPrimary = pod
				}
			}
		}

		// If the only pod that needs updating is the PRIMARY, initiate a switchover.
		if !actionTaken && waitPrimary != nil {
			if waitPrimary.Annotations[metadata.AnnotationDrainState] == "" {
				if waitPrimary.Annotations == nil {
					waitPrimary.Annotations = make(map[string]string)
				}
				waitPrimary.Annotations[metadata.AnnotationDrainState] = metadata.DrainStateRequested
				if err := r.Update(ctx, waitPrimary); err != nil {
					return fmt.Errorf("failed to request drain for primary pod %s: %w", waitPrimary.Name, err)
				}
				logger.Info("Requested switchover for primary pod rolling update", "pod", waitPrimary.Name)
				r.Recorder.Eventf(shard, "Normal", "RollingUpdateStarted", "Initiating primary switchover for rolling update of pod %s", waitPrimary.Name)
				actionTaken = true
			}
		}
	}

	// 4. Record drift metric & events
	monitoring.SetPoolPodsDrifted(clusterName, shard.Name, string(poolName), string(cellName), shard.Namespace, driftedCount)

	return nil
}

// resolvePodIndex parses the index from the pod name
func resolvePodIndex(podName string) int {
	lastDash := strings.LastIndex(podName, "-")
	if lastDash == -1 {
		return 0
	}
	index, err := strconv.Atoi(podName[lastDash+1:])
	if err != nil {
		return 0
	}
	return index
}

// selectPodToDrain chooses the best pod to drain during scale-down.
// Preference: non-ready, non-primary, highest index.
func (r *ShardReconciler) selectPodToDrain(
	extraPods []*corev1.Pod,
	shard *multigresv1alpha1.Shard,
) *corev1.Pod {
	if len(extraPods) == 0 {
		return nil
	}

	var bestPod *corev1.Pod
	var bestScore int

	for _, pod := range extraPods {
		score := 0

		lastDash := strings.LastIndex(pod.Name, "-")
		if lastDash != -1 {
			idx, _ := strconv.Atoi(pod.Name[lastDash+1:])
			score += idx // higher index gets higher score
		}

		// Avoid PRIMARY if possible
		if shard.Status.PodRoles != nil {
			// In etcd, roles are sometimes recorded as "PRIMARY", depending on implementation.
			// Compare loosely. We assume data-handler populates the raw pb enum string.
			if role, ok := shard.Status.PodRoles[pod.Name]; ok && role == "PRIMARY" {
				score -= 1000
			}
		}

		// Prefer non-ready pods
		isReady := false
		for _, cond := range pod.Status.Conditions {
			if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
				isReady = true
				break
			}
		}
		if !isReady {
			score += 500
		}

		if bestPod == nil || score > bestScore {
			bestPod = pod
			bestScore = score
		}
	}

	return bestPod
}

func (r *ShardReconciler) cleanupDrainedPod(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
	pod *corev1.Pod,
	poolName string,
	poolSpec multigresv1alpha1.PoolSpec,
) error {
	logger := log.FromContext(ctx)

	// Remove finalizer to allow Kubernetes to delete the pod
	if slices.Contains(pod.Finalizers, PoolPodFinalizer) {
		pod.Finalizers = slices.DeleteFunc(pod.Finalizers, func(f string) bool {
			return f == PoolPodFinalizer
		})
		if err := r.Update(ctx, pod); err != nil {
			return fmt.Errorf("failed to remove finalizer from pod %s: %w", pod.Name, err)
		}
		logger.Info("Removed finalizer from drained pod", "pod", pod.Name)
		r.Recorder.Eventf(shard, "Normal", "DrainCompleted", "Completed drain for pod %s", pod.Name)
	}

	// Handle PVC deletion based on policy
	policy := multigresv1alpha1.PVCDeletionPolicy{WhenScaled: multigresv1alpha1.RetainPVCRetentionPolicy}
	if poolSpec.PVCDeletionPolicy != nil && poolSpec.PVCDeletionPolicy.WhenScaled != "" {
		policy = *poolSpec.PVCDeletionPolicy
	} else if shard.Spec.PVCDeletionPolicy != nil && shard.Spec.PVCDeletionPolicy.WhenScaled != "" {
		policy = *shard.Spec.PVCDeletionPolicy
	}

	if policy.WhenScaled == multigresv1alpha1.DeletePVCRetentionPolicy {
		lastDash := strings.LastIndex(pod.Name, "-")
		if lastDash != -1 {
			idx, _ := strconv.Atoi(pod.Name[lastDash+1:])
			cellName := pod.Labels[metadata.LabelMultigresCell]
			pvcName := BuildPoolDataPVCName(shard, poolName, cellName, idx)
			pvc := &corev1.PersistentVolumeClaim{}
			err := r.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: pvcName}, pvc)
			if err == nil {
				if err := r.Delete(ctx, pvc); err != nil && !errors.IsNotFound(err) {
					logger.Error(err, "Failed to delete PVC for scaled down pod", "pvc", pvcName)
				} else {
					logger.Info("Deleted PVC for scaled down pod", "pvc", pvcName)
				}
			}
		}
	}

	return nil
}

// reconcilePoolPDB applies the PodDisruptionBudget for the pool in the specific cell.
func (r *ShardReconciler) reconcilePoolPDB(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
	poolName string,
	cellName string,
) error {
	desired, err := BuildPoolPodDisruptionBudget(shard, poolName, cellName, r.Scheme)
	if err != nil {
		return fmt.Errorf("failed to build pool PDB: %w", err)
	}

	// Server Side Apply for PDB
	desired.SetGroupVersionKind(policyv1.SchemeGroupVersion.WithKind("PodDisruptionBudget"))
	if err := r.Patch(
		ctx,
		desired,
		client.Apply,
		client.ForceOwnership,
		client.FieldOwner("multigres-operator"),
	); err != nil {
		return fmt.Errorf("failed to apply pool PDB: %w", err)
	}

	// Emit an event only if it was just created or modified
	if desired.ObjectMeta.ResourceVersion == "" {
		r.Recorder.Eventf(
			shard,
			"Normal",
			"Applied",
			"Applied %s %s",
			desired.GroupVersionKind().Kind,
			desired.Name,
		)
	}

	return nil
}

// podNeedsUpdate checks if a pod requires recreation due to spec changes.
// Since most pod fields are immutable, we rely on the pre-computed spec-hash annotation.
func podNeedsUpdate(
	existing *corev1.Pod,
	shard *multigresv1alpha1.Shard,
	poolName, cellName string,
	poolSpec multigresv1alpha1.PoolSpec,
	index int,
	scheme *runtime.Scheme,
) bool {
	// If it has a deletion timestamp, let it die
	if !existing.DeletionTimestamp.IsZero() {
		return false
	}

	// If missing the annotation entirely, it needs an update
	existingHash, ok := existing.Annotations[metadata.AnnotationSpecHash]
	if !ok {
		return true
	}

	// Compute desired hash by building the ideal pod spec
	// NOTE: BuildPoolPod doesn't make API calls, it's safe to call frequently.
	desired, err := BuildPoolPod(shard, poolName, cellName, poolSpec, index, scheme)
	if err != nil {
		return false // Assume no update needed if we can't build it
	}

	desiredHash := ComputeSpecHash(desired)
	return existingHash != desiredHash
}

// reconcileSharedBackupPVC creates or updates the shared backup PVC for a specific cell.
func (r *ShardReconciler) reconcileSharedBackupPVC(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
	cellName string,
) error {
	// S3 backups use object storage; no shared PVC is needed.
	// TODO: Consider cleaning up orphaned backup PVCs when migrating from filesystem to S3.
	if shard.Spec.Backup != nil && shard.Spec.Backup.Type == multigresv1alpha1.BackupTypeS3 {
		return nil
	}

	desired, err := BuildSharedBackupPVC(shard, cellName, r.Scheme)
	if err != nil {
		return fmt.Errorf("failed to build shared backup PVC: %w", err)
	}
	if desired == nil {
		return nil
	}

	// Server Side Apply
	desired.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("PersistentVolumeClaim"))
	if err := r.Patch(
		ctx,
		desired,
		client.Apply,
		client.ForceOwnership,
		client.FieldOwner("multigres-operator"),
	); err != nil {
		return fmt.Errorf("failed to apply shared backup PVC: %w", err)
	}

	r.Recorder.Eventf(
		shard,
		"Normal",
		"Applied",
		"Applied %s %s",
		desired.GroupVersionKind().Kind,
		desired.Name,
	)

	return nil
}

// reconcilePoolHeadlessService creates or updates the headless Service for a pool in a specific cell.
func (r *ShardReconciler) reconcilePoolHeadlessService(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
	poolName string,
	cellName string,
	poolSpec multigresv1alpha1.PoolSpec,
) error {
	desired, err := BuildPoolHeadlessService(shard, poolName, cellName, poolSpec, r.Scheme)
	if err != nil {
		return fmt.Errorf("failed to build pool headless Service: %w", err)
	}

	// Server Side Apply
	desired.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Service"))
	if err := r.Patch(
		ctx,
		desired,
		client.Apply,
		client.ForceOwnership,
		client.FieldOwner("multigres-operator"),
	); err != nil {
		return fmt.Errorf("failed to apply pool headless Service: %w", err)
	}

	r.Recorder.Eventf(
		shard,
		"Normal",
		"Applied",
		"Applied %s %s",
		desired.GroupVersionKind().Kind,
		desired.Name,
	)

	return nil
}

// updateStatus updates the Shard status based on observed state.
func (r *ShardReconciler) updateStatus(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
) error {
	oldPhase := shard.Status.Phase
	cellsSet := make(map[multigresv1alpha1.CellName]bool)

	// Update pools status
	totalPods, readyPods, err := r.updatePoolsStatus(ctx, shard, cellsSet)
	if err != nil {
		return err
	}

	// Update MultiOrch status
	if err := r.updateMultiOrchStatus(ctx, shard, cellsSet); err != nil {
		return err
	}

	// Update cells list from all observed cells
	shard.Status.Cells = cellSetToSlice(cellsSet)

	// Update aggregate status fields
	shard.Status.PoolsReady = (totalPods > 0 && totalPods == readyPods)

	// Update Phase
	if shard.Status.PoolsReady && shard.Status.OrchReady {
		shard.Status.Phase = multigresv1alpha1.PhaseHealthy
		shard.Status.Message = "Ready"
	} else {
		shard.Status.Phase = multigresv1alpha1.PhaseProgressing
		shard.Status.Message = fmt.Sprintf(
			"PoolsReady: %v, OrchReady: %v",
			shard.Status.PoolsReady,
			shard.Status.OrchReady,
		)
	}

	// Update conditions
	r.setConditions(shard, totalPods, readyPods)

	shard.Status.ObservedGeneration = shard.Generation

	// 1. Construct the Patch Object
	patchObj := &multigresv1alpha1.Shard{
		TypeMeta: metav1.TypeMeta{
			APIVersion: multigresv1alpha1.GroupVersion.String(),
			Kind:       "Shard",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      shard.Name,
			Namespace: shard.Namespace,
		},
		Status: shard.Status,
	}

	// 2. Apply the Patch
	if oldPhase != shard.Status.Phase {
		r.Recorder.Eventf(
			shard,
			"Normal",
			"PhaseChange",
			"Transitioned from '%s' to '%s'",
			oldPhase,
			shard.Status.Phase,
		)
	}

	// Note: We rely on Server-Side Apply (SSA) to handle idempotency.
	// If the status hasn't changed, the API server will treat this Patch as a no-op,
	// so we don't need a manual DeepEqual check here.
	if err := r.Status().Patch(
		ctx,
		patchObj,
		client.Apply,
		client.FieldOwner("multigres-operator"),
		client.ForceOwnership,
	); err != nil {
		return fmt.Errorf("failed to patch status: %w", err)
	}

	return nil
}

// updatePoolsStatus aggregates status from all pool pods.
// Returns total pods, ready pods, and tracks cells in the cellsSet.
func (r *ShardReconciler) updatePoolsStatus(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
	cellsSet map[multigresv1alpha1.CellName]bool,
) (int32, int32, error) {
	var totalPods, readyPods int32
	clusterName := shard.Labels[metadata.LabelMultigresCluster]

	for poolName, poolSpec := range shard.Spec.Pools {
		var poolTotal, poolReady int32

		// TODO(#91): Pool.Cells may contain duplicates - add +listType=set validation at API level
		for _, cell := range poolSpec.Cells {
			cellName := string(cell)
			cellsSet[cell] = true

			// List pods for this specific pool and cell
			labels := buildPoolLabelsWithCell(shard, string(poolName), cellName, poolSpec)
			selector := metadata.GetSelectorLabels(labels)
			podList := &corev1.PodList{}
			if err := r.List(
				ctx,
				podList,
				client.InNamespace(shard.Namespace),
				client.MatchingLabels(selector),
			); err != nil {
				return 0, 0, fmt.Errorf("failed to list pods for status: %w", err)
			}

			var cellTotal, cellReady int32
			for i := range podList.Items {
				pod := &podList.Items[i]

				// Exclude terminating pods from total/ready counts
				if !pod.DeletionTimestamp.IsZero() {
					continue
				}

				cellTotal++

				// Check if pod is ready
				isReady := false
				for _, cond := range pod.Status.Conditions {
					if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
						isReady = true
						break
					}
				}
				if isReady {
					cellReady++
				}
			}

			// Emit a warning explicitly if the cell pool should have replicas but is empty
			replicas := DefaultPoolReplicas
			if poolSpec.ReplicasPerCell != nil {
				replicas = *poolSpec.ReplicasPerCell
			}
			if replicas > 0 && cellReady == 0 {
				r.Recorder.Eventf(
					shard,
					"Warning",
					"PoolEmpty",
					"Pool %s in cell %s has 0 ready replicas",
					poolName,
					cellName,
				)
			}

			poolTotal += cellTotal
			poolReady += cellReady
		}

		totalPods += poolTotal
		readyPods += poolReady

		monitoring.SetShardPoolReplicas(
			clusterName, shard.Name, string(poolName), "", shard.Namespace,
			poolTotal, poolReady,
		)
	}

	return totalPods, readyPods, nil
}

// updateMultiOrchStatus checks MultiOrch Deployments and sets OrchReady status.
// Also tracks cells in the cellsSet.
func (r *ShardReconciler) updateMultiOrchStatus(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
	cellsSet map[multigresv1alpha1.CellName]bool,
) error {
	multiOrchCells, err := getMultiOrchCells(shard)
	if err != nil {
		shard.Status.OrchReady = false
		return nil
	}

	orchReady := true
	for _, cell := range multiOrchCells {
		cellName := string(cell)
		cellsSet[cell] = true

		// Check MultiOrch Deployment status (deployments use long names)
		deployName := buildMultiOrchNameWithCell(shard, cellName, name.DefaultConstraints)
		deploy := &appsv1.Deployment{}
		err := r.Get(
			ctx,
			client.ObjectKey{Namespace: shard.Namespace, Name: deployName},
			deploy,
		)
		if err != nil {
			if errors.IsNotFound(err) {
				orchReady = false
				break
			}
			return fmt.Errorf("failed to get MultiOrch Deployment for status: %w", err)
		}

		// Check if deployment is ready
		if deploy.Spec.Replicas == nil ||
			deploy.Status.ObservedGeneration != deploy.Generation ||
			deploy.Status.ReadyReplicas != *deploy.Spec.Replicas {
			orchReady = false
			break
		}
	}

	shard.Status.OrchReady = orchReady
	return nil
}

// cellSetToSlice converts a cell set (map) to a slice.
func cellSetToSlice(cellsSet map[multigresv1alpha1.CellName]bool) []multigresv1alpha1.CellName {
	cells := make([]multigresv1alpha1.CellName, 0, len(cellsSet))
	for cell := range cellsSet {
		cells = append(cells, cell)
	}
	slices.Sort(cells)
	return cells
}

// setConditions creates status conditions based on observed state.
func (r *ShardReconciler) setConditions(
	shard *multigresv1alpha1.Shard,
	totalPods, readyPods int32,
) {
	// Available condition
	availableCondition := metav1.Condition{
		Type:               "Available",
		ObservedGeneration: shard.Generation,
		Status:             metav1.ConditionFalse,
		Reason:             "NotAllPodsReady",
		Message:            fmt.Sprintf("%d/%d pods ready", readyPods, totalPods),
	}

	if readyPods == totalPods && totalPods > 0 {
		availableCondition.Status = metav1.ConditionTrue
		availableCondition.Reason = "AllPodsReady"
		availableCondition.Message = fmt.Sprintf("All %d pods are ready", readyPods)
	}

	meta.SetStatusCondition(&shard.Status.Conditions, availableCondition)
}

// getMultiOrchCells returns the list of cells where MultiOrch should be deployed.
// If MultiOrch.Cells is specified, it uses that.
// Otherwise, it infers cells from all pools (union of pool cells).
func getMultiOrchCells(shard *multigresv1alpha1.Shard) ([]multigresv1alpha1.CellName, error) {
	cells := shard.Spec.MultiOrch.Cells

	// If MultiOrch specifies cells explicitly, use them
	// TODO(#91): Add +listType=set validation to MultiOrch.Cells to prevent duplicates at API level
	if len(cells) > 0 {
		return cells, nil
	}

	// Otherwise, collect unique cells from all pools
	cellSet := make(map[multigresv1alpha1.CellName]bool)
	for _, pool := range shard.Spec.Pools {
		for _, cell := range pool.Cells {
			cellSet[cell] = true
		}
	}

	// Convert set to slice
	cells = make([]multigresv1alpha1.CellName, 0, len(cellSet))
	for cell := range cellSet {
		cells = append(cells, cell)
	}

	// If still no cells found, error
	if len(cells) == 0 {
		return nil, fmt.Errorf(
			"MultiOrch has no cells specified and no cells found in pools - cannot deploy without cell information",
		)
	}

	slices.Sort(cells)
	return cells, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ShardReconciler) SetupWithManager(mgr ctrl.Manager, opts ...controller.Options) error {
	controllerOpts := controller.Options{
		MaxConcurrentReconciles: 20,
	}
	if len(opts) > 0 {
		controllerOpts = opts[0]
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&multigresv1alpha1.Shard{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&appsv1.Deployment{}).
		Owns(&appsv1.StatefulSet{}). // Kept temporarily during pod migration (removed in Commit 7)
		Owns(&corev1.Pod{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		WithOptions(controllerOpts).
		Complete(r)
}
