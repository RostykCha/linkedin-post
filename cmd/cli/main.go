package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/linkedin-agent/internal/agent/discovery"
	"github.com/linkedin-agent/internal/agent/publisher"
	"github.com/linkedin-agent/internal/ai"
	"github.com/linkedin-agent/internal/config"
	"github.com/linkedin-agent/internal/linkedin"
	"github.com/linkedin-agent/internal/models"
	"github.com/linkedin-agent/internal/source"
	"github.com/linkedin-agent/internal/source/custom"
	"github.com/linkedin-agent/internal/source/rss"
	"github.com/linkedin-agent/internal/storage"
	"github.com/linkedin-agent/internal/storage/sqlite"
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

	// Initialize database
	repo, err = sqlite.New(cfg.Database.DSN)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
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
