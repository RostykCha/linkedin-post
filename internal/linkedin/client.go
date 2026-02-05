package linkedin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/linkedin-agent/internal/models"
	"github.com/linkedin-agent/pkg/logger"
	"github.com/linkedin-agent/pkg/ratelimit"
)

const (
	baseURL         = "https://api.linkedin.com/v2"
	restliVersion   = "2.0.0"
	linkedinVersion = "202401" // LinkedIn API version
)

// Client handles LinkedIn API requests
type Client struct {
	httpClient   *http.Client
	oauthManager *OAuthManager
	rateLimiter  *ratelimit.MultiLimiter
	log          *logger.Logger
}

// NewClient creates a new LinkedIn API client
func NewClient(oauth *OAuthManager, limiter *ratelimit.MultiLimiter, log *logger.Logger) *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		oauthManager: oauth,
		rateLimiter:  limiter,
		log:          log.WithComponent("linkedin"),
	}
}

// do performs an HTTP request with proper authentication and headers
func (c *Client) do(ctx context.Context, method, path string, body interface{}) (*http.Response, error) {
	// Wait for rate limiter
	if err := c.rateLimiter.Wait(ctx, ratelimit.LimiterLinkedIn); err != nil {
		return nil, fmt.Errorf("rate limit error: %w", err)
	}

	// Get valid token
	token, err := c.oauthManager.GetValidToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("authentication error: %w", err)
	}

	// Prepare request body
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(data)
		previewLen := 500
		if len(data) < previewLen {
			previewLen = len(data)
		}
		c.log.Info().
			Int("body_length", len(data)).
			Str("body_preview", string(data[:previewLen])).
			Msg("LinkedIn API request body")
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set required headers
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("X-Restli-Protocol-Version", restliVersion)
	req.Header.Set("LinkedIn-Version", linkedinVersion)
	req.Header.Set("Content-Type", "application/json")

	c.log.Debug().
		Str("method", method).
		Str("path", path).
		Msg("Making LinkedIn API request")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	// Log response status
	c.log.Debug().
		Int("status", resp.StatusCode).
		Msg("LinkedIn API response")

	return resp, nil
}

// GetProfile retrieves the authenticated user's profile
func (c *Client) GetProfile(ctx context.Context) (*Profile, error) {
	resp, err := c.do(ctx, "GET", "/userinfo", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get profile: %s - %s", resp.Status, string(body))
	}

	var profile Profile
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return nil, fmt.Errorf("failed to decode profile: %w", err)
	}

	return &profile, nil
}

