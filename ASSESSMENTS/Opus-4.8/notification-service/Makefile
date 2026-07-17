# Notification service — developer tasks.
# All targets assume a working Go toolchain (>= 1.26) on the PATH.

BINARY := notification-service
PKG    := ./...

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

.PHONY: run
run: ## Run the server locally (defaults on :8080)
	go run ./cmd/server

.PHONY: build
build: ## Compile the server binary into ./bin
	go build -o bin/$(BINARY) ./cmd/server

.PHONY: test
test: ## Run all tests
	go test $(PKG)

.PHONY: race
race: ## Run all tests with the race detector
	go test -race $(PKG)

.PHONY: cover
cover: ## Run tests and print total coverage
	go test -cover $(PKG)

.PHONY: coverhtml
coverhtml: ## Generate an HTML coverage report at coverage.html
	go test -coverprofile=coverage.out $(PKG)
	go tool cover -html=coverage.out -o coverage.html
	@echo "open coverage.html"

.PHONY: vet
vet: ## Run go vet
	go vet $(PKG)

.PHONY: fmt
fmt: ## Format all Go source
	gofmt -w .

.PHONY: tidy
tidy: ## Tidy module dependencies
	go mod tidy

.PHONY: check
check: fmt vet race ## Format, vet and run tests with race (pre-commit gate)

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf bin coverage.out coverage.html
