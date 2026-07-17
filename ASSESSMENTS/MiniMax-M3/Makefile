.PHONY: help build run test test-race lint clean smoke

BINARY := bin/notification-server
PKG    := ./...

help:
	@echo "Targets:"
	@echo "  make build      - compile the server binary into $(BINARY)"
	@echo "  make run        - run the server (go run ./cmd/server)"
	@echo "  make test       - run all tests"
	@echo "  make test-race  - run all tests with -race"
	@echo "  make lint       - run go vet"
	@echo "  make smoke      - end-to-end smoke test against a running server on :8080"
	@echo "  make clean      - remove build artifacts"

build:
	@mkdir -p bin
	go build -o $(BINARY) ./cmd/server

run:
	go run ./cmd/server

test:
	go test $(PKG)

test-race:
	go test -race -count=1 $(PKG)

lint:
	go vet $(PKG)

smoke:
	bash scripts/smoke.sh

clean:
	rm -rf bin
