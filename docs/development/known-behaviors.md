# Known Behaviors & Quirks

## Infinite "Configured" Loop (Client-Side Apply)

**The Symptom:**
When running `kubectl apply -f ...` on a manifest (like `no-templates.yaml`) repeatedly, `kubectl` reports `configured` every time, even though nothing changes in the cluster.

**The Cause:**
This is a conflict between **Client-Side Apply (CSA)** and **Mutating Webhooks**.
1.  **The Diff:** Legacy `kubectl apply` compares your local file (which omits defaults like `replicas`) against the live server object (where the webhook has injected `replicas: 1`).
2.  **The Patch:** `kubectl` sees a discrepancy and sends a PATCH request to **remove** the field (setting it to `null`).
3.  **The Webhook:** The Webhook intercepts this PATCH and immediately puts `replicas: 1` back.
4.  **The No-Op:** The API Server sees that the final state matches the initial state and performs a **No-Op** (no `Generation` or `ResourceVersion` bump).
5.  **The Report:** Despite the server-side no-op, `kubectl` reports `configured` because it successfully sent a non-empty patch.

**The Verdict:**
This is **standard Kubernetes behavior** for Operators with defaulting webhooks (common in Istio, Cert-Manager, etc.). It is a limitation of the legacy `kubectl apply` logic, not a bug in the operator. **Critically, this is purely cosmetic** - no actual changes occur on the server (confirmed by no `Generation` increment), and controllers do not unnecessarily reconcile.

**The Solutions:**

If seeing repeated `configured` messages is disturbing, users can:

* **Recommended:** Use Server-Side Apply (`kubectl apply --server-side`). This moves the merge logic to the API server, which correctly handles ownership and defaults without fighting.
* **Alternative:** Explicitly set all default values in your local YAML to match the server state (e.g., manually add all defaulted fields).

**Rejected Alternative Solutions:**

We considered several approaches to eliminate this behavior entirely, but each has significant drawbacks:

**Option 1: Remove the Defaulting Webhook** ❌
* **Why it would work:** Without the webhook adding defaults, there would be no discrepancy for `kubectl` to detect.
* **Why we rejected it:** Our defaulting logic includes complex computed defaults (e.g., deriving resource requirements, setting up template references) that are essential for operator functionality and good UX. Removing the webhook would break the API design and force users to specify every field manually.

**Option 2: Make All Fields Required** ❌
* **Why it would work:** If every field must be specified in the YAML, the webhook wouldn't add anything new.
* **Why we rejected it:** This defeats the entire purpose of defaults and creates terrible user experience. Users would need to write massive YAML files with hundreds of fields just to create a simple cluster.

**Option 3: Use CRD-Level Defaults Instead of Webhooks** ⚠️
* **Why it would work:** CRD OpenAPIv3 schemas support `default:` values that are applied by the API server before storage. `kubectl apply` sees these in the OpenAPI schema and includes them in comparison.
* **Why we rejected it:** CRD defaults are extremely limited - they only support simple scalar values (e.g., `replicas: 1`), not complex computed logic like "derive this field from these other fields" or "populate template references." Our defaulting logic is too sophisticated for CRD-level defaults.

**Option 4: Document and Accept** ✅
* **What we did:** Documented this as a known cosmetic quirk with zero operational impact.
* **Why this is correct:** The behavior is harmless, standard across the ecosystem, and users have easy workarounds. The alternatives would compromise our API design or user experience for purely cosmetic gain.
