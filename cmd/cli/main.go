package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/linkedin-agent/internal/agent/commenter"
	"github.com/linkedin-agent/internal/agent/discovery"
	"github.com/linkedin-agent/internal/agent/publisher"
	"github.com/linkedin-agent/internal/ai"
	"github.com/linkedin-agent/internal/config"
	"github.com/linkedin-agent/internal/linkedin"
	"github.com/linkedin-agent/internal/media/unsplash"
	"github.com/linkedin-agent/internal/models"
	"github.com/linkedin-agent/internal/source"
	"github.com/linkedin-agent/internal/source/custom"
	"github.com/linkedin-agent/internal/source/rss"
	"github.com/linkedin-agent/internal/storage"
	"github.com/linkedin-agent/internal/storage/sheets"
	"github.com/linkedin-agent/internal/storage/sqlite"
	"github.com/linkedin-agent/internal/tracker"
	"github.com/linkedin-agent/pkg/logger"
	"github.com/linkedin-agent/pkg/ratelimit"
)

var (
	cfgFile string
	cfg     *config.Config
	log     *logger.Logger
	repo    storage.Repository
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "linkedin-agent",
		Short: "LinkedIn posting agent powered by AI",
		Long: `An autonomous agent that discovers trending topics and publishes
engaging content to LinkedIn using Claude AI.`,
		PersistentPreRunE: initializeApp,
	}

	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is ./configs/config.yaml)")

	// Add subcommands
	rootCmd.AddCommand(discoverCmd())
	rootCmd.AddCommand(publishCmd())
	rootCmd.AddCommand(oauthCmd())
	rootCmd.AddCommand(topicsCmd())
	rootCmd.AddCommand(postsCmd())
	rootCmd.AddCommand(trackerCmd())
	rootCmd.AddCommand(commentsCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func initializeApp(cmd *cobra.Command, args []string) error {
	var err error

	// Load config
	cfg, err = config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Initialize logger
	log = logger.New(logger.Config{
		Level:  cfg.Logging.Level,
		Format: cfg.Logging.Format,
		Output: cfg.Logging.Output,
	})

	// Initialize storage based on configuration
	// Use Google Sheets as database when tracker is enabled and has credentials
	if cfg.Tracker.Enabled && (cfg.Tracker.ServiceAccountJSON != "" || cfg.Tracker.CredentialsFile != "") {
		log.Info().Msg("Using Google Sheets as primary storage")
		repo, err = sheets.New(sheets.Config{
			SpreadsheetID:      cfg.Tracker.SpreadsheetID,
			ServiceAccountJSON: cfg.Tracker.ServiceAccountJSON,
			CredentialsFile:    cfg.Tracker.CredentialsFile,
		}, log)
		if err != nil {
			return fmt.Errorf("failed to connect to Google Sheets: %w", err)
		}
	} else {
		// Fall back to SQLite
		log.Info().Msg("Using SQLite as primary storage")
		repo, err = sqlite.New(cfg.Database.DSN)
		if err != nil {
			return fmt.Errorf("failed to connect to database: %w", err)
		}
	}

	// Run migrations
	if err := repo.Migrate(); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}

// ============ DISCOVER COMMANDS ============

func discoverCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Topic discovery commands",
	}

	cmd.AddCommand(discoverRunCmd())
	return cmd
}

