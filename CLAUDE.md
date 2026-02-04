# LinkedIn Agentic Workflows - Claude Code Rules

## Project Overview

This is an autonomous AI-powered LinkedIn content automation system built in Go 1.24. It discovers trending topics from multiple sources and uses Claude AI to generate and publish engaging LinkedIn posts.

### Architecture

```
linkedin-post/
├── cmd/
│   ├── cli/main.go              # CLI application (interactive commands)
│   └── scheduler/main.go         # Background daemon (cron-based automation)
├── internal/
│   ├── agent/
│   │   ├── discovery/agent.go   # Topic discovery orchestration
│   │   └── publisher/agent.go   # Content generation & publishing
│   ├── ai/
│   │   ├── client.go            # Anthropic SDK wrapper
│   │   ├── prompts.go           # System prompts for AI
│   │   └── ranking.go           # Topic scoring logic
│   ├── config/config.go         # Viper configuration management
│   ├── linkedin/
│   │   ├── client.go            # LinkedIn API client
│   │   └── oauth.go             # OAuth 2.0 authentication
│   ├── models/                  # Data models (Topic, Post, OAuth)
│   ├── source/                  # Content sources (RSS, custom keywords)
│   ├── storage/                 # Repository pattern (SQLite/PostgreSQL)
│   └── tracker/sheets.go        # Google Sheets integration
├── pkg/
│   ├── logger/logger.go         # Structured logging (zerolog)
│   └── ratelimit/limiter.go     # Multi-service rate limiting
├── configs/
│   ├── config.yaml              # Default configuration
│   └── config.local.yaml        # Local overrides
└── Makefile                     # Build targets
```

## Go Coding Conventions

### Constructor Pattern

Always use `New*` constructors that return typed pointers with dependency injection:

```go
// CORRECT
func NewClient(cfg config.AnthropicConfig, limiter *ratelimit.MultiLimiter, log *logger.Logger) *Client {
    return &Client{
        config:  cfg,
        limiter: limiter,
        log:     log,
    }
}

// INCORRECT - avoid global state
var globalClient *Client
```

### Interface-Based Design

Define interfaces for abstraction, implement in separate packages:

```go
// internal/source/interface.go - Define interface
type TopicSource interface {
    Name() string
    Type() string
    Fetch(ctx context.Context) ([]*models.RawTopic, error)
    HealthCheck(ctx context.Context) error
}

// internal/source/rss/source.go - Implement
type RSSSource struct { ... }
func (s *RSSSource) Fetch(ctx context.Context) ([]*models.RawTopic, error) { ... }
```

### Custom Types with SQL Support

For JSON fields stored in SQLite, implement `driver.Valuer` and `sql.Scanner`:

```go
type JSON map[string]interface{}

func (j JSON) Value() (driver.Value, error) {
    return json.Marshal(j)
}

func (j *JSON) Scan(value interface{}) error {
    if value == nil {
        *j = nil
        return nil
    }
    return json.Unmarshal(value.([]byte), j)
}
```

### String Constants for Enums

Use typed string constants, not iota:

```go
type TopicStatus string

const (
    TopicStatusPending  TopicStatus = "pending"
    TopicStatusApproved TopicStatus = "approved"
    TopicStatusRejected TopicStatus = "rejected"
    TopicStatusUsed     TopicStatus = "used"
)
```

## Error Handling

### Wrap Errors with Context

Always wrap errors with `%w` verb for error chain preservation:

```go
// CORRECT
if err := a.repository.CreateTopic(ctx, topic); err != nil {
    return fmt.Errorf("failed to save topic %q: %w", topic.Title, err)
}

// INCORRECT - loses error chain
return fmt.Errorf("failed to save topic: %s", err.Error())
```

### Batch Processing with Error Collection

Continue processing on individual failures, collect errors:

```go
var errors []error
for _, topic := range topics {
    if err := process(topic); err != nil {
        errors = append(errors, fmt.Errorf("topic %d: %w", topic.ID, err))
        continue // Don't stop processing
    }
}
return results, errors
```

### Result Structs

Return explicit result types with both data and metadata:

