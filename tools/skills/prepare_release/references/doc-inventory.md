# Documentation Structure Guide

How to discover and audit all documentation in the multigres-operator repo.

## Discovery

Find all markdown files to audit:

```bash
find . -name '*.md' -not -path './.git/*' -not -path './.agents/*' -not -path './agent-docs/*' -not -path './CLAUDE.md' | sort
```

## Documentation Zones

Each zone has a different audience and staleness profile.

### User-facing (`README.md`, `CONTRIBUTING.md`, `docs/*.md`)

- **Audience:** Operators deploying the cluster, new contributors
- **Staleness triggers:** CRD field changes, new/removed features, CLI flag changes, Makefile target changes, installation changes, new deployment options
- **Key checks:** Installation commands still work, resource hierarchy diagram matches CRDs, configuration tables match actual flags, constraints section matches current limits

### Developer docs (`docs/development/*.md`)

- **Audience:** Contributors to the operator codebase
- **Staleness triggers:** Controller refactors, package renames/moves, new controllers, changes to drain/phase/topo logic, new CRD fields, webhook changes
- **Key checks:** File paths reference real files, function/type names exist in code, architecture descriptions match current implementation, phase values match constants

### Monitoring runbooks (`docs/monitoring/runbooks/*.md`)

- **Audience:** On-call operators
- **Staleness triggers:** Alert rule changes in `config/monitoring/`, new/removed alerts, metric name changes, controller behavior changes
- **Key checks:** Alert names match PrometheusRule manifests, investigation steps reference current resource types, remediation steps are still valid

### Demos (`demo/*/`)

- **Audience:** Users following walkthroughs
- **Staleness triggers:** Kustomize overlay changes, cert-manager config changes, observability stack changes
- **Key checks:** YAML samples match current CRD schema, kubectl commands work, referenced files exist

### Observer (`tools/observer/`)

- **Audience:** Users deploying the observer tool
- **Staleness triggers:** Observer code changes, new/removed health checks, API endpoint changes, configuration changes
- **Key checks:** Check categories match code, API response format matches implementation, deployment instructions work

### Plans (`plans/`)

- **Audience:** Design reference
- **Staleness triggers:** Rarely -- these are historical. Only flag if a plan contradicts current implementation without noting it's superseded.

### Samples (`config/samples/`)

- **Audience:** Users creating clusters
- **Staleness triggers:** CRD schema changes, new required fields, removed fields, default value changes
- **Key checks:** YAML validates against current CRDs, README walkthrough matches actual field behavior

## What to Check For

Common staleness patterns across all zones:

- References to deleted/renamed files or packages
- CRD fields added/removed but not reflected
- Makefile targets that changed
- Controller behavior that evolved (phases, conditions, events)
- Container args or env vars that changed
- Alert rules or metrics added/removed/renamed
- Sample YAML that no longer matches current CRD schema
- Missing documentation for new features (even if unrelated to recent changes)
