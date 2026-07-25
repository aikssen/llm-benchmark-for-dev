BINARY  := notifier
PKG     := ./cmd/notifier
BUILD   := build
COVER   := $(BUILD)/coverage.out

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show the available targets
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

.PHONY: run
run: ## Run the service locally (see .env.example for configuration)
	go run $(PKG)

.PHONY: build
build: ## Compile the binary into build/
	@mkdir -p $(BUILD)
	go build -trimpath -ldflags '-s -w' -o $(BUILD)/$(BINARY) $(PKG)

.PHONY: test
test: ## Run the full test suite with the race detector
	go test -race -count=1 ./...

.PHONY: test-short
test-short: ## Run the tests without the race detector (faster feedback)
	go test -count=1 ./...

.PHONY: cover
cover: ## Run the tests and report total coverage
	@mkdir -p $(BUILD)
	go test -count=1 -coverprofile=$(COVER) -coverpkg=./internal/... ./... > /dev/null
	@go tool cover -func=$(COVER) | tail -1

.PHONY: cover-html
cover-html: cover ## Open the coverage report in a browser
	go tool cover -html=$(COVER)

.PHONY: lint
lint: ## Run gofmt and go vet
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then echo "gofmt needed:"; echo "$$unformatted"; exit 1; fi
	go vet ./...

.PHONY: tidy
tidy: ## Tidy the module definition
	go mod tidy

.PHONY: demo
demo: ## Exercise a running service through its API (make run in another shell)
	./scripts/demo.sh

.PHONY: docker-build
docker-build: ## Build the container image
	docker build -t $(BINARY):latest .

.PHONY: docker-run
docker-run: docker-build ## Run the service in a container on port 8080
	docker run --rm -p 8080:8080 $(BINARY):latest

.PHONY: clean
clean: ## Remove build artefacts
	rm -rf $(BUILD)
