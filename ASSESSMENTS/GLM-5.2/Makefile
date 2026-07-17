SHELL := /bin/bash

BIN := bin/notification-service
PKG := ./...

.PHONY: all build run test test-race cover vet fmt tidy clean

all: build

build:
	@mkdir -p bin
	go build -o $(BIN) ./cmd/server

run: build
	./$(BIN)

test:
	go test $(PKG)

test-race:
	go test -race $(PKG)

cover:
	go test -coverprofile=coverage.out $(PKG)
	go tool cover -html=coverage.out

vet:
	go vet $(PKG)

fmt:
	gofmt -s -w .

tidy:
	go mod tidy

clean:
	rm -rf bin coverage.out
