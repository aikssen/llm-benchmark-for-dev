FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /notification-service ./cmd/server

FROM scratch
COPY --from=build /notification-service /notification-service
EXPOSE 8080
USER 65532:65532
ENTRYPOINT ["/notification-service"]

