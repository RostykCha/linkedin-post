package models

import (
	"time"
)

// PostType represents the type of LinkedIn post
type PostType string

const (
	PostTypeText    PostType = "text"
	PostTypePoll    PostType = "poll"
	PostTypeArticle PostType = "article"
)

// PostStatus represents the current state of a post
type PostStatus string

const (
	PostStatusDraft      PostStatus = "draft"
	PostStatusScheduled  PostStatus = "scheduled"
	PostStatusPublishing PostStatus = "publishing"
	PostStatusPublished  PostStatus = "published"
	PostStatusFailed     PostStatus = "failed"
)

// Post represents a LinkedIn post (draft or published)
type Post struct {
	ID               uint       `gorm:"primaryKey" json:"id"`
	TopicID          *uint      `gorm:"index" json:"topic_id"` // Nullable for manual posts
	Topic            *Topic     `gorm:"foreignKey:TopicID" json:"topic,omitempty"`
	Content          string     `gorm:"type:text;not null" json:"content"`
	PostType         PostType   `gorm:"default:'text'" json:"post_type"`
	PostFormat       JSON       `gorm:"type:json" json:"post_format"` // Poll options, article metadata
	GenerationPrompt string     `gorm:"type:text" json:"generation_prompt"`
	AIMetadata       JSON       `gorm:"type:json" json:"ai_metadata"` // Claude's generation metadata
	LinkedInPostURN  string     `gorm:"index" json:"linkedin_post_urn"`
	Status           PostStatus `gorm:"default:'draft'" json:"status"`
	ScheduledFor     *time.Time `gorm:"index" json:"scheduled_for"`
	PublishedAt      *time.Time `json:"published_at"`
	ErrorMessage     string     `json:"error_message"`
	RetryCount       int        `gorm:"default:0" json:"retry_count"`
	CreatedAt        time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt        time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

// PollOption represents a single option in a LinkedIn poll
type PollOption struct {
	Text string `json:"text"`
}

// PollFormat represents the format data for a poll post
type PollFormat struct {
	Question string       `json:"question"`
	Options  []PollOption `json:"options"`
	Duration string       `json:"duration"` // ONE_DAY, THREE_DAYS, ONE_WEEK, TWO_WEEKS
}

// ShouldAutoPublish returns true if the post should be auto-published (high score topic)
func (p *Post) ShouldAutoPublish() bool {
	if p.Topic != nil {
		return p.Topic.IsHighScore()
	}
	return false
}

// CanRetry returns true if the post can be retried (max 3 attempts)
func (p *Post) CanRetry() bool {
	return p.Status == PostStatusFailed && p.RetryCount < 3
}
