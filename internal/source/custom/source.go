package custom

import (
	"context"
	"time"

	"github.com/linkedin-agent/internal/config"
	"github.com/linkedin-agent/internal/models"
	"github.com/linkedin-agent/internal/source"
	"github.com/linkedin-agent/pkg/logger"
)

// Source implements TopicSource for custom keywords/topics
type Source struct {
	keywords []string
	log      *logger.Logger
}

// New creates a new custom source
func New(cfg config.CustomConfig, log *logger.Logger) *Source {
	return &Source{
		keywords: cfg.Keywords,
		log:      log.WithSource("custom", "keywords"),
	}
}

// Name returns the source name
func (s *Source) Name() string {
	return "custom-keywords"
}

// Type returns "custom"
func (s *Source) Type() string {
	return "custom"
}

// Fetch returns the configured keywords as topics for AI expansion
func (s *Source) Fetch(ctx context.Context) ([]*models.RawTopic, error) {
	s.log.Debug().Int("count", len(s.keywords)).Msg("Returning custom keywords as topics")

	topics := make([]*models.RawTopic, 0, len(s.keywords))

	for _, keyword := range s.keywords {
		topic := &models.RawTopic{
			Title:       keyword,
			Description: "Custom keyword for AI-powered topic expansion",
			URL:         "", // No URL for custom keywords
			SourceType:  "custom",
			SourceName:  "keywords",
			Keywords:    []string{keyword},
			PublishedAt: time.Now(),
			RawData: map[string]interface{}{
				"type": "keyword",
			},
		}
		topics = append(topics, topic)
	}

	s.log.Info().
		Int("count", len(topics)).
		Msg("Returned custom keyword topics")

	return topics, nil
}

// HealthCheck always succeeds for custom source
func (s *Source) HealthCheck(ctx context.Context) error {
	return nil
}

// Ensure Source implements source.TopicSource
var _ source.TopicSource = (*Source)(nil)
