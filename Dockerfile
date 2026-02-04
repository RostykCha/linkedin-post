# Build stage
FROM golang:1.24-alpine AS builder

# Install build dependencies for CGO (required for SQLite)
RUN apk add --no-cache gcc musl-dev

WORKDIR /app

# Copy dependency files first for better layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build both binaries with CGO enabled for SQLite support
RUN CGO_ENABLED=1 GOOS=linux go build -o linkedin-agent ./cmd/cli
RUN CGO_ENABLED=1 GOOS=linux go build -o linkedin-scheduler ./cmd/scheduler

# Runtime stage
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache ca-certificates sqlite tzdata

WORKDIR /app

# Create non-root user for security
RUN adduser -D -u 1000 appuser

# Copy binaries from builder
COPY --from=builder /app/linkedin-agent .
COPY --from=builder /app/linkedin-scheduler .

# Copy default config
COPY configs/config.yaml ./configs/

# Create data directory for SQLite database and set permissions
RUN mkdir -p /app/data && chown -R appuser:appuser /app

# Declare volume for persistent data
VOLUME ["/app/data"]

# Switch to non-root user
USER appuser

# Expose health check port
EXPOSE 10000

# Health check for container orchestration
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s \
  CMD wget --no-verbose --tries=1 --spider http://localhost:10000/health || exit 1

# Default command runs the scheduler
CMD ["./linkedin-scheduler"]
