.PHONY: build run test clean

APP?=./cmd/server
BIN?=./bin/server

build:
	@mkdir -p bin
	go build -o $(BIN) $(APP)

run: build
	$(BIN)

test:
	go test -race ./...

clean:
	rm -rf bin
