package sheets

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"

	"github.com/linkedin-agent/internal/models"
	"github.com/linkedin-agent/internal/storage"
	"github.com/linkedin-agent/pkg/logger"
)

const (
	topicsSheetName = "Topics"
	postsSheetName  = "Posts"
)

// Config holds configuration for Sheets repository
type Config struct {
	SpreadsheetID      string
	ServiceAccountJSON string
	CredentialsFile    string
}

// Repository implements storage.Repository using Google Sheets
type Repository struct {
	service       *sheets.Service
	spreadsheetID string
	log           *logger.Logger
	mu            sync.RWMutex
	nextTopicID   uint
	nextPostID    uint
}

// New creates a new Sheets repository
func New(cfg Config, log *logger.Logger) (*Repository, error) {
	ctx := context.Background()

	var srv *sheets.Service
	var err error

	if cfg.ServiceAccountJSON != "" {
		srv, err = sheets.NewService(ctx, option.WithCredentialsJSON([]byte(cfg.ServiceAccountJSON)))
	} else if cfg.CredentialsFile != "" {
		srv, err = sheets.NewService(ctx, option.WithCredentialsFile(cfg.CredentialsFile))
	} else {
		return nil, fmt.Errorf("no Google credentials provided")
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create sheets service: %w", err)
	}

	repo := &Repository{
		service:       srv,
		spreadsheetID: cfg.SpreadsheetID,
		log:           log.WithComponent("sheets-repo"),
		nextTopicID:   1,
		nextPostID:    1,
	}

	return repo, nil
}

// Migrate creates sheets and headers if they don't exist
func (r *Repository) Migrate() error {
	ctx := context.Background()

	// Create Topics sheet
	if err := r.ensureSheetExists(ctx, topicsSheetName, topicHeaders()); err != nil {
		return fmt.Errorf("failed to create Topics sheet: %w", err)
	}

	// Create Posts sheet
	if err := r.ensureSheetExists(ctx, postsSheetName, postHeaders()); err != nil {
		return fmt.Errorf("failed to create Posts sheet: %w", err)
	}

	// Initialize next IDs from existing data
	if err := r.initNextIDs(ctx); err != nil {
		r.log.Warn().Err(err).Msg("Failed to initialize IDs from existing data")
	}

	r.log.Info().Msg("Sheets repository migrated successfully")
	return nil
}

// Close is a no-op for Sheets
func (r *Repository) Close() error {
	return nil
}

// ============ TOPIC OPERATIONS ============

// CreateTopic creates a new topic in Google Sheets
func (r *Repository) CreateTopic(ctx context.Context, topic *models.Topic) error {
	r.mu.Lock()
	topic.ID = r.nextTopicID
	r.nextTopicID++
	r.mu.Unlock()

	if topic.DiscoveredAt.IsZero() {
		topic.DiscoveredAt = time.Now()
	}
	topic.UpdatedAt = time.Now()

	row := topicToRow(topic)
	return r.appendRow(ctx, topicsSheetName, row)
}

// GetTopicByID retrieves a topic by its ID
func (r *Repository) GetTopicByID(ctx context.Context, id uint) (*models.Topic, error) {
	topics, err := r.readAllTopics(ctx)
	if err != nil {
		return nil, err
	}

	for _, t := range topics {
		if t.ID == id {
			return t, nil
		}
	}
	return nil, fmt.Errorf("topic with ID %d not found", id)
}

// GetTopicByExternalID retrieves a topic by external ID
func (r *Repository) GetTopicByExternalID(ctx context.Context, externalID string) (*models.Topic, error) {
	topics, err := r.readAllTopics(ctx)
	if err != nil {
		return nil, err
	}

	for _, t := range topics {
		if t.ExternalID == externalID {
			return t, nil
		}
	}
	return nil, fmt.Errorf("topic with external ID %s not found", externalID)
}

// ListTopics lists topics with optional filtering
func (r *Repository) ListTopics(ctx context.Context, filter storage.TopicFilter) ([]*models.Topic, error) {
	topics, err := r.readAllTopics(ctx)
	if err != nil {
		return nil, err
	}

	// Apply filters
	var filtered []*models.Topic
	for _, t := range topics {
		if filter.Status != nil && t.Status != *filter.Status {
			continue
		}
		if filter.SourceType != nil && t.SourceType != *filter.SourceType {
			continue
		}
		if filter.MinScore != nil && t.AIScore < *filter.MinScore {
			continue
		}
		if filter.MaxScore != nil && t.AIScore > *filter.MaxScore {
			continue
		}
		filtered = append(filtered, t)
	}

	// Sort
	sortTopics(filtered, filter.OrderBy, filter.OrderDesc)

	// Apply offset and limit
	if filter.Offset > 0 && filter.Offset < len(filtered) {
		filtered = filtered[filter.Offset:]
	}
	if filter.Limit > 0 && filter.Limit < len(filtered) {
		filtered = filtered[:filter.Limit]
	}

	return filtered, nil
}

