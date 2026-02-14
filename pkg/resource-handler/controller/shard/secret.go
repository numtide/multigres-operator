package shard

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/util/metadata"
)

// DefaultPostgresPassword is the default password for the PostgreSQL
// superuser during v1alpha1. Future versions will support user-supplied
// or auto-generated credentials.
const DefaultPostgresPassword = "postgres"

// BuildPostgresPasswordSecret creates a Secret containing the PostgreSQL
// superuser password. Both pgctld (via PGPASSWORD env var) and multipooler
// (via --connpool-admin-password) source credentials from this Secret.
func BuildPostgresPasswordSecret(
	shard *multigresv1alpha1.Shard,
	scheme *runtime.Scheme,
) (*corev1.Secret, error) {
	clusterName := shard.Labels["multigres.com/cluster"]
	labels := metadata.BuildStandardLabels(clusterName, "postgres-password")
	labels = metadata.MergeLabels(labels, shard.GetObjectMeta().GetLabels())

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      PostgresPasswordSecretName,
			Namespace: shard.Namespace,
			Labels:    labels,
		},
		Data: map[string][]byte{
			PostgresPasswordSecretKey: []byte(DefaultPostgresPassword),
		},
	}

	if err := ctrl.SetControllerReference(shard, secret, scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference: %w", err)
	}

	return secret, nil
}
