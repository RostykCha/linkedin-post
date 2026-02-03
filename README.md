# LinkedIn Agentic Workflows

An autonomous AI-powered system for LinkedIn content automation, built with Go and Claude AI.

## Features

- **Topic Discovery Agent**: Automatically finds trending topics from multiple sources
  - RSS feeds (HBR, Forbes, Inc.com, Entrepreneur)
  - Custom keywords for AI expansion
  - Extensible source architecture

- **Content Publisher Agent**: Generates and publishes LinkedIn posts
  - AI-powered content generation using Claude
  - Support for text posts and polls
  - Hybrid approval mode (auto-publish high-score topics, queue low-score for review)

- **Background Scheduler**: Runs autonomous discovery and publishing cycles

## Quick Start

### 1. Configure API Keys

Copy and edit the config file:
```bash
cp configs/config.yaml configs/config.local.yaml
```

Add your API keys:
- `anthropic.api_key` - Your Claude API key
- `linkedin.client_id` / `client_secret` - From [LinkedIn Developer Portal](https://developer.linkedin.com/)

### 2. Build

```bash
# Using Go directly
go build -o bin/linkedin-agent ./cmd/cli
go build -o bin/linkedin-scheduler ./cmd/scheduler

# Or using Make
make build
```

### 3. Authenticate with LinkedIn

```bash
./bin/linkedin-agent oauth login
```

### 4. Discover Topics

```bash
./bin/linkedin-agent discover run
./bin/linkedin-agent topics list
```

### 5. Generate & Publish Content

```bash
# Generate content for a topic
./bin/linkedin-agent publish generate --topic-id=1

# Publish immediately
./bin/linkedin-agent publish now 1

# Or schedule for later
./bin/linkedin-agent publish schedule 1 --at="2024-01-15 09:00"
```

### 6. Run Autonomous Mode

```bash
./bin/linkedin-scheduler
```

## CLI Commands

```
linkedin-agent discover run          # Run topic discovery
linkedin-agent topics list           # List discovered topics
linkedin-agent publish generate      # Generate content for a topic
linkedin-agent publish now           # Publish immediately
linkedin-agent publish schedule      # Schedule for later
linkedin-agent publish approve       # Approve draft for publishing
linkedin-agent oauth login           # LinkedIn authentication
linkedin-agent oauth status          # Check token status
linkedin-agent posts list            # List all posts
linkedin-agent posts queue           # View scheduled queue
```

## Configuration

See `configs/config.yaml` for all options:

- **Database**: SQLite (default) or PostgreSQL
- **Sources**: RSS feeds, NewsAPI, Reddit, Twitter, custom keywords
- **Scheduler**: Cron expressions for discovery and publishing
- **Publishing**: Auto-approve threshold, max posts per day, brand voice

## Architecture

```
├── cmd/
│   ├── cli/           # CLI application
│   └── scheduler/     # Background daemon
├── internal/
│   ├── agent/         # Discovery & Publisher agents
│   ├── ai/            # Claude AI integration
│   ├── linkedin/      # LinkedIn OAuth & API
│   ├── source/        # Topic sources (RSS, custom)
│   ├── storage/       # Database layer (SQLite)
│   ├── config/        # Configuration
│   └── models/        # Data models
└── pkg/
    ├── logger/        # Structured logging
    └── ratelimit/     # Rate limiting
```

## Tech Stack

- **Go 1.22+**
- **Claude AI** (Anthropic SDK)
- **LinkedIn Marketing API**
- **GORM** (SQLite/PostgreSQL)
- **Cobra** (CLI)
- **Viper** (Configuration)
- **Zerolog** (Logging)

## License

MIT
