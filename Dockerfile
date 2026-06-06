# syntax=docker/dockerfile:1

# ── Stage 1: build ─────────────────────────────────────────────────────────────
FROM golang:1.25 AS builder

WORKDIR /src

# Cache module downloads separately from source
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build fully static binaries
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -trimpath -o /out/supervisor ./cmd/supervisor
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -trimpath -o /out/hunter     ./cmd/hunter

# ── Stage 2: runtime ───────────────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12:nonroot

# Copy binaries from build stage
COPY --from=builder /out/supervisor /supervisor
COPY --from=builder /out/hunter     /hunter

# Supervisor is the default entry point; override with /hunter for the hunt profile
ENTRYPOINT ["/supervisor"]
