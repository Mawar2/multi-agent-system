# syntax=docker/dockerfile:1

# ── Build stage ──────────────────────────────────────────────────────────────
FROM golang:1.25-alpine AS builder

WORKDIR /build

# Cache dependencies before copying source.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
      -ldflags="-s -w" -trimpath \
      -o /bin/supervisor \
      ./cmd/supervisor

RUN CGO_ENABLED=0 GOOS=linux go build \
      -ldflags="-s -w" -trimpath \
      -o /bin/hunter \
      ./cmd/hunter

# ── Runtime stage ─────────────────────────────────────────────────────────────
# distroless/static has no shell, no libc, no package manager — minimal attack surface.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /bin/supervisor /supervisor
COPY --from=builder /bin/hunter    /hunter

ENTRYPOINT ["/supervisor"]
