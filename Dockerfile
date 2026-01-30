# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Copy go mod files first for caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /discovery ./cmd/discovery

# Test stage
FROM builder AS test
RUN go test -v ./...

# Production stage
FROM alpine:3.21

WORKDIR /app

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Copy binary from builder
COPY --from=builder /discovery /app/discovery

# Create non-root user
RUN addgroup -g 1000 discovery && \
    adduser -u 1000 -G discovery -s /bin/sh -D discovery

USER discovery

EXPOSE 8080

ENTRYPOINT ["/app/discovery"]
