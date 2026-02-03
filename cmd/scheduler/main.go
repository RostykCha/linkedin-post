package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/robfig/cron/v3"
	"github.com/spf13/cobra"

	"github.com/linkedin-agent/internal/agent/discovery"
	"github.com/linkedin-agent/internal/agent/publisher"
	"github.com/linkedin-agent/internal/ai"
	"github.com/linkedin-agent/internal/config"
	"github.com/linkedin-agent/internal/linkedin"
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
		Use:   "linkedin-scheduler",
		Short: "Background scheduler for LinkedIn agent",
		Long: `Runs scheduled discovery and publishing tasks in the background.
This daemon should be run as a service for autonomous operation.`,
		RunE: runScheduler,
	}

	rootCmd.Flags().StringVar(&cfgFile, "config", "", "config file path")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runScheduler(cmd *cobra.Command, args []string) error {
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

	log.Info().Msg("Starting LinkedIn Agent Scheduler")

	// Initialize database
	repo, err = sqlite.New(cfg.Database.DSN)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer repo.Close()

	if err := repo.Migrate(); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	// Initialize rate limiter
	limiter := ratelimit.NewDefaultLimiter()

	// Initialize AI client
	aiClient := ai.NewClient(cfg.Anthropic, limiter, log)

	// Initialize source manager
	sourceManager := source.NewManager()
	if cfg.Sources.RSS.Enabled {
		for _, src := range rss.NewMultiple(cfg.Sources.RSS, log) {
			sourceManager.Register(src)
		}
	}
	if cfg.Sources.Custom.Enabled {
		sourceManager.Register(custom.New(cfg.Sources.Custom, log))
	}

	// Initialize LinkedIn client
	oauthManager := linkedin.NewOAuthManager(cfg.LinkedIn, repo, log)
	linkedinClient := linkedin.NewClient(oauthManager, limiter, log)

	// Create agents
	discoveryAgent := discovery.NewAgent(sourceManager, aiClient, repo, log)
	publisherAgent := publisher.NewAgent(aiClient, linkedinClient, repo, cfg.Publishing, log)

	// Create cron scheduler
	c := cron.New(cron.WithLogger(cronLogger{log}))

	// Schedule discovery job
	_, err = c.AddFunc(cfg.Scheduler.DiscoveryCron, func() {
		ctx := context.Background()
		log.Info().Msg("Running scheduled discovery")

		result, err := discoveryAgent.Run(ctx)
		if err != nil {
			log.Error().Err(err).Msg("Scheduled discovery failed")
			return
		}

		log.Info().
			Int("topics_found", result.TopicsFound).
			Int("topics_saved", result.TopicsSaved).
			Msg("Scheduled discovery completed")
	})
	if err != nil {
		return fmt.Errorf("failed to schedule discovery job: %w", err)
	}
	log.Info().Str("cron", cfg.Scheduler.DiscoveryCron).Msg("Discovery job scheduled")

	// Schedule publish job
	_, err = c.AddFunc(cfg.Scheduler.PublishCron, func() {
		ctx := context.Background()
		log.Info().Msg("Running scheduled publish")

		published, errors := publisherAgent.ProcessScheduledPosts(ctx)
		if len(errors) > 0 {
			for _, e := range errors {
				log.Error().Err(e).Msg("Publish error")
			}
		}

		log.Info().
			Int("published", published).
			Int("errors", len(errors)).
			Msg("Scheduled publish completed")
	})
	if err != nil {
		return fmt.Errorf("failed to schedule publish job: %w", err)
	}
	log.Info().Str("cron", cfg.Scheduler.PublishCron).Msg("Publish job scheduled")

	// Start scheduler
	c.Start()
	log.Info().Msg("Scheduler started")

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Info().Msg("Shutting down scheduler")
	c.Stop()

	return nil
}

// cronLogger adapts our logger for cron
type cronLogger struct {
	log *logger.Logger
}

func (l cronLogger) Info(msg string, keysAndValues ...interface{}) {
	l.log.Info().Msgf(msg, keysAndValues...)
}

func (l cronLogger) Error(err error, msg string, keysAndValues ...interface{}) {
	l.log.Error().Err(err).Msgf(msg, keysAndValues...)
}
