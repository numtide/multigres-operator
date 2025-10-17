---
title: Full envtest integration and validation for integration test spec
state: draft
tags:
- testing
- envtest
- integration
- nix
---

# Summary

Establish a comprehensive envtest setup strategy that works seamlessly in both Nix and non-Nix development environments, supporting integration tests both per-module and for the entire workspace. Address the challenges of envtest binary management in pure Nix environments while maintaining developer experience.

# Motivation

## The Challenge

envtest (part of controller-runtime) provides a real Kubernetes API server for integration testing without requiring a full cluster. However, it has complications:

1. **Binary Management**: envtest downloads Kubernetes binaries (kube-apiserver, etcd, kubectl) at runtime via `setup-envtest`
2. **Nix Purity**: Runtime binary downloads conflict with Nix's reproducibility model
3. **Multi-Module Testing**: Each module needs its own test environment with KUBEBUILDER_ASSETS set correctly
4. **Developer Experience**: Setup should be transparent whether using Nix or not

## Current Problems

- No clear strategy for envtest in Nix environments
- Unclear how to run tests per-module vs whole workspace
- Setup instructions missing for both Nix and non-Nix users
- KUBEBUILDER_ASSETS path management is manual and error-prone

## Goals

- **Dual Environment Support**: Work seamlessly in both Nix and non-Nix environments
- **Per-Module Testing**: Each module can run integration tests independently
- **Whole-Workspace Testing**: Run all integration tests across all modules
- **Explicit Version Control**: Allow explicit Kubernetes version selection for testing
- **Clear Setup Instructions**: Documented procedures for both environments
- **CI/CD Integration**: Consistent behavior in automated environments
- **Developer Experience**: Minimal manual intervention required

## Non-Goals

- Pure Nix packaging of Kubernetes binaries (accept controlled impurity)
- Supporting Windows
- Replacing envtest with alternative testing frameworks

# Proposal

Support two approaches based on environment:

1. **Non-Nix Approach**: Use Makefile targets to run `setup-envtest` which downloads binaries to `bin/k8s/`
2. **Nix Approach**: Accept controlled impurity by allowing `setup-envtest` to run within Nix shell, with clear documentation

Both approaches use the same Makefile targets, ensuring consistency.

# Design Details

## How envtest Works

envtest requires three Kubernetes binaries:
- `kube-apiserver`: Kubernetes API server
- `etcd`: Key-value store used by API server
- `kubectl`: Command-line tool (optional, for debugging)

These are managed by `setup-envtest`:
```bash
# Downloads and extracts binaries for specific K8s version
setup-envtest use 1.31 --bin-dir ./bin/k8s

# Returns path to binaries
setup-envtest use 1.31 --bin-dir ./bin/k8s -p path
# Output: /path/to/project/bin/k8s/1.31.0-linux-amd64
```

Tests then use:
```bash
KUBEBUILDER_ASSETS=/path/to/binaries go test ./...
```

## Makefile Integration

### Variables

```makefile
# Tool Binaries
ENVTEST ?= $(LOCALBIN)/setup-envtest

# Kubernetes version for envtest - explicitly configured
ENVTEST_K8S_VERSION ?= 1.31

# Directory for envtest binaries
ENVTEST_ASSETS_DIR ?= $(shell pwd)/bin/k8s
```

### Setup Target

```makefile
.PHONY: setup-envtest
setup-envtest: envtest ## Download and setup envtest binaries
	@echo "==> Setting up envtest binaries for Kubernetes $(ENVTEST_K8S_VERSION)"
	@mkdir -p $(ENVTEST_ASSETS_DIR)
	@$(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(ENVTEST_ASSETS_DIR) -p path || { \
		echo "ERROR: Failed to set up envtest binaries for version $(ENVTEST_K8S_VERSION)"; \
		exit 1; \
	}
	@echo "==> envtest binaries ready at $(ENVTEST_ASSETS_DIR)"

.PHONY: envtest-path
envtest-path: setup-envtest ## Print path to envtest binaries
	@$(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(ENVTEST_ASSETS_DIR) -p path

.PHONY: envtest
envtest: $(ENVTEST) ## Download setup-envtest locally if necessary
$(ENVTEST): $(LOCALBIN)
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest,$(ENVTEST_VERSION))
```

### Test Targets

