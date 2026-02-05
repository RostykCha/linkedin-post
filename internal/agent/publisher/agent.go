package publisher

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/linkedin-agent/internal/ai"
	"github.com/linkedin-agent/internal/config"
	"github.com/linkedin-agent/internal/linkedin"
	"github.com/linkedin-agent/internal/media/unsplash"
	"github.com/linkedin-agent/internal/models"
	"github.com/linkedin-agent/internal/storage"
	"github.com/linkedin-agent/internal/tracker"
	"github.com/linkedin-agent/pkg/logger"
)

// Agent handles content generation and publishing to LinkedIn
type Agent struct {
	aiClient        *ai.Client
	linkedinClient  *linkedin.Client
	repository      storage.Repository
	config          config.PublishingConfig
	mediaConfig     config.MediaConfig
	unsplashClient  *unsplash.Client
	log             *logger.Logger
	tracker         *tracker.SheetsTracker
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

// SetTracker sets the Google Sheets tracker for the agent
func (a *Agent) SetTracker(t *tracker.SheetsTracker) {
	a.tracker = t
}

// SetMediaConfig configures media/image support for the agent
func (a *Agent) SetMediaConfig(mediaCfg config.MediaConfig, unsplashClient *unsplash.Client) {
	a.mediaConfig = mediaCfg
	a.unsplashClient = unsplashClient
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

		// Use AI-generated content directly (post-processing adds header/footer in ai/ranking.go)
		fullContent := content.Content

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

	// Attach image if media is enabled (before saving so image info is persisted)
	if a.mediaConfig.Enabled && a.unsplashClient != nil && postType == models.PostTypeText {
		if err := a.AttachImageToPost(ctx, post, topic); err != nil {
			a.log.Warn().Err(err).Msg("Failed to attach image to post, will publish as text-only")
		}
	}

	// Save draft
	if err := a.repository.CreatePost(ctx, post); err != nil {
		return nil, fmt.Errorf("failed to save post: %w", err)
	}

	// Track in Google Sheets
	if a.tracker != nil {
		if err := a.tracker.TrackNewPost(ctx, topic, post); err != nil {
			a.log.Warn().Err(err).Msg("Failed to track post in Google Sheets")
		}
	}

	// Determine if should auto-publish based on hybrid approval mode
	if topic.IsHighScore() && a.config.AutoApprove {
		post.Status = models.PostStatusScheduled
		now := time.Now()
		post.ScheduledFor = &now
		if err := a.repository.UpdatePost(ctx, post); err != nil {
			a.log.Warn().Err(err).Msg("Failed to schedule high-score post")
		}
		// Update tracker with scheduled status
		if a.tracker != nil {
			a.tracker.UpdatePostScheduled(ctx, topic.ID, now)
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

// AttachImageToPost fetches an image from Unsplash and attaches it to the post
func (a *Agent) AttachImageToPost(ctx context.Context, post *models.Post, topic *models.Topic) error {
	if !a.mediaConfig.Enabled || a.unsplashClient == nil {
		return nil
	}

	// Generate search keywords from topic
	keywords, err := a.aiClient.GenerateImageSearchKeywords(ctx, topic)
	if err != nil {
		a.log.Warn().Err(err).Msg("Failed to generate image search keywords, using topic title")
		keywords = &ai.ImageSearchKeywords{Primary: topic.Title}
	}

	// Search for a photo
	photo, err := a.unsplashClient.GetBestPhoto(ctx, keywords.Primary)
	if err != nil {
		a.log.Warn().Err(err).Str("keyword", keywords.Primary).Msg("Failed to find image")
		return err
	}

	// Store the photo URL (we'll download and upload during publish)
	post.MediaType = models.MediaTypeImage
	post.MediaURL = photo.URLs.Regular

	// Store attribution in AI metadata
	if post.AIMetadata == nil {
		post.AIMetadata = models.JSON{}
	}
	post.AIMetadata["image_attribution"] = a.unsplashClient.GetAttribution(photo)
	post.AIMetadata["image_id"] = photo.ID

	a.log.Info().
		Str("photo_id", photo.ID).
		Str("photographer", photo.User.Name).
		Str("keyword", keywords.Primary).
		Msg("Image attached to post")

	return nil
}

// publishWithImage handles publishing a post with an image attachment
func (a *Agent) publishWithImage(ctx context.Context, post *models.Post) (string, error) {
	// Download directly from stored MediaURL
	if post.MediaURL == "" {
		a.log.Warn().Msg("No image URL found, falling back to text post")
		return a.linkedinClient.CreatePost(ctx, post)
	}

	a.log.Info().
		Str("media_url", post.MediaURL).
		Msg("Downloading image from stored URL")

	// Download the image directly from the stored URL
	imageData, err := a.downloadImageFromURL(ctx, post.MediaURL)
	if err != nil || len(imageData) == 0 {
		if a.mediaConfig.FallbackToText {
			a.log.Warn().Err(err).Msg("Failed to download image, falling back to text post")
			return a.linkedinClient.CreatePost(ctx, post)
		}
		return "", fmt.Errorf("failed to download image: %w", err)
	}

	// Upload to LinkedIn and create post
	postURN, assetURN, err := a.linkedinClient.UploadAndCreateImagePost(ctx, post, imageData)
	if err != nil {
		if a.mediaConfig.FallbackToText {
			a.log.Warn().Err(err).Msg("Failed to upload image to LinkedIn, falling back to text post")
			return a.linkedinClient.CreatePost(ctx, post)
		}
		return "", err
	}

	// Store the asset URN
	post.MediaAssetURN = assetURN

	a.log.Info().
		Str("post_urn", postURN).
		Str("asset_urn", assetURN).
		Int("image_size", len(imageData)).
		Msg("Image post published successfully")

	return postURN, nil
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
		Int("content_length", len(post.Content)).
		Str("content_start", post.Content[:min(100, len(post.Content))]).
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
		// Check if post has image to upload
		if post.MediaType == models.MediaTypeImage && post.MediaURL != "" && a.unsplashClient != nil {
			urn, err = a.publishWithImage(ctx, post)
		} else {
			urn, err = a.linkedinClient.CreatePost(ctx, post)
		}
	}

	if err != nil {
		post.Status = models.PostStatusFailed
		post.ErrorMessage = err.Error()
		post.RetryCount++
		a.repository.UpdatePost(ctx, post)

		// Track failure in Google Sheets
		if a.tracker != nil && post.TopicID != nil {
			a.tracker.UpdatePostFailed(ctx, *post.TopicID, err.Error())
		}

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
		// Track success in Google Sheets
		if a.tracker != nil {
			a.tracker.UpdatePostPublished(ctx, *post.TopicID, urn)
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

// GetTodayPublishCount returns the number of posts published today
func (a *Agent) GetTodayPublishCount(ctx context.Context) (int, error) {
	status := models.PostStatusPublished
	posts, err := a.repository.ListPosts(ctx, storage.PostFilter{
		Status: &status,
	})
	if err != nil {
		return 0, err
	}

	// Count posts published today
	today := time.Now().Truncate(24 * time.Hour)
	count := 0
	for _, p := range posts {
		if p.PublishedAt != nil && p.PublishedAt.After(today) {
			count++
		}
	}
	return count, nil
}

// GetMaxPostsPerDay returns the configured maximum posts per day
func (a *Agent) GetMaxPostsPerDay() int {
	return a.config.MaxPostsPerDay
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

// DigestResult contains the result of digest generation
type DigestResult struct {
	Post      *models.Post
	Preview   string
	TopicIDs  []uint
}

// GenerateDigest creates a daily digest post from the top 3 trending topics
func (a *Agent) GenerateDigest(ctx context.Context) (*DigestResult, error) {
	a.log.Info().Msg("Generating daily tech news digest")

	// Get top 3 approved topics by score
	topics, err := a.repository.GetTopTopics(ctx, 3, a.config.MinScoreThreshold)
	if err != nil {
		return nil, fmt.Errorf("failed to get top topics: %w", err)
	}

	if len(topics) < 3 {
		return nil, fmt.Errorf("not enough topics for digest (need 3, got %d)", len(topics))
	}

	// Convert to digest topics
	digestTopics := make([]ai.DigestTopic, 3)
	topicIDs := make([]uint, 3)
	for i, topic := range topics[:3] {
		digestTopics[i] = ai.DigestTopic{
			Title:       topic.Title,
			Description: topic.Description,
			Source:      topic.SourceName,
		}
		topicIDs[i] = topic.ID
	}

	// Generate digest content
	digest, err := a.aiClient.GenerateDigest(ctx, digestTopics, a.config.BrandVoice)
	if err != nil {
		return nil, fmt.Errorf("failed to generate digest: %w", err)
	}

	// Append hashtags to content
	fullContent := digest.Content
	if len(digest.Hashtags) > 0 {
		fullContent += "\n\n"
		for _, tag := range digest.Hashtags {
			if tag[0] != '#' {
				fullContent += "#"
			}
			fullContent += tag + " "
		}
	}

	// Create post (link to first topic for tracking)
	post := &models.Post{
		TopicID:          &topicIDs[0],
		Content:          fullContent,
		PostType:         models.PostTypeText,
		GenerationPrompt: "Daily tech news digest - top 3 stories",
		AIMetadata: models.JSON{
			"hook":      digest.Hook,
			"cta":       digest.CTA,
			"hashtags":  digest.Hashtags,
			"topic_ids": topicIDs,
			"is_digest": true,
		},
		Status: models.PostStatusDraft,
	}

	// Attach image if media is enabled (use first/top topic for image keywords)
	if a.mediaConfig.Enabled && a.unsplashClient != nil {
		if err := a.AttachImageToPost(ctx, post, topics[0]); err != nil {
			a.log.Warn().Err(err).Msg("Failed to attach image to digest, will publish as text-only")
		}
	}

	// Save draft
	if err := a.repository.CreatePost(ctx, post); err != nil {
		return nil, fmt.Errorf("failed to save digest post: %w", err)
	}

	// Auto-schedule if enabled
	if a.config.AutoApprove {
		post.Status = models.PostStatusScheduled
		now := time.Now()
		post.ScheduledFor = &now
		if err := a.repository.UpdatePost(ctx, post); err != nil {
			a.log.Warn().Err(err).Msg("Failed to auto-schedule digest")
		}
	}

	a.log.Info().
		Uint("post_id", post.ID).
		Uints("topic_ids", topicIDs).
		Msg("Daily digest generated")

	return &DigestResult{
		Post:     post,
		Preview:  post.Content,
		TopicIDs: topicIDs,
	}, nil
}

// downloadImageFromURL downloads an image from a URL and returns the raw bytes
func (a *Agent) downloadImageFromURL(ctx context.Context, imageURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", imageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("image download failed with status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read image data: %w", err)
	}

	a.log.Info().
		Int("size_bytes", len(data)).
		Msg("Image downloaded successfully")

	return data, nil
}