func discoverRunCmd() *cobra.Command {
	var sourceName string

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run topic discovery",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			// Initialize rate limiter
			limiter := ratelimit.NewDefaultLimiter()

			// Initialize AI client
			aiClient := ai.NewClient(cfg.Anthropic, limiter, log)

			// Initialize source manager
			sourceManager := source.NewManager()

			// Register RSS sources
			if cfg.Sources.RSS.Enabled {
				for _, src := range rss.NewMultiple(cfg.Sources.RSS, log) {
					sourceManager.Register(src)
				}
			}

			// Register custom source
			if cfg.Sources.Custom.Enabled {
				sourceManager.Register(custom.New(cfg.Sources.Custom, log))
			}

			// Create discovery agent
			agent := discovery.NewAgent(sourceManager, aiClient, repo, log)

			// Run discovery
			var result *discovery.DiscoveryResult
			var err error

			if sourceName != "" {
				result, err = agent.RunForSource(ctx, sourceName)
			} else {
				result, err = agent.Run(ctx)
			}

			if err != nil {
				return err
			}

			// Print results
			fmt.Printf("\n=== Discovery Results ===\n")
			fmt.Printf("Topics Found:   %d\n", result.TopicsFound)
			fmt.Printf("Topics Ranked:  %d\n", result.TopicsRanked)
			fmt.Printf("Topics Saved:   %d\n", result.TopicsSaved)
			fmt.Printf("Topics Skipped: %d\n", result.TopicsSkipped)
			fmt.Printf("Duration:       %s\n", result.Duration)

			if len(result.Errors) > 0 {
				fmt.Printf("\nErrors:\n")
				for _, e := range result.Errors {
					fmt.Printf("  - %s\n", e)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&sourceName, "source", "", "Run discovery for specific source only")
	return cmd
}

// ============ PUBLISH COMMANDS ============

func publishCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Content publishing commands",
	}

	cmd.AddCommand(publishGenerateCmd())
	cmd.AddCommand(publishDigestCmd())
	cmd.AddCommand(publishNowCmd())
	cmd.AddCommand(publishScheduleCmd())
	cmd.AddCommand(publishApproveCmd())
	return cmd
}

func publishGenerateCmd() *cobra.Command {
	var topicID uint
	var postType string
	var preview bool

	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate content for a topic",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			limiter := ratelimit.NewDefaultLimiter()
			aiClient := ai.NewClient(cfg.Anthropic, limiter, log)
			oauthManager := linkedin.NewOAuthManager(cfg.LinkedIn, repo, log)
			linkedinClient := linkedin.NewClient(oauthManager, limiter, log)

			agent := publisher.NewAgent(aiClient, linkedinClient, repo, cfg.Publishing, log)

			// Configure media support if enabled
			if cfg.Media.Enabled && cfg.Media.UnsplashAPIKey != "" {
				unsplashClient := unsplash.NewClient(cfg.Media.UnsplashAPIKey, log)
				agent.SetMediaConfig(cfg.Media, unsplashClient)
				log.Info().Msg("Media support enabled with Unsplash")
			}

			// Set up tracker if enabled
			if cfg.Tracker.Enabled {
				t, err := tracker.NewSheetsTracker(tracker.Config{
					Enabled:            cfg.Tracker.Enabled,
					SpreadsheetID:      cfg.Tracker.SpreadsheetID,
					SheetName:          cfg.Tracker.SheetName,
					CredentialsFile:    cfg.Tracker.CredentialsFile,
					ServiceAccountJSON: cfg.Tracker.ServiceAccountJSON,
				}, log)
				if err != nil {
					log.Warn().Err(err).Msg("Failed to create tracker")
				} else if t != nil {
					agent.SetTracker(t)
				}
			}

			pType := models.PostTypeText
			if postType == "poll" {
				pType = models.PostTypePoll
			}

			result, err := agent.GenerateContent(ctx, topicID, pType)
			if err != nil {
				return err
			}

			fmt.Printf("\n=== Generated Content ===\n")
			fmt.Printf("Post ID: %d\n", result.Post.ID)
			fmt.Printf("Status:  %s\n", result.Post.Status)
			fmt.Printf("\n--- Preview ---\n%s\n", result.Preview)

			if !preview && result.Post.Status == models.PostStatusDraft {
				fmt.Printf("\nPost saved as draft. Use 'publish approve %d' to schedule or 'publish now %d' to publish immediately.\n",
					result.Post.ID, result.Post.ID)
			}

			return nil
		},
	}

	cmd.Flags().UintVar(&topicID, "topic-id", 0, "Topic ID to generate content for (required)")
	cmd.Flags().StringVar(&postType, "type", "text", "Post type: text or poll")
	cmd.Flags().BoolVar(&preview, "preview", false, "Preview only, don't save")
	cmd.MarkFlagRequired("topic-id")

	return cmd
}

