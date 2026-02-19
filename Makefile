.PHONY: help init deps-update build test tidy static lint lint-update vuln-check modernize outdated fmt vet check run-server docker-build deps install-protoc proto
.DEFAULT_GOAL := help

# Variables
BINARY_NAME := gradebot
BUILD_DIR := bin

## help: Show this help message
help:
	@echo "Available targets:"
	@sed -n 's/^##//p' $(MAKEFILE_LIST) | column -t -s ':' | sed -e 's/^/ /'

## init: Initialize complete development environment (protoc, git hooks)
init: install-protoc
	@echo "Initializing development environment..."
	@bash .githooks/install-hooks.sh
	@go install golang.org/x/tools/cmd/goimports@latest
	@echo "Development environment initialized ✓"

deps-update: lint-update
	@echo "Updating Go modules to latest versions..."
	@go get -u -t ./...
	@go mod tidy
	@echo "Go modules updated ✓"

build:
	@echo "Building $(BINARY_NAME) server"
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY_NAME)-server ./cmd/server
	@echo "Build completed successfully: $(BUILD_DIR)/$(BINARY_NAME)-server"

## test: Run all tests with coverage
test:
	@echo "Running tests..."
	@go test -timeout 30s -shuffle=on -race -cover -coverprofile=coverage.out ./...

tidy:
	@echo "Tidying Go modules..."
	@go mod tidy
	@echo "Go modules tidied ✓"

## static: Run all linting tools
static: tidy vet lint modernize vuln-check outdated
	@echo "All linting completed ✓"

## lint: Run golangci-lint with auto-fix enabled
lint:
	@echo "Running $$(go tool -modfile=golangci-lint.mod golangci-lint version)..."
	@go tool -modfile=golangci-lint.mod golangci-lint run --fix ./...

## lint-update: Update golangci-lint to latest version
lint-update:
	@echo "Updating golangci-lint..."
	@go get -tool -modfile=golangci-lint.mod github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
	@go mod tidy -modfile=golangci-lint.mod
	@echo "Updated $$(go tool -modfile=golangci-lint.mod golangci-lint version)"

vuln-check:
	@echo "Checking for vulnerabilities..."
	@go run golang.org/x/vuln/cmd/govulncheck@latest ./...

## modernize: Check for outdated Go patterns and suggest improvements
modernize:
	@echo "Running modernize analysis..."
	@go run golang.org/x/tools/gopls/internal/analysis/modernize/cmd/modernize@latest -fix -test ./...
	
outdated:
	@echo "Checking for outdated direct dependencies..."
	@go list -u -m -f '{{if not .Indirect}}{{.}}{{end}}' all 2>/dev/null | grep '\[' || echo "All direct dependencies are up to date"

## fmt: Format code
fmt:
	@echo "Formatting code..."
	@go fmt ./...
	@go run golang.org/x/tools/cmd/goimports@latest -w .

## vet: Run go vet
vet:
	@echo "Running go vet..."
	@go vet ./...

## check: Run all checks (format, vet, lint, test)
check: tidy fmt static test
	@echo "All checks completed ✓"

## run-server: Run gradebot in server mode
run-server:
	@echo "Starting gradebot server..."
	@go run ./cmd/server

## docker-build: Build Docker image (if Dockerfile exists)
docker-build:
	@if [ -f Dockerfile ]; then \
		echo "Building Docker image..."; \
		docker build -t $(BINARY_NAME):latest .; \
	else \
		echo "No Dockerfile found"; \
	fi

## install-protoc: Install latest protoc compiler and Go plugins
install-protoc:
	@echo "Installing latest protoc..."
	@ARCH=$$(uname -m); \
	if [ "$$ARCH" = "x86_64" ]; then \
		PROTOC_ARCH=x86_64; \
	elif [ "$$ARCH" = "aarch64" ]; then \
		PROTOC_ARCH=aarch_64; \
	else \
		echo "Unsupported architecture: $$ARCH"; exit 1; \
	fi; \
	LATEST_VERSION=$$(curl -s https://api.github.com/repos/protocolbuffers/protobuf/releases/latest | grep '"tag_name"' | sed -E 's/.*"v([^"]+)".*/\1/'); \
	echo "Latest protoc version: $$LATEST_VERSION"; \
	PROTOC_ZIP=protoc-$$LATEST_VERSION-linux-$$PROTOC_ARCH.zip; \
	cd /tmp && \
	wget -q https://github.com/protocolbuffers/protobuf/releases/download/v$$LATEST_VERSION/$$PROTOC_ZIP && \
	unzip -q -o $$PROTOC_ZIP -d /tmp/protoc && \
	sudo cp -f /tmp/protoc/bin/protoc /usr/local/bin/ && \
	sudo cp -r /tmp/protoc/include/* /usr/local/include/ && \
	rm -rf /tmp/protoc /tmp/$$PROTOC_ZIP
	@echo "protoc installed successfully"
	@echo "Installing protoc Go plugins..."
	@go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	@go install connectrpc.com/connect/cmd/protoc-gen-connect-go@latest
	@echo "protoc toolchain installation complete ✓"

## proto: Regenerate protobuf Go files (installs protoc if needed)
proto:
	@echo "Generating protobuf Go files..."
	@which protoc >/dev/null 2>&1 || $(MAKE) install-protoc
	@protoc --version
	@protoc --go_out=. --go_opt=paths=source_relative \
		--connect-go_out=. --connect-go_opt=paths=source_relative \
		pkg/proto/quality.proto
	@protoc --go_out=. --go_opt=paths=source_relative \
		--connect-go_out=. --connect-go_opt=paths=source_relative \
		pkg/proto/rubric.proto
	@echo "Protobuf generation completed."
