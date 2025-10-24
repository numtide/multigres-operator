---
title: Streamline Makefile for Multi-Module Architecture and Developer Experience
state: work-in-progress
tags:
- cicd
- tooling
- multi-module
---

# Summary

Restructure the Makefile to properly support our multi-module Go architecture with clear targets for building, testing, and managing all modules. Add version management with semantic versioning tags per module, improve developer workflows, and integrate dependency management tooling (renovate).

# Motivation

## Why Makefile Changes Are Needed

1. **Multi-Module Architecture**: The project uses separate Go modules for API, cluster-handler, data-handler, and resource-handler. The Makefile needs explicit targets to build and test across all modules.

2. **Clearer Interaction Methods**: Developers need obvious targets like `make build`, `make test`, `make lint` that work across the entire workspace without needing to know about individual modules.

3. **Easier Review and Adjustments**: Well-organized Makefile sections with clear naming make it easier to review changes and adjust build processes.

4. **Version Management**: Each module can be independently versioned using semantic version tags with directory prefixes (e.g., `api/v0.1.0`, `pkg/cluster-handler/v0.2.0`), though in practice modules are typically versioned together.

5. **Dependency Management**: Integrate renovate markers for automated dependency updates across all modules.

6. **Developer Experience**: Provide convenient targets for common workflows (`check`, `verify`, coverage reporting).

7. **Consistent Container Builds**: Provide `make container` target similar to otel operator for building container images.

## Current Problems

- No explicit multi-module build/test targets
- Image versioning doesn't follow semantic versioning
- No build metadata (version, commit, date) in binaries
- Missing renovate integration for dependency management
- No easy way to verify all modules are properly formatted/vetted/linted
- Inconsistent container build targets (`docker-build` vs industry standard `container`)

## Goals

- Support multi-module build and test operations
- Implement semantic versioning per module with directory-prefixed tags
- Add renovate markers for dependency management
- Improve image naming and tagging
- Add build metadata to compiled binaries
- Provide clear developer workflow targets
- Add `make container` target for container image builds
- Maintain compatibility with Nix environment

## Non-Goals

- Changing kubebuilder scaffolding patterns
- Supporting non-Linux platforms
- Adding complex build orchestration beyond what's needed

# Proposal

Restructure the Makefile to explicitly support multi-module operations, add semantic versioning per module, integrate renovate for dependency management, and improve developer workflows.

# Design Details

## Multi-Module Architecture

The project structure:
```
multigres-operator/
├── go.mod                          # Root module (main binary)
├── cmd/multigres-operator/         # Main entry point
├── api/
│   └── go.mod                      # API module
├── pkg/
│   ├── cluster-handler/
│   │   └── go.mod                  # Cluster handler module
│   ├── data-handler/
│   │   └── go.mod                  # Data handler module
│   └── resource-handler/
│       └── go.mod                  # Resource handler module
└── go.work                         # Workspace (local development, not committed)
```

**Key Point**: While each module *can* be independently versioned, in practice they are typically versioned together for simplicity. The capability exists for cases where it's needed, but isn't the primary workflow.

## Semantic Versioning Per Module

Each module uses semantic versioning with directory-prefixed git tags:

```bash
# Tag format: <module-path>/<semver>
# Typical workflow: version all modules together
git tag api/v0.1.0
git tag pkg/cluster-handler/v0.1.0
git tag pkg/data-handler/v0.1.0
git tag pkg/resource-handler/v0.1.0
git tag v0.1.0  # Root module (operator binary)
```

**Note**: While independent versioning is possible (e.g., only bumping `api/v0.2.0` while others stay at `v0.1.0`), the typical practice is to version all modules together unless there's a specific reason not to.

## Makefile Structure

### Variables Section