func publishDigestCmd() *cobra.Command {
	var minScore float64

	cmd := &cobra.Command{
		Use:   "digest",
		Short: "Generate a daily digest post from top 3 topics",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			limiter := ratelimit.NewDefaultLimiter()
			aiClient := ai.NewClient(cfg.Anthropic, limiter, log)

			// Create publisher agent to save the digest
			oauthManager := linkedin.NewOAuthManagerEnvOnly(cfg.LinkedIn, log)
			linkedinClient := linkedin.NewClient(oauthManager, limiter, log)
			agent := publisher.NewAgent(aiClient, linkedinClient, repo, cfg.Publishing, log)

			// Configure media support if enabled
			if cfg.Media.Enabled && cfg.Media.UnsplashAPIKey != "" {
				unsplashClient := unsplash.NewClient(cfg.Media.UnsplashAPIKey, log)
				agent.SetMediaConfig(cfg.Media, unsplashClient)
				log.Info().Msg("Media support enabled with Unsplash")
			}

			// Get top 3 topics to show what will be used
			topics, err := repo.GetTopTopics(ctx, 3, minScore)
			if err != nil {
				return fmt.Errorf("failed to get topics: %w", err)
			}

			if len(topics) < 3 {
				return fmt.Errorf("need at least 3 pending topics with score >= %.0f, found %d", minScore, len(topics))
			}

			fmt.Println("Generating digest from top 3 topics:")
			for i, t := range topics[:3] {
				fmt.Printf("  [%d] %s (%s)\n", i+1, t.Title, t.SourceName)
			}
			fmt.Println()

			// Generate and save digest using publisher agent
			result, err := agent.GenerateDigest(ctx)
			if err != nil {
				return fmt.Errorf("failed to generate digest: %w", err)
			}

			fmt.Printf("=== Daily Digest (Post #%d) ===\n\n%s\n", result.Post.ID, result.Preview)

			return nil
		},
	}

	cmd.Flags().Float64Var(&minScore, "min-score", 70, "Minimum topic score")

	return cmd
}

func publishNowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "now [post-id]",
		Short: "Publish a post immediately",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			postID, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid post ID: %w", err)
			}

			limiter := ratelimit.NewDefaultLimiter()
			aiClient := ai.NewClient(cfg.Anthropic, limiter, log)
			oauthManager := linkedin.NewOAuthManager(cfg.LinkedIn, repo, log)
			linkedinClient := linkedin.NewClient(oauthManager, limiter, log)

			agent := publisher.NewAgent(aiClient, linkedinClient, repo, cfg.Publishing, log)

			// Configure media support if enabled
			if cfg.Media.Enabled && cfg.Media.UnsplashAPIKey != "" {
				unsplashClient := unsplash.NewClient(cfg.Media.UnsplashAPIKey, log)
				agent.SetMediaConfig(cfg.Media, unsplashClient)
				log.Info().Msg("Media support enabled with Unsplash")
			}

			// Set up tracker if enabled
			if cfg.Tracker.Enabled {
				t, err := tracker.NewSheetsTracker(tracker.Config{
					Enabled:            cfg.Tracker.Enabled,
					SpreadsheetID:      cfg.Tracker.SpreadsheetID,
					SheetName:          cfg.Tracker.SheetName,
					CredentialsFile:    cfg.Tracker.CredentialsFile,
					ServiceAccountJSON: cfg.Tracker.ServiceAccountJSON,
				}, log)
				if err != nil {
					log.Warn().Err(err).Msg("Failed to create tracker")
				} else if t != nil {
					agent.SetTracker(t)
				}
			}

			result, err := agent.Publish(ctx, uint(postID))
			if err != nil {
				return err
			}

			fmt.Printf("\n=== Publish Result ===\n")
			fmt.Printf("Post ID:      %d\n", result.PostID)
			fmt.Printf("Published:    %v\n", result.Published)
			fmt.Printf("LinkedIn URN: %s\n", result.LinkedInURN)

			return nil
		},
	}

	return cmd
}

