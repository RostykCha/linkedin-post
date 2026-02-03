package source

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/linkedin-agent/internal/models"
)

// TopicSource defines the interface for topic discovery sources
type TopicSource interface {
	// Name returns the unique name of this source
	Name() string

	// Type returns the source type (rss, newsapi, reddit, twitter, custom)
	Type() string

	// Fetch retrieves topics from the source
	Fetch(ctx context.Context) ([]*models.RawTopic, error)

	// HealthCheck verifies the source is accessible
	HealthCheck(ctx context.Context) error
}

// GenerateExternalID creates a unique ID for a topic based on source and URL
func GenerateExternalID(sourceType, url string) string {
	data := fmt.Sprintf("%s:%s", sourceType, url)
	hash := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", hash[:16]) // Use first 16 bytes (32 hex chars)
}

// Manager manages multiple topic sources
type Manager struct {
	sources []TopicSource
}

// NewManager creates a new source manager
func NewManager() *Manager {
	return &Manager{
		sources: make([]TopicSource, 0),
	}
}

// Register adds a source to the manager
func (m *Manager) Register(source TopicSource) {
	m.sources = append(m.sources, source)
}

// GetSources returns all registered sources
func (m *Manager) GetSources() []TopicSource {
	return m.sources
}

// GetSourceByName returns a source by name
func (m *Manager) GetSourceByName(name string) TopicSource {
	for _, s := range m.sources {
		if s.Name() == name {
			return s
		}
	}
	return nil
}

// GetSourcesByType returns all sources of a given type
func (m *Manager) GetSourcesByType(sourceType string) []TopicSource {
	var result []TopicSource
	for _, s := range m.sources {
		if s.Type() == sourceType {
			result = append(result, s)
		}
	}
	return result
}

// FetchAll fetches topics from all sources concurrently
func (m *Manager) FetchAll(ctx context.Context) ([]*models.RawTopic, []error) {
	type result struct {
		topics []*models.RawTopic
		err    error
	}

	results := make(chan result, len(m.sources))

	for _, source := range m.sources {
		go func(s TopicSource) {
			topics, err := s.Fetch(ctx)
			results <- result{topics: topics, err: err}
		}(source)
	}

	var allTopics []*models.RawTopic
	var errors []error

	for range m.sources {
		r := <-results
		if r.err != nil {
			errors = append(errors, r.err)
		} else {
			allTopics = append(allTopics, r.topics...)
		}
	}

	return allTopics, errors
}
