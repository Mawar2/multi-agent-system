# syntax=docker/dockerfile:1

# ── Build stage ────────────────────────────────────────────────────────────────
FROM golang:1.25-bookworm AS builder

WORKDIR /src

# Cache module downloads separately from source
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build both binaries as fully static executables (no CGO, no libc dependency)
RUN CGO_ENABLED=0 go build \
      -ldflags="-s -w" \
      -trimpath \
      -o /bin/supervisor \
      ./cmd/supervisor

RUN CGO_ENABLED=0 go build \
      -ldflags="-s -w" \
      -trimpath \
      -o /bin/hunter \
      ./cmd/hunter

# ── Runtime stage ──────────────────────────────────────────────────────────────
# gcr.io/distroless/static-debian12:nonroot contains no shell, no package manager,
# only ca-certificates and tzdata — minimal attack surface for a static binary.
FROM gcr.io/distroless/static-debian12:nonroot

# Copy binaries from builder
COPY --from=builder /bin/supervisor /supervisor
COPY --from=builder /bin/hunter /hunter

# Default to supervisor; override with /hunter for the hunt profile
ENTRYPOINT ["/supervisor"]