func publishScheduleCmd() *cobra.Command {
	var at string

	cmd := &cobra.Command{
		Use:   "schedule [post-id]",
		Short: "Schedule a post for later",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			postID, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid post ID: %w", err)
			}

			scheduledTime, err := time.Parse("2006-01-02 15:04", at)
			if err != nil {
				return fmt.Errorf("invalid time format, use: YYYY-MM-DD HH:MM")
			}

			limiter := ratelimit.NewDefaultLimiter()
			aiClient := ai.NewClient(cfg.Anthropic, limiter, log)
			oauthManager := linkedin.NewOAuthManager(cfg.LinkedIn, repo, log)
			linkedinClient := linkedin.NewClient(oauthManager, limiter, log)

			agent := publisher.NewAgent(aiClient, linkedinClient, repo, cfg.Publishing, log)

			if err := agent.SchedulePost(ctx, uint(postID), scheduledTime); err != nil {
				return err
			}

			fmt.Printf("Post %d scheduled for %s\n", postID, scheduledTime.Format(time.RFC1123))
			return nil
		},
	}

	cmd.Flags().StringVar(&at, "at", "", "Schedule time (YYYY-MM-DD HH:MM)")
	cmd.MarkFlagRequired("at")

	return cmd
}

func publishApproveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "approve [post-id]",
		Short: "Approve a draft post for publishing",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			postID, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid post ID: %w", err)
			}

			limiter := ratelimit.NewDefaultLimiter()
			aiClient := ai.NewClient(cfg.Anthropic, limiter, log)
			oauthManager := linkedin.NewOAuthManager(cfg.LinkedIn, repo, log)
			linkedinClient := linkedin.NewClient(oauthManager, limiter, log)

			agent := publisher.NewAgent(aiClient, linkedinClient, repo, cfg.Publishing, log)

			if err := agent.ApprovePost(ctx, uint(postID)); err != nil {
				return err
			}

			fmt.Printf("Post %d approved and scheduled for immediate publishing\n", postID)
			return nil
		},
	}

	return cmd
}

// ============ OAUTH COMMANDS ============

func oauthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "oauth",
		Short: "LinkedIn OAuth management",
	}

	cmd.AddCommand(oauthLoginCmd())
	cmd.AddCommand(oauthStatusCmd())
	cmd.AddCommand(oauthExportCmd())
	return cmd
}

func oauthLoginCmd() *cobra.Command {
	var port int

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Start LinkedIn OAuth login flow",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			oauthManager := linkedin.NewOAuthManager(cfg.LinkedIn, repo, log)

			fmt.Printf("Starting OAuth server on port %d...\n", port)
			authURL, err := oauthManager.StartOAuthServer(ctx, port)

			if err != nil {
				return fmt.Errorf("OAuth failed: %w", err)
			}

			fmt.Printf("\nPlease open this URL in your browser:\n%s\n", authURL)
			fmt.Println("\nAuthentication successful!")

			return nil
		},
	}

	cmd.Flags().IntVar(&port, "port", 8080, "Port for OAuth callback server")
	return cmd
}

func oauthStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check OAuth token status",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			oauthManager := linkedin.NewOAuthManager(cfg.LinkedIn, repo, log)
			valid, expiresAt, err := oauthManager.GetTokenStatus(ctx)

			if err != nil {
				fmt.Println("Status: Not authenticated")
				fmt.Println("Run 'linkedin-agent oauth login' to authenticate")
				return nil
			}

			fmt.Printf("Status:     %s\n", map[bool]string{true: "Valid", false: "Expired"}[valid])
			fmt.Printf("Expires at: %s\n", expiresAt.Format(time.RFC1123))

			if !valid {
				fmt.Println("\nToken expired. Run 'linkedin-agent oauth login' to re-authenticate")
			}

			return nil
		},
	}

	return cmd
}

func oauthExportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "export",
		Short: "Export OAuth token for environment variables (headless deployment)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			token, err := repo.GetToken(ctx, "linkedin")
			if err != nil {
				return fmt.Errorf("no token found - run 'oauth login' first: %w", err)
			}

			fmt.Println("# LinkedIn OAuth Token - Copy these to your environment variables:")
			fmt.Printf("LINKEDIN_ACCESS_TOKEN=%s\n", token.AccessToken)
			fmt.Printf("LINKEDIN_REFRESH_TOKEN=%s\n", token.RefreshToken)
			fmt.Printf("LINKEDIN_TOKEN_EXPIRES_AT=%s\n", token.ExpiresAt.Format(time.RFC3339))

			return nil
		},
	}
}