// GetTopTopics returns top-scoring topics
func (r *Repository) GetTopTopics(ctx context.Context, limit int, minScore float64) ([]*models.Topic, error) {
	topics, err := r.readAllTopics(ctx)
	if err != nil {
		return nil, err
	}

	// Filter by score and pending status
	var filtered []*models.Topic
	for _, t := range topics {
		if t.AIScore >= minScore && t.Status == models.TopicStatusPending {
			filtered = append(filtered, t)
		}
	}

	// Sort by score descending
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].AIScore > filtered[j].AIScore
	})

	// Limit
	if limit > 0 && limit < len(filtered) {
		filtered = filtered[:limit]
	}

	return filtered, nil
}

// UpdateTopic updates an existing topic
func (r *Repository) UpdateTopic(ctx context.Context, topic *models.Topic) error {
	topic.UpdatedAt = time.Now()

	rowNum, err := r.findRowByID(ctx, topicsSheetName, topic.ID)
	if err != nil {
		return err
	}

	row := topicToRow(topic)
	return r.updateRow(ctx, topicsSheetName, rowNum, row)
}

// DeleteTopic deletes a topic by ID
func (r *Repository) DeleteTopic(ctx context.Context, id uint) error {
	rowNum, err := r.findRowByID(ctx, topicsSheetName, id)
	if err != nil {
		return err
	}
	return r.deleteRow(ctx, topicsSheetName, rowNum)
}

// ============ POST OPERATIONS ============

// CreatePost creates a new post
func (r *Repository) CreatePost(ctx context.Context, post *models.Post) error {
	r.mu.Lock()
	post.ID = r.nextPostID
	r.nextPostID++
	r.mu.Unlock()

	if post.CreatedAt.IsZero() {
		post.CreatedAt = time.Now()
	}
	post.UpdatedAt = time.Now()

	row := postToRow(post)
	return r.appendRow(ctx, postsSheetName, row)
}

// GetPostByID retrieves a post by ID
func (r *Repository) GetPostByID(ctx context.Context, id uint) (*models.Post, error) {
	posts, err := r.readAllPosts(ctx)
	if err != nil {
		return nil, err
	}

	for _, p := range posts {
		if p.ID == id {
			// Load associated topic if exists
			if p.TopicID != nil {
				topic, _ := r.GetTopicByID(ctx, *p.TopicID)
				p.Topic = topic
			}
			return p, nil
		}
	}
	return nil, fmt.Errorf("post with ID %d not found", id)
}

// ListPosts lists posts with optional filtering
func (r *Repository) ListPosts(ctx context.Context, filter storage.PostFilter) ([]*models.Post, error) {
	posts, err := r.readAllPosts(ctx)
	if err != nil {
		return nil, err
	}

	// Apply filters
	var filtered []*models.Post
	for _, p := range posts {
		if filter.Status != nil && p.Status != *filter.Status {
			continue
		}
		if filter.TopicID != nil && (p.TopicID == nil || *p.TopicID != *filter.TopicID) {
			continue
		}
		filtered = append(filtered, p)
	}

	// Sort
	sortPosts(filtered, filter.OrderBy, filter.OrderDesc)

	// Apply offset and limit
	if filter.Offset > 0 && filter.Offset < len(filtered) {
		filtered = filtered[filter.Offset:]
	}
	if filter.Limit > 0 && filter.Limit < len(filtered) {
		filtered = filtered[:filter.Limit]
	}

	return filtered, nil
}

// UpdatePost updates an existing post
func (r *Repository) UpdatePost(ctx context.Context, post *models.Post) error {
	post.UpdatedAt = time.Now()

	rowNum, err := r.findRowByID(ctx, postsSheetName, post.ID)
	if err != nil {
		return err
	}

	row := postToRow(post)
	return r.updateRow(ctx, postsSheetName, rowNum, row)
}

