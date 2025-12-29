package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/resolver"
)

// MultigresClusterDefaulter handles the mutation of MultigresCluster resources.
// It uses the shared resolver module to apply the same defaults that the operator
// would apply during reconciliation, ensuring consistency.
type MultigresClusterDefaulter struct {
	Resolver *resolver.Resolver
	decoder  admission.Decoder
}

// NewMultigresClusterDefaulter creates a new defaulter handler.
func NewMultigresClusterDefaulter(r *resolver.Resolver) *MultigresClusterDefaulter {
	return &MultigresClusterDefaulter{
		Resolver: r,
	}
}

// InjectDecoder injects the decoder.
func (d *MultigresClusterDefaulter) InjectDecoder(decoder admission.Decoder) error {
	d.decoder = decoder
	return nil
}

// Handle implements the admission.Handler interface.
func (d *MultigresClusterDefaulter) Handle(ctx context.Context, req admission.Request) admission.Response {
	cluster := &multigresv1alpha1.MultigresCluster{}

	err := d.decoder.Decode(req, cluster)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	// Apply defaults using the central resolver.
	// This ensures that whether defaults are applied here (webhook) or in the
	// controller (reconcile loop), the logic is identical.
	if err := d.Resolver.Resolve(ctx, cluster); err != nil {
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("failed to calculate defaults: %w", err))
	}

	marshaled, err := json.Marshal(cluster)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("failed to marshal defaulted object: %w", err))
	}

	return admission.PatchResponseFromRaw(req.Object.Raw, marshaled)
}
