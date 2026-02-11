###----------------------------------------
##   Variables
#------------------------------------------
# Multi-module paths
# Each module must be tagged with its directory path prefix for Go module resolution
# Example tags: v0.1.0 (root), api/v0.1.0, pkg/cluster-handler/v0.1.0, etc.
#
# Auto-discovered modules: finds all directories with go.mod files
# Can be overridden: MODULES="./api ./pkg/resource-handler" make test
#                or: MODULES="$(shell make changed-modules)" make test
MODULES ?= $(shell find . -name go.mod -exec dirname {} \; | sort)

# Detect changed modules compared to origin/main
# Usage: make test MODULES="$(shell make changed-modules)"
.PHONY: changed-modules
changed-modules:
	@changed_files=$$(if git rev-parse --verify origin/main >/dev/null 2>&1; then \
		git diff --name-only origin/main...HEAD 2>/dev/null; \
	else \
		git diff --name-only HEAD 2>/dev/null; \
	fi); \
	modules=$$(find . -name go.mod -exec dirname {} \;); \
	for mod in $$modules; do \
		modpath=$${mod#./}; \
		if [ "$$mod" = "." ]; then \
			echo "$$changed_files" | grep -qE '^[^/]+\.(go|mod|sum)$$|^(cmd|internal)/' && echo "$$mod"; \
		elif echo "$$changed_files" | grep -q "^$$modpath/"; then \
			echo "$$mod"; \
		fi; \
	done 2>/dev/null | sort -u | tr '\n' ' '

# Version from git tags (for root module - operator binary)
# Root module uses tags like v0.1.0 (without prefix)
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

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# CONTAINER_TOOL defines the container tool to be used for building images.
# Be aware that the target commands are only tested with Docker which is
# scaffolded by default. However, you might want to replace it to use other
# tools. (i.e. podman)
CONTAINER_TOOL ?= docker

# Kind cluster name for local development
KIND_CLUSTER ?= multigres-operator-dev

# TODO(user): To use a different vendor for e2e tests, modify the setup under 'tests/e2e'.
# The default setup assumes Kind is pre-installed and builds/loads the Manager Docker image locally.
# CertManager is installed by default; skip with:
# - CERT_MANAGER_INSTALL_SKIP=true
KIND_CLUSTER_E2E ?= multigres-operator-test-e2e

# Local kubeconfig for kind cluster (doesn't modify user's ~/.kube/config)
KIND_KUBECONFIG ?= $(shell pwd)/kubeconfig.yaml

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUBECTL ?= kubectl
KIND ?= kind
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
GOLANGCI_LINT = $(LOCALBIN)/golangci-lint

## Tool Versions
# renovate: datasource=github-releases depName=kubernetes-sigs/kustomize
KUSTOMIZE_VERSION ?= v5.6.0
# renovate: datasource=github-releases depName=kubernetes-sigs/controller-tools
CONTROLLER_TOOLS_VERSION ?= v0.18.0
# renovate: datasource=github-releases depName=golangci/golangci-lint
GOLANGCI_LINT_VERSION ?= v2.3.0

CERT_MANAGER_VERSION ?= v1.19.2

## Envtest
#ENVTEST_VERSION is the version of controller-runtime release branch to fetch the envtest setup script (i.e. release-0.20)
ENVTEST_VERSION ?= $(shell go list -m -f "{{ .Version }}" sigs.k8s.io/controller-runtime | awk -F'[v.]' '{printf "release-%d.%d", $$2, $$3}')
#ENVTEST_K8S_VERSION is the version of Kubernetes to use for setting up ENVTEST binaries (i.e. 1.31)
# NOTE: This version should match the version defined in devshell.nix
ENVTEST_K8S_VERSION ?= 1.33

###----------------------------------------
##   Comamnds
#------------------------------------------

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk command is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./api/...;./pkg/webhook/..." output:crd:artifacts:config=config/crd/bases output:rbac:artifacts:config=config/rbac output:webhook:artifacts:config=config/webhook
.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object paths="./api/...;./pkg/webhook/..."
# $(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: fmt
fmt: ## Run go fmt against code in all modules
	@for mod in $(MODULES); do \
		echo "==> Formatting $$mod..."; \
		(cd $$mod && GOWORK=off go fmt ./...) || exit 1; \
	done

.PHONY: vet
vet: ## Run go vet against code in all modules
	@for mod in $(MODULES); do \
		echo "==> Vetting $$mod..."; \
		(cd $$mod && GOWORK=off go vet ./...) || exit 1; \
	done

.PHONY: lint
lint: golangci-lint ## Run golangci-lint linter across all modules
	@for mod in $(MODULES); do \
		echo "==> Linting $$mod..."; \
		(cd $$mod && GOWORK=off $(GOLANGCI_LINT) run) || exit 1; \
	done

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint linter and perform fixes
	@for mod in $(MODULES); do \
		echo "==> Fixing lint issues in $$mod..."; \
		(cd $$mod && GOWORK=off $(GOLANGCI_LINT) run --fix) || exit 1; \
	done

.PHONY: lint-config
lint-config: golangci-lint ## Verify golangci-lint linter configuration
	$(GOLANGCI_LINT) config verify

##@ Code Quality

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

.PHONY: verify-warn
verify-warn: manifests generate ## Verify generated files are up to date (non-blocking, warns only)
	@echo "==> Verifying generated files are committed"
	@if ! git diff --exit-code config/crd api/ >/dev/null 2>&1; then \
		echo ""; \
		echo "========================================"; \
		echo "WARNING: Generated files are out of date"; \
		echo "========================================"; \
		echo ""; \
		echo "The following files have changes:"; \
		git diff --stat config/crd api/; \
		echo ""; \
		echo "This is not blocking the build, but you should run"; \
		echo "'make manifests generate' and commit the changes"; \
		echo "before merging to ensure CRDs are synchronized."; \
		echo ""; \
		echo "========================================"; \
		exit 0; \
	else \
		echo "==> Verification passed!"; \
	fi

.PHONY: pre-commit
pre-commit: modules-tidy fmt vet lint test ## Run full pre-commit checks (tidy, fmt, vet, lint, test)
	@echo "==> Pre-commit checks passed!"

##@ Multi-Module Operations

.PHONY: modules-tidy
modules-tidy: ## Run go mod tidy on all modules
	@for mod in $(MODULES); do \
		echo "==> Tidying $$mod..."; \
		(cd $$mod && GOWORK=off go mod tidy) || exit 1; \
	done

.PHONY: modules-download
modules-download: ## Download dependencies for all modules
	@for mod in $(MODULES); do \
		echo "==> Downloading dependencies for $$mod..."; \
		(cd $$mod && GOWORK=off go mod download) || exit 1; \
	done

.PHONY: modules-verify
modules-verify: ## Verify dependencies for all modules
	@for mod in $(MODULES); do \
		echo "==> Verifying $$mod..."; \
		(cd $$mod && GOWORK=off go mod verify) || exit 1; \
	done


##@ Build

.PHONY: build
build: manifests generate fmt vet ## Build manager binary with version metadata
	@echo "==> Building operator binary (version: $(VERSION))"
	go build -ldflags="$(LDFLAGS)" -o bin/multigres-operator cmd/multigres-operator/main.go

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run ./cmd/multigres-operator/main.go

# If you wish to build the manager image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
.PHONY: container
container: ## Build container image
	$(CONTAINER_TOOL) build -t ${IMG} .

.PHONY: minikube-load
minikube-load:
	minikube image load ${IMG}

.PHONY: container-push
container-push: ## Push container image
	$(CONTAINER_TOOL) push ${IMG}

# PLATFORMS defines the target platforms for the manager image be built to provide support to multiple
# architectures. (i.e. make docker-buildx IMG=myregistry/mypoperator:0.0.1). To use this option you need to:
# - be able to use docker buildx. More info: https://docs.docker.com/build/buildx/
# - have enabled BuildKit. More info: https://docs.docker.com/develop/develop-images/build_enhancements/
# - be able to push the image to your registry (i.e. if you do not set a valid value via IMG=<myregistry/image:<tag>> then the export will fail)
# To adequately provide solutions that are compatible with multiple platforms, you should consider using this option.
PLATFORMS ?= linux/arm64,linux/amd64,linux/s390x,linux/ppc64le
.PHONY: docker-buildx
docker-buildx: ## Build and push docker image for the manager for cross-platform support
	# copy existing Dockerfile and insert --platform=${BUILDPLATFORM} into Dockerfile.cross, and preserve the original Dockerfile
	sed -e '1 s/\(^FROM\)/FROM --platform=\$$\{BUILDPLATFORM\}/; t' -e ' 1,// s//FROM --platform=\$$\{BUILDPLATFORM\}/' Dockerfile > Dockerfile.cross
	- $(CONTAINER_TOOL) buildx create --name multigres-operator-builder
	$(CONTAINER_TOOL) buildx use multigres-operator-builder
	- $(CONTAINER_TOOL) buildx build --push --platform=$(PLATFORMS) --tag ${IMG} -f Dockerfile.cross .
	- $(CONTAINER_TOOL) buildx rm multigres-operator-builder
	rm Dockerfile.cross

.PHONY: build-installer
build-installer: manifests generate kustomize ## Generate a consolidated YAML with CRDs and deployment.
	mkdir -p dist
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default > dist/install.yaml

##@ Test

.PHONY: test
test: manifests generate fmt vet ## Run tests for all modules (no integration testing)
	@echo "==> Running tests across all modules"
	@for mod in $(MODULES); do \
		echo "==> Testing $$mod..."; \
		(cd $$mod && \
			KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" \
			GOWORK=off go test $$(go list ./... | grep -v /e2e) -coverprofile=cover.out) || exit 1; \
	done

.PHONY: test-integration
test-integration: manifests generate fmt vet setup-envtest ## Run integration tests for all modules
	@echo "==> Running integration tests across all modules"
	@for mod in $(MODULES); do \
		echo "==> Integration testing $$mod..."; \
		(cd $$mod && \
			KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" \
			GOWORK=off go test -tags=integration,verbose $$(go list ./... | grep -v /e2e) -coverprofile=cover.out) || exit 1; \
	done

.PHONY: test-coverage
test-coverage: manifests generate fmt vet setup-envtest ## Generate coverage report with HTML
	@mkdir -p coverage
	@echo "==> Generating coverage across all modules"
	@project_root=$$(pwd); \
	for mod in $(MODULES); do \
		modname=$$(basename $$mod); \
		[ "$$modname" = "." ] && modname="root"; \
		echo "==> Coverage for $$mod..."; \
		(cd $$mod && \
			KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" \
			GOWORK=off go test -tags=integration,verbose ./... -coverprofile=$$project_root/coverage/$$modname.out -covermode=atomic && \
			GOWORK=off go tool cover -html=$$project_root/coverage/$$modname.out -o=$$project_root/coverage/$$modname.html) || exit 1; \
		echo "Generated: coverage/$$modname.html"; \
	done
	@echo "==> Merging coverage files..."
	@echo "mode: atomic" > coverage/combined.out
	@for out in coverage/*.out; do \
		if [ "$$out" != "coverage/combined.out" ]; then \
			tail -n +2 $$out >> coverage/combined.out; \
		fi \
	done
	@echo "==> Generating combined HTML report..."
	@if [ -f go.work ]; then \
		go tool cover -html=coverage/combined.out -o=coverage/combined.html 2>/dev/null && echo "Generated: coverage/combined.html" || echo "Note: Combined HTML requires go.work (skipping, but combined.out is available)"; \
	else \
		echo "Note: Combined HTML requires go.work for multi-module coverage (skipping, but combined.out is available)"; \
	fi
	@echo "==> Calculating total coverage..."
	@go tool cover -func=coverage/combined.out 2>/dev/null | tail -1 || echo "Total coverage: see individual module reports"
	@echo ""
	@echo "Coverage reports in coverage/"
	@echo "  - Individual: coverage/<module>.html"
	@echo "  - Combined data: coverage/combined.out (for CI/codecov)"

##@ Test End-to-End

.PHONY: setup-test-e2e
setup-test-e2e: ## Set up a Kind cluster for e2e tests if it does not exist
	@command -v $(KIND) >/dev/null 2>&1 || { \
		echo "Kind is not installed. Please install Kind manually."; \
		exit 1; \
	}
	@case "$$($(KIND) get clusters)" in \
		*"$(KIND_CLUSTER_E2E)"*) \
			echo "Kind cluster '$(KIND_CLUSTER_E2E)' already exists. Skipping creation." ;; \
		*) \
			echo "Creating Kind cluster '$(KIND_CLUSTER_E2E)'..."; \
			$(KIND) create cluster --name $(KIND_CLUSTER_E2E) ;; \
	esac

.PHONY: test-e2e
test-e2e: manifests generate fmt vet setup-test-e2e ## Run the e2e tests. Expected an isolated environment using Kind.
	KIND=$(KIND) KIND_CLUSTER=$(KIND_CLUSTER_E2E) go test -tags=e2e ./test/e2e/ -v -ginkgo.v
	$(MAKE) cleanup-test-e2e

.PHONY: cleanup-test-e2e
cleanup-test-e2e: ## Tear down the Kind cluster used for e2e tests
	@$(KIND) delete cluster --name $(KIND_CLUSTER_E2E)

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) apply --server-side -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: deploy
deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default | $(KUBECTL) apply --server-side -f -

.PHONY: undeploy
undeploy: kustomize ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/default | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

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
		echo "==> Exporting kubeconfig to $(KIND_KUBECONFIG)"; \
		$(KIND) get kubeconfig --name $(KIND_CLUSTER) > $(KIND_KUBECONFIG); \
	else \
		echo "Creating kind cluster '$(KIND_CLUSTER)'..."; \
		$(KIND) create cluster --name $(KIND_CLUSTER) --kubeconfig $(KIND_KUBECONFIG); \
	fi
	@echo "==> Cluster ready. Use: export KUBECONFIG=$(KIND_KUBECONFIG)"

.PHONY: kind-load
kind-load: container ## Build and load image into kind cluster
	@echo "==> Loading image $(IMG) into kind cluster..."
	$(KIND) load docker-image $(IMG) --name $(KIND_CLUSTER)

.PHONY: kind-deploy
kind-deploy: kind-up manifests kustomize kind-load ## Deploy operator to kind cluster using webhook with self-signed certificates
	@echo "==> Installing CRDs..."
	KUBECONFIG=$(KIND_KUBECONFIG) $(KUSTOMIZE) build config/crd | KUBECONFIG=$(KIND_KUBECONFIG) $(KUBECTL) apply --server-side -f -
	@echo "==> Deploying operator..."
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	KUBECONFIG=$(KIND_KUBECONFIG) $(KUSTOMIZE) build config/default | KUBECONFIG=$(KIND_KUBECONFIG) $(KUBECTL) apply --server-side -f -
	@echo "==> Deployment complete!"
	@echo "Check status: KUBECONFIG=$(KIND_KUBECONFIG) kubectl get pods -n multigres-operator"

.PHONY: kind-deploy-certmanager
kind-deploy-certmanager: kind-up install-certmanager manifests kustomize kind-load ## Deploy operator to kind cluster using cert manager
	@echo "==> Installing CRDs..."
	KUBECONFIG=$(KIND_KUBECONFIG) $(KUSTOMIZE) build config/crd | \
		KUBECONFIG=$(KIND_KUBECONFIG) $(KUBECTL) apply --server-side -f -
	@echo "==> Deploying operator (Cert-Manager Mode)..."
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	# POINT TO THE OVERLAY:
	KUBECONFIG=$(KIND_KUBECONFIG) $(KUSTOMIZE) build config/deploy-certmanager | \
		KUBECONFIG=$(KIND_KUBECONFIG) $(KUBECTL) apply --server-side -f -
	@echo "==> Deployment complete!"
	@echo "Check status: KUBECONFIG=$(KIND_KUBECONFIG) kubectl get pods -n multigres-operator"

.PHONY: kind-deploy-no-webhook
kind-deploy-no-webhook: kind-up manifests kustomize kind-load ## Deploy controller to Kind without the webhook enabled.
	@echo "==> Installing CRDs..."
	KUBECONFIG=$(KIND_KUBECONFIG) $(KUSTOMIZE) build config/crd | KUBECONFIG=$(KIND_KUBECONFIG) $(KUBECTL) apply --server-side -f -
	@echo "==> Deploying operator..."
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	KUBECONFIG=$(KIND_KUBECONFIG) $(KUSTOMIZE) build config/no-webhook | KUBECONFIG=$(KIND_KUBECONFIG) $(KUBECTL) apply --server-side -f -
	@echo "==> Deployment complete!"
	@echo "Check status: KUBECONFIG=$(KIND_KUBECONFIG) kubectl get pods -n multigres-operator"

.PHONY: kind-deploy-observability
kind-deploy-observability: kind-up manifests kustomize kind-load ## Deploy operator with full observability stack (Prometheus, Tempo, Grafana)
	@echo "==> Installing CRDs..."
	KUBECONFIG=$(KIND_KUBECONFIG) $(KUSTOMIZE) build config/crd | \
		KUBECONFIG=$(KIND_KUBECONFIG) $(KUBECTL) apply --server-side -f -
	@echo "==> Installing Prometheus Operator CRDs (for PrometheusRules)..."
	KUBECONFIG=$(KIND_KUBECONFIG) $(KUBECTL) apply --server-side -f \
		https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/v0.80.0/example/prometheus-operator-crd/monitoring.coreos.com_prometheusrules.yaml
	@echo "==> Deploying operator with observability stack..."
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	KUBECONFIG=$(KIND_KUBECONFIG) $(KUSTOMIZE) build config/deploy-observability | \
		KUBECONFIG=$(KIND_KUBECONFIG) $(KUBECTL) apply --server-side -f -
	@echo "==> Waiting for observability stack..."
	KUBECONFIG=$(KIND_KUBECONFIG) $(KUBECTL) wait --for=condition=Available \
		deployment/observability -n multigres-operator --timeout=120s
	@echo "==> Deployment complete!"
	@echo "Run 'make kind-observability-ui' to port-forward Grafana, Prometheus, and Tempo"

.PHONY: kind-observability-ui
kind-observability-ui: kind-deploy-observability ## Deploy observability stack and port-forward Grafana (3000), Prometheus (9090), Tempo (3200)
	@echo "==> Starting port-forwards (Ctrl+C to stop)..."
	@echo "    Grafana:    http://localhost:3000"
	@echo "    Prometheus: http://localhost:9090"
	@echo "    Tempo:      http://localhost:3200"
	@KUBECONFIG=$(KIND_KUBECONFIG) $(KUBECTL) port-forward \
		svc/observability 3000:3000 9090:9090 3200:3200 \
		-n multigres-operator


.PHONY: kind-redeploy
kind-redeploy: kind-load ## Rebuild image, reload to kind, and restart pods
	@echo "==> Restarting operator pods..."
	KUBECONFIG=$(KIND_KUBECONFIG) $(KUBECTL) rollout restart deployment -n multigres-operator

.PHONY: kind-down
kind-down: ## Delete the kind cluster
	@echo "==> Deleting kind cluster '$(KIND_CLUSTER)'..."
	$(KIND) delete cluster --name $(KIND_CLUSTER)
	@rm -f $(KIND_KUBECONFIG)
	@echo "==> Cluster and kubeconfig deleted"

##@ Dependencies

.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v5,$(KUSTOMIZE_VERSION))

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen,$(CONTROLLER_TOOLS_VERSION))

.PHONY: setup-envtest
setup-envtest: envtest ## Download the binaries required for ENVTEST in the local bin directory.
	@echo "Setting up envtest binaries for Kubernetes version $(ENVTEST_K8S_VERSION)..."
	@$(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path || { \
		echo "Error: Failed to set up envtest binaries for version $(ENVTEST_K8S_VERSION)."; \
		exit 1; \
	}

.PHONY: envtest
envtest: $(ENVTEST) ## Download setup-envtest locally if necessary.
$(ENVTEST): $(LOCALBIN)
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest,$(ENVTEST_VERSION))

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/v2/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))

.PHONY: install-certmanager
install-certmanager: ## Install Cert-Manager into the cluster
	@echo "==> Installing Cert-Manager $(CERT_MANAGER_VERSION)..."
	$(KUBECTL) apply -f https://github.com/cert-manager/cert-manager/releases/download/$(CERT_MANAGER_VERSION)/cert-manager.yaml
	@echo "==> Waiting for Cert-Manager to be ready..."
	$(KUBECTL) wait --for=condition=Available deployment --all -n cert-manager --timeout=300s

# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of binary
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f "$(1)-$(3)" ] && [ "$$(readlink -- "$(1)" 2>/dev/null)" = "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -f $(1) ;\
GOBIN=$(LOCALBIN) go install $${package} ;\
mv $(1) $(1)-$(3) ;\
} ;\
ln -sf $$(realpath $(1)-$(3)) $(1)
endef


## Non kubebuilder setup
.PHONY: check-coverage
check-coverage:
	go test ./... -coverprofile=./cover.out -covermode=atomic -coverpkg=./...
	go tool cover -html=cover.out -o=cover.html
	echo now open cover.html

##@ Backward Compatibility Aliases

.PHONY: docker-build
docker-build: container ## Alias for container (backward compatibility)

.PHONY: docker-push
docker-push: container-push ## Alias for container-push (backward compatibility)
