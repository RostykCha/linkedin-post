package linkedin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// CreateCommentRequest represents a comment creation request
type CreateCommentRequest struct {
	Actor   string `json:"actor"`
	Message string `json:"message"`
}

// CommentResponse represents the response from creating a comment
type CommentResponse struct {
	ID string `json:"id"`
}

// CreateComment posts a comment on a LinkedIn post
func (c *Client) CreateComment(ctx context.Context, postURN, content string) (string, error) {
	// Get user profile for actor URN
	profile, err := c.GetProfile(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get profile: %w", err)
	}

	actorURN := fmt.Sprintf("urn:li:person:%s", profile.Sub)

	// Sanitize comment content
	content = sanitizeForLinkedIn(content)

	// LinkedIn comments endpoint
	// POST /socialActions/{postUrn}/comments
	endpoint := fmt.Sprintf("/socialActions/%s/comments", postURN)

	reqBody := map[string]interface{}{
		"actor":   actorURN,
		"message": content,
	}

	resp, err := c.do(ctx, "POST", endpoint, reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to create comment: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		c.log.Error().
			Int("status", resp.StatusCode).
			Str("body", string(body)).
			Msg("Failed to create comment")
		return "", fmt.Errorf("failed to create comment: %s - %s", resp.Status, string(body))
	}

	// Extract comment ID from response
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err == nil {
		if id, ok := result["id"].(string); ok {
			c.log.Info().
				Str("comment_id", id).
				Str("post_urn", postURN).
				Msg("Comment created successfully")
			return id, nil
		}
	}

	// Try to get from header
	commentID := resp.Header.Get("x-restli-id")
	if commentID == "" {
		commentID = resp.Header.Get("Location")
	}

	c.log.Info().
		Str("comment_id", commentID).
		Str("post_urn", postURN).
		Msg("Comment created successfully")

	return commentID, nil
}

// LinkedInPost represents a post from the LinkedIn API
type LinkedInPost struct {
	URN          string `json:"id"`
	Author       string `json:"author"`
	Commentary   string `json:"commentary"`
	PublishedAt  int64  `json:"publishedAt"`
	LikeCount    int    `json:"likeCount"`
	CommentCount int    `json:"commentCount"`
}

// GetPostsByAuthor fetches recent posts from a specific author
// Note: This requires the r_organization_social or specific permissions
func (c *Client) GetPostsByAuthor(ctx context.Context, authorURN string, count int) ([]*LinkedInPost, error) {
	if count <= 0 {
		count = 10
	}
	if count > 50 {
		count = 50
	}

	// LinkedIn API endpoint for fetching posts by author
	// Note: This may require specific API permissions
	endpoint := fmt.Sprintf("/posts?author=%s&count=%d&sortBy=LAST_MODIFIED", authorURN, count)

	resp, err := c.do(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch posts: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		c.log.Warn().
			Int("status", resp.StatusCode).
			Str("body", string(body)).
			Msg("Failed to fetch posts - API may require additional permissions")
		return nil, fmt.Errorf("failed to fetch posts: %s", resp.Status)
	}

	var result struct {
		Elements []LinkedInPost `json:"elements"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse posts response: %w", err)
	}

	posts := make([]*LinkedInPost, len(result.Elements))
	for i := range result.Elements {
		posts[i] = &result.Elements[i]
	}

	c.log.Debug().
		Str("author", authorURN).
		Int("count", len(posts)).
		Msg("Fetched posts")

	return posts, nil
}