// DeletePost deletes a post by ID
func (r *Repository) DeletePost(ctx context.Context, id uint) error {
	rowNum, err := r.findRowByID(ctx, postsSheetName, id)
	if err != nil {
		return err
	}
	return r.deleteRow(ctx, postsSheetName, rowNum)
}

// GetScheduledPosts retrieves posts scheduled before a given time
func (r *Repository) GetScheduledPosts(ctx context.Context, before time.Time) ([]*models.Post, error) {
	posts, err := r.readAllPosts(ctx)
	if err != nil {
		return nil, err
	}

	var scheduled []*models.Post
	for _, p := range posts {
		if p.Status == models.PostStatusScheduled && p.ScheduledFor != nil && p.ScheduledFor.Before(before) {
			// Load associated topic
			if p.TopicID != nil {
				topic, _ := r.GetTopicByID(ctx, *p.TopicID)
				p.Topic = topic
			}
			scheduled = append(scheduled, p)
		}
	}

	return scheduled, nil
}

// ============ OAUTH TOKEN OPERATIONS (NOT SUPPORTED - USE ENV VARS) ============

// SaveToken is not supported - use environment variables
func (r *Repository) SaveToken(ctx context.Context, token *models.OAuthToken) error {
	r.log.Warn().Msg("SaveToken called but Sheets repo uses env vars for OAuth - token not persisted")
	return nil
}

// GetToken is not supported - use environment variables
func (r *Repository) GetToken(ctx context.Context, provider string) (*models.OAuthToken, error) {
	return nil, fmt.Errorf("OAuth tokens are managed via environment variables, not Sheets storage")
}

// DeleteToken is not supported
func (r *Repository) DeleteToken(ctx context.Context, provider string) error {
	return nil
}

// ============ SOURCE CONFIG OPERATIONS (NOT SUPPORTED - USE CONFIG FILE) ============

// GetSourceConfigs returns empty - use config file
func (r *Repository) GetSourceConfigs(ctx context.Context) ([]*models.SourceConfig, error) {
	return nil, nil
}

// SaveSourceConfig is not supported - use config file
func (r *Repository) SaveSourceConfig(ctx context.Context, config *models.SourceConfig) error {
	return nil
}

// ============ SCHEDULE OPERATIONS (NOT SUPPORTED - USE CONFIG FILE) ============

// GetSchedules returns empty - use config file
func (r *Repository) GetSchedules(ctx context.Context) ([]*models.Schedule, error) {
	return nil, nil
}

// SaveSchedule is not supported - use config file
func (r *Repository) SaveSchedule(ctx context.Context, schedule *models.Schedule) error {
	return nil
}

// ============ HELPER METHODS ============

func (r *Repository) ensureSheetExists(ctx context.Context, sheetName string, headers []string) error {
	// Get spreadsheet to check existing sheets
	spreadsheet, err := r.service.Spreadsheets.Get(r.spreadsheetID).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("failed to get spreadsheet: %w", err)
	}

	// Check if sheet already exists
	sheetExists := false
	for _, sheet := range spreadsheet.Sheets {
		if sheet.Properties.Title == sheetName {
			sheetExists = true
			break
		}
	}

	// Create sheet if needed
	if !sheetExists {
		r.log.Info().Str("sheet", sheetName).Msg("Creating new sheet")
		req := &sheets.BatchUpdateSpreadsheetRequest{
			Requests: []*sheets.Request{
				{
					AddSheet: &sheets.AddSheetRequest{
						Properties: &sheets.SheetProperties{
							Title: sheetName,
						},
					},
				},
			},
		}
		_, err = r.service.Spreadsheets.BatchUpdate(r.spreadsheetID, req).Context(ctx).Do()
		if err != nil {
			return fmt.Errorf("failed to create sheet: %w", err)
		}
	}

	// Check if headers exist
	readRange := fmt.Sprintf("%s!A1:%s1", sheetName, columnLetter(len(headers)))
	resp, err := r.service.Spreadsheets.Values.Get(r.spreadsheetID, readRange).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("failed to read sheet: %w", err)
	}

	// Write headers if empty
	if len(resp.Values) == 0 {
		var headerRow []interface{}
		for _, h := range headers {
			headerRow = append(headerRow, h)
		}

		writeRange := fmt.Sprintf("%s!A1", sheetName)
		valueRange := &sheets.ValueRange{
			Values: [][]interface{}{headerRow},
		}

		_, err = r.service.Spreadsheets.Values.Update(r.spreadsheetID, writeRange, valueRange).
			ValueInputOption("RAW").
			Context(ctx).
			Do()
		if err != nil {
			return fmt.Errorf("failed to write headers: %w", err)
		}
		r.log.Info().Str("sheet", sheetName).Msg("Headers initialized")
	}

	return nil
}

