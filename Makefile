.PHONY: build test run clean docker docker-run lint fmt

# Variables
BINARY_NAME=discovery
TESTCLIENT_NAME=testclient
MAIN_PATH=./cmd/discovery
TESTCLIENT_PATH=./cmd/testclient

# Build the binary
build:
	go build -o $(BINARY_NAME) $(MAIN_PATH)

# Build the test client
build-testclient:
	go build -o $(TESTCLIENT_NAME) $(TESTCLIENT_PATH)

# Build all binaries
build-all: build build-testclient

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
	rm -f $(BINARY_NAME) $(TESTCLIENT_NAME)
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

# View all logs
logs-all:
	docker compose logs -f

# Stop services
stop:
	docker compose down

# ==========================================
# Integration Testing Commands
# ==========================================

# Generate a test keypair
test-keygen:
	@go run $(TESTCLIENT_PATH) -cmd keygen

# Start the test environment (local relay + discovery service)
test-env-start:
	@echo "Generating test keypair..."
	@if [ -z "$$NOSTR_PRIVATE_KEY" ]; then \
		export NOSTR_PRIVATE_KEY=$$(openssl rand -hex 32); \
		echo "Generated NOSTR_PRIVATE_KEY=$$NOSTR_PRIVATE_KEY"; \
		echo "export NOSTR_PRIVATE_KEY=$$NOSTR_PRIVATE_KEY" > .env.test; \
	fi
	@echo "Starting test environment..."
	docker compose up -d
	@echo "Waiting for services to start..."
	@sleep 5
	@echo ""
	@echo "Test environment ready!"
	@echo "  Local relay:      ws://localhost:17080"
	@echo "  Discovery API:    http://localhost:18080"
	@echo "  Admin dashboard:  http://localhost:18080/admin/dashboard"
	@echo "  Dragonfly:        localhost:16379"
	@echo ""
	@echo "To run tests, source the env file first:"
	@echo "  source .env.test"

# Stop the test environment
test-env-stop:
	docker compose down

# Query the discovery service HTTP API
test-http:
	@go run $(TESTCLIENT_PATH) -cmd query-http -api http://localhost:18080

# Listen for NDP events on local relay
test-listen:
	@go run $(TESTCLIENT_PATH) -cmd listen -relay ws://localhost:17080

# Publish a test inventory event
test-publish-inventory:
	@go run $(TESTCLIENT_PATH) -cmd publish-inventory -relay ws://localhost:17080 -inventory-relay wss://test.example.com

# Publish a test activity event
test-publish-activity:
	@go run $(TESTCLIENT_PATH) -cmd publish-activity -relay ws://localhost:17080 -activity streaming

# Send a discovery query (find relays)
test-query-relays:
	@go run $(TESTCLIENT_PATH) -cmd query -relay ws://localhost:17080 -query-type find_relays -health online

# Send a discovery query (pubkey location)
test-query-pubkey:
	@go run $(TESTCLIENT_PATH) -cmd query -relay ws://localhost:17080 -query-type pubkey_location -pubkey $(PUBKEY)

# Run full integration test suite
test-integration: build-testclient
	@echo "=== NDP Integration Test Suite ==="
	@echo ""
	@echo "1. Checking services..."
	@curl -s http://localhost:18080/health > /dev/null && echo "   Discovery service: OK" || echo "   Discovery service: FAILED"
	@curl -s http://localhost:17080 > /dev/null 2>&1 && echo "   Local relay: OK" || echo "   Local relay: FAILED"
	@echo ""
	@echo "2. Testing HTTP API..."
	@./$(TESTCLIENT_NAME) -cmd query-http -api http://localhost:18080
	@echo ""
	@echo "3. Publishing test events..."
	@if [ -n "$$NOSTR_PRIVATE_KEY" ]; then \
		./$(TESTCLIENT_NAME) -cmd publish-inventory -relay ws://localhost:17080 -inventory-relay wss://test.example.com; \
		echo ""; \
		./$(TESTCLIENT_NAME) -cmd publish-activity -relay ws://localhost:17080 -activity online; \
	else \
		echo "   Skipping - NOSTR_PRIVATE_KEY not set"; \
	fi
	@echo ""
	@echo "=== Integration tests complete ==="

# Test against cloistr relay (public, be careful!)
test-cloistr:
	@echo "Testing against wss://relay.cloistr.xyz"
	@go run $(TESTCLIENT_PATH) -cmd listen -relay wss://relay.cloistr.xyz -timeout 10s

# Show help for testing
test-help:
	@echo "NDP Integration Testing Commands"
	@echo "================================="
	@echo ""
	@echo "Setup:"
	@echo "  make test-keygen        Generate a test keypair"
	@echo "  make test-env-start     Start local relay + discovery service"
	@echo "  make test-env-stop      Stop test environment"
	@echo ""
	@echo "Testing:"
	@echo "  make test-http          Query discovery service HTTP API"
	@echo "  make test-listen        Listen for NDP events on local relay"
	@echo "  make test-integration   Run full integration test suite"
	@echo ""
	@echo "Publishing (requires NOSTR_PRIVATE_KEY):"
	@echo "  make test-publish-inventory   Publish kind 30066 inventory"
	@echo "  make test-publish-activity    Publish kind 30067 activity"
	@echo "  make test-query-relays        Send kind 30068 find_relays query"
	@echo "  make test-query-pubkey        Send kind 30068 pubkey_location query"
	@echo ""
	@echo "External:"
	@echo "  make test-cloistr       Test against relay.cloistr.xyz"
