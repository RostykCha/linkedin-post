package sqlite

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/linkedin-agent/internal/models"
	"github.com/linkedin-agent/internal/storage"
)

// Repository implements storage.Repository using SQLite
type Repository struct {
	db *gorm.DB
}

// New creates a new SQLite repository
func New(dsn string) (*Repository, error) {
	// Ensure directory exists
	dir := filepath.Dir(dsn)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create data directory: %w", err)
		}
	}

	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	return &Repository{db: db}, nil
}

// Migrate runs database migrations
func (r *Repository) Migrate() error {
	return r.db.AutoMigrate(
		&models.Topic{},
		&models.Post{},
		&models.OAuthToken{},
		&models.SourceConfig{},
		&models.Schedule{},
	)
}

// Close closes the database connection
func (r *Repository) Close() error {
	sqlDB, err := r.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// Topic operations

func (r *Repository) CreateTopic(ctx context.Context, topic *models.Topic) error {
	return r.db.WithContext(ctx).Create(topic).Error
}

func (r *Repository) GetTopicByID(ctx context.Context, id uint) (*models.Topic, error) {
	var topic models.Topic
	if err := r.db.WithContext(ctx).First(&topic, id).Error; err != nil {
		return nil, err
	}
	return &topic, nil
}

func (r *Repository) GetTopicByExternalID(ctx context.Context, externalID string) (*models.Topic, error) {
	var topic models.Topic
	if err := r.db.WithContext(ctx).Where("external_id = ?", externalID).First(&topic).Error; err != nil {
		return nil, err
	}
	return &topic, nil
}

func (r *Repository) ListTopics(ctx context.Context, filter storage.TopicFilter) ([]*models.Topic, error) {
	var topics []*models.Topic
	query := r.db.WithContext(ctx).Model(&models.Topic{})

	if filter.Status != nil {
		query = query.Where("status = ?", *filter.Status)
	}
	if filter.SourceType != nil {
		query = query.Where("source_type = ?", *filter.SourceType)
	}
	if filter.MinScore != nil {
		query = query.Where("ai_score >= ?", *filter.MinScore)
	}
	if filter.MaxScore != nil {
		query = query.Where("ai_score <= ?", *filter.MaxScore)
	}

	// Ordering
	orderCol := "ai_score"
	if filter.OrderBy != "" {
		orderCol = filter.OrderBy
	}
	if filter.OrderDesc {
		query = query.Order(orderCol + " DESC")
	} else {
		query = query.Order(orderCol + " ASC")
	}

	// Pagination
	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	}
	if filter.Offset > 0 {
		query = query.Offset(filter.Offset)
	}

	if err := query.Find(&topics).Error; err != nil {
		return nil, err
	}
	return topics, nil
}

func (r *Repository) UpdateTopic(ctx context.Context, topic *models.Topic) error {
	return r.db.WithContext(ctx).Save(topic).Error
}

func (r *Repository) DeleteTopic(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&models.Topic{}, id).Error
}

// Post operations

func (r *Repository) CreatePost(ctx context.Context, post *models.Post) error {
	return r.db.WithContext(ctx).Create(post).Error
}

func (r *Repository) GetPostByID(ctx context.Context, id uint) (*models.Post, error) {
	var post models.Post
	if err := r.db.WithContext(ctx).Preload("Topic").First(&post, id).Error; err != nil {
		return nil, err
	}
	return &post, nil
}

func (r *Repository) ListPosts(ctx context.Context, filter storage.PostFilter) ([]*models.Post, error) {
	var posts []*models.Post
	query := r.db.WithContext(ctx).Model(&models.Post{}).Preload("Topic")

	if filter.Status != nil {
		query = query.Where("status = ?", *filter.Status)
	}
	if filter.TopicID != nil {
		query = query.Where("topic_id = ?", *filter.TopicID)
	}

	// Ordering
	orderCol := "created_at"
	if filter.OrderBy != "" {
		orderCol = filter.OrderBy
	}
	if filter.OrderDesc {
		query = query.Order(orderCol + " DESC")
	} else {
		query = query.Order(orderCol + " ASC")
	}

	// Pagination
	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	}
	if filter.Offset > 0 {
		query = query.Offset(filter.Offset)
	}

	if err := query.Find(&posts).Error; err != nil {
		return nil, err
	}
	return posts, nil
}

func (r *Repository) UpdatePost(ctx context.Context, post *models.Post) error {
	return r.db.WithContext(ctx).Save(post).Error
}

func (r *Repository) DeletePost(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&models.Post{}, id).Error
}

func (r *Repository) GetScheduledPosts(ctx context.Context, before time.Time) ([]*models.Post, error) {
	var posts []*models.Post
	status := models.PostStatusScheduled
	if err := r.db.WithContext(ctx).
		Where("status = ? AND scheduled_for <= ?", status, before).
		Preload("Topic").
		Find(&posts).Error; err != nil {
		return nil, err
	}
	return posts, nil
}

// OAuth token operations

func (r *Repository) SaveToken(ctx context.Context, token *models.OAuthToken) error {
	// Upsert - update if exists, create if not
	var existing models.OAuthToken
	if err := r.db.WithContext(ctx).Where("provider = ?", token.Provider).First(&existing).Error; err == nil {
		token.ID = existing.ID
	}
	return r.db.WithContext(ctx).Save(token).Error
}

func (r *Repository) GetToken(ctx context.Context, provider string) (*models.OAuthToken, error) {
	var token models.OAuthToken
	if err := r.db.WithContext(ctx).Where("provider = ?", provider).First(&token).Error; err != nil {
		return nil, err
	}
	return &token, nil
}

func (r *Repository) DeleteToken(ctx context.Context, provider string) error {
	return r.db.WithContext(ctx).Where("provider = ?", provider).Delete(&models.OAuthToken{}).Error
}

// Source config operations

func (r *Repository) GetSourceConfigs(ctx context.Context) ([]*models.SourceConfig, error) {
	var configs []*models.SourceConfig
	if err := r.db.WithContext(ctx).Find(&configs).Error; err != nil {
		return nil, err
	}
	return configs, nil
}

func (r *Repository) SaveSourceConfig(ctx context.Context, config *models.SourceConfig) error {
	return r.db.WithContext(ctx).Save(config).Error
}

// Schedule operations

func (r *Repository) GetSchedules(ctx context.Context) ([]*models.Schedule, error) {
	var schedules []*models.Schedule
	if err := r.db.WithContext(ctx).Find(&schedules).Error; err != nil {
		return nil, err
	}
	return schedules, nil
}

func (r *Repository) SaveSchedule(ctx context.Context, schedule *models.Schedule) error {
	return r.db.WithContext(ctx).Save(schedule).Error
}
