package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/linkedin-agent/internal/models"
)

// stripMarkdownCodeBlock removes markdown code block delimiters from AI responses
func stripMarkdownCodeBlock(response string) string {
	response = strings.TrimSpace(response)

	// Find the first { which starts valid JSON
	startIdx := strings.Index(response, "{")
	if startIdx == -1 {
		return response
	}

	// Find the last } which ends valid JSON
	endIdx := strings.LastIndex(response, "}")
	if endIdx == -1 || endIdx < startIdx {
		return response
	}

	// Extract just the JSON object
	return response[startIdx : endIdx+1]
}

// TopicRanking represents the AI's analysis of a topic
type TopicRanking struct {
	Score          float64  `json:"score"`
	Analysis       string   `json:"analysis"`
	SuggestedAngle string   `json:"suggested_angle"`
	Hashtags       []string `json:"hashtags"`
}

// BatchRankingResponse represents the response for batch ranking
type BatchRankingResponse struct {
	Rankings []struct {
		Index          int      `json:"index"`
		Score          float64  `json:"score"`
		Analysis       string   `json:"analysis"`
		SuggestedAngle string   `json:"suggested_angle"`
		Hashtags       []string `json:"hashtags"`
	} `json:"rankings"`
}

// RankTopic analyzes a single topic and returns a score
func (c *Client) RankTopic(ctx context.Context, topic *models.RawTopic) (*TopicRanking, error) {
	userPrompt := fmt.Sprintf(TopicRankingUserPrompt,
		topic.Title,
		topic.Description,
		topic.SourceName,
		topic.URL,
	)

	response, err := c.CompleteWithJSON(ctx, TopicRankingSystemPrompt, userPrompt)
	if err != nil {
		return nil, err
	}

	var ranking TopicRanking
	if err := json.Unmarshal([]byte(stripMarkdownCodeBlock(response)), &ranking); err != nil {
		c.log.Error().
			Err(err).
			Str("response", response).
			Msg("Failed to parse ranking response")
		return nil, fmt.Errorf("failed to parse ranking response: %w", err)
	}

	return &ranking, nil
}

// RankTopics analyzes multiple topics in batch (more efficient)
func (c *Client) RankTopics(ctx context.Context, topics []*models.RawTopic) ([]*TopicRanking, error) {
	if len(topics) == 0 {
		return nil, nil
	}

	// For small batches, rank individually
	if len(topics) <= 3 {
		rankings := make([]*TopicRanking, 0, len(topics))
		for _, topic := range topics {
			ranking, err := c.RankTopic(ctx, topic)
			if err != nil {
				c.log.Warn().
					Err(err).
					Str("title", topic.Title).
					Msg("Failed to rank topic, skipping")
				continue
			}
			rankings = append(rankings, ranking)
		}
		return rankings, nil
	}

	// Build topics list for batch prompt
	topicsText := ""
	for i, topic := range topics {
		topicsText += fmt.Sprintf("\n[%d] Title: %s\nDescription: %s\nSource: %s\n",
			i, topic.Title, topic.Description, topic.SourceName)
	}

	userPrompt := fmt.Sprintf(BatchTopicRankingUserPrompt, topicsText)

	response, err := c.CompleteWithJSON(ctx, TopicRankingSystemPrompt, userPrompt)
	if err != nil {
		return nil, err
	}

	var batchResponse BatchRankingResponse
	if err := json.Unmarshal([]byte(stripMarkdownCodeBlock(response)), &batchResponse); err != nil {
		c.log.Error().
			Err(err).
			Str("response", response).
			Msg("Failed to parse batch ranking response")
		return nil, fmt.Errorf("failed to parse batch ranking response: %w", err)
	}

	// Convert to TopicRanking slice
	rankings := make([]*TopicRanking, len(topics))
	for _, r := range batchResponse.Rankings {
		if r.Index >= 0 && r.Index < len(rankings) {
			rankings[r.Index] = &TopicRanking{
				Score:          r.Score,
				Analysis:       r.Analysis,
				SuggestedAngle: r.SuggestedAngle,
				Hashtags:       r.Hashtags,
			}
		}
	}

	return rankings, nil
}

