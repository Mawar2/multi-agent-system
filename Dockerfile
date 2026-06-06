# Build stage — compiles both binaries as fully static Go binaries (CGO disabled).
FROM golang:1.25 AS builder

WORKDIR /build

# Cache module downloads before copying source.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" \
    -o /out/supervisor ./cmd/supervisor

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" \
    -o /out/hunter ./cmd/hunter

# Runtime stage — distroless keeps the image under 50 MB and eliminates a shell
# attack surface. The nonroot variant runs as uid 65532 by default.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/supervisor /supervisor
COPY --from=builder /out/hunter /hunter

ENTRYPOINT ["/supervisor"]
CMD ["--config", "/orchestrator.yml"]
