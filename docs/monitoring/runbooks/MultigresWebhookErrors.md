# MultigresWebhookErrors

## Meaning

The mutating or validating admission webhook is returning errors. The `multigres_operator_webhook_request_total{result="error"}` counter is increasing.

## Impact

Users may be unable to create or update MultigresCluster resources. Depending on the webhook's `failurePolicy`, requests may be rejected (`Fail`) or allowed without mutation/validation (`Ignore`).

## Investigation

```bash
# Check operator logs for webhook errors
kubectl logs -n multigres-operator deploy/multigres-operator-controller-manager --tail=200 | grep -i webhook

# Check webhook configuration
kubectl get mutatingwebhookconfigurations,validatingwebhookconfigurations | grep multigres

# Test a dry-run apply
kubectl apply --dry-run=server -f <cluster-manifest.yaml>

# Check webhook certificate validity
kubectl get secret -n multigres-operator -l app.kubernetes.io/name=multigres-operator
```

## Remediation

1. **Certificate expiry** — If using cert-manager, check that the webhook certificate is valid and has been renewed.
2. **Resolver errors** — The defaulter webhook depends on the resolver. Check that templates referenced in the cluster spec exist.
3. **Validation errors** — If the validator is rejecting requests, check the error message. The spec may reference non-existent templates or have invalid values.
4. **Webhook availability** — Ensure the operator pod is running and the webhook service is reachable: `kubectl get endpoints -n multigres-operator`.