// GeneratedContent represents AI-generated LinkedIn content
type GeneratedContent struct {
	Content  string   `json:"content"`
	Hashtags []string `json:"hashtags"`
	Hook     string   `json:"hook"`
	CTA      string   `json:"cta"`
}

// postProcessContent ensures header and footer are present in the content
func postProcessContent(content string) string {
	// Generate header with today's date
	today := time.Now().Format("Jan 2, 2006")
	header := "Tech Insights from Ros | " + today
	footer := "---\nLinkedIn: https://www.linkedin.com/in/qa-lead-rostyslav-chabria/\nInstagram: https://www.instagram.com/rostislav_cha"

	// Always ensure correct header with today's date
	if strings.HasPrefix(content, "Tech Insights from Ros") {
		// Replace the first line (header) with correct date
		if idx := strings.Index(content, "\n"); idx != -1 {
			content = header + content[idx:]
		}
	} else {
		content = header + "\n\n" + content
	}

	// Check if footer is present
	if !strings.Contains(content, "linkedin.com/in/qa-lead-rostyslav-chabria") {
		// Add footer after content
		content = strings.TrimSpace(content) + "\n\n" + footer
	}

	return content
}

// GenerateContent creates LinkedIn post content for a topic
func (c *Client) GenerateContent(ctx context.Context, topic *models.Topic, brandVoice string) (*GeneratedContent, error) {
	systemPrompt := fmt.Sprintf(ContentGenerationSystemPrompt, brandVoice)

	// Get suggested angle from AI metadata if available
	suggestedAngle := ""
	if topic.RawData != nil {
		if angle, ok := topic.RawData["suggested_angle"].(string); ok {
			suggestedAngle = angle
		}
	}

	userPrompt := fmt.Sprintf(ContentGenerationUserPrompt,
		topic.Title,
		suggestedAngle,
		topic.Description,
	)

	response, err := c.CompleteWithJSON(ctx, systemPrompt, userPrompt)
	if err != nil {
		return nil, err
	}

	var content GeneratedContent
	if err := json.Unmarshal([]byte(stripMarkdownCodeBlock(response)), &content); err != nil {
		c.log.Error().
			Err(err).
			Str("response", response).
			Msg("Failed to parse content response")
		return nil, fmt.Errorf("failed to parse content response: %w", err)
	}

	// Post-process to ensure header and footer are present
	content.Content = postProcessContent(content.Content)
	c.log.Info().
		Str("content_start", content.Content[:min(60, len(content.Content))]).
		Bool("has_header", strings.HasPrefix(content.Content, "Tech Insights")).
		Bool("has_footer", strings.Contains(content.Content, "linkedin.com/in/qa-lead")).
		Msg("Post-processed content")

	return &content, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// GeneratedPoll represents an AI-generated LinkedIn poll
type GeneratedPoll struct {
	Question  string   `json:"question"`
	Options   []string `json:"options"`
	IntroText string   `json:"intro_text"`
	Hashtags  []string `json:"hashtags"`
}

// GeneratePoll creates a LinkedIn poll for a topic
func (c *Client) GeneratePoll(ctx context.Context, topic *models.Topic, brandVoice string) (*GeneratedPoll, error) {
	systemPrompt := fmt.Sprintf(ContentGenerationSystemPrompt, brandVoice)

	userPrompt := fmt.Sprintf(PollGenerationUserPrompt,
		topic.Title,
		topic.Description,
	)

	response, err := c.CompleteWithJSON(ctx, systemPrompt, userPrompt)
	if err != nil {
		return nil, err
	}

	var poll GeneratedPoll
	if err := json.Unmarshal([]byte(stripMarkdownCodeBlock(response)), &poll); err != nil {
		c.log.Error().
			Err(err).
			Str("response", response).
			Msg("Failed to parse poll response")
		return nil, fmt.Errorf("failed to parse poll response: %w", err)
	}

	return &poll, nil
}

// ExpandedTopic represents an AI-expanded topic from a keyword
type ExpandedTopic struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Angle       string `json:"angle"`
	Timeliness  string `json:"timeliness"`
}

