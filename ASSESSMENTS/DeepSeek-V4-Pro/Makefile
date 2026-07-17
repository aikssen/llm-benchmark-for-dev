.PHONY: build run test clean vet lint

build:
	go build -o bin/notify-service ./cmd/server/

run: build
	./bin/notify-service

test:
	go test -v -count=1 -timeout 30s ./...

vet:
	go vet ./...

clean:
	rm -rf bin/

dev:
	go run ./cmd/server/