```makefile
# Multi-module paths
MODULES := . ./api ./pkg/cluster-handler ./pkg/data-handler ./pkg/resource-handler

# Version from git tags
VERSION ?= $(shell git describe --tags --match "v*" --always --dirty 2>/dev/null || echo "v0.0.1-dev")
VERSION_SHORT ?= $(shell echo $(VERSION) | sed 's/^v//')

# Image configuration
IMG_PREFIX ?= ghcr.io/numtide
IMG_REPO ?= multigres-operator
IMG ?= $(IMG_PREFIX)/$(IMG_REPO):$(VERSION_SHORT)

# Build metadata
BUILD_DATE ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
GIT_COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null || echo "unknown")

# LDFLAGS for version info
LDFLAGS := -X main.version=$(VERSION) \
           -X main.buildDate=$(BUILD_DATE) \
           -X main.gitCommit=$(GIT_COMMIT)

# Container tool (docker or podman)
CONTAINER_TOOL ?= docker

# Kind cluster name for local development
KIND_CLUSTER ?= multigres-operator-dev

# Local kubeconfig for kind cluster (doesn't modify user's ~/.kube/config)
KIND_KUBECONFIG ?= $(shell pwd)/kubeconfig.yaml
```

### Multi-Module Targets

```makefile
##@ Multi-Module Operations

.PHONY: modules-tidy
modules-tidy: ## Run go mod tidy on all modules
	@for mod in $(MODULES); do \
		echo "==> Tidying $$mod..."; \
		(cd $$mod && go mod tidy) || exit 1; \
	done

.PHONY: modules-download
modules-download: ## Download dependencies for all modules
	@for mod in $(MODULES); do \
		echo "==> Downloading dependencies for $$mod..."; \
		(cd $$mod && go mod download) || exit 1; \
	done

.PHONY: modules-verify
modules-verify: ## Verify dependencies for all modules
	@for mod in $(MODULES); do \
		echo "==> Verifying $$mod..."; \
		(cd $$mod && go mod verify) || exit 1; \
	done
```

### Build and Test Targets

```makefile
##@ Build

.PHONY: build
build: manifests generate fmt vet
	@echo "==> Building operator binary (version: $(VERSION))"
	go build -ldflags="$(LDFLAGS)" -o bin/multigres-operator cmd/multigres-operator/main.go

.PHONY: fmt
fmt: ## Run go fmt against code in all modules
	@for mod in $(MODULES); do \
		echo "==> Formatting $$mod..."; \
		(cd $$mod && go fmt ./...) || exit 1; \
	done

.PHONY: vet
vet: ## Run go vet against code in all modules
	@for mod in $(MODULES); do \
		echo "==> Vetting $$mod..."; \
		(cd $$mod && go vet ./...) || exit 1; \
	done

.PHONY: container
container: ## Build container image (similar to docker-build)
	$(CONTAINER_TOOL) build -t ${IMG} .

.PHONY: docker-build
docker-build: container ## Alias for container (backward compatibility)

##@ Testing

.PHONY: test
test: manifests generate fmt vet setup-envtest ## Run tests for all modules
	@echo "==> Running tests across all modules"
	@for mod in $(MODULES); do \
		echo "==> Testing $$mod..."; \
		(cd $$mod && \
			KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" \
			go test $$(go list ./... | grep -v /e2e) -coverprofile=cover.out) || exit 1; \
	done

.PHONY: test-unit
test-unit: manifests generate fmt vet setup-envtest ## Run unit tests for all modules (fast, no e2e)
	@echo "==> Running unit tests across all modules"
	@for mod in $(MODULES); do \
		echo "==> Unit testing $$mod..."; \
		(cd $$mod && \
			KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" \
			go test $$(go list ./... | grep -v /e2e) -short -v) || exit 1; \
	done

.PHONY: test-coverage
test-coverage: manifests generate fmt vet setup-envtest ## Generate coverage report with HTML
	@mkdir -p coverage
	@echo "==> Generating coverage across all modules"
	@for mod in $(MODULES); do \
		modname=$$(basename $$mod); \
		[ "$$modname" = "." ] && modname="root"; \
		echo "==> Coverage for $$mod..."; \
		(cd $$mod && \
			KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" \
			go test ./... -coverprofile=../coverage/$$modname.out -covermode=atomic -coverpkg=./...) || exit 1; \
	done
	@echo "==> Generating HTML reports"
	@for out in coverage/*.out; do \
		html=$${out%.out}.html; \
		go tool cover -html=$$out -o=$$html; \
		echo "Generated: $$html"; \
	done
	@echo "Coverage reports in coverage/"
```

