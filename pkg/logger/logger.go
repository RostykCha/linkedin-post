package logger

import (
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
)

// Logger wraps zerolog.Logger with additional context
type Logger struct {
	zerolog.Logger
}

// Config holds logger configuration
type Config struct {
	Level  string // debug, info, warn, error
	Format string // json or console
	Output string // stdout or file path
}

// New creates a new logger with the given configuration
func New(cfg Config) *Logger {
	var output io.Writer = os.Stdout

	// Set output
	if cfg.Output != "" && cfg.Output != "stdout" {
		file, err := os.OpenFile(cfg.Output, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err == nil {
			output = file
		}
	}

	// Set format
	if cfg.Format == "console" {
		output = zerolog.ConsoleWriter{
			Out:        output,
			TimeFormat: time.RFC3339,
		}
	}

	// Parse level
	level, err := zerolog.ParseLevel(cfg.Level)
	if err != nil {
		level = zerolog.InfoLevel
	}

	logger := zerolog.New(output).
		Level(level).
		With().
		Timestamp().
		Caller().
		Logger()

	return &Logger{Logger: logger}
}

// Default creates a default console logger
func Default() *Logger {
	return New(Config{
		Level:  "info",
		Format: "console",
		Output: "stdout",
	})
}

// WithComponent adds a component field to the logger
func (l *Logger) WithComponent(component string) *Logger {
	return &Logger{
		Logger: l.With().Str("component", component).Logger(),
	}
}

// WithSource adds a source field to the logger (for topic sources)
func (l *Logger) WithSource(sourceType, sourceName string) *Logger {
	return &Logger{
		Logger: l.With().
			Str("source_type", sourceType).
			Str("source_name", sourceName).
			Logger(),
	}
}

// WithTopicID adds a topic ID to the logger
func (l *Logger) WithTopicID(id uint) *Logger {
	return &Logger{
		Logger: l.With().Uint("topic_id", id).Logger(),
	}
}

// WithPostID adds a post ID to the logger
func (l *Logger) WithPostID(id uint) *Logger {
	return &Logger{
		Logger: l.With().Uint("post_id", id).Logger(),
	}
}
