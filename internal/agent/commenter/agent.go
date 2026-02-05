package commenter

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"time"

	"github.com/linkedin-agent/internal/ai"
	"github.com/linkedin-agent/internal/config"
	"github.com/linkedin-agent/internal/linkedin"
	"github.com/linkedin-agent/internal/models"
	"github.com/linkedin-agent/internal/storage"
	"github.com/linkedin-agent/pkg/logger"
)

// Agent handles automated commenting on LinkedIn posts
type Agent struct {
	aiClient       *ai.Client
	linkedinClient *linkedin.Client
	repository     storage.Repository
	config         config.CommenterConfig
	log            *logger.Logger
}

// NewAgent creates a new commenter agent
func NewAgent(
	aiClient *ai.Client,
	linkedinClient *linkedin.Client,
	repository storage.Repository,
	commenterConfig config.CommenterConfig,
	log *logger.Logger,
) *Agent {
	return &Agent{
		aiClient:       aiClient,
		linkedinClient: linkedinClient,
		repository:     repository,
		config:         commenterConfig,
		log:            log.WithComponent("commenter"),
	}
}

// CommentResult contains the result of a comment run
type CommentResult struct {
	PostsDiscovered  int
	CommentsGenerated int
	CommentsPosted   int
	CommentsSkipped  int
	Errors           []error
	Duration         time.Duration
}

// Run executes the comment automation workflow
func (a *Agent) Run(ctx context.Context) (*CommentResult, error) {
	startTime := time.Now()
	result := &CommentResult{}

	if !a.config.Enabled {
		a.log.Info().Msg("Commenter is disabled")
		return result, nil
	}

	// Check if within active hours
	if !a.isWithinActiveHours() {
		a.log.Info().
			Int("current_hour", time.Now().Hour()).
			Int("active_start", a.config.ActiveHoursStart).
			Int("active_end", a.config.ActiveHoursEnd).
			Msg("Outside active hours, skipping")
		return result, nil
	}

	// Check if enough time has passed since last comment
	canComment, waitTime := a.canCommentNow(ctx)
	if !canComment {
		a.log.Info().
			Dur("wait_time", waitTime).
			Msg("Too soon since last comment, skipping")
		return result, nil
	}

	// Check daily limit
	todayCount, err := a.repository.GetTodayCommentCount(ctx)
	if err != nil {
		a.log.Warn().Err(err).Msg("Failed to get today's comment count")
	} else if todayCount >= a.config.MaxCommentsPerDay {
		a.log.Info().
			Int("today_count", todayCount).
			Int("max_per_day", a.config.MaxCommentsPerDay).
			Msg("Daily comment limit reached")
		return result, nil
	}

	// Discover and rank posts to comment on
	posts, err := a.discoverPosts(ctx)
	if err != nil {
		a.log.Error().Err(err).Msg("Failed to discover posts")
		result.Errors = append(result.Errors, err)
		return result, err
	}

	// Filter by age
	posts = a.filterPostsByAge(posts)

	// Rank by engagement velocity
	posts = a.rankPosts(posts)

	result.PostsDiscovered = len(posts)

	if len(posts) == 0 {
		a.log.Info().Msg("No posts found to comment on")
		result.Duration = time.Since(startTime)
		return result, nil
	}

	// Only post ONE comment per run (timing controls handle frequency)
	for _, post := range posts {
		// Check if we already commented on this post
		existing, _ := a.repository.GetCommentByTargetURN(ctx, post.URN)
		if existing != nil {
			a.log.Debug().Str("post_urn", post.URN).Msg("Already commented on this post, skipping")
			result.CommentsSkipped++
			continue
		}

		// Get the comment style to use
		style := a.getNextCommentStyle(ctx)

		// Generate and post comment
		if err := a.generateAndPostCommentWithStyle(ctx, post, style); err != nil {
			a.log.Warn().Err(err).Str("post_urn", post.URN).Msg("Failed to comment on post")
			result.Errors = append(result.Errors, err)
			result.CommentsSkipped++
			continue
		}

		result.CommentsGenerated++
		result.CommentsPosted++

		// Only post one comment per run
		break
	}

	result.Duration = time.Since(startTime)

	a.log.Info().
		Int("discovered", result.PostsDiscovered).
		Int("generated", result.CommentsGenerated).
		Int("posted", result.CommentsPosted).
		Int("skipped", result.CommentsSkipped).
		Dur("duration", result.Duration).
		Msg("Comment run completed")

	return result, nil
}

