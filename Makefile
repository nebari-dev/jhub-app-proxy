.PHONY: build test clean install run dev lint fmt deps help

# Build variables
BINARY_NAME=jhub-app-proxy
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-ldflags "-X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME}"

# Build the binary
build:
	@echo "Building ${BINARY_NAME}..."
	go build ${LDFLAGS} -o ${BINARY_NAME} ./cmd/jhub-app-proxy

# Build with race detector (for development)
build-race:
	@echo "Building ${BINARY_NAME} with race detector..."
	go build -race ${LDFLAGS} -o ${BINARY_NAME} ./cmd/jhub-app-proxy

# Run tests
test:
	@echo "Running tests..."
	go test -v -race -coverprofile=coverage.out ./...

# Run tests with coverage report
test-coverage: test
	@echo "Generating coverage report..."
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -f ${BINARY_NAME}
	rm -f coverage.out coverage.html
	go clean

# Install the binary
install: build
	@echo "Installing ${BINARY_NAME}..."
	go install ${LDFLAGS} ./cmd/jhub-app-proxy

# Run with example configuration (development)
dev: build
	@echo "Running ${BINARY_NAME} in development mode..."
	./${BINARY_NAME} \
		--log-format=pretty \
		--log-level=debug \
		--log-caller \
		--help

# Format code
fmt:
	@echo "Formatting code..."
	go fmt ./...

# Run linters (requires golangci-lint)
lint:
	@echo "Running linters..."
	@which golangci-lint > /dev/null || (echo "golangci-lint not installed. Run: curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin" && exit 1)
	golangci-lint run ./...

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	go mod download
	go mod tidy

# Initialize project (first time setup)
init: deps
	@echo "Initializing project..."
	@echo "Installing development tools..."
	@which golangci-lint > /dev/null || curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(shell go env GOPATH)/bin
	@echo "Setup complete!"

# Show help
help:
	@echo "Available targets:"
	@echo "  build           - Build the binary"
	@echo "  build-race      - Build with race detector"
	@echo "  test            - Run tests"
	@echo "  test-coverage   - Run tests with coverage report"
	@echo "  clean           - Clean build artifacts"
	@echo "  install         - Install the binary"
	@echo "  dev             - Run in development mode"
	@echo "  fmt             - Format code"
	@echo "  lint            - Run linters"
	@echo "  deps            - Download dependencies"
	@echo "  init            - Initialize project (first time)"
	@echo "  help            - Show this help message"