```go
type DiscoveryResult struct {
    TopicsFound    int
    TopicsRanked   int
    TopicsSaved    int
    TopicsSkipped  int
    Errors         []error
    Duration       time.Duration
}
```

### Graceful Degradation

Skip failed items, track counts separately:

```go
for _, topic := range rankedTopics {
    if err := a.repository.CreateTopic(ctx, topic); err != nil {
        a.log.Warn().Err(err).Str("title", topic.Title).Msg("Failed to save topic")
        result.TopicsSkipped++
    } else {
        result.TopicsSaved++
    }
}
```

## API Integration Patterns

### HTTP Client with Auth and Rate Limiting

Layer HTTP clients with automatic authentication and rate limiting:

```go
func (c *Client) do(ctx context.Context, method, path string, body interface{}) (*http.Response, error) {
    // 1. Rate limiting
    if err := c.rateLimiter.Wait(ctx, ratelimit.LimiterLinkedIn); err != nil {
        return nil, fmt.Errorf("rate limit error: %w", err)
    }

    // 2. Token refresh
    token, err := c.oauthManager.GetValidToken(ctx)
    if err != nil {
        return nil, fmt.Errorf("authentication error: %w", err)
    }

    // 3. Build request with headers
    req.Header.Set("Authorization", "Bearer "+token.AccessToken)

    // 4. Execute
    return c.httpClient.Do(req)
}
```

### Request/Response Structs

Use explicit structs with JSON tags:

```go
type PostRequest struct {
    Author         string       `json:"author"`
    Commentary     string       `json:"commentary"`
    Visibility     string       `json:"visibility"`
    Distribution   Distribution `json:"distribution"`
    LifecycleState string       `json:"lifecycleState"`
}
```

### Timeout Configuration

Set timeouts per service:

```go
httpClient: &http.Client{
    Timeout: 30 * time.Second,
}
```

## Configuration Management

### Viper Hierarchy

Configuration loads in order (later overrides earlier):
1. Hardcoded defaults via `setDefaults()`
2. YAML config file (`configs/config.yaml` or `configs/config.local.yaml`)
3. Environment variables with `LINKEDIN_` prefix

### Environment Variables

All settings can be overridden via environment variables:

```bash
LINKEDIN_DATABASE_DRIVER=sqlite
LINKEDIN_DATABASE_DSN=./data/linkedin.db
LINKEDIN_ANTHROPIC_API_KEY=sk-ant-...
LINKEDIN_LINKEDIN_CLIENT_ID=...
LINKEDIN_LINKEDIN_CLIENT_SECRET=...
LINKEDIN_SOURCES_RSS_ENABLED=true
```

### Sensitive Data

Never commit to config files:
- `configs/config.local.yaml` - Use for local development
- Environment variables - Use for production
- `configs/google-credentials.json` - Service account keys

## Database Patterns

### Repository Interface

Abstract all database operations behind interfaces:

```go
type Repository interface {
    // Topics
    CreateTopic(ctx context.Context, topic *models.Topic) error
    GetTopicByID(ctx context.Context, id uint) (*models.Topic, error)
    GetTopicByExternalID(ctx context.Context, externalID string) (*models.Topic, error)
    ListTopics(ctx context.Context, filter TopicFilter) ([]*models.Topic, error)
    UpdateTopic(ctx context.Context, topic *models.Topic) error

    // Posts
    CreatePost(ctx context.Context, post *models.Post) error
    UpdatePost(ctx context.Context, post *models.Post) error
    // ...
}
```

### GORM Query Building

Build queries with filters:

```go
func (r *Repository) ListTopics(ctx context.Context, filter storage.TopicFilter) ([]*models.Topic, error) {
    query := r.db.WithContext(ctx).Model(&models.Topic{})

    if filter.Status != nil {
        query = query.Where("status = ?", *filter.Status)
    }
    if filter.MinScore != nil {
        query = query.Where("ai_score >= ?", *filter.MinScore)
    }

    return topics, query.Find(&topics).Error
}
```

### Migrations

GORM auto-migrates on startup. Models define schema:

