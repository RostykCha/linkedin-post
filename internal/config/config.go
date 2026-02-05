package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

// Config represents the application configuration
type Config struct {
	Database   DatabaseConfig   `mapstructure:"database"`
	LinkedIn   LinkedInConfig   `mapstructure:"linkedin"`
	Anthropic  AnthropicConfig  `mapstructure:"anthropic"`
	Sources    SourcesConfig    `mapstructure:"sources"`
	Scheduler  SchedulerConfig  `mapstructure:"scheduler"`
	RateLimit  RateLimitConfig  `mapstructure:"rate_limit"`
	Logging    LoggingConfig    `mapstructure:"logging"`
	Publishing PublishingConfig `mapstructure:"publishing"`
	Tracker    TrackerConfig    `mapstructure:"tracker"`
	Media      MediaConfig      `mapstructure:"media"`
	Commenter  CommenterConfig  `mapstructure:"commenter"`
}

// DatabaseConfig holds database connection settings
type DatabaseConfig struct {
	Driver string `mapstructure:"driver"` // sqlite or postgres
	DSN    string `mapstructure:"dsn"`    // Connection string
}

// LinkedInConfig holds LinkedIn API settings
type LinkedInConfig struct {
	ClientID     string   `mapstructure:"client_id"`
	ClientSecret string   `mapstructure:"client_secret"`
	RedirectURI  string   `mapstructure:"redirect_uri"`
	Scopes       []string `mapstructure:"scopes"`
	// Token injection from environment (for headless deployment)
	AccessToken    string `mapstructure:"access_token"`
	RefreshToken   string `mapstructure:"refresh_token"`
	TokenExpiresAt string `mapstructure:"token_expires_at"`
}

// AnthropicConfig holds Claude API settings
type AnthropicConfig struct {
	APIKey      string  `mapstructure:"api_key"`
	Model       string  `mapstructure:"model"`
	MaxTokens   int     `mapstructure:"max_tokens"`
	Temperature float64 `mapstructure:"temperature"`
}

// SourcesConfig holds all topic source configurations
type SourcesConfig struct {
	NewsAPI NewsAPIConfig `mapstructure:"newsapi"`
	RSS     RSSConfig     `mapstructure:"rss"`
	Twitter TwitterConfig `mapstructure:"twitter"`
	Reddit  RedditConfig  `mapstructure:"reddit"`
	Custom  CustomConfig  `mapstructure:"custom"`
}

// NewsAPIConfig holds NewsAPI settings
type NewsAPIConfig struct {
	Enabled       bool     `mapstructure:"enabled"`
	APIKey        string   `mapstructure:"api_key"`
	Categories    []string `mapstructure:"categories"`
	Language      string   `mapstructure:"language"`
	FetchInterval string   `mapstructure:"fetch_interval"`
}

// RSSConfig holds RSS feed settings
type RSSConfig struct {
	Enabled       bool       `mapstructure:"enabled"`
	Feeds         []RSSFeed  `mapstructure:"feeds"`
	FetchInterval string     `mapstructure:"fetch_interval"`
}

// RSSFeed represents a single RSS feed
type RSSFeed struct {
	Name string `mapstructure:"name"`
	URL  string `mapstructure:"url"`
}

// TwitterConfig holds Twitter/X API settings
type TwitterConfig struct {
	Enabled       bool     `mapstructure:"enabled"`
	BearerToken   string   `mapstructure:"bearer_token"`
	SearchQueries []string `mapstructure:"search_queries"`
	FetchInterval string   `mapstructure:"fetch_interval"`
}

// RedditConfig holds Reddit API settings
type RedditConfig struct {
	Enabled       bool     `mapstructure:"enabled"`
	ClientID      string   `mapstructure:"client_id"`
	ClientSecret  string   `mapstructure:"client_secret"`
	Subreddits    []string `mapstructure:"subreddits"`
	FetchInterval string   `mapstructure:"fetch_interval"`
}

// CustomConfig holds custom keyword settings
type CustomConfig struct {
	Enabled  bool     `mapstructure:"enabled"`
	Keywords []string `mapstructure:"keywords"`
}

// SchedulerConfig holds scheduler settings
type SchedulerConfig struct {
	DiscoveryCron string   `mapstructure:"discovery_cron"`
	DigestCron    string   `mapstructure:"digest_cron"`
	PublishCron   string   `mapstructure:"publish_cron"`    // Single cron (backward compat)
	PublishCrons  []string `mapstructure:"publish_crons"`   // Multiple publish windows
	CleanupCron   string   `mapstructure:"cleanup_cron"`
}

// RateLimitConfig holds rate limiting settings
type RateLimitConfig struct {
	LinkedInRequestsPerDay     int `mapstructure:"linkedin_requests_per_day"`
	AnthropicRequestsPerMinute int `mapstructure:"anthropic_requests_per_minute"`
	SourceRequestsPerHour      int `mapstructure:"source_requests_per_hour"`
}

// LoggingConfig holds logging settings
type LoggingConfig struct {
	Level  string `mapstructure:"level"`  // debug, info, warn, error
	Format string `mapstructure:"format"` // json or console
	Output string `mapstructure:"output"` // stdout or file path
}

