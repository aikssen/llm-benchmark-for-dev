# Build stage. Pinned to the toolchain the module targets so builds are
# reproducible rather than following whatever "latest" happens to be.
FROM golang:1.24-alpine AS build

WORKDIR /src

# The module has no third-party dependencies, so this layer is cached unless
# the module definition itself changes.
COPY go.mod ./
RUN go mod download

COPY . .

# CGO is off so the result is a static binary that runs on a scratch image.
# -trimpath keeps build paths out of the binary; -s -w drops the symbol table.
RUN CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' -o /out/notifier ./cmd/notifier

# Runtime stage: scratch, because a static Go binary needs nothing else. No
# shell and no package manager means a far smaller attack surface.
FROM scratch

# Copied so outbound TLS would work once real providers replace the simulated
# ones.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/notifier /notifier

# Run unprivileged. The numeric form avoids needing /etc/passwd in the image.
USER 65534:65534

EXPOSE 8080
ENV SERVER_ADDR=:8080

ENTRYPOINT ["/notifier"]
