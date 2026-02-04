package main

import (
	"context"
	"fmt"
	"net/http"
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
	"github.com/linkedin-agent/internal/storage/sheets"
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
	defer repo.Close()

	if err := repo.Migrate(); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	// Start health check server for Render
	go startHealthServer()

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

	// Initialize LinkedIn client with env-only OAuth (tokens from env vars)
	oauthManager := linkedin.NewOAuthManagerEnvOnly(cfg.LinkedIn, log)
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

// startHealthServer starts a simple HTTP server for health checks (used by Render)
func startHealthServer() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "10000"
	}

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("LinkedIn Agent Scheduler"))
	})

	log.Info().Str("port", port).Msg("Health check server starting")
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Error().Err(err).Msg("Health server failed")
	}
}