```go
type Topic struct {
    ID          uint           `gorm:"primaryKey"`
    ExternalID  string         `gorm:"uniqueIndex;size:255"`
    Title       string         `gorm:"size:500"`
    AIScore     float64        `gorm:"index"`
    Status      TopicStatus    `gorm:"size:20;default:'pending'"`
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

## Logging Conventions

### Structured Logging with zerolog

Use the wrapped logger with context helpers:

```go
// Add context
log := logger.WithComponent("discovery")
log = log.WithSource("rss", "techcrunch")

// Log with fields
log.Info().
    Int("topics_found", result.TopicsFound).
    Int("topics_saved", result.TopicsSaved).
    Dur("duration", result.Duration).
    Msg("Discovery completed")

// Log errors
log.Error().
    Err(err).
    Str("topic_title", topic.Title).
    Msg("Failed to save topic")
```

### Log Levels

- `Debug` - Detailed debugging info (disabled in production)
- `Info` - Normal operations, progress updates
- `Warn` - Recoverable issues, skipped items
- `Error` - Failures that need attention

## Concurrency Patterns

### Channel-Based Fan-Out/Fan-In

For concurrent source fetching:

```go
func (m *Manager) FetchAll(ctx context.Context) ([]*models.RawTopic, []error) {
    results := make(chan result, len(m.sources))

    for _, source := range m.sources {
        go func(s TopicSource) {
            topics, err := s.Fetch(ctx)
            results <- result{topics: topics, err: err}
        }(source)
    }

    // Collect results
    for range m.sources {
        r := <-results
        // ...
    }
}
```

### Context Threading

All long-running operations accept `context.Context`:

```go
func (a *Agent) Run(ctx context.Context) (*DiscoveryResult, error)
func (c *Client) Complete(ctx context.Context, systemPrompt, userMessage string) (string, error)
```

## Build Commands

```bash
# Build all
make build

# Build specific targets
make build-cli
make build-scheduler

# Run tests
make test

# Clean build artifacts
make clean

# Docker
docker-compose up scheduler     # Run scheduler daemon
docker-compose run cli discover run   # Run CLI command
```

## CLI Commands Reference

```bash
# Discovery
linkedin-agent discover run              # Discover topics from all sources

# Topic management
linkedin-agent topics list               # List discovered topics
linkedin-agent topics list --status=pending --min-score=70

# Publishing
linkedin-agent publish generate <topic-id>   # Generate post content
linkedin-agent publish now <post-id>         # Publish immediately
linkedin-agent publish schedule <post-id>    # Schedule for later

# OAuth
linkedin-agent oauth login               # Start OAuth flow (opens browser)
linkedin-agent oauth status              # Check token status

# Tracker
linkedin-agent tracker sync-topics       # Sync topics to Google Sheets
linkedin-agent tracker sync-posts        # Sync posts to Google Sheets
```

## Testing

### Test Requirements

- All new code should have corresponding tests
- Use table-driven tests for multiple scenarios
- Mock interfaces for unit tests
- Use testify/assert for assertions

### Test Patterns

```go
func TestAgent_Run(t *testing.T) {
    tests := []struct {
        name     string
        setup    func(*mockRepo, *mockAI)
        expected *DiscoveryResult
        wantErr  bool
    }{
        // Test cases...
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // ...
        })
    }
}
```

## Important Files

- `internal/ai/prompts.go` - AI system prompts (topic ranking, content generation)
- `internal/config/config.go` - All configuration options with defaults
- `configs/config.yaml` - Default configuration template
- `cmd/cli/main.go` - All CLI commands and flags
- `cmd/scheduler/main.go` - Cron job definitions

## Common Tasks

### Adding a New Content Source

1. Create `internal/source/<name>/source.go`
2. Implement `TopicSource` interface
3. Add config struct in `internal/config/config.go`
4. Register in source manager initialization

### Adding a New CLI Command

1. Add command in `cmd/cli/main.go`
2. Follow existing patterns (cobra command, flags, run function)
3. Use dependency injection for services

### Modifying AI Prompts

1. Edit `internal/ai/prompts.go`
2. Test with `linkedin-agent publish generate` command
3. Adjust temperature/max_tokens in config if needed
