package multigrescluster

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	multigresv1alpha1 "github.com/multigres/multigres-operator/api/v1alpha1"
)

const (
	// CertSecretName is the secret name for the multigateway TLS certificate,
	// matching the convention used by non-HA projects.
	CertSecretName = "generated-certs"

	// CertIssuerName is the cert-manager ClusterIssuer used for TLS certificates.
	CertIssuerName = "supabase-issuer"

	// CertDuration is the certificate duration (5 years), matching non-HA projects.
	CertDuration = "44640h0m0s"

	// CertLiteralSubjectTemplate is the literal subject template for certificates.
	// The CN placeholder is replaced with the certCommonName.
	CertLiteralSubjectTemplate = "C=US, ST=Delware, L=New Castle,O=Supabase Inc, CN=%s"
)

var certGVK = schema.GroupVersionKind{
	Group:   "cert-manager.io",
	Version: "v1",
	Kind:    "Certificate",
}

// buildCertificate constructs an unstructured cert-manager Certificate for the
// multigateway TLS certificate. The Certificate spec matches what non-HA
// projects use, with the supabase-issuer ClusterIssuer.
// The owner is the MultigresCluster so there is exactly one reconciler
// and one ownerRef — no conflict when multiple cells share the same CN.
func buildCertificate(
	cluster *multigresv1alpha1.MultigresCluster,
	scheme *runtime.Scheme,
) (*unstructured.Unstructured, error) {
	cn := cluster.Spec.CertCommonName

	// Build the secondary SAN by stripping the "db." prefix if present.
	dnsNames := []any{cn}
	if after, ok := strings.CutPrefix(cn, "db."); ok {
		dnsNames = append(dnsNames, after)
	}

	cert := &unstructured.Unstructured{}
	cert.SetGroupVersionKind(certGVK)
	cert.SetName(cn)
	cert.SetNamespace(cluster.Namespace)

	if err := ctrl.SetControllerReference(cluster, cert, scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference: %w", err)
	}

	cert.Object["spec"] = map[string]any{
		"secretName":     CertSecretName,
		"dnsNames":       dnsNames,
		"duration":       CertDuration,
		"literalSubject": fmt.Sprintf(CertLiteralSubjectTemplate, cn),
		"issuerRef": map[string]any{
			"name":  CertIssuerName,
			"kind":  "ClusterIssuer",
			"group": "cert-manager.io",
		},
		"privateKey": map[string]any{
			"algorithm": "RSA",
			"size":      int64(2048),
		},
		"usages": []any{
			"digital signature",
			"key encipherment",
			"server auth",
		},
	}

	return cert, nil
}

// reconcileCertificate ensures the cert-manager Certificate matches the
// cluster spec. When CertCommonName is set it creates or updates the
// Certificate. When CertCommonName is empty it deletes a previously-managed
// Certificate (identified by the generated-certs secret convention) so that
// disabling TLS is deterministic.
func (r *MultigresClusterReconciler) reconcileCertificate(
	ctx context.Context,
	cluster *multigresv1alpha1.MultigresCluster,
) error {
	// Clean up stale Certificates owned by this cluster. When the CN
	// changes the old Certificate (named after the previous CN) would
	// otherwise linger and conflict on the same generated-certs secret.
	// When the CN is empty this removes all our Certificates.
	if err := r.deleteOwnedCertificates(
		ctx, cluster, cluster.Spec.CertCommonName,
	); err != nil {
		return err
	}

	if cluster.Spec.CertCommonName == "" {
		return nil
	}

	desired, err := buildCertificate(cluster, r.Scheme)
	if err != nil {
		return fmt.Errorf(
			"failed to build cert-manager Certificate: %w", err,
		)
	}
	if err := r.Patch(
		ctx,
		desired,
		client.Apply,
		client.ForceOwnership,
		client.FieldOwner("multigres-operator"),
	); err != nil {
		return fmt.Errorf(
			"failed to apply cert-manager Certificate: %w", err,
		)
	}
	return nil
}

// deleteOwnedCertificates removes cert-manager Certificates owned by this
// cluster whose name does not match keepName. Pass "" for keepName to delete
// all Certificates owned by this cluster (used when TLS is disabled).
func (r *MultigresClusterReconciler) deleteOwnedCertificates(
	ctx context.Context,
	cluster *multigresv1alpha1.MultigresCluster,
	keepName string,
) error {
	logger := log.FromContext(ctx)

	certList := &unstructured.UnstructuredList{}
	certList.SetGroupVersionKind(certGVK)
	if err := r.List(
		ctx,
		certList,
		client.InNamespace(cluster.Namespace),
	); err != nil {
		if errors.IsNotFound(err) || isNoMatchError(err) {
			return nil
		}
		return fmt.Errorf(
			"failed to list cert-manager Certificates: %w", err,
		)
	}

	for i := range certList.Items {
		cert := &certList.Items[i]
		if !isOwnedBy(cert, cluster) {
			continue
		}
		if cert.GetName() == keepName {
			continue
		}
		if err := r.Delete(ctx, cert); err != nil &&
			!errors.IsNotFound(err) {
			return fmt.Errorf(
				"failed to delete cert-manager Certificate %q: %w",
				cert.GetName(), err,
			)
		}
		logger.Info(
			"Deleted stale TLS Certificate",
			"certificate", cert.GetName(),
		)
	}

	return nil
}

// isOwnedBy checks whether an unstructured object has an ownerReference
// pointing to the given cluster.
func isOwnedBy(
	obj *unstructured.Unstructured,
	cluster *multigresv1alpha1.MultigresCluster,
) bool {
	for _, ref := range obj.GetOwnerReferences() {
		if ref.UID == cluster.UID {
			return true
		}
	}
	return false
}

// isNoMatchError returns true when the API server has no resource mapping
// for the requested GVK (e.g. cert-manager CRD not installed).
func isNoMatchError(err error) bool {
	// meta.NoKindMatchError and discovery errors surface as
	// *errors.StatusError with reason NotFound, but some client
	// implementations return a plain NoMatchError. Check the string
	// as a catch-all.
	return strings.Contains(err.Error(), "no matches for kind")
}
