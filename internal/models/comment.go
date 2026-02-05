package models

import (
	"time"
)

// CommentStatus represents the current state of a comment
type CommentStatus string

const (
	CommentStatusPending CommentStatus = "pending"
	CommentStatusPosted  CommentStatus = "posted"
	CommentStatusFailed  CommentStatus = "failed"
	CommentStatusSkipped CommentStatus = "skipped"
)

// Comment represents a LinkedIn comment (generated or posted)
type Comment struct {
	ID               uint          `gorm:"primaryKey" json:"id"`
	TargetPostURN    string        `gorm:"size:255;index" json:"target_post_urn"`
	TargetAuthorURN  string        `gorm:"size:255" json:"target_author_urn"`
	TargetAuthorName string        `gorm:"size:255" json:"target_author_name"`
	TargetPostTitle  string        `gorm:"size:500" json:"target_post_title"`
	Content          string        `gorm:"type:text" json:"content"`
	CommentURN       string        `gorm:"size:255" json:"comment_urn"`
	Status           CommentStatus `gorm:"size:20;default:'pending'" json:"status"`
	ErrorMessage     string        `json:"error_message"`
	// New fields for tracking and analytics
	CommentStyle     string        `gorm:"size:50" json:"comment_style"`    // Style used (insightful, question, supportive)
	AIReasoning      string        `gorm:"type:text" json:"ai_reasoning"`   // AI's reasoning for the comment
	PostEngagement   int           `json:"post_engagement"`                 // Engagement at time of comment
	PostedAt         *time.Time    `json:"posted_at"`                       // When actually posted to LinkedIn
	CreatedAt        time.Time     `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt        time.Time     `gorm:"autoUpdateTime" json:"updated_at"`
}

// TargetPost represents a LinkedIn post to potentially comment on
type TargetPost struct {
	URN                string    `json:"urn"`
	AuthorURN          string    `json:"author_urn"`
	AuthorName         string    `json:"author_name"`
	Content            string    `json:"content"`
	LikeCount          int       `json:"like_count"`
	CommentCount       int       `json:"comment_count"`
	PublishedAt        time.Time `json:"published_at"`
	EngagementVelocity float64   `json:"engagement_velocity"` // Engagements per hour since posted
}