// discoverPosts finds posts to comment on from target influencers
func (a *Agent) discoverPosts(ctx context.Context) ([]*models.TargetPost, error) {
	var allPosts []*models.TargetPost

	// Fetch posts from target influencers
	for _, influencer := range a.config.TargetInfluencers {
		// Resolve username/identifier to URN
		influencerURN, err := a.linkedinClient.ResolveToURN(ctx, influencer)
		if err != nil {
			a.log.Warn().
				Err(err).
				Str("influencer", influencer).
				Msg("Failed to resolve influencer to URN, skipping")
			continue
		}

		posts, err := a.linkedinClient.GetPostsByAuthor(ctx, influencerURN, 5)
		if err != nil {
			a.log.Warn().
				Err(err).
				Str("influencer", influencerURN).
				Msg("Failed to fetch posts from influencer")
			continue
		}

		for _, post := range posts {
			// Filter by engagement threshold
			engagement := post.LikeCount + post.CommentCount

			// Skip posts with too little engagement
			if engagement < a.config.MinPostEngagement {
				a.log.Debug().
					Str("post_urn", post.URN).
					Int("engagement", engagement).
					Int("min_required", a.config.MinPostEngagement).
					Msg("Skipping post: engagement too low")
				continue
			}

			// Skip mega-viral posts where comments get buried
			if a.config.MaxPostEngagement > 0 && engagement > a.config.MaxPostEngagement {
				a.log.Debug().
					Str("post_urn", post.URN).
					Int("engagement", engagement).
					Int("max_allowed", a.config.MaxPostEngagement).
					Msg("Skipping post: too viral, comment would get buried")
				continue
			}

			allPosts = append(allPosts, &models.TargetPost{
				URN:          post.URN,
				AuthorURN:    post.Author,
				AuthorName:   "", // Could be populated if we fetch profile
				Content:      post.Commentary,
				LikeCount:    post.LikeCount,
				CommentCount: post.CommentCount,
				PublishedAt:  time.Unix(post.PublishedAt/1000, 0),
			})
		}
	}

	a.log.Debug().
		Int("influencers", len(a.config.TargetInfluencers)).
		Int("posts_found", len(allPosts)).
		Msg("Post discovery completed")

	return allPosts, nil
}

// generateAndPostComment creates and posts a comment on a target post (uses configured style)
func (a *Agent) generateAndPostComment(ctx context.Context, post *models.TargetPost) error {
	return a.generateAndPostCommentWithStyle(ctx, post, a.config.CommentStyle)
}

