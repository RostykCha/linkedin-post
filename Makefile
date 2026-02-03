# LinkedIn Agent Makefile

.PHONY: build run test clean deps migrate

# Go binary path (for Windows with Go installed in user folder)
GO ?= go

# Build output
BUILD_DIR = bin
CLI_BINARY = $(BUILD_DIR)/linkedin-agent
SCHEDULER_BINARY = $(BUILD_DIR)/linkedin-scheduler

# Default target
all: build

# Install dependencies
deps:
	$(GO) mod download
	$(GO) mod tidy

# Build all binaries
build: build-cli build-scheduler

build-cli:
	@mkdir -p $(BUILD_DIR)
	$(GO) build -o $(CLI_BINARY) ./cmd/cli

build-scheduler:
	@mkdir -p $(BUILD_DIR)
	$(GO) build -o $(SCHEDULER_BINARY) ./cmd/scheduler

# Run CLI
run-cli: build-cli
	./$(CLI_BINARY) $(ARGS)

# Run scheduler
run-scheduler: build-scheduler
	./$(SCHEDULER_BINARY)

# Run discovery
discover: build-cli
	./$(CLI_BINARY) discover run

# List topics
topics: build-cli
	./$(CLI_BINARY) topics list

# List posts
posts: build-cli
	./$(CLI_BINARY) posts list

# OAuth login
login: build-cli
	./$(CLI_BINARY) oauth login

# Database migration
migrate:
	@echo "Migrations run automatically on startup"

# Run tests
test:
	$(GO) test -v ./...

# Clean build artifacts
clean:
	rm -rf $(BUILD_DIR)
	rm -f ./data/*.db

# Initialize project (first time setup)
init: deps
	@mkdir -p data
	@cp -n configs/config.yaml configs/config.local.yaml 2>/dev/null || true
	@echo "Project initialized!"
	@echo "1. Edit configs/config.local.yaml with your API keys"
	@echo "2. Run 'make login' to authenticate with LinkedIn"
	@echo "3. Run 'make discover' to find topics"
	@echo "4. Run 'make topics' to see discovered topics"

# Development helpers
dev-cli:
	$(GO) run ./cmd/cli $(ARGS)

dev-scheduler:
	$(GO) run ./cmd/scheduler

# Format code
fmt:
	$(GO) fmt ./...

# Lint code
lint:
	golangci-lint run

# Help
help:
	@echo "LinkedIn Agent - Available targets:"
	@echo ""
	@echo "  make build          - Build all binaries"
	@echo "  make deps           - Download dependencies"
	@echo "  make init           - Initialize project for first use"
	@echo "  make login          - Authenticate with LinkedIn"
	@echo "  make discover       - Run topic discovery"
	@echo "  make topics         - List discovered topics"
	@echo "  make posts          - List posts"
	@echo "  make run-scheduler  - Run background scheduler"
	@echo "  make test           - Run tests"
	@echo "  make clean          - Clean build artifacts"
	@echo ""
	@echo "Use ARGS='...' to pass arguments:"
	@echo "  make run-cli ARGS='topics list --min-score=80'"
