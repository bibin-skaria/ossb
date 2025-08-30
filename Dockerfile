# Multi-stage Dockerfile for OSSB (Open Source Slim Builder)

# Development stage - includes all build tools and source code
FROM golang:1.21-alpine AS development

# Install build dependencies
RUN apk add --no-cache \
    git \
    make \
    bash \
    curl \
    ca-certificates

# Set working directory
WORKDIR /app

# Copy go mod files first for better caching
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download && go mod verify

# Copy source code
COPY . .

# Build the application
RUN make build

# Run tests (optional, comment out for faster builds)
RUN make test

# Production builder stage - minimal image for building
FROM golang:1.21-alpine AS builder

# Install minimal dependencies
RUN apk add --no-cache git ca-certificates

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build static binary with optimizations
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags "-s -w -X main.Version=$(git describe --tags --always --dirty 2>/dev/null || echo 'container') -X main.GitCommit=$(git rev-parse HEAD 2>/dev/null || echo 'unknown') -X main.BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -a -installsuffix cgo \
    -o ossb \
    ./cmd

# Production stage - minimal runtime image
FROM scratch AS production

# Copy CA certificates for HTTPS requests
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy binary from builder stage
COPY --from=builder /app/ossb /usr/local/bin/ossb

# Create basic directory structure
COPY --from=builder --chown=65534:65534 /tmp /tmp
USER 65534:65534

# Set entrypoint
ENTRYPOINT ["/usr/local/bin/ossb"]

# Default command
CMD ["--help"]

# Metadata
LABEL org.opencontainers.image.title="OSSB" \
      org.opencontainers.image.description="Open Source Slim Builder - A monolithic container builder" \
      org.opencontainers.image.vendor="OSSB Project" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.source="https://github.com/bibin-skaria/ossb" \
      org.opencontainers.image.documentation="https://github.com/bibin-skaria/ossb/blob/main/README.md"

# Alpine-based production stage - alternative with shell access
FROM alpine:latest AS alpine-production

# Install minimal runtime dependencies
RUN apk add --no-cache ca-certificates

# Create non-root user
RUN adduser -D -s /bin/sh ossb

# Copy binary from builder stage
COPY --from=builder /app/ossb /usr/local/bin/ossb

# Switch to non-root user
USER ossb

# Set working directory
WORKDIR /home/ossb

# Set entrypoint
ENTRYPOINT ["/usr/local/bin/ossb"]

# Default command
CMD ["--help"]

# Debian-based production stage - alternative for compatibility
FROM debian:bookworm-slim AS debian-production

# Install minimal runtime dependencies
RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates && \
    rm -rf /var/lib/apt/lists/*

# Create non-root user
RUN useradd -r -s /bin/false ossb

# Copy binary from builder stage
COPY --from=builder /app/ossb /usr/local/bin/ossb

# Switch to non-root user
USER ossb

# Set working directory
WORKDIR /tmp

# Set entrypoint
ENTRYPOINT ["/usr/local/bin/ossb"]

# Default command
CMD ["--help"]