// PublishingConfig holds publishing settings
type PublishingConfig struct {
	AutoApprove       bool    `mapstructure:"auto_approve"`
	MaxPostsPerDay    int     `mapstructure:"max_posts_per_day"`
	MinScoreThreshold float64 `mapstructure:"min_score_threshold"`
	DefaultPostType   string  `mapstructure:"default_post_type"`
	BrandVoice        string  `mapstructure:"brand_voice"`
}

// TrackerConfig holds Google Sheets tracker settings
type TrackerConfig struct {
	Enabled            bool   `mapstructure:"enabled"`
	SpreadsheetID      string `mapstructure:"spreadsheet_id"`
	SheetName          string `mapstructure:"sheet_name"`
	CredentialsFile    string `mapstructure:"credentials_file"`
	ServiceAccountJSON string `mapstructure:"service_account_json"`
}

// MediaConfig holds image/media settings
type MediaConfig struct {
	Enabled        bool   `mapstructure:"enabled"`
	Provider       string `mapstructure:"provider"`         // "unsplash" or "none"
	UnsplashAPIKey string `mapstructure:"unsplash_api_key"` // Unsplash API access key
	FallbackToText bool   `mapstructure:"fallback_to_text"` // If image fails, post text-only
}

// CommenterConfig holds auto-comment settings
type CommenterConfig struct {
	Enabled           bool     `mapstructure:"enabled"`
	MaxCommentsPerDay int      `mapstructure:"max_comments_per_day"` // Limit to avoid spam detection
	TargetInfluencers []string `mapstructure:"target_influencers"`   // List of person URNs to monitor
	TargetKeywords    []string `mapstructure:"target_keywords"`      // Keywords to search for posts
	MinPostEngagement int      `mapstructure:"min_post_engagement"`  // Min likes/reactions to comment
	MaxPostEngagement int      `mapstructure:"max_post_engagement"`  // Max engagement (skip mega-viral)
	CommentStyle      string   `mapstructure:"comment_style"`        // insightful, question, supportive
	// Timing controls to avoid spam detection
	MinIntervalMinutes int      `mapstructure:"min_interval_minutes"` // Min minutes between comments
	MaxIntervalMinutes int      `mapstructure:"max_interval_minutes"` // Max minutes for randomization
	ActiveHoursStart   int      `mapstructure:"active_hours_start"`   // Start hour (0-23)
	ActiveHoursEnd     int      `mapstructure:"active_hours_end"`     // End hour (0-23)
	MaxPostAgeHours    int      `mapstructure:"max_post_age_hours"`   // Skip posts older than this
	MinPostAgeMinutes  int      `mapstructure:"min_post_age_minutes"` // Skip very new posts
	// Style rotation
	CommentStyleRotation bool     `mapstructure:"comment_style_rotation"` // Rotate between styles
	CommentStyles        []string `mapstructure:"comment_styles"`         // Available styles to rotate
}

// Load loads configuration from file and environment variables
func Load(configPath string) (*Config, error) {
	// Load .env file if present (ignore errors if not found)
	_ = godotenv.Load()
	_ = godotenv.Load(".env.local")

	v := viper.New()

	// Set defaults
	setDefaults(v)

	// Config file
	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		// Look for config in current directory and configs folder
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("./configs")

		// Also check user's home directory
		home, err := os.UserHomeDir()
		if err == nil {
			v.AddConfigPath(filepath.Join(home, ".linkedin-agent"))
		}
	}

	// Environment variables
	v.SetEnvPrefix("LINKEDIN")
	v.AutomaticEnv()

	// Explicit bindings for nested keys (Viper doesn't auto-bind underscored nested keys)
	v.BindEnv("anthropic.api_key", "LINKEDIN_ANTHROPIC_API_KEY")
	v.BindEnv("linkedin.client_id", "LINKEDIN_LINKEDIN_CLIENT_ID")
	v.BindEnv("linkedin.client_secret", "LINKEDIN_LINKEDIN_CLIENT_SECRET")
	v.BindEnv("linkedin.access_token", "LINKEDIN_LINKEDIN_ACCESS_TOKEN")
	v.BindEnv("linkedin.refresh_token", "LINKEDIN_LINKEDIN_REFRESH_TOKEN")
	v.BindEnv("database.driver", "LINKEDIN_DATABASE_DRIVER")
	v.BindEnv("database.dsn", "LINKEDIN_DATABASE_DSN")
	v.BindEnv("tracker.enabled", "LINKEDIN_TRACKER_ENABLED")
	v.BindEnv("tracker.spreadsheet_id", "LINKEDIN_TRACKER_SPREADSHEET_ID")
	v.BindEnv("tracker.credentials_file", "LINKEDIN_TRACKER_CREDENTIALS_FILE")
	v.BindEnv("tracker.service_account_json", "LINKEDIN_TRACKER_SERVICE_ACCOUNT_JSON")
	v.BindEnv("publishing.auto_approve", "LINKEDIN_PUBLISHING_AUTO_APPROVE")
	v.BindEnv("publishing.min_score_threshold", "LINKEDIN_PUBLISHING_MIN_SCORE_THRESHOLD")
	v.BindEnv("media.enabled", "LINKEDIN_MEDIA_ENABLED")
	v.BindEnv("media.unsplash_api_key", "LINKEDIN_MEDIA_UNSPLASH_API_KEY")

	// Read config file (ignore if not found)
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("error reading config file: %w", err)
		}
	}

	var config Config
	if err := v.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("error unmarshaling config: %w", err)
	}

	return &config, nil
}

