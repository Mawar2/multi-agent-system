# Stage 1: build fully-static Go binaries with CGO disabled.
FROM golang:1.25.1-alpine AS builder

WORKDIR /build

# Cache dependency downloads before copying source.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -trimpath -o supervisor ./cmd/supervisor

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -trimpath -o hunter ./cmd/hunter

# Stage 2: minimal distroless runtime — no shell, no package manager.
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

COPY --from=builder /build/supervisor /supervisor
COPY --from=builder /build/hunter    /hunter

# Default: run the supervisor; override via compose command: field.
ENTRYPOINT ["/supervisor"]
CMD ["--config", "/app/orchestrator.yml"]
