# syntax=docker/dockerfile:1

# Stage 1: Build
FROM golang:1.25-alpine AS builder

WORKDIR /build

# Download dependencies first (layer cache)
COPY go.mod go.sum ./
RUN go mod download

# Build all binaries as fully static (no libc dependency)
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -trimpath -o supervisor ./cmd/supervisor && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -trimpath -o hunter ./cmd/hunter

# Stage 2: Runtime — distroless for minimal attack surface
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

# Copy binaries and example configs
COPY --from=builder /build/supervisor /app/supervisor
COPY --from=builder /build/hunter /app/hunter
COPY orchestrator.example.yml /app/orchestrator.example.yml
COPY hunter.example.yml /app/hunter.example.yml

# Runtime directories (tasks queue + cloned workspaces)
VOLUME ["/app/tasks", "/app/projects"]

# Health check: verify the binary is executable
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/app/supervisor", "--help"]

EXPOSE 8080

ENTRYPOINT ["/app/supervisor"]
CMD ["--config", "/app/orchestrator.yml"]
