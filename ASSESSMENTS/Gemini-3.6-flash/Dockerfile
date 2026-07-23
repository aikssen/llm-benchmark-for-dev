# Build Stage
FROM golang:1.22-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o notification-service ./cmd/server

# Final Runtime Stage
FROM alpine:3.19

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

COPY --from=builder /app/notification-service /app/notification-service

EXPOSE 8080

ENTRYPOINT ["/app/notification-service"]