func (r *Repository) initNextIDs(ctx context.Context) error {
	// Find max topic ID
	topics, err := r.readAllTopics(ctx)
	if err == nil {
		for _, t := range topics {
			if t.ID >= r.nextTopicID {
				r.nextTopicID = t.ID + 1
			}
		}
	}

	// Find max post ID
	posts, err := r.readAllPosts(ctx)
	if err == nil {
		for _, p := range posts {
			if p.ID >= r.nextPostID {
				r.nextPostID = p.ID + 1
			}
		}
	}

	r.log.Info().Uint("next_topic_id", r.nextTopicID).Uint("next_post_id", r.nextPostID).Msg("IDs initialized")
	return nil
}

func (r *Repository) appendRow(ctx context.Context, sheetName string, row []interface{}) error {
	appendRange := fmt.Sprintf("%s!A:Z", sheetName)
	valueRange := &sheets.ValueRange{
		Values: [][]interface{}{row},
	}

	_, err := r.service.Spreadsheets.Values.Append(r.spreadsheetID, appendRange, valueRange).
		ValueInputOption("RAW").
		InsertDataOption("INSERT_ROWS").
		Context(ctx).
		Do()
	if err != nil {
		return fmt.Errorf("failed to append row: %w", err)
	}

	return nil
}

func (r *Repository) updateRow(ctx context.Context, sheetName string, rowNum int, row []interface{}) error {
	updateRange := fmt.Sprintf("%s!A%d:%s%d", sheetName, rowNum, columnLetter(len(row)), rowNum)
	valueRange := &sheets.ValueRange{
		Values: [][]interface{}{row},
	}

	_, err := r.service.Spreadsheets.Values.Update(r.spreadsheetID, updateRange, valueRange).
		ValueInputOption("RAW").
		Context(ctx).
		Do()
	if err != nil {
		return fmt.Errorf("failed to update row: %w", err)
	}

	return nil
}

func (r *Repository) deleteRow(ctx context.Context, sheetName string, rowNum int) error {
	// Get sheet ID
	spreadsheet, err := r.service.Spreadsheets.Get(r.spreadsheetID).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("failed to get spreadsheet: %w", err)
	}

	var sheetID int64
	for _, sheet := range spreadsheet.Sheets {
		if sheet.Properties.Title == sheetName {
			sheetID = sheet.Properties.SheetId
			break
		}
	}

	req := &sheets.BatchUpdateSpreadsheetRequest{
		Requests: []*sheets.Request{
			{
				DeleteDimension: &sheets.DeleteDimensionRequest{
					Range: &sheets.DimensionRange{
						SheetId:    sheetID,
						Dimension:  "ROWS",
						StartIndex: int64(rowNum - 1),
						EndIndex:   int64(rowNum),
					},
				},
			},
		},
	}

	_, err = r.service.Spreadsheets.BatchUpdate(r.spreadsheetID, req).Context(ctx).Do()
	return err
}

func (r *Repository) findRowByID(ctx context.Context, sheetName string, id uint) (int, error) {
	readRange := fmt.Sprintf("%s!A:A", sheetName)
	resp, err := r.service.Spreadsheets.Values.Get(r.spreadsheetID, readRange).Context(ctx).Do()
	if err != nil {
		return 0, fmt.Errorf("failed to read IDs: %w", err)
	}

	idStr := strconv.FormatUint(uint64(id), 10)
	for i, row := range resp.Values {
		if i == 0 {
			continue // Skip header
		}
		if len(row) > 0 && fmt.Sprintf("%v", row[0]) == idStr {
			return i + 1, nil // 1-indexed row number
		}
	}

	return 0, fmt.Errorf("row with ID %d not found in %s", id, sheetName)
}

func (r *Repository) readAllTopics(ctx context.Context) ([]*models.Topic, error) {
	readRange := fmt.Sprintf("%s!A2:Z", topicsSheetName)
	resp, err := r.service.Spreadsheets.Values.Get(r.spreadsheetID, readRange).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to read topics: %w", err)
	}

	var topics []*models.Topic
	for _, row := range resp.Values {
		topic := rowToTopic(row)
		if topic != nil {
			topics = append(topics, topic)
		}
	}

	return topics, nil
}

