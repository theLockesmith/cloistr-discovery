.PHONY: build test run clean docker docker-run lint fmt

# Variables
BINARY_NAME=discovery
MAIN_PATH=./cmd/discovery

# Build the binary
build:
	go build -o $(BINARY_NAME) $(MAIN_PATH)

# Run tests
test:
	go test -v ./...

# Run tests with coverage
test-coverage:
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Run tests with race detection
test-race:
	go test -v -race ./...

# Run the service locally
run:
	go run $(MAIN_PATH)

# Run with docker-compose
docker-run:
	docker compose up -d

# Build docker image
docker:
	docker build -t coldforge-discovery:latest .

# Run docker tests
docker-test:
	docker build --target test -t coldforge-discovery:test .
	docker run --rm coldforge-discovery:test

# Clean build artifacts
clean:
	rm -f $(BINARY_NAME)
	rm -f coverage.out coverage.html
	docker compose down -v 2>/dev/null || true

# Lint the code
lint:
	golangci-lint run

# Format the code
fmt:
	go fmt ./...
	goimports -w .

# Download dependencies
deps:
	go mod download
	go mod tidy

# View logs
logs:
	docker compose logs -f discovery

# Stop services
stop:
	docker compose down
