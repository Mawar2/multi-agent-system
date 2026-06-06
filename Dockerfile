# ── builder ────────────────────────────────────────────────────────────────────
FROM golang:1.25.1-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ENV CGO_ENABLED=0
RUN go build -ldflags="-s -w" -trimpath -o /out/supervisor ./cmd/supervisor && \
    go build -ldflags="-s -w" -trimpath -o /out/hunter    ./cmd/hunter

# ── runtime ────────────────────────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/supervisor /supervisor
COPY --from=builder /out/hunter     /hunter

ENTRYPOINT ["/supervisor"]
