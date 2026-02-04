package storage

import (
	"context"
	"time"

	"github.com/linkedin-agent/internal/models"
)

// Repository defines the interface for data persistence
type Repository interface {
	// Topic operations
	CreateTopic(ctx context.Context, topic *models.Topic) error
	GetTopicByID(ctx context.Context, id uint) (*models.Topic, error)
	GetTopicByExternalID(ctx context.Context, externalID string) (*models.Topic, error)
	ListTopics(ctx context.Context, filter TopicFilter) ([]*models.Topic, error)
	GetTopTopics(ctx context.Context, limit int, minScore float64) ([]*models.Topic, error)
	UpdateTopic(ctx context.Context, topic *models.Topic) error
	DeleteTopic(ctx context.Context, id uint) error

	// Post operations
	CreatePost(ctx context.Context, post *models.Post) error
	GetPostByID(ctx context.Context, id uint) (*models.Post, error)
	ListPosts(ctx context.Context, filter PostFilter) ([]*models.Post, error)
	UpdatePost(ctx context.Context, post *models.Post) error
	DeletePost(ctx context.Context, id uint) error
	GetScheduledPosts(ctx context.Context, before time.Time) ([]*models.Post, error)

	// OAuth token operations
	SaveToken(ctx context.Context, token *models.OAuthToken) error
	GetToken(ctx context.Context, provider string) (*models.OAuthToken, error)
	DeleteToken(ctx context.Context, provider string) error

	// Source config operations
	GetSourceConfigs(ctx context.Context) ([]*models.SourceConfig, error)
	SaveSourceConfig(ctx context.Context, config *models.SourceConfig) error

	// Schedule operations
	GetSchedules(ctx context.Context) ([]*models.Schedule, error)
	SaveSchedule(ctx context.Context, schedule *models.Schedule) error

	// Maintenance
	Close() error
	Migrate() error
}

// TopicFilter defines filtering options for topics
type TopicFilter struct {
	Status      *models.TopicStatus
	SourceType  *string
	MinScore    *float64
	MaxScore    *float64
	Limit       int
	Offset      int
	OrderBy     string // "score", "discovered_at"
	OrderDesc   bool
}

// PostFilter defines filtering options for posts
type PostFilter struct {
	Status    *models.PostStatus
	TopicID   *uint
	Limit     int
	Offset    int
	OrderBy   string
	OrderDesc bool
}

// DefaultTopicFilter returns a filter with sensible defaults
func DefaultTopicFilter() TopicFilter {
	return TopicFilter{
		Limit:     50,
		OrderBy:   "ai_score",
		OrderDesc: true,
	}
}

// DefaultPostFilter returns a filter with sensible defaults
func DefaultPostFilter() PostFilter {
	return PostFilter{
		Limit:     50,
		OrderBy:   "created_at",
		OrderDesc: true,
	}
}
