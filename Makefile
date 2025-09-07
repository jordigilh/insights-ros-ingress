# Insights ROS Ingress Makefile
# Following CLAUDE.md guidelines for using podman instead of docker

# Variables
APP_NAME := insights-ros-ingress
VERSION ?= latest
IMAGE_NAME := $(APP_NAME):$(VERSION)
REGISTRY ?= quay.io/redhat-insights

# Go variables
GO_VERSION := 1.21
GOOS ?= linux
GOARCH ?= amd64
CGO_ENABLED ?= 1

# Build directories
BUILD_DIR := build
BIN_DIR := $(BUILD_DIR)/bin

# Go flags
LDFLAGS := -w -s -X main.version=$(VERSION)
GO_BUILD_FLAGS := -ldflags "$(LDFLAGS)"

.PHONY: help
help: ## Display this help message
	@echo "Available targets:"
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.PHONY: clean
clean: clean-mocks ## Clean build artifacts
	@echo "Cleaning build artifacts..."
	rm -rf $(BUILD_DIR)
	podman rmi $(IMAGE_NAME) 2>/dev/null || true

.PHONY: deps
deps: ## Download and verify dependencies
	@echo "Downloading dependencies..."
	go mod download
	go mod verify
	go mod tidy

.PHONY: fmt
fmt: ## Format Go code
	@echo "Formatting Go code..."
	go fmt ./...
	goimports -w .

.PHONY: lint
lint: ## Run linters
	@echo "Running linters..."
	golangci-lint run ./...

.PHONY: vet
vet: ## Run go vet
	@echo "Running go vet..."
	go vet ./...

.PHONY: test
test: mocks ## Run tests
	@echo "Running tests..."
	go test -v -race -coverprofile=coverage.out ./...

.PHONY: test-coverage
test-coverage: test ## Run tests and generate coverage report
	@echo "Generating coverage report..."
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

.PHONY: mocks
mocks: install-tools ## Generate mocks for testing
	@echo "Ensuring mocks exist..."
	@mkdir -p internal/auth/mocks
	@if [ ! -f internal/auth/mocks/mock_auth_client.go ]; then \
		echo "Generating mocks with go generate..."; \
		cd internal/auth/mocks/ && \
		GOMOD=$$(pwd)/../../../go.mod go generate -mod=mod ./generate.go  || \
		echo "Mocks already present or generation failed - using existing mocks"; \
	fi
	@echo "Mocks ready in internal/auth/mocks/"

.PHONY: clean-mocks
clean-mocks: ## Remove generated mocks
	@echo "Cleaning generated mocks..."
	rm -f internal/auth/mocks/mock_auth_client.go
	@echo "Mocks cleaned"

.PHONY: build
build: clean deps ## Build the binary
	@echo "Building $(APP_NAME)..."
	mkdir -p $(BIN_DIR)
ifeq ($(CGO_ENABLED),1)
	@echo "Building with CGO enabled for current platform..."
	CGO_ENABLED=$(CGO_ENABLED) go build \
		$(GO_BUILD_FLAGS) \
		-o $(BIN_DIR)/$(APP_NAME) \
		./cmd/$(APP_NAME)
else
	@echo "Building with CGO disabled for $(GOOS)/$(GOARCH)..."
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) go build \
		$(GO_BUILD_FLAGS) \
		-o $(BIN_DIR)/$(APP_NAME) \
		./cmd/$(APP_NAME)
endif
	@echo "Binary built: $(BIN_DIR)/$(APP_NAME)"

.PHONY: image
image: ## Build container image using podman
	@echo "Building container image with podman..."
	podman build -t $(IMAGE_NAME) .
	@echo "Image built: $(IMAGE_NAME)"

.PHONY: image-push
image-push: image ## Push container image to registry
	@echo "Pushing image to registry..."
	podman tag $(IMAGE_NAME) $(REGISTRY)/$(IMAGE_NAME)
	podman push $(REGISTRY)/$(IMAGE_NAME)

.PHONY: run
run: build ## Run the application locally
	@echo "Running $(APP_NAME)..."
	./$(BIN_DIR)/$(APP_NAME)

.PHONY: dev-env-up
dev-env-up: ## Start development environment with podman-compose
	@echo "Starting development environment..."
	podman-compose -f scripts/docker-compose.yml up -d

.PHONY: dev-env-down
dev-env-down: ## Stop development environment
	@echo "Stopping development environment..."
	podman-compose -f scripts/docker-compose.yml down

.PHONY: dev-env-logs
dev-env-logs: ## Show development environment logs
	podman-compose -f scripts/docker-compose.yml logs -f

.PHONY: install-tools
install-tools: ## Install required development tools
	@echo "Installing development tools..."
	@command -v golangci-lint >/dev/null 2>&1 || { \
		echo "Installing golangci-lint..."; \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin v1.54.2; \
	}
	@command -v goimports >/dev/null 2>&1 || { \
		echo "Installing goimports..."; \
		go install golang.org/x/tools/cmd/goimports@latest; \
	}
	@command -v mockgen >/dev/null 2>&1 || { \
		echo "Installing mockgen..."; \
		go install github.com/golang/mock/mockgen@latest; \
	}

.PHONY: helm-lint
helm-lint: ## Lint Helm chart
	@echo "Linting Helm chart..."
	helm lint deployments/helm/$(APP_NAME)

.PHONY: helm-template
helm-template: ## Generate Helm templates
	@echo "Generating Helm templates..."
	helm template $(APP_NAME) deployments/helm/$(APP_NAME) --values deployments/helm/$(APP_NAME)/values.yaml

.PHONY: helm-package
helm-package: helm-lint ## Package Helm chart
	@echo "Packaging Helm chart..."
	helm package deployments/helm/$(APP_NAME) -d $(BUILD_DIR)

.PHONY: security-scan
security-scan: ## Run security scan on container image
	@echo "Running security scan..."
	podman run --rm -v /var/run/docker.sock:/var/run/docker.sock \
		-v $$(pwd):/root/.cache/ \
		aquasec/trivy:latest image $(IMAGE_NAME)

.PHONY: check
check: fmt vet lint test ## Run all checks (format, vet, lint, test)

.PHONY: ci
ci: install-tools check build image helm-lint ## Run CI pipeline

.PHONY: all
all: check build image helm-package ## Build everything

# Development helpers
.PHONY: watch
watch: ## Watch for changes and rebuild
	@echo "Watching for changes..."
	@command -v entr >/dev/null 2>&1 || { \
		echo "entr is required for watch. Install with: brew install entr"; \
		exit 1; \
	}
	find . -name "*.go" | entr -r make build run

.PHONY: debug
debug: ## Build and run with debugging
	@echo "Building debug version..."
	CGO_ENABLED=1 go build -gcflags="all=-N -l" -o $(BIN_DIR)/$(APP_NAME)-debug ./cmd/$(APP_NAME)
	dlv exec $(BIN_DIR)/$(APP_NAME)-debug

# OpenShift deployment helpers
.PHONY: oc-deploy
oc-deploy: image helm-package ## Deploy to OpenShift
	@echo "Deploying to OpenShift..."
	oc project insights-ros || oc new-project insights-ros
	helm upgrade --install $(APP_NAME) deployments/helm/$(APP_NAME) \
		--set image.repository=$(REGISTRY)/$(APP_NAME) \
		--set image.tag=$(VERSION)

.PHONY: oc-undeploy
oc-undeploy: ## Remove from OpenShift
	@echo "Removing from OpenShift..."
	helm uninstall $(APP_NAME) || true

# Default target
.DEFAULT_GOAL := help