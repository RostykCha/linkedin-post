package models

import (
	"time"

	"golang.org/x/oauth2"
)

// OAuthToken stores OAuth tokens for external services
type OAuthToken struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	Provider     string    `gorm:"uniqueIndex;not null" json:"provider"` // linkedin
	AccessToken  string    `gorm:"type:text;not null" json:"access_token"`
	RefreshToken string    `gorm:"type:text" json:"refresh_token"`
	TokenType    string    `gorm:"default:'Bearer'" json:"token_type"`
	Scope        string    `json:"scope"`
	ExpiresAt    time.Time `json:"expires_at"`
	CreatedAt    time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt    time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// IsExpired returns true if the token has expired
func (t *OAuthToken) IsExpired() bool {
	return time.Now().After(t.ExpiresAt)
}

// NeedsRefresh returns true if the token expires within 5 minutes
func (t *OAuthToken) NeedsRefresh() bool {
	return time.Now().Add(5 * time.Minute).After(t.ExpiresAt)
}

// ToOAuth2Token converts to golang.org/x/oauth2.Token
func (t *OAuthToken) ToOAuth2Token() *oauth2.Token {
	return &oauth2.Token{
		AccessToken:  t.AccessToken,
		RefreshToken: t.RefreshToken,
		TokenType:    t.TokenType,
		Expiry:       t.ExpiresAt,
	}
}

// FromOAuth2Token updates from golang.org/x/oauth2.Token
func (t *OAuthToken) FromOAuth2Token(token *oauth2.Token) {
	t.AccessToken = token.AccessToken
	t.RefreshToken = token.RefreshToken
	t.TokenType = token.TokenType
	t.ExpiresAt = token.Expiry
}
