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

# Copy binaries from builder
COPY --from=builder /app/linkedin-agent .
COPY --from=builder /app/linkedin-scheduler .

# Copy default config
COPY configs/config.yaml ./configs/

# Create data directory for SQLite database
RUN mkdir -p /app/data

# Declare volume for persistent data
VOLUME ["/app/data"]

# Default command runs the scheduler
CMD ["./linkedin-scheduler"]