### Lint Targets

```makefile
##@ Code Quality

.PHONY: lint
lint: golangci-lint ## Run golangci-lint linter across all modules
	@for mod in $(MODULES); do \
		echo "==> Linting $$mod..."; \
		(cd $$mod && $(GOLANGCI_LINT) run) || exit 1; \
	done

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint linter and perform fixes
	@for mod in $(MODULES); do \
		echo "==> Fixing lint issues in $$mod..."; \
		(cd $$mod && $(GOLANGCI_LINT) run --fix) || exit 1; \
	done
```

### Developer Convenience

```makefile
##@ Developer Workflow

.PHONY: check
check: lint test ## Run all checks before committing (lint + test)
	@echo "==> All checks passed!"

.PHONY: verify
verify: manifests generate ## Verify generated files are up to date
	@echo "==> Verifying generated files are committed"
	@git diff --exit-code config/crd api/ || { \
		echo "ERROR: Generated files are out of date."; \
		echo "Run 'make manifests generate' and commit the changes."; \
		exit 1; \
	}
	@echo "==> Verification passed!"

.PHONY: pre-commit
pre-commit: modules-tidy fmt vet lint test ## Run full pre-commit checks (tidy, fmt, vet, lint, test)
	@echo "==> Pre-commit checks passed!"
```

### Kind Development Workflow

```makefile
##@ Kind Cluster (Local Development)

.PHONY: kind-up
kind-up: ## Create a kind cluster for local development
	@command -v $(KIND) >/dev/null 2>&1 || { \
		echo "ERROR: kind is not installed."; \
		echo "Install it from: https://kind.sigs.k8s.io/docs/user/quick-start/"; \
		exit 1; \
	}
	@if $(KIND) get clusters | grep -q "^$(KIND_CLUSTER)$$"; then \
		echo "Kind cluster '$(KIND_CLUSTER)' already exists."; \
	else \
		echo "Creating kind cluster '$(KIND_CLUSTER)'..."; \
		$(KIND) create cluster --name $(KIND_CLUSTER); \
	fi
	@echo "==> Exporting kubeconfig to $(KIND_KUBECONFIG)"
	@$(KIND) get kubeconfig --name $(KIND_CLUSTER) > $(KIND_KUBECONFIG)
	@echo "==> Cluster ready. Use: export KUBECONFIG=$(KIND_KUBECONFIG)"

.PHONY: kind-load
kind-load: container ## Build and load image into kind cluster
	@echo "==> Loading image $(IMG) into kind cluster..."
	$(KIND) load docker-image $(IMG) --name $(KIND_CLUSTER)

.PHONY: kind-deploy
kind-deploy: kind-up manifests kustomize kind-load ## Deploy operator to kind cluster
	@echo "==> Installing CRDs..."
	KUBECONFIG=$(KIND_KUBECONFIG) $(KUSTOMIZE) build config/crd | KUBECONFIG=$(KIND_KUBECONFIG) $(KUBECTL) apply -f -
	@echo "==> Deploying operator..."
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	KUBECONFIG=$(KIND_KUBECONFIG) $(KUSTOMIZE) build config/default | KUBECONFIG=$(KIND_KUBECONFIG) $(KUBECTL) apply -f -
	@echo "==> Deployment complete!"
	@echo "Check status: KUBECONFIG=$(KIND_KUBECONFIG) kubectl get pods -n multigres-operator-system"

.PHONY: kind-redeploy
kind-redeploy: kind-load ## Rebuild image, reload to kind, and restart pods
	@echo "==> Restarting operator pods..."
	KUBECONFIG=$(KIND_KUBECONFIG) $(KUBECTL) rollout restart deployment -n multigres-operator-system

.PHONY: kind-down
kind-down: ## Delete the kind cluster
	@echo "==> Deleting kind cluster '$(KIND_CLUSTER)'..."
	$(KIND) delete cluster --name $(KIND_CLUSTER)
	@rm -f $(KIND_KUBECONFIG)
	@echo "==> Cluster and kubeconfig deleted"
```

