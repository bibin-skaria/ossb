# OSSB Makefile

.PHONY: build test clean install lint fmt vet deps build-all help

# Build variables
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null || echo "unknown")
BUILD_DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

# Go variables
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
BINARY_NAME = ossb
BINARY_DIR = bin
MAIN_PATH = ./cmd

# Linker flags
LDFLAGS = -ldflags "-X main.Version=$(VERSION) -X main.GitCommit=$(GIT_COMMIT) -X main.BuildDate=$(BUILD_DATE) -s -w"

# Default target
help: ## Show this help message
	@echo "OSSB - Open Source Slim Builder"
	@echo ""
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

build: deps ## Build the binary for current platform
	@echo "Building $(BINARY_NAME) for $(GOOS)/$(GOARCH)..."
	@mkdir -p $(BINARY_DIR)
	CGO_ENABLED=0 go build $(LDFLAGS) -o $(BINARY_DIR)/$(BINARY_NAME) $(MAIN_PATH)
	@echo "Built: $(BINARY_DIR)/$(BINARY_NAME)"

build-all: deps ## Build binaries for all platforms
	@echo "Building binaries for all platforms..."
	@mkdir -p $(BINARY_DIR)
	
	@echo "Building for linux/amd64..."
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build $(LDFLAGS) -o $(BINARY_DIR)/$(BINARY_NAME)-linux-amd64 $(MAIN_PATH)
	
	@echo "Building for linux/arm64..."
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build $(LDFLAGS) -o $(BINARY_DIR)/$(BINARY_NAME)-linux-arm64 $(MAIN_PATH)
	
	@echo "Building for darwin/amd64..."
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build $(LDFLAGS) -o $(BINARY_DIR)/$(BINARY_NAME)-darwin-amd64 $(MAIN_PATH)
	
	@echo "Building for darwin/arm64..."
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build $(LDFLAGS) -o $(BINARY_DIR)/$(BINARY_NAME)-darwin-arm64 $(MAIN_PATH)
	
	@echo "Building for windows/amd64..."
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build $(LDFLAGS) -o $(BINARY_DIR)/$(BINARY_NAME)-windows-amd64.exe $(MAIN_PATH)
	
	@echo "All binaries built successfully!"
	@ls -la $(BINARY_DIR)/

test: ## Run tests
	@echo "Running tests..."
	go test -v -race -coverprofile=coverage.out ./...
	@echo "Tests completed."

test-coverage: test ## Run tests with coverage report
	@echo "Generating coverage report..."
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

clean: ## Clean build artifacts
	@echo "Cleaning build artifacts..."
	rm -rf $(BINARY_DIR)
	rm -f coverage.out coverage.html
	go clean -cache -testcache -modcache
	@echo "Clean completed."

install: build ## Install binary to $GOPATH/bin
	@echo "Installing $(BINARY_NAME) to $(shell go env GOPATH)/bin..."
	cp $(BINARY_DIR)/$(BINARY_NAME) $(shell go env GOPATH)/bin/
	@echo "Installation completed."

deps: ## Download and verify dependencies
	@echo "Downloading dependencies..."
	go mod download
	go mod verify
	go mod tidy
	@echo "Dependencies updated."

lint: ## Run linting
	@echo "Running linters..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not found, running basic checks..."; \
		$(MAKE) vet; \
		$(MAKE) fmt-check; \
	fi

fmt: ## Format code
	@echo "Formatting code..."
	go fmt ./...
	@echo "Code formatted."

fmt-check: ## Check code formatting
	@echo "Checking code formatting..."
	@unformatted=$$(go fmt ./...); \
	if [ -n "$$unformatted" ]; then \
		echo "The following files are not formatted:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi
	@echo "All files are properly formatted."

vet: ## Run go vet
	@echo "Running go vet..."
	go vet ./...
	@echo "go vet completed."

docker-build: ## Build Docker image
	@echo "Building Docker image..."
	docker build -t ossb:$(VERSION) .
	@echo "Docker image built: ossb:$(VERSION)"

docker-build-dev: ## Build development Docker image
	@echo "Building development Docker image..."
	docker build -f Dockerfile --target development -t ossb:dev .
	@echo "Development Docker image built: ossb:dev"

release: clean deps test lint build-all ## Prepare release (clean, test, lint, build all platforms)
	@echo "Release preparation completed!"
	@echo "Version: $(VERSION)"
	@echo "Commit: $(GIT_COMMIT)"
	@echo "Build Date: $(BUILD_DATE)"
	@ls -la $(BINARY_DIR)/

dev-setup: ## Set up development environment
	@echo "Setting up development environment..."
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install gotest.tools/gotestsum@latest
	@echo "Development environment setup completed."

version: ## Show version information
	@echo "Version: $(VERSION)"
	@echo "Git Commit: $(GIT_COMMIT)"
	@echo "Build Date: $(BUILD_DATE)"
	@echo "Go Version: $(shell go version)"
	@echo "Platform: $(GOOS)/$(GOARCH)"

# Demo and examples
demo: build ## Run a simple demo build
	@echo "Running demo build..."
	@mkdir -p demo
	@echo 'FROM alpine:latest\nRUN echo "Hello from OSSB!"' > demo/Dockerfile
	./$(BINARY_DIR)/$(BINARY_NAME) build demo -t demo:latest
	@echo "Demo completed. Check the output above."
	@rm -rf demo

run-example: build ## Build an example Dockerfile
	@if [ -f example/Dockerfile ]; then \
		echo "Building example..."; \
		./$(BINARY_DIR)/$(BINARY_NAME) build example -t example:latest; \
	else \
		echo "No example/Dockerfile found. Create one to test."; \
	fi