package models

import (
	"database/sql/driver"
	"encoding/json"
	"time"
)

// TopicStatus represents the current state of a topic
type TopicStatus string

const (
	TopicStatusPending  TopicStatus = "pending"
	TopicStatusApproved TopicStatus = "approved"
	TopicStatusRejected TopicStatus = "rejected"
	TopicStatusUsed     TopicStatus = "used"
)

// StringSlice is a custom type for storing string arrays in JSON
type StringSlice []string

func (s StringSlice) Value() (driver.Value, error) {
	return json.Marshal(s)
}

func (s *StringSlice) Scan(value interface{}) error {
	if value == nil {
		*s = nil
		return nil
	}
	return json.Unmarshal(value.([]byte), s)
}

// JSON is a custom type for storing arbitrary JSON data
type JSON map[string]interface{}

func (j JSON) Value() (driver.Value, error) {
	return json.Marshal(j)
}

func (j *JSON) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}
	return json.Unmarshal(value.([]byte), j)
}

// Topic represents a discovered topic from various sources
type Topic struct {
	ID           uint        `gorm:"primaryKey" json:"id"`
	ExternalID   string      `gorm:"uniqueIndex;not null" json:"external_id"` // Hash of source + URL
	Title        string      `gorm:"not null" json:"title"`
	Description  string      `json:"description"`
	URL          string      `json:"url"`
	SourceType   string      `gorm:"index;not null" json:"source_type"` // rss, newsapi, reddit, twitter, custom
	SourceName   string      `json:"source_name"`                       // Specific source identifier
	Keywords     StringSlice `gorm:"type:json" json:"keywords"`
	RawData      JSON        `gorm:"type:json" json:"raw_data"` // Original API response
	AIScore      float64     `json:"ai_score"`                  // Claude-generated relevance score (0-100)
	AIAnalysis   string      `json:"ai_analysis"`               // Claude's reasoning for the score
	Status       TopicStatus `gorm:"default:'pending'" json:"status"`
	DiscoveredAt time.Time   `gorm:"autoCreateTime" json:"discovered_at"`
	UpdatedAt    time.Time   `gorm:"autoUpdateTime" json:"updated_at"`
}

// IsHighScore returns true if the topic score is above the auto-publish threshold (80)
func (t *Topic) IsHighScore() bool {
	return t.AIScore >= 80.0
}

// RawTopic represents a topic before normalization (from source APIs)
type RawTopic struct {
	Title       string
	Description string
	URL         string
	SourceType  string
	SourceName  string
	Keywords    []string
	RawData     map[string]interface{}
	PublishedAt time.Time
}