**Kind Workflow Benefits**:
- **No kubeconfig pollution**: Uses local `kubeconfig.yaml` file, doesn't modify `~/.kube/config`
- **Explicit kubeconfig**: All kubectl commands use `KUBECONFIG` environment variable
- **Clean isolation**: Each developer can have multiple projects with their own kind clusters
- **Easy cleanup**: `make kind-down` removes both cluster and local kubeconfig
- **Developer-friendly**: `make kind-up` tells you to export KUBECONFIG for manual kubectl usage

**Usage**:
```bash
# Setup
make kind-up
export KUBECONFIG=$(pwd)/kubeconfig.yaml  # For manual kubectl commands

# Deploy
make kind-deploy

# Iterate
# ... make code changes ...
make kind-redeploy

# Manual operations
kubectl get pods  # Uses kubeconfig.yaml automatically

# Cleanup
make kind-down
```

## Renovate Integration

Add renovate markers to dependency tool versions (similar to otel operator):

```makefile
## Tool Versions
# renovate: datasource=github-releases depName=kubernetes-sigs/kustomize
KUSTOMIZE_VERSION ?= v5.6.0

# renovate: datasource=github-releases depName=kubernetes-sigs/controller-tools
CONTROLLER_TOOLS_VERSION ?= v0.18.0

# renovate: datasource=github-releases depName=golangci/golangci-lint
GOLANGCI_LINT_VERSION ?= v2.3.0

# renovate: datasource=github-releases depName=kubernetes-sigs/kind
KIND_VERSION ?= v0.24.0
```

### Renovate Configuration

Create `.github/renovate.json`:

```json
{
  "$schema": "https://docs.renovatebot.com/renovate-schema.json",
  "extends": [
    "config:base"
  ],
  "postUpdateOptions": [
    "gomodTidy"
  ],
  "packageRules": [
    {
      "matchManagers": ["gomod"],
      "matchUpdateTypes": ["minor", "patch"],
      "groupName": "Go dependencies (non-major)"
    },
    {
      "matchDatasources": ["go"],
      "matchPackagePatterns": ["^k8s.io"],
      "groupName": "Kubernetes dependencies"
    }
  ],
  "customManagers": [
    {
      "customType": "regex",
      "fileMatch": ["^Makefile$"],
      "matchStrings": [
        "# renovate: datasource=(?<datasource>.*?) depName=(?<depName>.*?)\\n.*?_VERSION \\?= (?<currentValue>.*)"
      ]
    }
  ]
}
```

**Benefits**:
- Automated dependency updates across all modules
- Grouped updates for easier review
- Tool version updates in Makefile
- Consistent with otel operator patterns

## Version Management in main.go

Update `cmd/multigres-operator/main.go`:

```go
var (
	version   = "dev"
	buildDate = "unknown"
	gitCommit = "unknown"
)

func main() {
	// ...
	setupLog.Info("Starting Multigres Operator",
		"version", version,
		"buildDate", buildDate,
		"gitCommit", gitCommit,
	)
	// ... rest of main
}
```

## Container Build Target

The `make container` target follows otel operator patterns:

```makefile
.PHONY: container
container: ## Build container image
	$(CONTAINER_TOOL) build -t ${IMG} .

.PHONY: docker-build
docker-build: container ## Alias for container (backward compatibility)

.PHONY: docker-push
docker-push: ## Push container image
	$(CONTAINER_TOOL) push ${IMG}
```