// generateAndPostCommentWithStyle creates and posts a comment with a specific style
func (a *Agent) generateAndPostCommentWithStyle(ctx context.Context, post *models.TargetPost, style string) error {
	// Truncate content for AI if too long
	content := post.Content
	if len(content) > 1000 {
		content = content[:1000] + "..."
	}

	// Generate comment using AI
	generated, err := a.aiClient.GenerateComment(ctx, post.AuthorName, content, style)
	if err != nil {
		return fmt.Errorf("failed to generate comment: %w", err)
	}

	// Calculate engagement at time of comment
	engagement := post.LikeCount + post.CommentCount

	// Create comment record with new fields
	now := time.Now()
	comment := &models.Comment{
		TargetPostURN:    post.URN,
		TargetAuthorURN:  post.AuthorURN,
		TargetAuthorName: post.AuthorName,
		TargetPostTitle:  truncate(post.Content, 100),
		Content:          generated.Comment,
		Status:           models.CommentStatusPending,
		CommentStyle:     style,
		AIReasoning:      generated.Reasoning,
		PostEngagement:   engagement,
	}

	// Save to database first
	if err := a.repository.CreateComment(ctx, comment); err != nil {
		return fmt.Errorf("failed to save comment: %w", err)
	}

	// Post to LinkedIn
	commentURN, err := a.linkedinClient.CreateComment(ctx, post.URN, generated.Comment)
	if err != nil {
		comment.Status = models.CommentStatusFailed
		comment.ErrorMessage = err.Error()
		a.repository.UpdateComment(ctx, comment)
		return fmt.Errorf("failed to post comment: %w", err)
	}

	// Update comment record with success
	comment.Status = models.CommentStatusPosted
	comment.CommentURN = commentURN
	comment.PostedAt = &now
	if err := a.repository.UpdateComment(ctx, comment); err != nil {
		a.log.Warn().Err(err).Msg("Failed to update comment status")
	}

	a.log.Info().
		Str("post_urn", post.URN).
		Str("comment_urn", commentURN).
		Str("style", style).
		Int("comment_length", len(generated.Comment)).
		Float64("engagement_velocity", post.EngagementVelocity).
		Msg("Comment posted successfully")

	return nil
}

// GenerateCommentPreview generates a comment without posting (for review)
func (a *Agent) GenerateCommentPreview(ctx context.Context, postURN, authorName, content string) (*models.Comment, error) {
	generated, err := a.aiClient.GenerateComment(ctx, authorName, content, a.config.CommentStyle)
	if err != nil {
		return nil, fmt.Errorf("failed to generate comment: %w", err)
	}

	comment := &models.Comment{
		TargetPostURN:    postURN,
		TargetAuthorName: authorName,
		TargetPostTitle:  truncate(content, 100),
		Content:          generated.Comment,
		Status:           models.CommentStatusPending,
	}

	return comment, nil
}

