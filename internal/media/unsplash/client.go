package unsplash

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"time"

	"github.com/linkedin-agent/pkg/logger"
)

const (
	baseURL = "https://api.unsplash.com"
)

// Photo represents an Unsplash photo
type Photo struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	AltDesc     string `json:"alt_description"`
	URLs        URLs   `json:"urls"`
	User        User   `json:"user"`
	Links       Links  `json:"links"`
}

// URLs contains different size URLs for the photo
type URLs struct {
	Raw     string `json:"raw"`
	Full    string `json:"full"`
	Regular string `json:"regular"` // 1080px width - good for LinkedIn
	Small   string `json:"small"`
	Thumb   string `json:"thumb"`
}

// User represents the photographer
type User struct {
	Name     string `json:"name"`
	Username string `json:"username"`
}

// Links contains API links for the photo
type Links struct {
	Download         string `json:"download"`
	DownloadLocation string `json:"download_location"` // Use this to trigger download count
}

// SearchResult represents the API response for photo search
type SearchResult struct {
	Total      int     `json:"total"`
	TotalPages int     `json:"total_pages"`
	Results    []Photo `json:"results"`
}

// Client is the Unsplash API client
type Client struct {
	apiKey     string
	httpClient *http.Client
	log        *logger.Logger
}

// NewClient creates a new Unsplash client
func NewClient(apiKey string, log *logger.Logger) *Client {
	return &Client{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		log: log.WithComponent("unsplash"),
	}
}

// SearchPhotos searches for photos matching the query
func (c *Client) SearchPhotos(ctx context.Context, query string, perPage int) ([]Photo, error) {
	if perPage <= 0 {
		perPage = 5
	}
	if perPage > 30 {
		perPage = 30
	}

	endpoint := fmt.Sprintf("%s/search/photos", baseURL)
	params := url.Values{}
	params.Set("query", query)
	params.Set("per_page", fmt.Sprintf("%d", perPage))
	params.Set("orientation", "landscape") // Best for LinkedIn posts

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Client-ID "+c.apiKey)
	req.Header.Set("Accept-Version", "v1")

	c.log.Debug().Str("query", query).Msg("Searching Unsplash photos")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var result SearchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	c.log.Debug().
		Int("total", result.Total).
		Int("returned", len(result.Results)).
		Msg("Search completed")

	return result.Results, nil
}

// DownloadPhoto downloads a photo and returns the image data
// This also triggers the download endpoint to give credit to the photographer
func (c *Client) DownloadPhoto(ctx context.Context, photo *Photo) ([]byte, error) {
	// First, trigger the download endpoint (required by Unsplash API guidelines)
	if photo.Links.DownloadLocation != "" {
		triggerReq, _ := http.NewRequestWithContext(ctx, "GET", photo.Links.DownloadLocation, nil)
		triggerReq.Header.Set("Authorization", "Client-ID "+c.apiKey)
		c.httpClient.Do(triggerReq) // Ignore errors, this is just for tracking
	}

	// Download the regular size image (1080px width, good for LinkedIn)
	imageURL := photo.URLs.Regular
	if imageURL == "" {
		imageURL = photo.URLs.Full
	}

	c.log.Debug().Str("photo_id", photo.ID).Msg("Downloading photo")

	req, err := http.NewRequestWithContext(ctx, "GET", imageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create download request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read image data: %w", err)
	}

	c.log.Info().
		Str("photo_id", photo.ID).
		Int("size_bytes", len(data)).
		Str("photographer", photo.User.Name).
		Msg("Photo downloaded")

	return data, nil
}

// GetAttribution returns the attribution text for a photo (required by Unsplash)
func (c *Client) GetAttribution(photo *Photo) string {
	return fmt.Sprintf("Photo by %s on Unsplash", photo.User.Name)
}

// GetBestPhoto searches and returns a random photo from top results for variety
func (c *Client) GetBestPhoto(ctx context.Context, query string) (*Photo, error) {
	photos, err := c.SearchPhotos(ctx, query, 10)
	if err != nil {
		return nil, err
	}
	if len(photos) == 0 {
		return nil, fmt.Errorf("no photos found for query: %s", query)
	}
	// Randomly select from top results to avoid using the same image repeatedly
	idx := rand.Intn(len(photos))
	c.log.Debug().
		Int("total_results", len(photos)).
		Int("selected_index", idx).
		Str("photo_id", photos[idx].ID).
		Msg("Randomly selected photo from search results")
	return &photos[idx], nil
}
