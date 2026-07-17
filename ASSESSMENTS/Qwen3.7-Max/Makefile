.PHONY: build run test clean lint

build:
	go build -o bin/server ./cmd/server

run: build
	./bin/server

test:
	go test -v -race ./...

test-cover:
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

clean:
	rm -rf bin/
	rm -f coverage.out coverage.html

lint:
	go vet ./...

tidy:
	go mod tidy
