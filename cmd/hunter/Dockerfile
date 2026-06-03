# Dockerfile for Hunter Agent
# Phase 1: This will build and deploy the Hunter to Cloud Run

# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o hunter ./cmd/hunter

# Runtime stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy the binary from builder
COPY --from=builder /app/hunter .

# Expose port (Cloud Run requires PORT env var)
ENV PORT=8080
EXPOSE 8080

# Run the hunter
CMD ["./hunter"]