```makefile
##@ Testing

.PHONY: test
test: manifests generate fmt vet setup-envtest ## Run tests for all modules
	@echo "==> Running tests across all modules"
	@export KUBEBUILDER_ASSETS="$$($(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(ENVTEST_ASSETS_DIR) -p path)"; \
	for mod in $(MODULES); do \
		echo "==> Testing $$mod..."; \
		(cd $$mod && go test $$(go list ./... | grep -v /e2e) -coverprofile=cover.out) || exit 1; \
	done

.PHONY: test-unit
test-unit: manifests generate fmt vet setup-envtest ## Run unit tests for all modules (fast)
	@echo "==> Running unit tests across all modules"
	@export KUBEBUILDER_ASSETS="$$($(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(ENVTEST_ASSETS_DIR) -p path)"; \
	for mod in $(MODULES); do \
		echo "==> Unit testing $$mod..."; \
		(cd $$mod && go test $$(go list ./... | grep -v /e2e) -short -v) || exit 1; \
	done

.PHONY: test-integration
test-integration: setup-envtest ## Run integration tests only (with envtest)
	@echo "==> Running integration tests"
	@export KUBEBUILDER_ASSETS="$$($(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(ENVTEST_ASSETS_DIR) -p path)"; \
	for mod in $(MODULES); do \
		echo "==> Integration testing $$mod..."; \
		(cd $$mod && go test -tags=integration ./... -v) || exit 1; \
	done
```

## Non-Nix Environment Setup

### Developer Workflow

```bash
# One-time setup (or when changing ENVTEST_K8S_VERSION)
make setup-envtest

# Run tests (uses cached binaries)
make test

# Test specific Kubernetes version
ENVTEST_K8S_VERSION=1.32 make setup-envtest
ENVTEST_K8S_VERSION=1.32 make test

# Per-module testing
cd api
KUBEBUILDER_ASSETS=$(make -C .. envtest-path) go test ./...
```

### Testing Against Multiple Kubernetes Versions

```bash
# Test against K8s 1.31
ENVTEST_K8S_VERSION=1.31 make setup-envtest
ENVTEST_K8S_VERSION=1.31 make test

# Test against K8s 1.32
ENVTEST_K8S_VERSION=1.32 make setup-envtest
ENVTEST_K8S_VERSION=1.32 make test

# Test against K8s 1.33
ENVTEST_K8S_VERSION=1.33 make setup-envtest
ENVTEST_K8S_VERSION=1.33 make test
```

Note how selecting v1.33 would result in a directory `/bin/k8s/1.33.0-linux-amd64` like directory to be created, but the actual version deployed will be based on the latest patch release (e.g. v1.33.4).

## Nix Environment Setup

### devshell.nix Configuration

```nix
# devshell.nix
{ pkgs, ... }:

{
  # ...

  # Add environment variables
  env = {
    "ENVTEST_K8S_VERSION"= "1.33";  # Default version for Nix users
  };

  # Load custom bash code
  shellHook = ''
    export PRJ_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
    export KUBEBUILDER_ASSETS="$PRJ_ROOT/bin/k8s/${env.ENVTEST_K8S_VERSION}.0-${pkgs.go.GOOS}-${pkgs.go.GOARCH}"
  '';
}
```

### Developer Workflow (Nix)

```bash
# Enter Nix shell
nix develop  # or direnv if configured

# One-time setup per shell session
make setup-envtest

# Run tests (KUBEBUILDER_ASSETS set automatically)
make test

# Per-module testing
cd api
go test ./...

# Test against different K8s version
ENVTEST_K8S_VERSION=1.32 make setup-envtest
ENVTEST_K8S_VERSION=1.32 make test
```

### Documentation for Nix Users

Add to `docs/development.md`:

```markdown
## Testing with Nix

Integration tests use envtest which requires Kubernetes binaries. These are downloaded
by `make setup-envtest` and cached in `bin/k8s/`.

**Initial Setup** (one-time per Nix shell session):
\`\`\`bash
nix develop
make setup-envtest
\`\`\`

The `KUBEBUILDER_ASSETS` environment variable is set automatically by the Nix shell.

**Running Tests**:
\`\`\`bash
make test          # All modules
make test-unit     # Fast unit tests only
cd api && go test ./...  # Single module
\`\`\`

**Testing Against Different Kubernetes Versions**:
\`\`\`bash
ENVTEST_K8S_VERSION=1.32 make setup-envtest
ENVTEST_K8S_VERSION=1.32 make test
\`\`\`

**Note**: envtest binaries are downloaded at runtime (not pure Nix), but cached in
`bin/k8s/` for reuse. This is standard practice for Go Kubernetes projects.
```

