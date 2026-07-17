.PHONY: run build test check fmt vet clean

run: ## Run the service locally
	go run ./cmd/server

build: ## Build the server binary into ./bin
	go build -o bin/server ./cmd/server

test: ## Run all tests with the race detector
	go test -race ./...

fmt: ## Check formatting
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "not gofmt'ed:"; echo "$$out"; exit 1; fi

vet: ## Run go vet
	go vet ./...

check: fmt vet test ## Formatting + vet + tests

clean:
	rm -rf bin
