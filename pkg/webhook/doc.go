/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package webhook provides the entry point for configuring Kubernetes admission
// webhooks for the Multigres Operator.
//
// The package exposes a [Setup] function that registers all webhook handlers with
// the controller-runtime manager. It wires together:
//
//   - Mutating Webhooks: Apply defaults to MultigresCluster resources before they
//     are persisted (see pkg/webhook/handlers for implementation).
//
//   - Validating Webhooks: Enforce semantic rules for MultigresCluster, template
//     resources, and child resources that cannot be expressed in CRD schemas.
//
// # Configuration
//
// The [Options] struct controls webhook behavior:
//   - Enable: Whether to start the webhook server.
//   - Namespace: Operator namespace for service account identity.
//   - ServiceAccountName: Used to construct the operator principal for child
//     resource validation (only the operator can modify its managed resources).
//
// # TLS Certificates
//
// Certificate management is handled by the generic pkg/cert module. This package
// provides webhook-specific helpers (PatchWebhookCABundle, FindOperatorDeployment)
// that are wired into the cert module's hooks by main.go.
package webhook
