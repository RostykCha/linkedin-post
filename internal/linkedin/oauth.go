package linkedin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"sync"
	"time"

	"golang.org/x/oauth2"

	"github.com/linkedin-agent/internal/config"
	"github.com/linkedin-agent/internal/models"
	"github.com/linkedin-agent/internal/storage"
	"github.com/linkedin-agent/pkg/logger"
)

// OAuthManager handles LinkedIn OAuth 2.0 flow
type OAuthManager struct {
	config     *oauth2.Config
	repository storage.Repository // Optional, can be nil for env-only mode
	log        *logger.Logger

	// In-memory token storage (used when repository is nil, or as cache)
	mu           sync.RWMutex
	currentToken *models.OAuthToken
}

// NewOAuthManager creates a new OAuth manager
func NewOAuthManager(cfg config.LinkedInConfig, repo storage.Repository, log *logger.Logger) *OAuthManager {
	m := &OAuthManager{
		config: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURI,
			Scopes:       cfg.Scopes,
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://www.linkedin.com/oauth/v2/authorization",
				TokenURL: "https://www.linkedin.com/oauth/v2/accessToken",
			},
		},
		repository: repo,
		log:        log.WithComponent("oauth"),
	}

	// Initialize from config if access token provided (env vars)
	if cfg.AccessToken != "" {
		expiry, err := time.Parse(time.RFC3339, cfg.TokenExpiresAt)
		if err != nil {
			expiry = time.Now().Add(60 * 24 * time.Hour) // Default 60 days
		}

		m.currentToken = &models.OAuthToken{
			Provider:     "linkedin",
			AccessToken:  cfg.AccessToken,
			RefreshToken: cfg.RefreshToken,
			TokenType:    "Bearer",
			ExpiresAt:    expiry,
		}
		m.log.Info().
			Time("expires_at", expiry).
			Msg("OAuth token initialized from environment")
	}

	return m
}

// NewOAuthManagerEnvOnly creates an OAuth manager without database dependency
func NewOAuthManagerEnvOnly(cfg config.LinkedInConfig, log *logger.Logger) *OAuthManager {
	return NewOAuthManager(cfg, nil, log)
}

// GenerateState creates a random state for OAuth CSRF protection
func GenerateState() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// GetAuthURL returns the OAuth authorization URL
func (m *OAuthManager) GetAuthURL(state string) string {
	return m.config.AuthCodeURL(state, oauth2.AccessTypeOffline)
}

// ExchangeCode exchanges the authorization code for tokens
func (m *OAuthManager) ExchangeCode(ctx context.Context, code string) (*models.OAuthToken, error) {
	m.log.Info().Msg("Exchanging authorization code for token")

	token, err := m.config.Exchange(ctx, code)
	if err != nil {
		m.log.Error().Err(err).Msg("Failed to exchange code")
		return nil, fmt.Errorf("failed to exchange code: %w", err)
	}

	// Convert to our model
	oauthToken := &models.OAuthToken{
		Provider:     "linkedin",
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		TokenType:    token.TokenType,
		ExpiresAt:    token.Expiry,
	}

	// Store in memory
	m.mu.Lock()
	m.currentToken = oauthToken
	m.mu.Unlock()

	// Save to database if available
	if m.repository != nil {
		if err := m.repository.SaveToken(ctx, oauthToken); err != nil {
			m.log.Warn().Err(err).Msg("Failed to save token to database (using in-memory only)")
		}
	}

	m.log.Info().
		Time("expires_at", token.Expiry).
		Msg("Token saved successfully")

	return oauthToken, nil
}

// GetValidToken returns a valid access token, refreshing if necessary
func (m *OAuthManager) GetValidToken(ctx context.Context) (*models.OAuthToken, error) {
	var token *models.OAuthToken

	// Try to get from in-memory cache first
	m.mu.RLock()
	if m.currentToken != nil {
		token = m.currentToken
	}
	m.mu.RUnlock()

	// If no in-memory token and repository available, try database
	if token == nil && m.repository != nil {
		dbToken, err := m.repository.GetToken(ctx, "linkedin")
		if err == nil && dbToken != nil {
			m.mu.Lock()
			m.currentToken = dbToken
			token = dbToken
			m.mu.Unlock()
		}
	}

	if token == nil {
		return nil, fmt.Errorf("no LinkedIn token found: configure via environment variables or run 'oauth login'")
	}

	// Check if token needs refresh
	if token.NeedsRefresh() {
		m.log.Info().Msg("Token expiring soon, refreshing")
		var err error
		token, err = m.refreshToken(ctx, token)
		if err != nil {
			return nil, err
		}
	}

	return token, nil
}

