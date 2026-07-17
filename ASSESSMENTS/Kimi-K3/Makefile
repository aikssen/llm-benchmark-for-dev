.PHONY: build run test vet fmt clean

build:
	go build -o bin/server ./cmd/server

run: build
	./bin/server

test:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

clean:
	rm -rf bin