// setDefaults sets default configuration values
func setDefaults(v *viper.Viper) {
	// Database defaults
	v.SetDefault("database.driver", "sqlite")
	v.SetDefault("database.dsn", "./data/linkedin.db")

	// LinkedIn defaults
	v.SetDefault("linkedin.redirect_uri", "http://localhost:8080/callback")
	v.SetDefault("linkedin.scopes", []string{"w_member_social", "r_liteprofile"})

	// Anthropic defaults
	v.SetDefault("anthropic.model", "claude-sonnet-4-20250514")
	v.SetDefault("anthropic.max_tokens", 4096)
	v.SetDefault("anthropic.temperature", 0.7)

	// Sources defaults
	v.SetDefault("sources.newsapi.enabled", true)
	v.SetDefault("sources.newsapi.language", "en")
	v.SetDefault("sources.newsapi.categories", []string{"business", "technology"})
	v.SetDefault("sources.newsapi.fetch_interval", "2h")

	v.SetDefault("sources.rss.enabled", true)
	v.SetDefault("sources.rss.fetch_interval", "30m")

	v.SetDefault("sources.twitter.enabled", false)
	v.SetDefault("sources.twitter.fetch_interval", "1h")

	v.SetDefault("sources.reddit.enabled", true)
	v.SetDefault("sources.reddit.fetch_interval", "1h")

	v.SetDefault("sources.custom.enabled", true)

	// Scheduler defaults
	v.SetDefault("scheduler.discovery_cron", "0 */2 * * *") // Every 2 hours
	v.SetDefault("scheduler.digest_cron", "55 7 * * *")     // 7:55am daily - generate digest before publish
	v.SetDefault("scheduler.publish_cron", "0 8 * * *")     // 8am daily - single window (backward compat)
	v.SetDefault("scheduler.publish_crons", []string{       // Multiple publish windows for optimal engagement
		"0 8 * * *",  // 8:00 AM - morning commute
		"0 12 * * *", // 12:00 PM - lunch break
		"0 17 * * *", // 5:00 PM - end of workday
	})
	v.SetDefault("scheduler.cleanup_cron", "0 0 * * 0") // Weekly cleanup

	// Rate limit defaults
	v.SetDefault("rate_limit.linkedin_requests_per_day", 100)
	v.SetDefault("rate_limit.anthropic_requests_per_minute", 10)
	v.SetDefault("rate_limit.source_requests_per_hour", 60)

	// Logging defaults
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "console")
	v.SetDefault("logging.output", "stdout")

	// Publishing defaults
	v.SetDefault("publishing.auto_approve", false)
	v.SetDefault("publishing.max_posts_per_day", 3)
	v.SetDefault("publishing.min_score_threshold", 70.0)
	v.SetDefault("publishing.default_post_type", "text")
	v.SetDefault("publishing.brand_voice", "Professional, insightful, and engaging. Focus on actionable insights for business leaders.")

	// Tracker defaults
	v.SetDefault("tracker.enabled", false)
	v.SetDefault("tracker.sheet_name", "Posts")

	// Media defaults
	v.SetDefault("media.enabled", false)
	v.SetDefault("media.provider", "unsplash")
	v.SetDefault("media.fallback_to_text", true)

	// Commenter defaults
	v.SetDefault("commenter.enabled", false)
	v.SetDefault("commenter.max_comments_per_day", 10)
	v.SetDefault("commenter.min_post_engagement", 50)
	v.SetDefault("commenter.max_post_engagement", 5000)
	v.SetDefault("commenter.comment_style", "insightful")
	// Timing defaults - conservative to avoid spam detection
	v.SetDefault("commenter.min_interval_minutes", 45)
	v.SetDefault("commenter.max_interval_minutes", 90)
	v.SetDefault("commenter.active_hours_start", 8)  // 8 AM
	v.SetDefault("commenter.active_hours_end", 18)   // 6 PM
	v.SetDefault("commenter.max_post_age_hours", 24)
	v.SetDefault("commenter.min_post_age_minutes", 30)
	// Style rotation
	v.SetDefault("commenter.comment_style_rotation", true)
	v.SetDefault("commenter.comment_styles", []string{"insightful", "question", "supportive"})
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.Anthropic.APIKey == "" {
		return fmt.Errorf("anthropic.api_key is required")
	}
	if c.LinkedIn.ClientID == "" {
		return fmt.Errorf("linkedin.client_id is required")
	}
	if c.LinkedIn.ClientSecret == "" {
		return fmt.Errorf("linkedin.client_secret is required")
	}
	return nil
}
