.PHONY: run test race tidy build

run:
	go run ./cmd/server

test:
	go test ./...

race:
	go test ./... -race

tidy:
	go mod tidy

build:
	go build -o bin/server ./cmd/server
