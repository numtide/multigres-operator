# Deep Investigation Protocol

When a scenario results in UNSTABLE or persistent observer findings, follow this protocol to trace root cause. This delegates detailed investigation to the **`diagnose_with_observer` skill**, which has comprehensive triage procedures, log tracing commands, and code investigation guidance.

## Step 1 — Capture State

Before anything changes, capture the current state:
```bash
# Full observer snapshot
observer /api/status | jq . > /tmp/exercise-snapshot.json

# Finding history with pattern classification
observer /api/history | jq . > /tmp/exercise-history.json

# Targeted on-demand check for specific categories
observer '/api/check?categories=replication,connectivity' | jq .
```

## Step 2 — Triage with diagnose_with_observer

Switch to the `diagnose_with_observer` skill and follow its investigation protocol:
1. **Severity triage** (Section 4) — process fatals first, then errors, then warns
2. **Diagnostic patterns** (Section 5) — match findings to known patterns (silent data plane failure, replication chains, topology issues)
3. **Call chain tracing** (Section 7) — trace through gateway → multiorch → pooler → postgres logs
4. **Code investigation** (Section 8) — determine if the bug is in the operator or upstream multigres

## Step 3 — Classify and Report

After investigation, classify the finding:
- **Operator bug**: File location, code path, and suggested fix
- **Upstream bug**: Multigres component, version affected, and whether it's fixed on main
- **Expected transient**: Document why it's benign (e.g., rolling restart noise)

## Rules

1. **Do NOT auto-dismiss.** Every post-grace-period error is a potential real bug.
2. **Check operator code** — see `references/operator-knowledge.md` for where bugs hide, organized by observer check category.
3. **Check upstream multigres** — clone/pull `/tmp/multigres`:
   ```bash
   if [ -d /tmp/multigres ]; then cd /tmp/multigres && git pull origin main; else git clone https://github.com/multigres/multigres /tmp/multigres; fi
   ```
   Check which version is running: `cat api/v1alpha1/image_defaults.go` and compare with upstream HEAD.
4. **Report to user** with: what the observer detected, whether it's operator or upstream, the specific code path, and a suggested fix.
5. **Wait for user approval** before proceeding to the next scenario.
