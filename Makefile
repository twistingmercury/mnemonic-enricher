.PHONY: local build tests-unit tests-bench analyze help tests-db tests-db-agent tests-db-pattern tests-db-graph tests-e2e docs-swagger

GIT_COMMIT := $(shell git rev-parse --short=8 HEAD 2>/dev/null || echo "unknown")
GIT_TAG := $(shell git describe --tags --abbrev=0 2>/dev/null || echo "dev")
BUILD_DATE := $(shell date -u +%Y-%m-%d)
LD_FLAGS="-s -w \
	-X 'github.com/twistingmercury/mnemonic-enricher/cmd/version.version=${GIT_TAG}' \
	-X 'github.com/twistingmercury/mnemonic-enricher/cmd/version.buildDate=${BUILD_DATE}' \
	-X 'github.com/twistingmercury/mnemonic-enricher/cmd/version.commit=${GIT_COMMIT}'"

default: help

local:
	go build --ldflags ${LD_FLAGS} -o .bin/mnemonic-enricher cmd/main/main.go

build: ## Build Docker image locally using the full CI build script
	LOCAL=1 ./build/build.sh 

analyze: ## Run linters, formatters, security scanners, etc
	goimports -w .
	golangci-lint run
	govulncheck ./cmd/... ./internal/...
	gosec -quiet -exclude-dir=tests ./...

tests-db: tests-db-agent tests-db-pattern tests-db-graph ## Run all database integration tests

tests-db-agent: ## Run agent repository integration tests
	.src/internal/repository/tests/run-agent-integration-tests.sh

tests-db-pattern: ## Run pattern repository integration tests
	.src/internal/repository/tests/run-pattern-integration-tests.sh

tests-db-graph: ## Run graph repository integration tests
	.src/internal/repository/tests/run-graph-integration-tests.sh
	
tests-unit: ## Run the unit tests
	go test .src/internal/... -coverprofile=coverage.out
	go tool cover -html=coverage.out

tests-bench: ## Run benchmark tests
	go test .src/internal/... -bench=. -benchmem -run=^$

help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nAvailable targets:\n"} /^[a-zA-Z0-9_-]+:.*##/ { printf "  %-18s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)