// ExpandKeyword expands a keyword into specific topic ideas
func (c *Client) ExpandKeyword(ctx context.Context, keyword string) ([]*ExpandedTopic, error) {
	userPrompt := fmt.Sprintf(TopicExpansionUserPrompt, keyword)

	response, err := c.CompleteWithJSON(ctx, TopicExpansionSystemPrompt, userPrompt)
	if err != nil {
		return nil, err
	}

	var result struct {
		Topics []*ExpandedTopic `json:"topics"`
	}
	if err := json.Unmarshal([]byte(stripMarkdownCodeBlock(response)), &result); err != nil {
		c.log.Error().
			Err(err).
			Str("response", response).
			Msg("Failed to parse expansion response")
		return nil, fmt.Errorf("failed to parse expansion response: %w", err)
	}

	return result.Topics, nil
}

// DigestTopic represents a topic for the daily digest
type DigestTopic struct {
	Title       string
	Description string
	Source      string
}

// GeneratedDigest represents an AI-generated daily news digest
type GeneratedDigest struct {
	Content  string   `json:"content"`
	Hashtags []string `json:"hashtags"`
	Hook     string   `json:"hook"`
	CTA      string   `json:"cta"`
}

// postProcessDigestContent ensures header and footer are present with correct date
func postProcessDigestContent(content string) string {
	today := time.Now().Format("Jan 2, 2006")
	header := "Daily Updates from Ros - " + today
	footer := "---\nLinkedIn: https://www.linkedin.com/in/qa-lead-rostyslav-chabria/\nInstagram: https://www.instagram.com/rostislav_cha"

	// Replace unicode box-drawing characters with simple dashes
	content = strings.ReplaceAll(content, "━", "-")

	// Fix header with correct date
	if strings.HasPrefix(content, "Daily Updates from Ros") {
		if idx := strings.Index(content, "\n"); idx != -1 {
			content = header + content[idx:]
		}
	} else {
		content = header + "\n\n" + content
	}

	// Ensure footer is present
	if !strings.Contains(content, "linkedin.com/in/qa-lead-rostyslav-chabria") {
		content = strings.TrimSpace(content) + "\n\n" + footer
	}

	return content
}

// GenerateDigest creates a daily news digest post from top 3 topics
func (c *Client) GenerateDigest(ctx context.Context, topics []DigestTopic, brandVoice string) (*GeneratedDigest, error) {
	if len(topics) < 3 {
		return nil, fmt.Errorf("digest requires at least 3 topics, got %d", len(topics))
	}

	systemPrompt := fmt.Sprintf(DigestGenerationSystemPrompt, brandVoice)

	userPrompt := fmt.Sprintf(DigestGenerationUserPrompt,
		topics[0].Title, topics[0].Description, topics[0].Source,
		topics[1].Title, topics[1].Description, topics[1].Source,
		topics[2].Title, topics[2].Description, topics[2].Source,
	)

	response, err := c.CompleteWithJSON(ctx, systemPrompt, userPrompt)
	if err != nil {
		return nil, err
	}

	var digest GeneratedDigest
	if err := json.Unmarshal([]byte(stripMarkdownCodeBlock(response)), &digest); err != nil {
		c.log.Error().
			Err(err).
			Str("response", response).
			Msg("Failed to parse digest response")
		return nil, fmt.Errorf("failed to parse digest response: %w", err)
	}

	// Post-process to ensure correct date in header and footer
	digest.Content = postProcessDigestContent(digest.Content)

	return &digest, nil
}
