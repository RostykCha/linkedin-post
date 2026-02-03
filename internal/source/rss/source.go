package rss

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"

	"github.com/linkedin-agent/internal/config"
	"github.com/linkedin-agent/internal/models"
	"github.com/linkedin-agent/internal/source"
	"github.com/linkedin-agent/pkg/logger"
)

// Source implements TopicSource for RSS feeds
type Source struct {
	name   string
	url    string
	parser *gofeed.Parser
	log    *logger.Logger
}

// New creates a new RSS source for a single feed
func New(feed config.RSSFeed, log *logger.Logger) *Source {
	return &Source{
		name:   feed.Name,
		url:    feed.URL,
		parser: gofeed.NewParser(),
		log:    log.WithSource("rss", feed.Name),
	}
}

// NewMultiple creates multiple RSS sources from config
func NewMultiple(cfg config.RSSConfig, log *logger.Logger) []*Source {
	sources := make([]*Source, 0, len(cfg.Feeds))
	for _, feed := range cfg.Feeds {
		sources = append(sources, New(feed, log))
	}
	return sources
}

// Name returns the source name
func (s *Source) Name() string {
	return s.name
}

// Type returns "rss"
func (s *Source) Type() string {
	return "rss"
}

// Fetch retrieves topics from the RSS feed
func (s *Source) Fetch(ctx context.Context) ([]*models.RawTopic, error) {
	s.log.Debug().Str("url", s.url).Msg("Fetching RSS feed")

	feed, err := s.parser.ParseURLWithContext(s.url, ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to parse RSS feed %s: %w", s.name, err)
	}

	topics := make([]*models.RawTopic, 0, len(feed.Items))

	for _, item := range feed.Items {
		// Skip items older than 7 days
		publishedAt := time.Now()
		if item.PublishedParsed != nil {
			publishedAt = *item.PublishedParsed
			if time.Since(publishedAt) > 7*24*time.Hour {
				continue
			}
		}

		topic := &models.RawTopic{
			Title:       cleanText(item.Title),
			Description: cleanText(item.Description),
			URL:         item.Link,
			SourceType:  "rss",
			SourceName:  s.name,
			Keywords:    extractKeywords(item),
			PublishedAt: publishedAt,
			RawData: map[string]interface{}{
				"guid":        item.GUID,
				"author":      item.Author,
				"categories":  item.Categories,
				"published":   item.Published,
				"updated":     item.Updated,
			},
		}

		topics = append(topics, topic)
	}

	s.log.Info().
		Int("count", len(topics)).
		Str("feed", s.name).
		Msg("Fetched RSS topics")

	return topics, nil
}

// HealthCheck verifies the RSS feed is accessible
func (s *Source) HealthCheck(ctx context.Context) error {
	_, err := s.parser.ParseURLWithContext(s.url, ctx)
	return err
}

// cleanText removes HTML tags and extra whitespace
func cleanText(text string) string {
	// Remove HTML tags (simple approach)
	text = strings.ReplaceAll(text, "<br>", " ")
	text = strings.ReplaceAll(text, "<br/>", " ")
	text = strings.ReplaceAll(text, "<br />", " ")
	text = strings.ReplaceAll(text, "</p>", " ")
	text = strings.ReplaceAll(text, "<p>", "")

	// Remove remaining HTML tags
	var result strings.Builder
	inTag := false
	for _, r := range text {
		if r == '<' {
			inTag = true
		} else if r == '>' {
			inTag = false
		} else if !inTag {
			result.WriteRune(r)
		}
	}

	// Clean up whitespace
	text = result.String()
	text = strings.Join(strings.Fields(text), " ")
	return strings.TrimSpace(text)
}

// extractKeywords extracts keywords from feed item
func extractKeywords(item *gofeed.Item) []string {
	keywords := make([]string, 0)

	// Add categories as keywords
	keywords = append(keywords, item.Categories...)

	// Add author if present
	if item.Author != nil && item.Author.Name != "" {
		keywords = append(keywords, item.Author.Name)
	}

	return keywords
}

// Ensure Source implements source.TopicSource
var _ source.TopicSource = (*Source)(nil)
