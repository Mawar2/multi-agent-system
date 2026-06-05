# Makefile for Multi-Agent Orchestration System

.PHONY: all build test lint clean run help

# Default target
all: lint test build

# Build the supervisor binary
build:
	@echo "Building supervisor..."
	@mkdir -p bin
	@go build -o bin/supervisor ./cmd/supervisor
	@echo "Built: bin/supervisor"

# Run all tests
test:
	@echo "Running tests..."
	@go test -v -race -cover ./...

# Run linter
lint:
	@echo "Running linter..."
	@golangci-lint run || (echo "golangci-lint not installed. Run: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest" && exit 1)

# Format code
fmt:
	@echo "Formatting code..."
	@gofmt -w .

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf bin/
	@rm -rf tasks/
	@rm -rf projects/
	@echo "Cleaned"

# Run supervisor with example config
run: build
	@echo "Starting supervisor..."
	@./bin/supervisor --config orchestrator.example.yml

# Show help
help:
	@echo "Multi-Agent Orchestration System - Makefile"
	@echo ""
	@echo "Targets:"
	@echo "  all      - Run lint, test, and build (default)"
	@echo "  build    - Build supervisor binary to bin/"
	@echo "  test     - Run all tests with race detection"
	@echo "  lint     - Run golangci-lint"
	@echo "  fmt      - Format code with gofmt"
	@echo "  clean    - Remove build artifacts"
	@echo "  run      - Build and run supervisor with example config"
	@echo "  help     - Show this help message"
