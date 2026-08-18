BINARY  := bin/server
GO      ?= go
# Pinned so a new release cannot fail the build on an unrelated day.
LINT_VERSION := v2.12.2

.DEFAULT_GOAL := help

.PHONY: help
help: ## List targets
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

.PHONY: run
run: ## Run the server locally
	$(GO) run ./cmd/server

.PHONY: build
build: ## Build the server binary
	$(GO) build -trimpath -ldflags="-s -w" -o $(BINARY) ./cmd/server

.PHONY: test
test: ## Run tests with the race detector
	$(GO) test -race -shuffle=on ./...

.PHONY: cover
cover: ## Run tests and open the coverage report
	$(GO) test -race -coverprofile=coverage.out -covermode=atomic ./...
	$(GO) tool cover -html=coverage.out

.PHONY: lint
lint: ## Run golangci-lint
	golangci-lint run ./...

.PHONY: lint-version
lint-version: ## Show the golangci-lint version this project targets
	@echo $(LINT_VERSION)
	@golangci-lint version

.PHONY: fmt
fmt: ## Format the code
	golangci-lint fmt ./...

.PHONY: vuln
vuln: ## Check dependencies against the Go vulnerability database
	$(GO) tool govulncheck ./...

.PHONY: tidy
tidy: ## Tidy and verify modules
	$(GO) mod tidy
	$(GO) mod verify

.PHONY: check
check: tidy fmt lint test vuln ## Everything CI runs

.PHONY: clean
clean: ## Remove build and coverage output
	rm -rf bin coverage.out
