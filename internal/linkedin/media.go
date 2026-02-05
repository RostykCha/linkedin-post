package linkedin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/linkedin-agent/internal/models"
)

// InitializeUploadRequest is the request body for the Images API
type InitializeUploadRequest struct {
	InitializeUploadRequest InitializeUploadRequestInner `json:"initializeUploadRequest"`
}

// InitializeUploadRequestInner contains the owner for the upload
type InitializeUploadRequestInner struct {
	Owner string `json:"owner"`
}

// InitializeUploadResponse is the response from the Images API
type InitializeUploadResponse struct {
	Value InitializeUploadValue `json:"value"`
}

// InitializeUploadValue contains the upload details
type InitializeUploadValue struct {
	UploadURLExpiresAt int64  `json:"uploadUrlExpiresAt"`
	UploadURL          string `json:"uploadUrl"`
	Image              string `json:"image"` // urn:li:image:xxx
}

// ImagePostRequest represents a post with an image attachment
type ImagePostRequest struct {
	Author                    string       `json:"author"`
	Commentary                string       `json:"commentary"`
	Visibility                string       `json:"visibility"`
	Distribution              Distribution `json:"distribution"`
	LifecycleState            string       `json:"lifecycleState"`
	IsReshareDisabledByAuthor bool         `json:"isReshareDisabledByAuthor"`
	Content                   PostContent  `json:"content"`
}

// PostContent contains the media content for the post
type PostContent struct {
	Media Media `json:"media"`
}

// Media represents the media attachment
type Media struct {
	ID string `json:"id"` // The image URN (urn:li:image:xxx)
}

// InitializeImageUpload initializes an image upload with LinkedIn's Images API
func (c *Client) InitializeImageUpload(ctx context.Context, ownerURN string) (*InitializeUploadValue, error) {
	reqBody := InitializeUploadRequest{
		InitializeUploadRequest: InitializeUploadRequestInner{
			Owner: ownerURN,
		},
	}

	// Use the REST API endpoint for images
	resp, err := c.doREST(ctx, "POST", "/images?action=initializeUpload", reqBody)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		c.log.Error().
			Int("status", resp.StatusCode).
			Str("body", string(body)).
			Msg("Failed to initialize image upload")
		return nil, fmt.Errorf("failed to initialize upload: %s - %s", resp.Status, string(body))
	}

	var uploadResp InitializeUploadResponse
	if err := json.Unmarshal(body, &uploadResp); err != nil {
		return nil, fmt.Errorf("failed to parse upload response: %w", err)
	}

	c.log.Info().
		Str("image", uploadResp.Value.Image).
		Str("upload_url", uploadResp.Value.UploadURL[:min(80, len(uploadResp.Value.UploadURL))]).
		Msg("Image upload initialized successfully")

	return &uploadResp.Value, nil
}

// UploadImageToURL uploads image data to the provided upload URL
func (c *Client) UploadImageToURL(ctx context.Context, uploadURL string, imageData []byte) error {
	req, err := http.NewRequestWithContext(ctx, "PUT", uploadURL, bytes.NewReader(imageData))
	if err != nil {
		return fmt.Errorf("failed to create upload request: %w", err)
	}

	// Set content type based on image data (assume JPEG for now, could detect)
	req.Header.Set("Content-Type", "application/octet-stream")

	// Don't use the OAuth client for the upload URL - it's a pre-signed URL
	httpClient := &http.Client{Timeout: 60 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("upload request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		c.log.Error().
			Int("status", resp.StatusCode).
			Str("body", string(body)).
			Msg("Failed to upload image")
		return fmt.Errorf("upload failed: %s - %s", resp.Status, string(body))
	}

	c.log.Info().
		Int("size_bytes", len(imageData)).
		Msg("Image uploaded successfully")

	return nil
}

// CreatePostWithImage creates a LinkedIn post with an attached image
func (c *Client) CreatePostWithImage(ctx context.Context, post *models.Post, imageURN string) (string, error) {
	// Get user profile to get the author URN
	profile, err := c.GetProfile(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get profile: %w", err)
	}

	// Sanitize content
	content := sanitizeForLinkedIn(post.Content)

	// Truncate if needed
	if len(content) > maxCommentaryLength {
		c.log.Warn().
			Int("original_length", len(content)).
			Int("max_length", maxCommentaryLength).
			Msg("Content exceeds LinkedIn limit, truncating")
		content = content[:maxCommentaryLength-3] + "..."
	}

	postReq := ImagePostRequest{
		Author:     fmt.Sprintf("urn:li:person:%s", profile.Sub),
		Commentary: content,
		Visibility: "PUBLIC",
		Distribution: Distribution{
			FeedDistribution:               "MAIN_FEED",
			TargetEntities:                 []interface{}{},
			ThirdPartyDistributionChannels: []interface{}{},
		},
		LifecycleState:            "PUBLISHED",
		IsReshareDisabledByAuthor: false,
		Content: PostContent{
			Media: Media{
				ID: imageURN,
			},
		},
	}

	resp, err := c.do(ctx, "POST", "/posts", postReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		c.log.Error().
			Int("status", resp.StatusCode).
			Str("body", string(body)).
			Msg("Failed to create image post")
		return "", fmt.Errorf("failed to create image post: %s - %s", resp.Status, string(body))
	}

	postURN := resp.Header.Get("x-restli-id")
	if postURN == "" {
		postURN = resp.Header.Get("Location")
	}

	c.log.Info().
		Str("post_urn", postURN).
		Str("image_urn", imageURN).
		Msg("Image post created successfully")

	return postURN, nil
}

// UploadAndCreateImagePost is a convenience method that handles the full image upload flow
func (c *Client) UploadAndCreateImagePost(ctx context.Context, post *models.Post, imageData []byte) (string, string, error) {
	// Get user profile
	profile, err := c.GetProfile(ctx)
	if err != nil {
		return "", "", fmt.Errorf("failed to get profile: %w", err)
	}
	ownerURN := fmt.Sprintf("urn:li:person:%s", profile.Sub)

	// Step 1: Initialize the upload using Images API
	uploadInfo, err := c.InitializeImageUpload(ctx, ownerURN)
	if err != nil {
		return "", "", fmt.Errorf("failed to initialize upload: %w", err)
	}

	// Step 2: Upload the image to the provided URL
	if err := c.UploadImageToURL(ctx, uploadInfo.UploadURL, imageData); err != nil {
		return "", "", fmt.Errorf("failed to upload image: %w", err)
	}

	// Wait for LinkedIn to process the uploaded image
	time.Sleep(2 * time.Second)

	// Step 3: Create the post with the image URN
	postURN, err := c.CreatePostWithImage(ctx, post, uploadInfo.Image)
	if err != nil {
		return "", "", fmt.Errorf("failed to create image post: %w", err)
	}

	return postURN, uploadInfo.Image, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
