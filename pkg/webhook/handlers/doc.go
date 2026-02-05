// Package handlers implements the specific business logic for Kubernetes Admission Control.
//
// It contains implementations of the controller-runtime 'admission.Handler' interface for
// two primary purposes:
//
//  1. Mutation (Defaulters):
//     These handlers intercept CREATE and UPDATE requests to apply default values to resources.
//     They rely heavily on the 'pkg/resolver' module to ensure that defaults applied at
//     admission time are identical to those applied by the Reconciler during operation.
//     (See: MultigresClusterDefaulter).
//
//  2. Validation (Validators):
//     These handlers intercept CREATE, UPDATE, and DELETE requests to enforce semantic rules
//     that cannot be expressed in OpenAPI schemas (CRD Level 1) or CEL (CRD Level 2).
//     This includes:
//     - Stateful Validation: Checks requiring lookups of other objects (e.g., preventing
//     deletion of a template that is in use).
//     - Context-Aware Validation: Checks requiring access to request metadata (e.g.,
//     UserInfo) or old object states, serving as a fallback for clusters that do not
//     support 'ValidatingAdmissionPolicy'.
package handlers
