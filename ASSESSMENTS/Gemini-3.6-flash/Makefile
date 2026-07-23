.PHONY: build run test demo docker-build docker-run clean

BINARY_NAME=bin/notification-service

build:
	@echo "Building notification service..."
	@mkdir -p bin
	go build -o $(BINARY_NAME) ./cmd/server

run: build
	@echo "Running notification service..."
	./$(BINARY_NAME)

test:
	@echo "Running tests..."
	go test -v -race -cover ./...

demo:
	@echo "Running burst demo script..."
	go run ./scripts/demo_burst.go

docker-build:
	docker build -t notification-service:latest .

docker-run:
	docker-compose up --build

clean:
	rm -rf bin