// Profile represents a LinkedIn user profile
type Profile struct {
	Sub           string `json:"sub"` // LinkedIn member ID (URN format)
	Name          string `json:"name"`
	GivenName     string `json:"given_name"`
	FamilyName    string `json:"family_name"`
	Picture       string `json:"picture"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
}

// LinkedIn content limits
const maxCommentaryLength = 3000

// sanitizeForLinkedIn cleans content to ensure LinkedIn API accepts it properly
// LinkedIn's API can have issues with certain unicode characters
func sanitizeForLinkedIn(content string) string {
	// Replace common unicode box-drawing and decorative characters with ASCII equivalents
	replacements := map[string]string{
		"━": "-",
		"─": "-",
		"═": "=",
		"│": "|",
		"║": "|",
		"╔": "+",
		"╗": "+",
		"╚": "+",
		"╝": "+",
		"╠": "+",
		"╣": "+",
		"╦": "+",
		"╩": "+",
		"╬": "+",
		"┌": "+",
		"┐": "+",
		"└": "+",
		"┘": "+",
		"├": "+",
		"┤": "+",
		"┬": "+",
		"┴": "+",
		"┼": "+",
		"•": "-",
		"◦": "-",
		"▪": "-",
		"▫": "-",
		"►": ">",
		"◄": "<",
		"▲": "^",
		"▼": "v",
		"★": "*",
		"☆": "*",
		"✓": "[x]",
		"✗": "[ ]",
		"✔": "[x]",
		"✘": "[ ]",
		"□": "[ ]",
		"■": "[x]",
		"⚠": "[!]",
		"❌": "[x]",
		"✅": "[ok]",
		"→": "->",
		"←": "<-",
		"↑": "^",
		"↓": "v",
		"⇒": "=>",
		"⇐": "<=",
		"\u00A0": " ", // Non-breaking space
		"\u2003": " ", // Em space
		"\u2002": " ", // En space
		"\u2009": " ", // Thin space
		"\u200B": "",  // Zero-width space
		"\u200C": "",  // Zero-width non-joiner
		"\u200D": "",  // Zero-width joiner
		"\uFEFF": "",  // BOM
	}

	for old, new := range replacements {
		content = strings.ReplaceAll(content, old, new)
	}

	// Remove any remaining non-printable control characters (except newlines and tabs)
	var result strings.Builder
	result.Grow(len(content))
	for _, r := range content {
		if r == '\n' || r == '\r' || r == '\t' || (unicode.IsPrint(r) && r < 0x10000) {
			result.WriteRune(r)
		}
	}

	// Normalize line endings to just \n (LinkedIn handles this fine)
	content = result.String()
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")

	// Remove any sequences of more than 2 consecutive newlines
	for strings.Contains(content, "\n\n\n") {
		content = strings.ReplaceAll(content, "\n\n\n", "\n\n")
	}

	return strings.TrimSpace(content)
}

// CreatePost publishes a text post to LinkedIn
func (c *Client) CreatePost(ctx context.Context, post *models.Post) (string, error) {
	// Get user profile to get the author URN
	profile, err := c.GetProfile(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get profile: %w", err)
	}

	// Sanitize content to remove problematic unicode characters
	content := sanitizeForLinkedIn(post.Content)

	c.log.Info().
		Int("original_length", len(post.Content)).
		Int("sanitized_length", len(content)).
		Msg("Content sanitized for LinkedIn")

	// Truncate content if it exceeds LinkedIn's limit
	if len(content) > maxCommentaryLength {
		c.log.Warn().
			Int("original_length", len(content)).
			Int("max_length", maxCommentaryLength).
			Msg("Content exceeds LinkedIn limit, truncating")
		// Truncate at a reasonable point (try to find last complete paragraph)
		content = content[:maxCommentaryLength-3] + "..."
	}

	// Build the post request
	postReq := PostRequest{
		Author:     fmt.Sprintf("urn:li:person:%s", profile.Sub),
		Commentary: content,
		Visibility: "PUBLIC",
		Distribution: Distribution{
			FeedDistribution:               "MAIN_FEED",
			TargetEntities:                 []interface{}{},
			ThirdPartyDistributionChannels: []interface{}{},
		},
		LifecycleState:      "PUBLISHED",
		IsReshareDisabledByAuthor: false,
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
			Msg("Failed to create post")
		return "", fmt.Errorf("failed to create post: %s - %s", resp.Status, string(body))
	}

	// Extract post URN from response header or body
	postURN := resp.Header.Get("x-restli-id")
	if postURN == "" {
		// Try to extract from Location header
		postURN = resp.Header.Get("Location")
	}

	c.log.Info().
		Str("post_urn", postURN).
		Msg("Post created successfully")

	return postURN, nil
}

// PostRequest represents the LinkedIn Posts API request body
type PostRequest struct {
	Author                    string       `json:"author"`
	Commentary                string       `json:"commentary"`
	Visibility                string       `json:"visibility"`
	Distribution              Distribution `json:"distribution"`
	LifecycleState            string       `json:"lifecycleState"`
	IsReshareDisabledByAuthor bool         `json:"isReshareDisabledByAuthor"`
}

// Distribution represents post distribution settings
type Distribution struct {
	FeedDistribution               string        `json:"feedDistribution"`
	TargetEntities                 []interface{} `json:"targetEntities"`
	ThirdPartyDistributionChannels []interface{} `json:"thirdPartyDistributionChannels"`
}

// CreatePoll creates a poll post on LinkedIn
func (c *Client) CreatePoll(ctx context.Context, question string, options []string, durationDays int) (string, error) {
	// Get user profile
	profile, err := c.GetProfile(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get profile: %w", err)
	}

	// Sanitize question and options
	question = sanitizeForLinkedIn(question)
	for i, opt := range options {
		options[i] = sanitizeForLinkedIn(opt)
	}

	// Map duration to LinkedIn format
	duration := "THREE_DAYS"
	switch durationDays {
	case 1:
		duration = "ONE_DAY"
	case 7:
		duration = "ONE_WEEK"
	case 14:
		duration = "TWO_WEEKS"
	}

	// Build poll options
	pollOptions := make([]PollOption, len(options))
	for i, opt := range options {
		pollOptions[i] = PollOption{Text: opt}
	}

	pollReq := PollRequest{
		Author:     fmt.Sprintf("urn:li:person:%s", profile.Sub),
		Commentary: question,
		Visibility: "PUBLIC",
		Distribution: Distribution{
			FeedDistribution:               "MAIN_FEED",
			TargetEntities:                 []interface{}{},
			ThirdPartyDistributionChannels: []interface{}{},
		},
		LifecycleState: "PUBLISHED",
		Poll: Poll{
			Question: question,
			Options:  pollOptions,
			Settings: PollSettings{
				Duration: duration,
			},
		},
	}

	resp, err := c.do(ctx, "POST", "/posts", pollReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to create poll: %s - %s", resp.Status, string(body))
	}

	postURN := resp.Header.Get("x-restli-id")
	c.log.Info().
		Str("post_urn", postURN).
		Msg("Poll created successfully")

	return postURN, nil
}

// PollRequest represents a poll post request
type PollRequest struct {
	Author         string       `json:"author"`
	Commentary     string       `json:"commentary"`
	Visibility     string       `json:"visibility"`
	Distribution   Distribution `json:"distribution"`
	LifecycleState string       `json:"lifecycleState"`
	Poll           Poll         `json:"poll"`
}

// Poll represents poll data
type Poll struct {
	Question string       `json:"question"`
	Options  []PollOption `json:"options"`
	Settings PollSettings `json:"settings"`
}

// PollOption represents a single poll option
type PollOption struct {
	Text string `json:"text"`
}

// PollSettings represents poll settings
type PollSettings struct {
	Duration string `json:"duration"`
}
