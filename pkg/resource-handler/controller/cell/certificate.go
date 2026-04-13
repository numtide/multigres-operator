package cell

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

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

// buildCertificate constructs an unstructured cert-manager Certificate for the
// multigateway TLS certificate. The Certificate spec matches what non-HA
// projects use, with the supabase-issuer ClusterIssuer.
func buildCertificate(cell *multigresv1alpha1.Cell) *unstructured.Unstructured {
	cn := cell.Spec.CertCommonName

	// Build the secondary SAN by stripping the "db." prefix if present.
	dnsNames := []any{cn}
	if after, ok := strings.CutPrefix(cn, "db."); ok {
		dnsNames = append(dnsNames, after)
	}

	cert := &unstructured.Unstructured{}
	cert.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "cert-manager.io",
		Version: "v1",
		Kind:    "Certificate",
	})
	cert.SetName(cn)
	cert.SetNamespace(cell.Namespace)
	cert.SetOwnerReferences([]metav1.OwnerReference{
		*metav1.NewControllerRef(cell, cell.GroupVersionKind()),
	})

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

	return cert
}

// reconcileCertificate creates or updates the cert-manager Certificate for
// multigateway TLS. This is a no-op if CertCommonName is empty.
func (r *CellReconciler) reconcileCertificate(
	ctx context.Context,
	cell *multigresv1alpha1.Cell,
) error {
	if cell.Spec.CertCommonName == "" {
		return nil
	}

	desired := buildCertificate(cell)

	if err := r.Patch(
		ctx,
		desired,
		client.Apply,
		client.ForceOwnership,
		client.FieldOwner("multigres-operator"),
	); err != nil {
		return fmt.Errorf("failed to apply cert-manager Certificate: %w", err)
	}

	return nil
}