## Per-Module vs Whole-Workspace Testing

### Per-Module Testing

Each module can run integration tests independently:

```bash
# Option 1: Using Makefile helper
cd pkg/resource-handler
KUBEBUILDER_ASSETS=$(make -C ../.. envtest-path) go test ./...

# Option 2: If KUBEBUILDER_ASSETS already exported
cd pkg/resource-handler
export KUBEBUILDER_ASSETS=$(make -C ../.. envtest-path)
go test ./...

# Option 3: In Nix shell (KUBEBUILDER_ASSETS set automatically)
cd pkg/resource-handler
go test ./...
```

### Whole-Workspace Testing

Root Makefile handles workspace-wide testing:

```bash
# All modules, all tests
make test

# All modules, unit tests only (fast)
make test-unit

# All modules, integration tests only
make test-integration

# All modules with coverage
make test-coverage
```

## CI/CD Integration

### GitHub Actions

```yaml
name: Tests

on: [push, pull_request]

jobs:
  integration-tests:
    strategy:
      matrix:
        k8s-version: ['1.31', '1.32', '1.33']
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'

      - name: Cache envtest binaries
        uses: actions/cache@v4
        with:
          path: bin/k8s
          key: envtest-${{ runner.os }}-${{ matrix.k8s-version }}

      - name: Setup envtest
        run: ENVTEST_K8S_VERSION=${{ matrix.k8s-version }} make setup-envtest

      - name: Run tests
        run: ENVTEST_K8S_VERSION=${{ matrix.k8s-version }} make test

      - name: Upload coverage
        uses: codecov/codecov-action@v4
        with:
          files: ./coverage/*.out
          flags: k8s-${{ matrix.k8s-version }}
```

**Benefits**:
- Test against multiple Kubernetes versions (1.31, 1.32, 1.33)
- Cache envtest binaries per version
- Parallel test execution via matrix strategy

## Test Organization

### Unit vs Integration Tests

**Unit Tests**:
- Don't require envtest (mock Kubernetes clients)
- Fast, no external dependencies
- Run with `go test -short`
- Example: Testing pure resource builder functions

**Integration Tests**:
- Require envtest (real Kubernetes API)
- Test reconciliation loops, CRD validation, etc.
- Use build tags: `//go:build integration`
- Slower but validate real Kubernetes behavior

**Example Integration Test**:
```go
//go:build integration

package controller

import (
	"testing"
	"context"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func TestEtcdReconciler_Integration(t *testing.T) {
	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{"../../config/crd/bases"},
	}

	cfg, err := testEnv.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer testEnv.Stop()

	// Test with real Kubernetes API
	// ...
}
```

## Troubleshooting

### Common Issues

**Issue**: `KUBEBUILDER_ASSETS not set or empty`

**Solution**:
```bash
# Non-Nix
make setup-envtest
export KUBEBUILDER_ASSETS=$(make envtest-path)
go test ./...

# Nix
make setup-envtest
echo $KUBEBUILDER_ASSETS # Should be configured
```

**Issue**: `no such file or directory: kube-apiserver`

**Cause**: envtest binaries not downloaded or path incorrect

**Solution**:
```bash
# Check if binaries exist
ls bin/k8s/

# Re-download
rm -rf bin/k8s/
make setup-envtest
```

**Issue**: Nix shell doesn't set KUBEBUILDER_ASSETS

**Solution**: Run make setup-envtest in Nix shell:
```bash
make setup-envtest
echo $KUBEBUILDER_ASSETS  # Should be set
```

**Issue**: Tests fail with "unable to start control plane"

**Cause**: Usually port conflicts or stale etcd data

**Solution**:
```bash
# Kill any lingering processes
pkill -9 etcd kube-apiserver

# Clean up test artifacts
rm -rf /tmp/k8s-*
```

**Issue**: Version mismatch errors

**Cause**: ENVTEST_K8S_VERSION doesn't match downloaded binaries