// refreshToken refreshes an expired token
func (m *OAuthManager) refreshToken(ctx context.Context, token *models.OAuthToken) (*models.OAuthToken, error) {
	if token.RefreshToken == "" {
		return nil, fmt.Errorf("no refresh token available, please re-authenticate")
	}

	oauth2Token := token.ToOAuth2Token()
	tokenSource := m.config.TokenSource(ctx, oauth2Token)

	newToken, err := tokenSource.Token()
	if err != nil {
		m.log.Error().Err(err).Msg("Failed to refresh token")
		return nil, fmt.Errorf("failed to refresh token: %w", err)
	}

	// Update our token
	token.FromOAuth2Token(newToken)

	// Update in-memory cache
	m.mu.Lock()
	m.currentToken = token
	m.mu.Unlock()

	// Save updated token if repository available
	if m.repository != nil {
		if err := m.repository.SaveToken(ctx, token); err != nil {
			m.log.Warn().Err(err).Msg("Failed to save refreshed token to database (in-memory updated)")
		}
	}

	m.log.Info().
		Time("expires_at", newToken.Expiry).
		Msg("Token refreshed successfully")

	return token, nil
}

// IsAuthenticated checks if we have a valid token
func (m *OAuthManager) IsAuthenticated(ctx context.Context) bool {
	token, _ := m.GetValidToken(ctx)
	return token != nil && !token.IsExpired()
}

// GetTokenStatus returns information about the current token
func (m *OAuthManager) GetTokenStatus(ctx context.Context) (bool, time.Time, error) {
	m.mu.RLock()
	token := m.currentToken
	m.mu.RUnlock()

	// Try database if no in-memory token
	if token == nil && m.repository != nil {
		var err error
		token, err = m.repository.GetToken(ctx, "linkedin")
		if err != nil {
			return false, time.Time{}, err
		}
	}

	if token == nil {
		return false, time.Time{}, fmt.Errorf("no token found")
	}

	return !token.IsExpired(), token.ExpiresAt, nil
}

// ImportTokenFromEnv imports OAuth token from environment variables
// This is now handled automatically in NewOAuthManager, kept for backwards compatibility
func (m *OAuthManager) ImportTokenFromEnv(ctx context.Context, accessToken, refreshToken, expiresAt string) error {
	if accessToken == "" {
		return nil // No env token configured
	}

	// Check if valid token already exists
	m.mu.RLock()
	hasValidToken := m.currentToken != nil && m.currentToken.ExpiresAt.After(time.Now())
	m.mu.RUnlock()

	if hasValidToken {
		m.log.Debug().Msg("Valid token already exists, skipping env import")
		return nil
	}

	// Parse expiration time
	expiry, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		expiry = time.Now().Add(60 * 24 * time.Hour) // Default 60 days
	}

	token := &models.OAuthToken{
		Provider:     "linkedin",
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    "Bearer",
		ExpiresAt:    expiry,
	}

	// Store in memory
	m.mu.Lock()
	m.currentToken = token
	m.mu.Unlock()

	// Save to database if available
	if m.repository != nil {
		if err := m.repository.SaveToken(ctx, token); err != nil {
			m.log.Warn().Err(err).Msg("Failed to save imported token to database")
		}
	}

	m.log.Info().
		Time("expires_at", expiry).
		Msg("OAuth token imported from environment variables")
	return nil
}

// StartOAuthServer starts a temporary HTTP server for OAuth callback
func (m *OAuthManager) StartOAuthServer(ctx context.Context, port int) (string, error) {
	state, err := GenerateState()
	if err != nil {
		return "", err
	}

	authURL := m.GetAuthURL(state)

	// Channel to receive the auth code
	codeChan := make(chan string, 1)
	errChan := make(chan error, 1)

	server := &http.Server{Addr: fmt.Sprintf(":%d", port)}

	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		// Verify state
		if r.URL.Query().Get("state") != state {
			errChan <- fmt.Errorf("state mismatch")
			http.Error(w, "State mismatch", http.StatusBadRequest)
			return
		}

		// Check for error
		if errMsg := r.URL.Query().Get("error"); errMsg != "" {
			errChan <- fmt.Errorf("oauth error: %s - %s", errMsg, r.URL.Query().Get("error_description"))
			http.Error(w, errMsg, http.StatusBadRequest)
			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			errChan <- fmt.Errorf("no code in callback")
			http.Error(w, "No code", http.StatusBadRequest)
			return
		}

		codeChan <- code

		// Show success page
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `
			<html>
			<body style="font-family: sans-serif; text-align: center; padding: 50px;">
				<h1>✓ Authorization Successful!</h1>
				<p>You can close this window and return to the terminal.</p>
			</body>
			</html>
		`)
	})

	// Start server in background
	go func() {
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	m.log.Info().
		Str("url", authURL).
		Int("port", port).
		Msg("OAuth server started, waiting for callback")

	// Wait for code or error
	select {
	case code := <-codeChan:
		server.Shutdown(ctx)
		_, err := m.ExchangeCode(ctx, code)
		return authURL, err
	case err := <-errChan:
		server.Shutdown(ctx)
		return authURL, err
	case <-ctx.Done():
		server.Shutdown(ctx)
		return authURL, ctx.Err()
	}
}
