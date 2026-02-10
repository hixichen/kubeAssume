# KubeAssume Makefile

# Image settings
IMG ?= ghcr.io/kubeassume/kubeassume
TAG ?= dev
PLATFORMS ?= linux/amd64,linux/arm64

# Go settings
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
GOBIN ?= $(shell go env GOBIN)

# Tool versions
GOLANGCI_LINT_VERSION ?= v2.8.0
HELM_VERSION ?= v3.15.0

# Build settings
LDFLAGS := -ldflags="-w -s -X main.Version=$(TAG) -X main.GitCommit=$(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)"

.PHONY: all
all: build

##@ General

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: fmt
fmt: ## Run go fmt
	go fmt ./...

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: lint
lint: ## Run golangci-lint
	golangci-lint run ./...

.PHONY: test
test: ## Run unit tests
	go test -v -race -coverprofile=coverage.out ./...

.PHONY: test-coverage
test-coverage: test ## Run tests and show coverage
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

.PHONY: generate
generate: ## Run go generate
	go generate ./...

.PHONY: verify
verify: fmt vet lint test ## Run all verification steps

##@ Build

.PHONY: build
build: build-controller build-cli ## Build all binaries

.PHONY: build-controller
build-controller: ## Build controller binary
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(LDFLAGS) -o bin/controller ./cmd/controller

.PHONY: build-cli
build-cli: ## Build CLI binary
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(LDFLAGS) -o bin/kubeassume ./cmd/cli

.PHONY: install
install: build-cli ## Install CLI to GOBIN
	cp bin/kubeassume $(GOBIN)/kubeassume

##@ Docker

.PHONY: docker-build
docker-build: ## Build Docker image
	docker build -t $(IMG):$(TAG) .

.PHONY: docker-push
docker-push: ## Push Docker image
	docker push $(IMG):$(TAG)

.PHONY: docker-buildx
docker-buildx: ## Build and push multi-platform Docker image
	docker buildx build --platform $(PLATFORMS) -t $(IMG):$(TAG) --push .

##@ Helm

.PHONY: helm-lint
helm-lint: ## Lint Helm chart
	helm lint deploy/helm/kubeassume

.PHONY: helm-template
helm-template: ## Render Helm templates
	helm template kubeassume deploy/helm/kubeassume

.PHONY: helm-package
helm-package: ## Package Helm chart
	helm package deploy/helm/kubeassume -d dist/

##@ Deployment

.PHONY: deploy
deploy: ## Deploy to cluster using Helm
	helm upgrade --install kubeassume deploy/helm/kubeassume \
		--namespace kubeassume-system \
		--create-namespace

.PHONY: undeploy
undeploy: ## Remove deployment from cluster
	helm uninstall kubeassume --namespace kubeassume-system

##@ Testing

.PHONY: test-integration
test-integration: ## Run integration tests (requires envtest)
	go test -v -tags=integration ./test/integration/...

.PHONY: test-e2e
test-e2e: ## Run e2e tests (requires kind cluster)
	go test -v -tags=e2e ./test/e2e/...

.PHONY: kind-create
kind-create: ## Create kind cluster for testing
	kind create cluster --name kubeassume-test

.PHONY: kind-delete
kind-delete: ## Delete kind cluster
	kind delete cluster --name kubeassume-test

.PHONY: kind-load
kind-load: docker-build ## Load image into kind cluster
	kind load docker-image $(IMG):$(TAG) --name kubeassume-test

##@ Tools

.PHONY: tools
tools: ## Install development tools
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

##@ Release

.PHONY: release
release: verify docker-buildx helm-package ## Build release artifacts
	@echo "Release artifacts built successfully"

.PHONY: clean
clean: ## Clean build artifacts
	rm -rf bin/
	rm -rf dist/
	rm -f coverage.out coverage.html