// PostComment posts a previously generated comment
func (a *Agent) PostComment(ctx context.Context, commentID uint) error {
	// Get comment from database
	status := models.CommentStatusPending
	comments, err := a.repository.ListComments(ctx, storage.CommentFilter{
		Status: &status,
		Limit:  100,
	})
	if err != nil {
		return fmt.Errorf("failed to list comments: %w", err)
	}

	var comment *models.Comment
	for _, c := range comments {
		if c.ID == commentID {
			comment = c
			break
		}
	}

	if comment == nil {
		return fmt.Errorf("comment not found or not in pending status")
	}

	// Post to LinkedIn
	commentURN, err := a.linkedinClient.CreateComment(ctx, comment.TargetPostURN, comment.Content)
	if err != nil {
		comment.Status = models.CommentStatusFailed
		comment.ErrorMessage = err.Error()
		a.repository.UpdateComment(ctx, comment)
		return fmt.Errorf("failed to post comment: %w", err)
	}

	// Update status
	comment.Status = models.CommentStatusPosted
	comment.CommentURN = commentURN
	return a.repository.UpdateComment(ctx, comment)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// isWithinActiveHours checks if current time is within configured active hours
func (a *Agent) isWithinActiveHours() bool {
	hour := time.Now().Hour()
	return hour >= a.config.ActiveHoursStart && hour < a.config.ActiveHoursEnd
}

// canCommentNow checks if enough time has passed since the last comment
func (a *Agent) canCommentNow(ctx context.Context) (bool, time.Duration) {
	lastComment, err := a.repository.GetLastCommentTime(ctx)
	if err != nil || lastComment == nil {
		// No previous comments, can comment now
		return true, 0
	}

	minInterval := time.Duration(a.config.MinIntervalMinutes) * time.Minute
	elapsed := time.Since(*lastComment)

	if elapsed >= minInterval {
		return true, 0
	}

	return false, minInterval - elapsed
}

// getRandomInterval returns a random interval between min and max configured values
func (a *Agent) getRandomInterval() time.Duration {
	minMinutes := a.config.MinIntervalMinutes
	maxMinutes := a.config.MaxIntervalMinutes

	if maxMinutes <= minMinutes {
		return time.Duration(minMinutes) * time.Minute
	}

	randomMinutes := minMinutes + rand.Intn(maxMinutes-minMinutes)
	return time.Duration(randomMinutes) * time.Minute
}

// getNextCommentStyle returns the next comment style using rotation
func (a *Agent) getNextCommentStyle(ctx context.Context) string {
	// If rotation is disabled, use the configured style
	if !a.config.CommentStyleRotation {
		return a.config.CommentStyle
	}

	styles := a.config.CommentStyles
	if len(styles) == 0 {
		styles = []string{"insightful", "question", "supportive"}
	}

	// Get recent styles to avoid repetition
	recentStyles, err := a.repository.GetRecentCommentStyles(ctx, 5)
	if err != nil || len(recentStyles) == 0 {
		// No history, pick randomly
		return styles[rand.Intn(len(styles))]
	}

	// Count occurrences of each style
	styleCounts := make(map[string]int)
	for _, s := range styles {
		styleCounts[s] = 0
	}
	for _, s := range recentStyles {
		styleCounts[s]++
	}

	// Avoid using the most recent style
	lastStyle := recentStyles[0]

	// Find the least used style that isn't the last one used
	var bestStyle string
	minCount := len(recentStyles) + 1

	for _, s := range styles {
		if s == lastStyle {
			continue // Skip the most recent style
		}
		if styleCounts[s] < minCount {
			minCount = styleCounts[s]
			bestStyle = s
		}
	}

	if bestStyle == "" {
		// All styles are the same as last, pick any other
		for _, s := range styles {
			if s != lastStyle {
				return s
			}
		}
		return styles[0]
	}

	return bestStyle
}

// rankPosts sorts posts by engagement velocity (higher velocity = better)
func (a *Agent) rankPosts(posts []*models.TargetPost) []*models.TargetPost {
	for _, post := range posts {
		hoursSincePost := time.Since(post.PublishedAt).Hours()
		if hoursSincePost < 0.5 {
			hoursSincePost = 0.5 // Minimum 30 minutes to avoid division issues
		}
		// Engagement velocity: (likes + comments*2) / hours
		// Comments weighted more as they indicate deeper engagement
		post.EngagementVelocity = float64(post.LikeCount+post.CommentCount*2) / hoursSincePost
	}

	// Sort by engagement velocity descending
	sort.Slice(posts, func(i, j int) bool {
		return posts[i].EngagementVelocity > posts[j].EngagementVelocity
	})

	return posts
}

// filterPostsByAge filters posts based on configured age limits
func (a *Agent) filterPostsByAge(posts []*models.TargetPost) []*models.TargetPost {
	var filtered []*models.TargetPost

	minAge := time.Duration(a.config.MinPostAgeMinutes) * time.Minute
	maxAge := time.Duration(a.config.MaxPostAgeHours) * time.Hour

	for _, post := range posts {
		age := time.Since(post.PublishedAt)

		// Skip posts that are too new
		if age < minAge {
			a.log.Debug().
				Str("post_urn", post.URN).
				Dur("age", age).
				Dur("min_age", minAge).
				Msg("Skipping post: too new")
			continue
		}

		// Skip posts that are too old
		if age > maxAge {
			a.log.Debug().
				Str("post_urn", post.URN).
				Dur("age", age).
				Dur("max_age", maxAge).
				Msg("Skipping post: too old")
			continue
		}

		filtered = append(filtered, post)
	}

	return filtered
}
