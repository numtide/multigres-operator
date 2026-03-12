# Observer Skills

AI agent skills for working with the multigres observer. Each skill is a structured prompt that teaches an agent how to use the observer effectively for a specific task.

## Available Skills

| Skill | Mode | Description |
|-------|------|-------------|
| [exercise_cluster](exercise_cluster/SKILL.md) | Proactive | Deploy fixtures, run mutation scenarios, and validate health. Finds bugs by systematically exercising real cluster operations. |
| [diagnose_with_observer](diagnose_with_observer/SKILL.md) | Reactive | Triage findings by severity, trace root causes through component logs and code, and produce actionable bug reports. |

**exercise_cluster** creates situations that might expose bugs. **diagnose_with_observer** investigates when bugs are found. Use them together: exercise finds the problem, diagnose traces the root cause.

## How They Work Together

```
exercise_cluster                          diagnose_with_observer
────────────────                          ──────────────────────
1. Deploy fixture                         1. Fetch /api/status
2. Run mutation scenario                  2. Triage by severity
3. Verify stability via observer    ──→   3. Trace component logs
4. If UNSTABLE, hand off to ────────┘     4. Investigate code
                                          5. Produce bug report
```

## Reference Files

The `exercise_cluster` skill includes reference documents for guided investigation:

| File | Contents |
|------|----------|
| [references/scenarios.md](exercise_cluster/references/scenarios.md) | 40+ mutation scenarios across scale, config, lifecycle, template, webhook rejection, drain, backup, rolling update, failure injection, and concurrent operations |
| [references/operator-knowledge.md](exercise_cluster/references/operator-knowledge.md) | Operator architecture (resource-handler vs data-handler), code paths per observer check category, upstream vs operator decision tree, version checking, key ports |

## Observer API Endpoints

Both skills interact with the observer's HTTP API (default `:9090`):

| Endpoint | Description |
|----------|-------------|
| `GET /api/status` | Latest cycle snapshot — findings, probes, healthy checks |
| `GET /api/history` | Finding history — persistent, transient, flapping classification |
| `GET /api/check?categories=...` | On-demand targeted check (immediate, no wait for ticker) |
| `GET /metrics` | Prometheus metrics |
| `GET /healthz` | Liveness probe |

## Usage

The observer deploys automatically with `make kind-deploy`. Reference the skill directly or create a stub in your project skills directory that points to this skill directory.