func (r *Repository) readAllPosts(ctx context.Context) ([]*models.Post, error) {
	readRange := fmt.Sprintf("%s!A2:Z", postsSheetName)
	resp, err := r.service.Spreadsheets.Values.Get(r.spreadsheetID, readRange).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to read posts: %w", err)
	}

	var posts []*models.Post
	for _, row := range resp.Values {
		post := rowToPost(row)
		if post != nil {
			posts = append(posts, post)
		}
	}

	return posts, nil
}

// columnLetter converts a 1-based column index to Excel-style letter (1=A, 26=Z, 27=AA)
func columnLetter(n int) string {
	result := ""
	for n > 0 {
		n-- // Adjust for 0-based indexing
		result = string(rune('A'+n%26)) + result
		n /= 26
	}
	return result
}

func sortTopics(topics []*models.Topic, orderBy string, desc bool) {
	sort.Slice(topics, func(i, j int) bool {
		var less bool
		switch orderBy {
		case "score", "ai_score":
			less = topics[i].AIScore < topics[j].AIScore
		case "discovered_at":
			less = topics[i].DiscoveredAt.Before(topics[j].DiscoveredAt)
		default:
			less = topics[i].AIScore < topics[j].AIScore
		}
		if desc {
			return !less
		}
		return less
	})
}

func sortPosts(posts []*models.Post, orderBy string, desc bool) {
	sort.Slice(posts, func(i, j int) bool {
		var less bool
		switch orderBy {
		case "created_at":
			less = posts[i].CreatedAt.Before(posts[j].CreatedAt)
		case "scheduled_for":
			if posts[i].ScheduledFor == nil || posts[j].ScheduledFor == nil {
				less = posts[i].ScheduledFor == nil
			} else {
				less = posts[i].ScheduledFor.Before(*posts[j].ScheduledFor)
			}
		default:
			less = posts[i].CreatedAt.Before(posts[j].CreatedAt)
		}
		if desc {
			return !less
		}
		return less
	})
}

// ============ TOPIC SERIALIZATION ============

func topicHeaders() []string {
	return []string{
		"ID", "ExternalID", "Title", "Description", "URL",
		"SourceType", "SourceName", "Keywords", "RawData",
		"AIScore", "AIAnalysis", "Status", "DiscoveredAt", "UpdatedAt",
	}
}

func topicToRow(t *models.Topic) []interface{} {
	keywords := ""
	if len(t.Keywords) > 0 {
		keywords = strings.Join(t.Keywords, ",")
	}

	rawData := ""
	if t.RawData != nil {
		if b, err := json.Marshal(t.RawData); err == nil {
			rawData = string(b)
		}
	}

	return []interface{}{
		t.ID,
		t.ExternalID,
		t.Title,
		t.Description,
		t.URL,
		t.SourceType,
		t.SourceName,
		keywords,
		rawData,
		t.AIScore,
		t.AIAnalysis,
		string(t.Status),
		t.DiscoveredAt.Format(time.RFC3339),
		t.UpdatedAt.Format(time.RFC3339),
	}
}

func rowToTopic(row []interface{}) *models.Topic {
	if len(row) < 12 {
		return nil
	}

	t := &models.Topic{}

	t.ID = parseUint(row, 0)
	t.ExternalID = parseString(row, 1)
	t.Title = parseString(row, 2)
	t.Description = parseString(row, 3)
	t.URL = parseString(row, 4)
	t.SourceType = parseString(row, 5)
	t.SourceName = parseString(row, 6)

	// Keywords (comma-separated)
	if kw := parseString(row, 7); kw != "" {
		t.Keywords = strings.Split(kw, ",")
	}

	// RawData (JSON)
	if rd := parseString(row, 8); rd != "" {
		var rawData models.JSON
		if err := json.Unmarshal([]byte(rd), &rawData); err == nil {
			t.RawData = rawData
		}
	}

	t.AIScore = parseFloat(row, 9)
	t.AIAnalysis = parseString(row, 10)
	t.Status = models.TopicStatus(parseString(row, 11))
	t.DiscoveredAt = parseTime(row, 12)
	t.UpdatedAt = parseTime(row, 13)

	return t
}

// ============ POST SERIALIZATION ============

