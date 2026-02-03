package publisher

import (
	"context"
	"fmt"
	"time"

	"github.com/linkedin-agent/internal/ai"
	"github.com/linkedin-agent/internal/config"
	"github.com/linkedin-agent/internal/linkedin"
	"github.com/linkedin-agent/internal/models"
	"github.com/linkedin-agent/internal/storage"
	"github.com/linkedin-agent/pkg/logger"
)

// Agent handles content generation and publishing to LinkedIn
type Agent struct {
	aiClient       *ai.Client
	linkedinClient *linkedin.Client
	repository     storage.Repository
	config         config.PublishingConfig
	log            *logger.Logger
}

// NewAgent creates a new publisher agent
func NewAgent(
	aiClient *ai.Client,
	linkedinClient *linkedin.Client,
	repository storage.Repository,
	publishConfig config.PublishingConfig,
	log *logger.Logger,
) *Agent {
	return &Agent{
		aiClient:       aiClient,
		linkedinClient: linkedinClient,
		repository:     repository,
		config:         publishConfig,
		log:            log.WithComponent("publisher"),
	}
}

// GenerateResult contains the result of content generation
type GenerateResult struct {
	Post    *models.Post
	Preview string
}

// GenerateContent creates content for a topic
func (a *Agent) GenerateContent(ctx context.Context, topicID uint, postType models.PostType) (*GenerateResult, error) {
	// Get topic
	topic, err := a.repository.GetTopicByID(ctx, topicID)
	if err != nil {
		return nil, fmt.Errorf("topic not found: %w", err)
	}

	a.log.Info().
		Uint("topic_id", topicID).
		Str("post_type", string(postType)).
		Msg("Generating content")

	var post *models.Post

	switch postType {
	case models.PostTypePoll:
		poll, err := a.aiClient.GeneratePoll(ctx, topic, a.config.BrandVoice)
		if err != nil {
			return nil, fmt.Errorf("failed to generate poll: %w", err)
		}

		post = &models.Post{
			TopicID:          &topic.ID,
			Content:          poll.IntroText,
			PostType:         models.PostTypePoll,
			GenerationPrompt: fmt.Sprintf("Generate poll for: %s", topic.Title),
			PostFormat: models.JSON{
				"question": poll.Question,
				"options":  poll.Options,
				"duration": "THREE_DAYS",
			},
			AIMetadata: models.JSON{
				"hashtags": poll.Hashtags,
			},
			Status: models.PostStatusDraft,
		}

	default: // Text post
		content, err := a.aiClient.GenerateContent(ctx, topic, a.config.BrandVoice)
		if err != nil {
			return nil, fmt.Errorf("failed to generate content: %w", err)
		}

		// Append hashtags to content
		fullContent := content.Content
		if len(content.Hashtags) > 0 {
			fullContent += "\n\n"
			for _, tag := range content.Hashtags {
				if tag[0] != '#' {
					fullContent += "#"
				}
				fullContent += tag + " "
			}
		}

		post = &models.Post{
			TopicID:          &topic.ID,
			Content:          fullContent,
			PostType:         models.PostTypeText,
			GenerationPrompt: fmt.Sprintf("Generate LinkedIn post for: %s", topic.Title),
			AIMetadata: models.JSON{
				"hook":     content.Hook,
				"cta":      content.CTA,
				"hashtags": content.Hashtags,
			},
			Status: models.PostStatusDraft,
		}
	}

	// Save draft
	if err := a.repository.CreatePost(ctx, post); err != nil {
		return nil, fmt.Errorf("failed to save post: %w", err)
	}

	// Determine if should auto-publish based on hybrid approval mode
	if topic.IsHighScore() && a.config.AutoApprove {
		post.Status = models.PostStatusScheduled
		now := time.Now()
		post.ScheduledFor = &now
		if err := a.repository.UpdatePost(ctx, post); err != nil {
			a.log.Warn().Err(err).Msg("Failed to schedule high-score post")
		}
	}

	a.log.Info().
		Uint("post_id", post.ID).
		Float64("topic_score", topic.AIScore).
		Bool("auto_scheduled", post.Status == models.PostStatusScheduled).
		Msg("Content generated")

	return &GenerateResult{
		Post:    post,
		Preview: post.Content,
	}, nil
}