**Benefits**:
- Consistent with otel operator naming (`make container`)
- Backward compatible (`make docker-build` still works)
- Supports both docker and podman via `CONTAINER_TOOL`

## CI Integration

Update CI to use new targets:

```yaml
name: CI

on: [push, pull_request]

jobs:
  verify:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'

      - name: Verify dependencies
        run: make modules-verify

      - name: Verify generated files
        run: make verify

  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'

      - name: Run checks
        run: make check

      - name: Generate coverage
        run: make test-coverage

      - name: Upload coverage
        uses: codecov/codecov-action@v4
        with:
          files: ./coverage/*.out

  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'

      - name: Build container image
        run: make container
```

# Test Plan

## Verification Checklist

1. **Multi-module operations work**:

    ```bash
    make modules-tidy
    make modules-verify
    make fmt
    make vet
    make lint
    make test
    make test-unit
    ```

2. **Version management**:

    ```bash
    # Typical workflow: version all modules together
    git tag api/v0.1.0
    git tag pkg/cluster-handler/v0.1.0
    git tag pkg/data-handler/v0.1.0
    git tag pkg/resource-handler/v0.1.0
    git tag v0.1.0

    # Build and check version
    make build
    ./bin/multigres-operator --version
    ```

3. **Build metadata**:

    ```bash
    make build
    ./bin/multigres-operator # Should log version, buildDate, gitCommit
    ```

4. **Container builds**:

    ```bash
    make container     # New target (otel operator style)
    make docker-build  # Should still work (backward compat)
    docker images | grep multigres-operator
    ```

5. **Coverage reporting**:

    ```bash
    make test-coverage
    ls coverage/  # Should have .out and .html files for each module
    ```

6. **Developer workflows**:

    ```bash
    make check       # lint + test
    make verify      # check generated files
    make pre-commit  # full pre-commit validation
    ```

7. **Renovate markers**:
    - Verify renovate can parse Makefile markers
    - Test renovate configuration with `renovate-config-validator`

8. **Kind cluster workflow**:

    ```bash
    # Create cluster and get kubeconfig
    make kind-up
    ls kubeconfig.yaml  # Should exist

    # Export for manual kubectl usage
    export KUBECONFIG=$(pwd)/kubeconfig.yaml
    kubectl cluster-info  # Should connect to kind cluster

    # Deploy operator
    make kind-deploy
    KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl get pods -n multigres-operator-system

    # Make code changes and redeploy
    make kind-redeploy

    # Cleanup
    make kind-down
    ls kubeconfig.yaml  # Should be deleted
    ```

# Implementation History

- 2025-10-16: Restructured proposal to focus on multi-module architecture and renovate integration

# Drawbacks

1. **More verbose Makefile**: Multi-module loops add lines (mitigated: clearer behavior)
2. **Git dependency**: Version detection requires git (mitigated: fallback to default)
3. **Module versioning complexity**: Multiple version tags to manage (mitigated: typically version together)

# Alternatives

## Alternative 1: Single Module

Collapse all code into single Go module.

**Rejected**: Multi-module architecture is a deliberate design choice for:
- Independent module evolution (when needed)
- Clear API boundaries
- Testability in isolation
- Potential external consumption of API module

## Alternative 2: No Explicit Multi-Module Targets

Let developers run commands manually per module.

**Rejected**: Inconsistent developer experience, easy to miss modules, harder to review changes.

## Alternative 3: Use go.work Only

Rely entirely on Go workspace without Makefile support.

**Rejected**: While `go.work` helps with local development, it doesn't solve the need for explicit multi-module operations in testing (each module needs its own test execution context).

# Infrastructure Needed

- **Renovate bot**: GitHub app installation (already available in most orgs)
- **Git tags**: Need consistent tagging strategy documentation
- **CI/CD**: GitHub Actions (already in use)

No additional external infrastructure required.
