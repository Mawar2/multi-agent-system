# syntax=docker/dockerfile:1

# ── Build stage ───────────────────────────────────────────────────────────────
FROM golang:1.25 AS build

WORKDIR /src

# Cache dependency downloads separately from source compilation.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Both binaries are fully static: CGO disabled, debug symbols stripped,
# build path trimmed so the binary does not embed the host filesystem layout.
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -trimpath -o /out/supervisor ./cmd/supervisor

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -trimpath -o /out/hunter ./cmd/hunter

# ── Supervisor image ──────────────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12:nonroot AS supervisor

COPY --from=build /out/supervisor /supervisor

ENTRYPOINT ["/supervisor"]

# ── Hunter image ──────────────────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12:nonroot AS hunter

COPY --from=build /out/hunter /hunter

ENTRYPOINT ["/hunter"]
