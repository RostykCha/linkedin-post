package models

import (
	"time"
)

// SourceConfig represents configuration for a topic source
type SourceConfig struct {
	ID            uint        `gorm:"primaryKey" json:"id"`
	Name          string      `gorm:"uniqueIndex;not null" json:"name"`
	Type          string      `gorm:"not null" json:"type"` // rss, newsapi, reddit, twitter, custom
	Enabled       bool        `gorm:"default:true" json:"enabled"`
	Config        JSON        `gorm:"type:json" json:"config"`   // Source-specific configuration
	Keywords      StringSlice `gorm:"type:json" json:"keywords"` // Keywords to filter/search
	FetchInterval string      `gorm:"default:'1h'" json:"fetch_interval"`
	LastFetchAt   *time.Time  `json:"last_fetch_at"`
	CreatedAt     time.Time   `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt     time.Time   `gorm:"autoUpdateTime" json:"updated_at"`
}

// Schedule represents a scheduled task configuration
type Schedule struct {
	ID        uint       `gorm:"primaryKey" json:"id"`
	Name      string     `gorm:"not null" json:"name"`
	CronExpr  string     `gorm:"not null" json:"cron_expr"` // Cron expression
	TaskType  string     `gorm:"not null" json:"task_type"` // discover, publish, cleanup
	Config    JSON       `gorm:"type:json" json:"config"`   // Task-specific parameters
	Enabled   bool       `gorm:"default:true" json:"enabled"`
	LastRunAt *time.Time `json:"last_run_at"`
	NextRunAt *time.Time `json:"next_run_at"`
	CreatedAt time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}
