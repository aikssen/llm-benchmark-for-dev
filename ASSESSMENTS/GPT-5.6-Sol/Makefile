.PHONY: run test test-race vet fmt build

run:
	go run ./cmd/server

test:
	go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -w cmd internal

build:
	go build -o bin/notification-service ./cmd/server