**Solution**:
```bash
# Clean and re-download
rm -rf bin/k8s/
make setup-envtest

# Or specify version explicitly
ENVTEST_K8S_VERSION=1.31 make setup-envtest
```

# Test Plan

## Verification Checklist

### Non-Nix Environment

1. **Setup envtest**:
    ```bash
    make setup-envtest
    ls bin/k8s/  # Should contain Kubernetes binaries
    ```

2. **Run tests**:
    ```bash
    make test       # All modules
    make test-unit  # Fast tests
    ```

3. **Per-module testing**:
    ```bash
    cd pkg/resource-handler
    KUBEBUILDER_ASSETS=$(make -C ../.. envtest-path) go test ./...
    ```

4. **Test different K8s versions**:
    ```bash
    ENVTEST_K8S_VERSION=1.32 make setup-envtest
    ENVTEST_K8S_VERSION=1.32 make test
    ```

5. **Verify path helper**:
    ```bash
    make envtest-path
    # Should print: /path/to/project/bin/k8s/1.31.0-linux-amd64
    ```

### Nix Environment

1. **Enter Nix shell**:
    ```bash
    nix develop
    ```

2. **Setup envtest**:
    ```bash
    make setup-envtest
    echo $KUBEBUILDER_ASSETS  # Should be set
    ```

3. **Run tests**:
    ```bash
    make test
    cd api && go test ./...
    ```

4. **Test different versions**:
    ```bash
    ENVTEST_K8S_VERSION=1.32 make setup-envtest
    make test
    ```

### CI/CD

1. **Matrix testing across K8s versions**:
    - Tests run against 1.31, 1.32, 1.33
    - envtest binaries cached per version
    - Coverage uploaded per version

2. **Verify all modules tested**:
    - Check CI logs show all modules tested
    - Coverage reports include all modules

# Implementation History

- 2025-10-16: Initial draft created

# Drawbacks

1. **Impure Nix**: envtest downloads binaries at runtime, violating Nix purity
    - Mitigation: Accepted practice, similar to `go get`

2. **Storage Overhead**: Kubernetes binaries are ~100MB per version
    - Mitigation: Cached in `bin/k8s/`, can be cleaned with `make clean`

3. **Manual Version Selection**: Developers must choose ENVTEST_K8S_VERSION explicitly
    - Mitigation: Clear default (1.31), documented in Makefile and docs

4. **Developer Onboarding**: New developers need to run setup before tests
    - Mitigation: Clear documentation and automatic prompts in Makefile

# Alternatives

## Alternative 1: Auto-Detect Version from go.mod

Automatically derive Kubernetes version from k8s.io/api version in go.mod:

```makefile
ENVTEST_K8S_VERSION ?= $(shell go list -m -f "{{ .Version }}" k8s.io/api | awk -F'[v.]' '{printf "1.%d", $$3}')
```

**Rejected**: Less explicit, harder to test against different Kubernetes versions, adds complexity.

## Alternative 2: Pure Nix Packaging of Kubernetes Binaries

Package kube-apiserver and etcd in Nix derivations.

**Pros**: Pure Nix, reproducible
**Cons**:
- Significant maintenance burden
- Kubernetes binaries have complex build requirements
- Version synchronization is manual

**Rejected**: Pragmatism over purity for this use case.

## Alternative 3: Always Use Kind Clusters for Testing

Run all integration tests against real kind clusters instead of envtest.

**Pros**: True end-to-end testing
**Cons**:
- Much slower (cluster startup overhead)
- More complex setup
- Harder to run per-module tests

**Rejected**: envtest is faster and better suited for controller integration tests. Use kind for e2e tests separately.

## Alternative 4: Mock Kubernetes Client Entirely

Only use mocked clients, no real API server.

**Pros**: No envtest dependency, pure unit tests
**Cons**:
- Can't test CRD validation
- Can't test admission webhooks (future)
- Can't test watch/reconciliation behavior

**Rejected**: Integration tests with real API are essential for operator development.

# Infrastructure Needed

- **setup-envtest**: Installed via Makefile (no external dependency)
- **Kubernetes binaries**: Downloaded automatically by setup-envtest
- **Storage**: ~100MB per Kubernetes version in `bin/k8s/` directory
- **CI cache** (optional): GitHub Actions cache for faster runs

No additional external infrastructure required.
