package shard

import (
	_ "embed"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/cluster-handler/names"
)

// DefaultPgHbaTemplate is the default pg_hba.conf template for pooler instances.
// Uses trust authentication for testing/development. Production deployments should
// override this with proper authentication (scram-sha-256, SSL certificates, etc.).
//
//go:embed templates/pg_hba_template.conf
var DefaultPgHbaTemplate string

// BuildPgHbaConfigMap creates a ConfigMap containing the pg_hba.conf template.
// This ConfigMap is shared across all pools in a shard and mounted into postgres containers.
func BuildPgHbaConfigMap(
	shard *multigresv1alpha1.Shard,
	scheme *runtime.Scheme,
) (*corev1.ConfigMap, error) {
	// TODO: Add Shard.Spec.PgHbaTemplate field to allow custom templates
	template := DefaultPgHbaTemplate

	labels := map[string]string{
		"app.kubernetes.io/name":       "multigres",
		"app.kubernetes.io/instance":   names.JoinWithConstraints(names.ServiceConstraints, shard.Name),
		"app.kubernetes.io/component":  "pg-hba-config",
		"app.kubernetes.io/part-of":    "multigres",
		"app.kubernetes.io/managed-by": "multigres-operator",
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      PgHbaConfigMapName,
			Namespace: shard.Namespace,
			Labels:    labels,
		},
		Data: map[string]string{
			"pg_hba_template.conf": template,
		},
	}

	if err := ctrl.SetControllerReference(shard, cm, scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference: %w", err)
	}

	return cm, nil
}
