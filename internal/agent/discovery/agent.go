package discovery

import (
	"context"
	"fmt"
	"time"

	"github.com/linkedin-agent/internal/ai"
	"github.com/linkedin-agent/internal/models"
	"github.com/linkedin-agent/internal/source"
	"github.com/linkedin-agent/internal/storage"
	"github.com/linkedin-agent/pkg/logger"
)

// Agent handles daily IT/tech news discovery from multiple sources
type Agent struct {
	sourceManager *source.Manager
	aiClient      *ai.Client
	repository    storage.Repository
	log           *logger.Logger
}

// NewAgent creates a new discovery agent
func NewAgent(
	sourceManager *source.Manager,
	aiClient *ai.Client,
	repository storage.Repository,
	log *logger.Logger,
) *Agent {
	return &Agent{
		sourceManager: sourceManager,
		aiClient:      aiClient,
		repository:    repository,
		log:           log.WithComponent("discovery"),
	}
}

// DiscoveryResult contains the results of a discovery run
type DiscoveryResult struct {
	TopicsFound    int
	TopicsRanked   int
	TopicsSaved    int
	TopicsSkipped  int
	Errors         []error
	Duration       time.Duration
}

// Run executes the discovery process
func (a *Agent) Run(ctx context.Context) (*DiscoveryResult, error) {
	startTime := time.Now()
	result := &DiscoveryResult{}

	a.log.Info().Msg("Starting daily tech news discovery")

	// Step 1: Fetch topics from all sources
	rawTopics, fetchErrors := a.sourceManager.FetchAll(ctx)
	result.Errors = append(result.Errors, fetchErrors...)
	result.TopicsFound = len(rawTopics)

	a.log.Info().
		Int("topics_found", len(rawTopics)).
		Int("fetch_errors", len(fetchErrors)).
		Msg("Fetched topics from sources")

	if len(rawTopics) == 0 {
		a.log.Warn().Msg("No topics found from any source")
		result.Duration = time.Since(startTime)
		return result, nil
	}

	// Step 2: Deduplicate topics
	uniqueTopics := a.deduplicateTopics(ctx, rawTopics)
	a.log.Info().
		Int("unique_topics", len(uniqueTopics)).
		Int("duplicates_removed", len(rawTopics)-len(uniqueTopics)).
		Msg("Deduplicated topics")

	// Step 3: Rank topics with AI
	rankedTopics, rankErrors := a.rankTopics(ctx, uniqueTopics)
	result.Errors = append(result.Errors, rankErrors...)
	result.TopicsRanked = len(rankedTopics)

	// Step 4: Save topics to database
	for _, topic := range rankedTopics {
		if err := a.repository.CreateTopic(ctx, topic); err != nil {
			a.log.Warn().
				Err(err).
				Str("title", topic.Title).
				Msg("Failed to save topic")
			result.TopicsSkipped++
		} else {
			result.TopicsSaved++
		}
	}

	result.Duration = time.Since(startTime)

	a.log.Info().
		Int("topics_saved", result.TopicsSaved).
		Int("topics_skipped", result.TopicsSkipped).
		Dur("duration", result.Duration).
		Msg("Discovery completed")

	return result, nil
}

// deduplicateTopics removes duplicate topics based on external ID
func (a *Agent) deduplicateTopics(ctx context.Context, topics []*models.RawTopic) []*models.RawTopic {
	seen := make(map[string]bool)
	unique := make([]*models.RawTopic, 0)

	for _, topic := range topics {
		// Generate external ID
		externalID := source.GenerateExternalID(topic.SourceType, topic.URL)

		// Check if already in this batch
		if seen[externalID] {
			continue
		}

		// Check if already in database
		existing, _ := a.repository.GetTopicByExternalID(ctx, externalID)
		if existing != nil {
			continue
		}

		seen[externalID] = true
		unique = append(unique, topic)
	}

	return unique
}

// rankTopics uses AI to rank topics and converts them to models.Topic
func (a *Agent) rankTopics(ctx context.Context, rawTopics []*models.RawTopic) ([]*models.Topic, []error) {
	var errors []error
	topics := make([]*models.Topic, 0, len(rawTopics))

	// Process in batches of 10
	batchSize := 10
	for i := 0; i < len(rawTopics); i += batchSize {
		end := i + batchSize
		if end > len(rawTopics) {
			end = len(rawTopics)
		}
		batch := rawTopics[i:end]

		a.log.Debug().
			Int("batch_start", i).
			Int("batch_size", len(batch)).
			Msg("Ranking topic batch")

		rankings, err := a.aiClient.RankTopics(ctx, batch)
		if err != nil {
			a.log.Error().Err(err).Msg("Failed to rank topic batch")
			errors = append(errors, fmt.Errorf("batch ranking failed: %w", err))
			continue
		}

		// Convert raw topics to models with rankings
		for j, raw := range batch {
			topic := &models.Topic{
				ExternalID:  source.GenerateExternalID(raw.SourceType, raw.URL),
				Title:       raw.Title,
				Description: raw.Description,
				URL:         raw.URL,
				SourceType:  raw.SourceType,
				SourceName:  raw.SourceName,
				Keywords:    raw.Keywords,
				Status:      models.TopicStatusPending,
			}

			// Apply ranking if available
			if j < len(rankings) && rankings[j] != nil {
				topic.AIScore = rankings[j].Score
				topic.AIAnalysis = rankings[j].Analysis
				topic.RawData = models.JSON{
					"suggested_angle": rankings[j].SuggestedAngle,
					"hashtags":        rankings[j].Hashtags,
					"original_data":   raw.RawData,
				}
			}

			topics = append(topics, topic)
		}
	}

	return topics, errors
}

// RunForSource runs discovery for a specific source
func (a *Agent) RunForSource(ctx context.Context, sourceName string) (*DiscoveryResult, error) {
	startTime := time.Now()
	result := &DiscoveryResult{}

	src := a.sourceManager.GetSourceByName(sourceName)
	if src == nil {
		return nil, fmt.Errorf("source not found: %s", sourceName)
	}

	a.log.Info().Str("source", sourceName).Msg("Running discovery for source")

	rawTopics, err := src.Fetch(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch from %s: %w", sourceName, err)
	}

	result.TopicsFound = len(rawTopics)

	// Deduplicate and rank
	uniqueTopics := a.deduplicateTopics(ctx, rawTopics)
	rankedTopics, rankErrors := a.rankTopics(ctx, uniqueTopics)
	result.Errors = rankErrors
	result.TopicsRanked = len(rankedTopics)

	// Save
	for _, topic := range rankedTopics {
		if err := a.repository.CreateTopic(ctx, topic); err != nil {
			result.TopicsSkipped++
		} else {
			result.TopicsSaved++
		}
	}

	result.Duration = time.Since(startTime)
	return result, nil
}