// ============ TOPICS COMMANDS ============

func topicsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "topics",
		Short: "List and manage topics",
	}

	cmd.AddCommand(topicsListCmd())
	return cmd
}

func topicsListCmd() *cobra.Command {
	var status string
	var minScore float64
	var limit int

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List discovered topics",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			filter := storage.DefaultTopicFilter()
			filter.Limit = limit

			if minScore > 0 {
				filter.MinScore = &minScore
			}

			if status != "" {
				s := models.TopicStatus(status)
				filter.Status = &s
			}

			topics, err := repo.ListTopics(ctx, filter)
			if err != nil {
				return err
			}

			fmt.Printf("\n=== Topics (%d) ===\n\n", len(topics))
			for _, t := range topics {
				fmt.Printf("[%d] %.0f%% | %s\n", t.ID, t.AIScore, t.Title)
				fmt.Printf("    Source: %s | Status: %s\n", t.SourceName, t.Status)
				if t.AIAnalysis != "" {
					fmt.Printf("    Analysis: %s\n", t.AIAnalysis)
				}
				fmt.Println()
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&status, "status", "", "Filter by status (pending, approved, rejected, used)")
	cmd.Flags().Float64Var(&minScore, "min-score", 0, "Minimum AI score")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum topics to show")

	return cmd
}

// ============ POSTS COMMANDS ============

func postsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "posts",
		Short: "List and manage posts",
	}

	cmd.AddCommand(postsListCmd())
	cmd.AddCommand(postsQueueCmd())
	return cmd
}

func postsListCmd() *cobra.Command {
	var status string
	var limit int

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all posts",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			filter := storage.DefaultPostFilter()
			filter.Limit = limit

			if status != "" {
				s := models.PostStatus(status)
				filter.Status = &s
			}

			posts, err := repo.ListPosts(ctx, filter)
			if err != nil {
				return err
			}

			fmt.Printf("\n=== Posts (%d) ===\n\n", len(posts))
			for _, p := range posts {
				topicTitle := "N/A"
				if p.Topic != nil {
					topicTitle = p.Topic.Title
				}

				fmt.Printf("[%d] %s | %s\n", p.ID, p.Status, p.PostType)
				fmt.Printf("    Topic: %s\n", topicTitle)
				fmt.Printf("    Created: %s\n", p.CreatedAt.Format(time.RFC1123))

				if p.ScheduledFor != nil {
					fmt.Printf("    Scheduled: %s\n", p.ScheduledFor.Format(time.RFC1123))
				}
				if p.PublishedAt != nil {
					fmt.Printf("    Published: %s\n", p.PublishedAt.Format(time.RFC1123))
				}
				fmt.Println()
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&status, "status", "", "Filter by status")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum posts to show")

	return cmd
}

func postsQueueCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "queue",
		Short: "Show scheduled posts queue",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			// Get next 24 hours of scheduled posts
			posts, err := repo.GetScheduledPosts(ctx, time.Now().Add(24*time.Hour))
			if err != nil {
				return err
			}

			fmt.Printf("\n=== Scheduled Posts Queue (%d) ===\n\n", len(posts))

			if len(posts) == 0 {
				fmt.Println("No posts scheduled in the next 24 hours")
				return nil
			}

			for _, p := range posts {
				fmt.Printf("[%d] Scheduled for: %s\n", p.ID, p.ScheduledFor.Format(time.RFC1123))
				if p.Topic != nil {
					fmt.Printf("    Topic: %s\n", p.Topic.Title)
				}
				// Show preview (first 100 chars)
				preview := p.Content
				if len(preview) > 100 {
					preview = preview[:100] + "..."
				}
				fmt.Printf("    Preview: %s\n\n", preview)
			}

			return nil
		},
	}

	return cmd
}

// ============ TRACKER COMMANDS ============

func trackerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tracker",
		Short: "Google Sheets tracker management",
	}

	cmd.AddCommand(trackerInitCmd())
	cmd.AddCommand(trackerListCmd())
	cmd.AddCommand(trackerAddCmd())
	cmd.AddCommand(trackerSyncTopicsCmd())
	return cmd
}

func trackerInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize Google Sheet with headers",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			if !cfg.Tracker.Enabled {
				return fmt.Errorf("tracker is not enabled in config - set tracker.enabled=true and tracker.spreadsheet_id")
			}

			t, err := tracker.NewSheetsTracker(tracker.Config{
				Enabled:            cfg.Tracker.Enabled,
				SpreadsheetID:      cfg.Tracker.SpreadsheetID,
				SheetName:          cfg.Tracker.SheetName,
				CredentialsFile:    cfg.Tracker.CredentialsFile,
				ServiceAccountJSON: cfg.Tracker.ServiceAccountJSON,
			}, log)
			if err != nil {
				return fmt.Errorf("failed to create tracker: %w", err)
			}

			if err := t.InitializeSheet(ctx); err != nil {
				return fmt.Errorf("failed to initialize sheet: %w", err)
			}

			fmt.Println("Google Sheet initialized successfully!")
			fmt.Printf("Spreadsheet ID: %s\n", cfg.Tracker.SpreadsheetID)
			fmt.Printf("Sheet Name: %s\n", cfg.Tracker.SheetName)
			fmt.Println("\nColumns created:")
			for i, col := range tracker.SheetColumns {
				fmt.Printf("  %d. %s\n", i+1, col)
			}

			return nil
		},
	}
}

func trackerListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all tracked posts from Google Sheet",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			if !cfg.Tracker.Enabled {
				return fmt.Errorf("tracker is not enabled in config")
			}

			t, err := tracker.NewSheetsTracker(tracker.Config{
				Enabled:            cfg.Tracker.Enabled,
				SpreadsheetID:      cfg.Tracker.SpreadsheetID,
				SheetName:          cfg.Tracker.SheetName,
				CredentialsFile:    cfg.Tracker.CredentialsFile,
				ServiceAccountJSON: cfg.Tracker.ServiceAccountJSON,
			}, log)
			if err != nil {
				return fmt.Errorf("failed to create tracker: %w", err)
			}

			posts, err := t.GetAllPosts(ctx)
			if err != nil {
				return fmt.Errorf("failed to get posts: %w", err)
			}

			fmt.Printf("\n=== Tracked Posts (%d) ===\n\n", len(posts))
			for _, p := range posts {
				fmt.Printf("[%d] %s | %s\n", p.TopicID, p.Status, p.TopicTitle)
				if p.LinkedInURL != "" {
					fmt.Printf("    URL: %s\n", p.LinkedInURL)
				}
				fmt.Println()
			}

			return nil
		},
	}
}

func trackerAddCmd() *cobra.Command {
	var postID uint

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a post to the tracker manually",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			if !cfg.Tracker.Enabled {
				return fmt.Errorf("tracker is not enabled in config")
			}

			// Get post from database
			post, err := repo.GetPostByID(ctx, postID)
			if err != nil {
				return fmt.Errorf("post not found: %w", err)
			}

			// Get topic
			var topic *models.Topic
			if post.TopicID != nil {
				topic, err = repo.GetTopicByID(ctx, *post.TopicID)
				if err != nil {
					return fmt.Errorf("topic not found: %w", err)
				}
			} else {
				return fmt.Errorf("post has no associated topic")
			}

			t, err := tracker.NewSheetsTracker(tracker.Config{
				Enabled:            cfg.Tracker.Enabled,
				SpreadsheetID:      cfg.Tracker.SpreadsheetID,
				SheetName:          cfg.Tracker.SheetName,
				CredentialsFile:    cfg.Tracker.CredentialsFile,
				ServiceAccountJSON: cfg.Tracker.ServiceAccountJSON,
			}, log)
			if err != nil {
				return fmt.Errorf("failed to create tracker: %w", err)
			}

			// Track the post
			if err := t.TrackNewPost(ctx, topic, post); err != nil {
				return fmt.Errorf("failed to track post: %w", err)
			}

			// If published, update status
			if post.Status == models.PostStatusPublished && post.LinkedInPostURN != "" {
				if err := t.UpdatePostPublished(ctx, topic.ID, post.LinkedInPostURN); err != nil {
					return fmt.Errorf("failed to update publish status: %w", err)
				}
			}

			fmt.Printf("Post %d added to tracker successfully!\n", postID)
			fmt.Printf("Topic: %s\n", topic.Title)
			fmt.Printf("Status: %s\n", post.Status)
			if post.LinkedInPostURN != "" {
				fmt.Printf("LinkedIn: https://www.linkedin.com/feed/update/%s\n", post.LinkedInPostURN)
			}

			return nil
		},
	}

	cmd.Flags().UintVar(&postID, "post-id", 0, "Post ID to add to tracker (required)")
	cmd.MarkFlagRequired("post-id")

	return cmd
}

func trackerSyncTopicsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync-topics",
		Short: "Sync all discovered topics to Google Sheets",
		Long: `Syncs all topics from the database to a "Topics" sheet in Google Sheets.
This allows you to review all discovered topics and mark which ones to use for posts.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			if !cfg.Tracker.Enabled {
				return fmt.Errorf("tracker is not enabled in config")
			}

			t, err := tracker.NewSheetsTracker(tracker.Config{
				Enabled:            cfg.Tracker.Enabled,
				SpreadsheetID:      cfg.Tracker.SpreadsheetID,
				SheetName:          cfg.Tracker.SheetName,
				CredentialsFile:    cfg.Tracker.CredentialsFile,
				ServiceAccountJSON: cfg.Tracker.ServiceAccountJSON,
			}, log)
			if err != nil {
				return fmt.Errorf("failed to create tracker: %w", err)
			}

			// Get all topics from database (use high limit to get all)
			topics, err := repo.ListTopics(ctx, storage.TopicFilter{
				Limit:     1000,
				OrderBy:   "discovered_at",
				OrderDesc: true,
			})
			if err != nil {
				return fmt.Errorf("failed to get topics: %w", err)
			}

			fmt.Printf("Found %d topics in database, syncing to Google Sheets...\n", len(topics))

			// Sync topics to sheet
			added, updated, err := t.SyncTopics(ctx, topics)
			if err != nil {
				return fmt.Errorf("failed to sync topics: %w", err)
			}

			fmt.Printf("\nSync complete!\n")
			fmt.Printf("  Added: %d new topics\n", added)
			fmt.Printf("  Updated: %d existing topics\n", updated)
			fmt.Printf("\nView at: https://docs.google.com/spreadsheets/d/%s\n", cfg.Tracker.SpreadsheetID)

			return nil
		},
	}
}

// ============ COMMENTS COMMANDS ============

func commentsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "comments",
		Short: "Comment automation commands",
	}

	cmd.AddCommand(commentsListCmd())
	cmd.AddCommand(commentsRunCmd())
	cmd.AddCommand(commentsDiscoverCmd())
	return cmd
}

func commentsListCmd() *cobra.Command {
	var status string
	var limit int

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List comments",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			filter := storage.DefaultCommentFilter()
			filter.Limit = limit

			if status != "" {
				s := models.CommentStatus(status)
				filter.Status = &s
			}

			comments, err := repo.ListComments(ctx, filter)
			if err != nil {
				return err
			}

			fmt.Printf("\n=== Comments (%d) ===\n\n", len(comments))
			for _, c := range comments {
				fmt.Printf("[%d] %s | Style: %s\n", c.ID, c.Status, c.CommentStyle)
				fmt.Printf("    Target: %s\n", c.TargetPostTitle)
				if c.TargetAuthorName != "" {
					fmt.Printf("    Author: %s\n", c.TargetAuthorName)
				}
				fmt.Printf("    Comment: %s\n", truncateStr(c.Content, 100))
				if c.PostedAt != nil {
					fmt.Printf("    Posted: %s\n", c.PostedAt.Format(time.RFC1123))
				}
				if c.PostEngagement > 0 {
					fmt.Printf("    Post Engagement: %d\n", c.PostEngagement)
				}
				if c.ErrorMessage != "" {
					fmt.Printf("    Error: %s\n", c.ErrorMessage)
				}
				fmt.Println()
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&status, "status", "", "Filter by status (pending, posted, failed, skipped)")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum comments to show")

	return cmd
}

func commentsRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the comment automation (posts one comment if conditions are met)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			limiter := ratelimit.NewDefaultLimiter()
			aiClient := ai.NewClient(cfg.Anthropic, limiter, log)
			oauthManager := linkedin.NewOAuthManager(cfg.LinkedIn, repo, log)
			linkedinClient := linkedin.NewClient(oauthManager, limiter, log)

			agent := commenter.NewAgent(aiClient, linkedinClient, repo, cfg.Commenter, log)

			result, err := agent.Run(ctx)
			if err != nil {
				return err
			}

			fmt.Printf("\n=== Comment Run Results ===\n")
			fmt.Printf("Posts Discovered:   %d\n", result.PostsDiscovered)
			fmt.Printf("Comments Generated: %d\n", result.CommentsGenerated)
			fmt.Printf("Comments Posted:    %d\n", result.CommentsPosted)
			fmt.Printf("Comments Skipped:   %d\n", result.CommentsSkipped)
			fmt.Printf("Duration:           %s\n", result.Duration)

			if len(result.Errors) > 0 {
				fmt.Printf("\nErrors:\n")
				for _, e := range result.Errors {
					fmt.Printf("  - %s\n", e)
				}
			}

			return nil
		},
	}

	return cmd
}

func commentsDiscoverCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Discover posts that could be commented on (dry run)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			if len(cfg.Commenter.TargetInfluencers) == 0 {
				fmt.Println("No target influencers configured.")
				fmt.Println("Add LinkedIn profile URNs to commenter.target_influencers in your config.")
				return nil
			}

			limiter := ratelimit.NewDefaultLimiter()
			oauthManager := linkedin.NewOAuthManager(cfg.LinkedIn, repo, log)
			linkedinClient := linkedin.NewClient(oauthManager, limiter, log)

			fmt.Printf("Discovering posts from %d influencer(s)...\n\n", len(cfg.Commenter.TargetInfluencers))

			var allPosts []*linkedin.LinkedInPost

			for _, influencerURN := range cfg.Commenter.TargetInfluencers {
				posts, err := linkedinClient.GetPostsByAuthor(ctx, influencerURN, 5)
				if err != nil {
					fmt.Printf("  [ERROR] %s: %v\n", influencerURN, err)
					continue
				}

				for _, post := range posts {
					engagement := post.LikeCount + post.CommentCount

					// Apply filters
					if engagement < cfg.Commenter.MinPostEngagement {
						continue
					}
					if cfg.Commenter.MaxPostEngagement > 0 && engagement > cfg.Commenter.MaxPostEngagement {
						continue
					}

					allPosts = append(allPosts, post)
				}
			}

			if len(allPosts) == 0 {
				fmt.Println("No eligible posts found matching your criteria.")
				return nil
			}

			fmt.Printf("=== Eligible Posts (%d) ===\n\n", len(allPosts))
			for i, post := range allPosts {
				engagement := post.LikeCount + post.CommentCount
				publishedAt := time.Unix(post.PublishedAt/1000, 0)
				age := time.Since(publishedAt)

				// Calculate engagement velocity
				hoursOld := age.Hours()
				if hoursOld < 0.5 {
					hoursOld = 0.5
				}
				velocity := float64(post.LikeCount+post.CommentCount*2) / hoursOld

				fmt.Printf("[%d] URN: %s\n", i+1, post.URN)
				fmt.Printf("    Engagement: %d (Likes: %d, Comments: %d)\n", engagement, post.LikeCount, post.CommentCount)
				fmt.Printf("    Velocity: %.1f engagements/hour\n", velocity)
				fmt.Printf("    Age: %s\n", formatDuration(age))
				fmt.Printf("    Content: %s\n", truncateStr(post.Commentary, 150))

				// Check if already commented
				existing, _ := repo.GetCommentByTargetURN(ctx, post.URN)
				if existing != nil {
					fmt.Printf("    [Already commented]\n")
				}
				fmt.Println()
			}

			return nil
		},
	}

	return cmd
}

// Helper function to truncate strings
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// Helper function to format duration nicely
func formatDuration(d time.Duration) string {
	if d < time.Hour {
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%.1f hours", d.Hours())
	}
	return fmt.Sprintf("%.1f days", d.Hours()/24)
}