func postHeaders() []string {
	return []string{
		"ID", "TopicID", "Content", "PostType", "PostFormat",
		"GenerationPrompt", "AIMetadata", "LinkedInPostURN",
		"Status", "ScheduledFor", "PublishedAt", "ErrorMessage",
		"RetryCount", "CreatedAt", "UpdatedAt",
	}
}

func postToRow(p *models.Post) []interface{} {
	topicID := ""
	if p.TopicID != nil {
		topicID = strconv.FormatUint(uint64(*p.TopicID), 10)
	}

	postFormat := ""
	if p.PostFormat != nil {
		if b, err := json.Marshal(p.PostFormat); err == nil {
			postFormat = string(b)
		}
	}

	aiMetadata := ""
	if p.AIMetadata != nil {
		if b, err := json.Marshal(p.AIMetadata); err == nil {
			aiMetadata = string(b)
		}
	}

	scheduledFor := ""
	if p.ScheduledFor != nil {
		scheduledFor = p.ScheduledFor.Format(time.RFC3339)
	}

	publishedAt := ""
	if p.PublishedAt != nil {
		publishedAt = p.PublishedAt.Format(time.RFC3339)
	}

	return []interface{}{
		p.ID,
		topicID,
		p.Content,
		string(p.PostType),
		postFormat,
		p.GenerationPrompt,
		aiMetadata,
		p.LinkedInPostURN,
		string(p.Status),
		scheduledFor,
		publishedAt,
		p.ErrorMessage,
		p.RetryCount,
		p.CreatedAt.Format(time.RFC3339),
		p.UpdatedAt.Format(time.RFC3339),
	}
}

func rowToPost(row []interface{}) *models.Post {
	if len(row) < 13 {
		return nil
	}

	p := &models.Post{}

	p.ID = parseUint(row, 0)

	// TopicID (nullable)
	if tid := parseString(row, 1); tid != "" {
		id := parseUintStr(tid)
		p.TopicID = &id
	}

	p.Content = parseString(row, 2)
	p.PostType = models.PostType(parseString(row, 3))

	// PostFormat (JSON)
	if pf := parseString(row, 4); pf != "" {
		var postFormat models.JSON
		if err := json.Unmarshal([]byte(pf), &postFormat); err == nil {
			p.PostFormat = postFormat
		}
	}

	p.GenerationPrompt = parseString(row, 5)

	// AIMetadata (JSON)
	if am := parseString(row, 6); am != "" {
		var aiMetadata models.JSON
		if err := json.Unmarshal([]byte(am), &aiMetadata); err == nil {
			p.AIMetadata = aiMetadata
		}
	}

	p.LinkedInPostURN = parseString(row, 7)
	p.Status = models.PostStatus(parseString(row, 8))

	// ScheduledFor (nullable time)
	if sf := parseString(row, 9); sf != "" {
		if t, err := time.Parse(time.RFC3339, sf); err == nil {
			p.ScheduledFor = &t
		}
	}

	// PublishedAt (nullable time)
	if pa := parseString(row, 10); pa != "" {
		if t, err := time.Parse(time.RFC3339, pa); err == nil {
			p.PublishedAt = &t
		}
	}

	p.ErrorMessage = parseString(row, 11)
	p.RetryCount = parseInt(row, 12)
	p.CreatedAt = parseTime(row, 13)
	p.UpdatedAt = parseTime(row, 14)

	return p
}

// ============ PARSING HELPERS ============

func parseString(row []interface{}, idx int) string {
	if idx < len(row) {
		return fmt.Sprintf("%v", row[idx])
	}
	return ""
}

func parseUint(row []interface{}, idx int) uint {
	if idx < len(row) {
		return parseUintStr(fmt.Sprintf("%v", row[idx]))
	}
	return 0
}

func parseUintStr(s string) uint {
	val, _ := strconv.ParseUint(s, 10, 64)
	return uint(val)
}

func parseInt(row []interface{}, idx int) int {
	if idx < len(row) {
		val, _ := strconv.Atoi(fmt.Sprintf("%v", row[idx]))
		return val
	}
	return 0
}

func parseFloat(row []interface{}, idx int) float64 {
	if idx < len(row) {
		val, _ := strconv.ParseFloat(fmt.Sprintf("%v", row[idx]), 64)
		return val
	}
	return 0
}

func parseTime(row []interface{}, idx int) time.Time {
	if idx < len(row) {
		t, _ := time.Parse(time.RFC3339, fmt.Sprintf("%v", row[idx]))
		return t
	}
	return time.Time{}
}