// PublishResult contains the result of publishing
type PublishResult struct {
	PostID      uint
	LinkedInURN string
	Published   bool
	Error       error
}

// Publish publishes a post to LinkedIn
func (a *Agent) Publish(ctx context.Context, postID uint) (*PublishResult, error) {
	result := &PublishResult{PostID: postID}

	// Get post
	post, err := a.repository.GetPostByID(ctx, postID)
	if err != nil {
		result.Error = fmt.Errorf("post not found: %w", err)
		return result, result.Error
	}

	// Check status
	if post.Status == models.PostStatusPublished {
		result.Error = fmt.Errorf("post already published")
		return result, result.Error
	}

	a.log.Info().
		Uint("post_id", postID).
		Str("post_type", string(post.PostType)).
		Msg("Publishing post")

	// Update status to publishing
	post.Status = models.PostStatusPublishing
	a.repository.UpdatePost(ctx, post)

	// Publish based on type
	var urn string
	switch post.PostType {
	case models.PostTypePoll:
		if post.PostFormat != nil {
			question, _ := post.PostFormat["question"].(string)
			optionsRaw, _ := post.PostFormat["options"].([]interface{})
			options := make([]string, len(optionsRaw))
			for i, o := range optionsRaw {
				options[i], _ = o.(string)
			}
			urn, err = a.linkedinClient.CreatePoll(ctx, question, options, 3)
		}
	default:
		urn, err = a.linkedinClient.CreatePost(ctx, post)
	}

	if err != nil {
		post.Status = models.PostStatusFailed
		post.ErrorMessage = err.Error()
		post.RetryCount++
		a.repository.UpdatePost(ctx, post)

		result.Error = err
		a.log.Error().
			Err(err).
			Uint("post_id", postID).
			Msg("Failed to publish post")
		return result, err
	}

	// Success
	now := time.Now()
	post.Status = models.PostStatusPublished
	post.LinkedInPostURN = urn
	post.PublishedAt = &now
	a.repository.UpdatePost(ctx, post)

	// Mark topic as used
	if post.TopicID != nil {
		if topic, err := a.repository.GetTopicByID(ctx, *post.TopicID); err == nil {
			topic.Status = models.TopicStatusUsed
			a.repository.UpdateTopic(ctx, topic)
		}
	}

	result.LinkedInURN = urn
	result.Published = true

	a.log.Info().
		Uint("post_id", postID).
		Str("linkedin_urn", urn).
		Msg("Post published successfully")

	return result, nil
}

// ProcessScheduledPosts publishes all scheduled posts that are due
func (a *Agent) ProcessScheduledPosts(ctx context.Context) (int, []error) {
	posts, err := a.repository.GetScheduledPosts(ctx, time.Now())
	if err != nil {
		return 0, []error{err}
	}

	var errors []error
	published := 0

	for _, post := range posts {
		result, err := a.Publish(ctx, post.ID)
		if err != nil {
			errors = append(errors, fmt.Errorf("post %d: %w", post.ID, err))
		} else if result.Published {
			published++
		}
	}

	return published, errors
}

// SchedulePost schedules a post for future publishing
func (a *Agent) SchedulePost(ctx context.Context, postID uint, scheduledFor time.Time) error {
	post, err := a.repository.GetPostByID(ctx, postID)
	if err != nil {
		return fmt.Errorf("post not found: %w", err)
	}

	if post.Status == models.PostStatusPublished {
		return fmt.Errorf("cannot schedule already published post")
	}

	post.Status = models.PostStatusScheduled
	post.ScheduledFor = &scheduledFor

	return a.repository.UpdatePost(ctx, post)
}

// ApprovePost approves a draft post for publishing
func (a *Agent) ApprovePost(ctx context.Context, postID uint) error {
	post, err := a.repository.GetPostByID(ctx, postID)
	if err != nil {
		return fmt.Errorf("post not found: %w", err)
	}

	if post.Status != models.PostStatusDraft {
		return fmt.Errorf("can only approve draft posts")
	}

	// Schedule for immediate publishing
	now := time.Now()
	post.Status = models.PostStatusScheduled
	post.ScheduledFor = &now

	return a.repository.UpdatePost(ctx, post)
}
