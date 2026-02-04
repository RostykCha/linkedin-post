package tracker

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"

	"github.com/linkedin-agent/internal/models"
	"github.com/linkedin-agent/pkg/logger"
)

// SheetColumns defines the column headers for the Posts tracking sheet
var SheetColumns = []string{
	"ID",
	"Topic ID",
	"Topic Title",
	"Source",
	"AI Score",
	"Status",
	"Content Preview",
	"Post Type",
	"Planned Date",
	"Published Date",
	"LinkedIn URN",
	"LinkedIn URL",
	"Error",
	"Created At",
	"Updated At",
}

// TopicsSheetColumns defines the column headers for the Topics sheet
var TopicsSheetColumns = []string{
	"ID",
	"Title",
	"Source Type",
	"Source Name",
	"URL",
	"AI Score",
	"AI Analysis",
	"Your Rating",
	"Notes",
	"Use for Post?",
	"Status",
	"Discovered At",
}

// PostStatus represents the status of a tracked post
type PostStatus string

const (
	StatusPlanned   PostStatus = "Planned"
	StatusGenerated PostStatus = "Generated"
	StatusScheduled PostStatus = "Scheduled"
	StatusPublished PostStatus = "Published"
	StatusFailed    PostStatus = "Failed"
)

// TrackedPost represents a post entry in the tracking sheet
type TrackedPost struct {
	ID            int
	TopicID       uint
	TopicTitle    string
	Source        string
	AIScore       float64
	Status        PostStatus
	ContentPreview string
	PostType      string
	PlannedDate   time.Time
	PublishedDate time.Time
	LinkedInURN   string
	LinkedInURL   string
	Error         string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// SheetsTracker handles Google Sheets integration for post tracking
type SheetsTracker struct {
	service       *sheets.Service
	spreadsheetID string
	sheetName     string
	log           *logger.Logger
}

// Config holds Google Sheets tracker configuration
type Config struct {
	Enabled            bool   `mapstructure:"enabled"`
	SpreadsheetID      string `mapstructure:"spreadsheet_id"`
	SheetName          string `mapstructure:"sheet_name"`
	CredentialsFile    string `mapstructure:"credentials_file"`
	ServiceAccountJSON string `mapstructure:"service_account_json"`
}

// NewSheetsTracker creates a new Google Sheets tracker
func NewSheetsTracker(cfg Config, log *logger.Logger) (*SheetsTracker, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	ctx := context.Background()

	var srv *sheets.Service
	var err error

	// Try service account JSON first (for env var injection)
	if cfg.ServiceAccountJSON != "" {
		srv, err = sheets.NewService(ctx, option.WithCredentialsJSON([]byte(cfg.ServiceAccountJSON)))
	} else if cfg.CredentialsFile != "" {
		srv, err = sheets.NewService(ctx, option.WithCredentialsFile(cfg.CredentialsFile))
	} else {
		return nil, fmt.Errorf("no Google credentials provided: set credentials_file or service_account_json")
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create sheets service: %w", err)
	}

	sheetName := cfg.SheetName
	if sheetName == "" {
		sheetName = "Posts"
	}

	tracker := &SheetsTracker{
		service:       srv,
		spreadsheetID: cfg.SpreadsheetID,
		sheetName:     sheetName,
		log:           log.WithComponent("sheets-tracker"),
	}

	return tracker, nil
}

// InitializeSheet creates the sheet and headers if they don't exist
func (t *SheetsTracker) InitializeSheet(ctx context.Context) error {
	// First, ensure the sheet exists
	if err := t.ensureSheetExists(ctx); err != nil {
		return err
	}

	// Check if headers exist
	readRange := fmt.Sprintf("%s!A1:O1", t.sheetName)
	resp, err := t.service.Spreadsheets.Values.Get(t.spreadsheetID, readRange).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("failed to read sheet: %w", err)
	}

	// If no data, add headers
	if len(resp.Values) == 0 {
		t.log.Info().Msg("Initializing sheet with headers")
		return t.writeHeaders(ctx)
	}

	t.log.Debug().Msg("Sheet already has headers")
	return nil
}

// ensureSheetExists creates the sheet if it doesn't exist
func (t *SheetsTracker) ensureSheetExists(ctx context.Context) error {
	// Get spreadsheet to check existing sheets
	spreadsheet, err := t.service.Spreadsheets.Get(t.spreadsheetID).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("failed to get spreadsheet: %w", err)
	}

	// Check if sheet already exists
	for _, sheet := range spreadsheet.Sheets {
		if sheet.Properties.Title == t.sheetName {
			t.log.Debug().Str("sheet", t.sheetName).Msg("Sheet already exists")
			return nil
		}
	}

	// Create the sheet
	t.log.Info().Str("sheet", t.sheetName).Msg("Creating new sheet")
	req := &sheets.BatchUpdateSpreadsheetRequest{
		Requests: []*sheets.Request{
			{
				AddSheet: &sheets.AddSheetRequest{
					Properties: &sheets.SheetProperties{
						Title: t.sheetName,
					},
				},
			},
		},
	}

	_, err = t.service.Spreadsheets.BatchUpdate(t.spreadsheetID, req).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("failed to create sheet: %w", err)
	}

	return nil
}

// writeHeaders writes column headers to the first row
func (t *SheetsTracker) writeHeaders(ctx context.Context) error {
	var headerRow []interface{}
	for _, col := range SheetColumns {
		headerRow = append(headerRow, col)
	}

	writeRange := fmt.Sprintf("%s!A1", t.sheetName)
	valueRange := &sheets.ValueRange{
		Values: [][]interface{}{headerRow},
	}

	_, err := t.service.Spreadsheets.Values.Update(t.spreadsheetID, writeRange, valueRange).
		ValueInputOption("RAW").
		Context(ctx).
		Do()
	if err != nil {
		return fmt.Errorf("failed to write headers: %w", err)
	}

	t.log.Info().Msg("Sheet headers initialized")
	return nil
}

// AddPlannedPost adds a new planned post entry
func (t *SheetsTracker) AddPlannedPost(ctx context.Context, topic *models.Topic) (*TrackedPost, error) {
	// Get next row number
	nextRow, err := t.getNextRow(ctx)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	post := &TrackedPost{
		ID:         nextRow - 1, // Row number minus header
		TopicID:    topic.ID,
		TopicTitle: topic.Title,
		Source:     fmt.Sprintf("%s/%s", topic.SourceType, topic.SourceName),
		AIScore:    topic.AIScore,
		Status:     StatusPlanned,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	if err := t.appendRow(ctx, post); err != nil {
		return nil, err
	}

	t.log.Info().
		Uint("topic_id", topic.ID).
		Str("title", topic.Title).
		Msg("Added planned post to tracker")

	return post, nil
}

// UpdatePostGenerated updates a post after content generation
func (t *SheetsTracker) UpdatePostGenerated(ctx context.Context, topicID uint, content string, postType string) error {
	rowNum, err := t.findRowByTopicID(ctx, topicID)
	if err != nil {
		return err
	}

	// Truncate content for preview
	preview := content
	if len(preview) > 200 {
		preview = preview[:200] + "..."
	}

	updates := map[string]interface{}{
		"F": string(StatusGenerated), // Status
		"G": preview,                 // Content Preview
		"H": postType,                // Post Type
		"O": time.Now().Format(time.RFC3339), // Updated At
	}

	return t.updateCells(ctx, rowNum, updates)
}

// UpdatePostScheduled updates a post when scheduled
func (t *SheetsTracker) UpdatePostScheduled(ctx context.Context, topicID uint, scheduledTime time.Time) error {
	rowNum, err := t.findRowByTopicID(ctx, topicID)
	if err != nil {
		return err
	}

	updates := map[string]interface{}{
		"F": string(StatusScheduled),              // Status
		"I": scheduledTime.Format(time.RFC3339),   // Planned Date
		"O": time.Now().Format(time.RFC3339),      // Updated At
	}

	return t.updateCells(ctx, rowNum, updates)
}

// UpdatePostPublished updates a post after successful publication
func (t *SheetsTracker) UpdatePostPublished(ctx context.Context, topicID uint, linkedinURN string) error {
	rowNum, err := t.findRowByTopicID(ctx, topicID)
	if err != nil {
		return err
	}

	now := time.Now()
	linkedinURL := ""
	if linkedinURN != "" {
		// Extract post ID from URN like "urn:li:share:123456"
		linkedinURL = fmt.Sprintf("https://www.linkedin.com/feed/update/%s", linkedinURN)
	}

	updates := map[string]interface{}{
		"F": string(StatusPublished),       // Status
		"J": now.Format(time.RFC3339),      // Published Date
		"K": linkedinURN,                   // LinkedIn URN
		"L": linkedinURL,                   // LinkedIn URL
		"O": now.Format(time.RFC3339),      // Updated At
	}

	return t.updateCells(ctx, rowNum, updates)
}

// UpdatePostFailed updates a post after a failed publish attempt
func (t *SheetsTracker) UpdatePostFailed(ctx context.Context, topicID uint, errMsg string) error {
	rowNum, err := t.findRowByTopicID(ctx, topicID)
	if err != nil {
		return err
	}

	updates := map[string]interface{}{
		"F": string(StatusFailed),            // Status
		"M": errMsg,                          // Error
		"O": time.Now().Format(time.RFC3339), // Updated At
	}

	return t.updateCells(ctx, rowNum, updates)
}

// TrackNewPost creates a complete tracking entry for a new post
func (t *SheetsTracker) TrackNewPost(ctx context.Context, topic *models.Topic, post *models.Post) error {
	nextRow, err := t.getNextRow(ctx)
	if err != nil {
		return err
	}

	now := time.Now()

	// Truncate content for preview
	preview := post.Content
	if len(preview) > 200 {
		preview = preview[:200] + "..."
	}

	trackedPost := &TrackedPost{
		ID:            nextRow - 1,
		TopicID:       topic.ID,
		TopicTitle:    topic.Title,
		Source:        fmt.Sprintf("%s/%s", topic.SourceType, topic.SourceName),
		AIScore:       topic.AIScore,
		Status:        StatusGenerated,
		ContentPreview: preview,
		PostType:      string(post.PostType),
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := t.appendRow(ctx, trackedPost); err != nil {
		return err
	}

	t.log.Info().
		Uint("topic_id", topic.ID).
		Uint("post_id", post.ID).
		Msg("Tracked new post")

	return nil
}

// getNextRow returns the next empty row number
func (t *SheetsTracker) getNextRow(ctx context.Context) (int, error) {
	readRange := fmt.Sprintf("%s!A:A", t.sheetName)
	resp, err := t.service.Spreadsheets.Values.Get(t.spreadsheetID, readRange).Context(ctx).Do()
	if err != nil {
		return 0, fmt.Errorf("failed to get row count: %w", err)
	}
	return len(resp.Values) + 1, nil
}

// findRowByTopicID finds the row number for a given topic ID
func (t *SheetsTracker) findRowByTopicID(ctx context.Context, topicID uint) (int, error) {
	readRange := fmt.Sprintf("%s!B:B", t.sheetName)
	resp, err := t.service.Spreadsheets.Values.Get(t.spreadsheetID, readRange).Context(ctx).Do()
	if err != nil {
		return 0, fmt.Errorf("failed to search for topic: %w", err)
	}

	topicIDStr := fmt.Sprintf("%d", topicID)
	for i, row := range resp.Values {
		if len(row) > 0 && fmt.Sprintf("%v", row[0]) == topicIDStr {
			return i + 1, nil // 1-indexed
		}
	}

	return 0, fmt.Errorf("topic ID %d not found in tracker", topicID)
}

// appendRow appends a new row to the sheet
func (t *SheetsTracker) appendRow(ctx context.Context, post *TrackedPost) error {
	row := []interface{}{
		post.ID,
		post.TopicID,
		post.TopicTitle,
		post.Source,
		post.AIScore,
		string(post.Status),
		post.ContentPreview,
		post.PostType,
		formatTime(post.PlannedDate),
		formatTime(post.PublishedDate),
		post.LinkedInURN,
		post.LinkedInURL,
		post.Error,
		post.CreatedAt.Format(time.RFC3339),
		post.UpdatedAt.Format(time.RFC3339),
	}

	appendRange := fmt.Sprintf("%s!A:O", t.sheetName)
	valueRange := &sheets.ValueRange{
		Values: [][]interface{}{row},
	}

	_, err := t.service.Spreadsheets.Values.Append(t.spreadsheetID, appendRange, valueRange).
		ValueInputOption("RAW").
		InsertDataOption("INSERT_ROWS").
		Context(ctx).
		Do()
	if err != nil {
		return fmt.Errorf("failed to append row: %w", err)
	}

	return nil
}

// updateCells updates specific cells in a row
func (t *SheetsTracker) updateCells(ctx context.Context, rowNum int, updates map[string]interface{}) error {
	var requests []*sheets.Request

	for col, value := range updates {
		cellRange := fmt.Sprintf("%s!%s%d", t.sheetName, col, rowNum)
		valueRange := &sheets.ValueRange{
			Values: [][]interface{}{{value}},
		}

		_, err := t.service.Spreadsheets.Values.Update(t.spreadsheetID, cellRange, valueRange).
			ValueInputOption("RAW").
			Context(ctx).
			Do()
		if err != nil {
			return fmt.Errorf("failed to update cell %s: %w", cellRange, err)
		}
	}

	_ = requests // Not used in simple update approach
	return nil
}

// GetAllPosts retrieves all tracked posts from the sheet
func (t *SheetsTracker) GetAllPosts(ctx context.Context) ([]*TrackedPost, error) {
	readRange := fmt.Sprintf("%s!A2:O", t.sheetName) // Skip header
	resp, err := t.service.Spreadsheets.Values.Get(t.spreadsheetID, readRange).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to read posts: %w", err)
	}

	var posts []*TrackedPost
	for _, row := range resp.Values {
		post := parseRow(row)
		if post != nil {
			posts = append(posts, post)
		}
	}

	return posts, nil
}

// parseRow parses a sheet row into a TrackedPost
func parseRow(row []interface{}) *TrackedPost {
	if len(row) < 15 {
		return nil
	}

	getString := func(i int) string {
		if i < len(row) {
			return fmt.Sprintf("%v", row[i])
		}
		return ""
	}

	getInt := func(i int) int {
		if i < len(row) {
			var val int
			fmt.Sscanf(fmt.Sprintf("%v", row[i]), "%d", &val)
			return val
		}
		return 0
	}

	getFloat := func(i int) float64 {
		if i < len(row) {
			var val float64
			fmt.Sscanf(fmt.Sprintf("%v", row[i]), "%f", &val)
			return val
		}
		return 0
	}

	getTime := func(i int) time.Time {
		if i < len(row) {
			t, _ := time.Parse(time.RFC3339, fmt.Sprintf("%v", row[i]))
			return t
		}
		return time.Time{}
	}

	return &TrackedPost{
		ID:            getInt(0),
		TopicID:       uint(getInt(1)),
		TopicTitle:    getString(2),
		Source:        getString(3),
		AIScore:       getFloat(4),
		Status:        PostStatus(getString(5)),
		ContentPreview: getString(6),
		PostType:      getString(7),
		PlannedDate:   getTime(8),
		PublishedDate: getTime(9),
		LinkedInURN:   getString(10),
		LinkedInURL:   getString(11),
		Error:         getString(12),
		CreatedAt:     getTime(13),
		UpdatedAt:     getTime(14),
	}
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// ============ TOPICS SHEET FUNCTIONS ============

const topicsSheetName = "Topics"

// InitializeTopicsSheet creates the Topics sheet with headers
func (t *SheetsTracker) InitializeTopicsSheet(ctx context.Context) error {
	// Check if sheet exists
	spreadsheet, err := t.service.Spreadsheets.Get(t.spreadsheetID).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("failed to get spreadsheet: %w", err)
	}

	sheetExists := false
	for _, sheet := range spreadsheet.Sheets {
		if sheet.Properties.Title == topicsSheetName {
			sheetExists = true
			break
		}
	}

	// Create sheet if needed
	if !sheetExists {
		t.log.Info().Msg("Creating Topics sheet")
		req := &sheets.BatchUpdateSpreadsheetRequest{
			Requests: []*sheets.Request{
				{
					AddSheet: &sheets.AddSheetRequest{
						Properties: &sheets.SheetProperties{
							Title: topicsSheetName,
						},
					},
				},
			},
		}
		_, err = t.service.Spreadsheets.BatchUpdate(t.spreadsheetID, req).Context(ctx).Do()
		if err != nil {
			return fmt.Errorf("failed to create Topics sheet: %w", err)
		}
	}

	// Check if headers exist
	readRange := fmt.Sprintf("%s!A1:L1", topicsSheetName)
	resp, err := t.service.Spreadsheets.Values.Get(t.spreadsheetID, readRange).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("failed to read Topics sheet: %w", err)
	}

	// Add headers if empty
	if len(resp.Values) == 0 {
		var headerRow []interface{}
		for _, col := range TopicsSheetColumns {
			headerRow = append(headerRow, col)
		}

		writeRange := fmt.Sprintf("%s!A1", topicsSheetName)
		valueRange := &sheets.ValueRange{
			Values: [][]interface{}{headerRow},
		}

		_, err = t.service.Spreadsheets.Values.Update(t.spreadsheetID, writeRange, valueRange).
			ValueInputOption("RAW").
			Context(ctx).
			Do()
		if err != nil {
			return fmt.Errorf("failed to write Topics headers: %w", err)
		}
		t.log.Info().Msg("Topics sheet headers initialized")
	}

	return nil
}

// SyncTopics syncs all topics to the Topics sheet using batch operations
func (t *SheetsTracker) SyncTopics(ctx context.Context, topics []*models.Topic) (int, int, error) {
	// Initialize sheet first
	if err := t.InitializeTopicsSheet(ctx); err != nil {
		return 0, 0, err
	}

	// Get existing topic IDs from sheet
	existingIDs, err := t.getExistingTopicIDs(ctx)
	if err != nil {
		return 0, 0, err
	}

	// Separate new and existing topics
	var newRows [][]interface{}
	var toUpdate []*models.Topic

	for _, topic := range topics {
		if _, exists := existingIDs[topic.ID]; exists {
			toUpdate = append(toUpdate, topic)
		} else {
			// Build row for new topic
			row := []interface{}{
				topic.ID,
				topic.Title,
				topic.SourceType,
				topic.SourceName,
				topic.URL,
				topic.AIScore,
				topic.AIAnalysis,
				"",                   // Your Rating - empty for user
				"",                   // Notes - empty for user
				"",                   // Use for Post? - empty for user
				string(topic.Status),
				topic.DiscoveredAt.Format(time.RFC3339),
			}
			newRows = append(newRows, row)
		}
	}

	added := 0
	updated := 0

	// Batch append all new topics in a single API call
	if len(newRows) > 0 {
		appendRange := fmt.Sprintf("%s!A:L", topicsSheetName)
		valueRange := &sheets.ValueRange{
			Values: newRows,
		}

		_, err := t.service.Spreadsheets.Values.Append(t.spreadsheetID, appendRange, valueRange).
			ValueInputOption("RAW").
			InsertDataOption("INSERT_ROWS").
			Context(ctx).
			Do()
		if err != nil {
			return 0, 0, fmt.Errorf("failed to batch append topics: %w", err)
		}
		added = len(newRows)
		t.log.Info().Int("count", added).Msg("Batch appended new topics")
	}

	// Update existing topics (still individual calls, but usually fewer)
	for _, topic := range toUpdate {
		if err := t.updateTopicRow(ctx, topic); err != nil {
			t.log.Warn().Err(err).Uint("topic_id", topic.ID).Msg("Failed to update topic")
			continue
		}
		updated++
	}

	t.log.Info().Int("added", added).Int("updated", updated).Msg("Topics synced to sheet")
	return added, updated, nil
}

// getExistingTopicIDs gets all topic IDs already in the sheet
func (t *SheetsTracker) getExistingTopicIDs(ctx context.Context) (map[uint]int, error) {
	readRange := fmt.Sprintf("%s!A:A", topicsSheetName)
	resp, err := t.service.Spreadsheets.Values.Get(t.spreadsheetID, readRange).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to read topic IDs: %w", err)
	}

	ids := make(map[uint]int)
	for i, row := range resp.Values {
		if i == 0 || len(row) == 0 {
			continue // Skip header
		}
		var id uint
		fmt.Sscanf(fmt.Sprintf("%v", row[0]), "%d", &id)
		if id > 0 {
			ids[id] = i + 1 // Row number (1-indexed)
		}
	}
	return ids, nil
}

// appendTopicRow adds a new topic to the Topics sheet
func (t *SheetsTracker) appendTopicRow(ctx context.Context, topic *models.Topic) error {
	row := []interface{}{
		topic.ID,
		topic.Title,
		topic.SourceType,
		topic.SourceName,
		topic.URL,
		topic.AIScore,
		topic.AIAnalysis,
		"",                                   // Your Rating - empty for user
		"",                                   // Notes - empty for user
		"",                                   // Use for Post? - empty for user
		string(topic.Status),
		topic.DiscoveredAt.Format(time.RFC3339),
	}

	appendRange := fmt.Sprintf("%s!A:L", topicsSheetName)
	valueRange := &sheets.ValueRange{
		Values: [][]interface{}{row},
	}

	_, err := t.service.Spreadsheets.Values.Append(t.spreadsheetID, appendRange, valueRange).
		ValueInputOption("RAW").
		InsertDataOption("INSERT_ROWS").
		Context(ctx).
		Do()

	return err
}

// updateTopicRow updates an existing topic's status and score
func (t *SheetsTracker) updateTopicRow(ctx context.Context, topic *models.Topic) error {
	existingIDs, err := t.getExistingTopicIDs(ctx)
	if err != nil {
		return err
	}

	rowNum, exists := existingIDs[topic.ID]
	if !exists {
		return fmt.Errorf("topic %d not found in sheet", topic.ID)
	}

	// Only update AI Score, AI Analysis, and Status (preserve user columns)
	updates := []struct {
		col   string
		value interface{}
	}{
		{"F", topic.AIScore},
		{"G", topic.AIAnalysis},
		{"K", string(topic.Status)},
	}

	for _, u := range updates {
		cellRange := fmt.Sprintf("%s!%s%d", topicsSheetName, u.col, rowNum)
		valueRange := &sheets.ValueRange{
			Values: [][]interface{}{{u.value}},
		}
		_, err := t.service.Spreadsheets.Values.Update(t.spreadsheetID, cellRange, valueRange).
			ValueInputOption("RAW").
			Context(ctx).
			Do()
		if err != nil {
			return fmt.Errorf("failed to update cell %s: %w", cellRange, err)
		}
	}

	return nil
}

// GetTopicsFromSheet reads all topics from the Topics sheet
func (t *SheetsTracker) GetTopicsFromSheet(ctx context.Context) ([]map[string]interface{}, error) {
	readRange := fmt.Sprintf("%s!A2:L", topicsSheetName)
	resp, err := t.service.Spreadsheets.Values.Get(t.spreadsheetID, readRange).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to read topics: %w", err)
	}

	var topics []map[string]interface{}
	for _, row := range resp.Values {
		if len(row) < 6 {
			continue
		}
		topic := map[string]interface{}{
			"id":           row[0],
			"title":        safeString(row, 1),
			"source_type":  safeString(row, 2),
			"source_name":  safeString(row, 3),
			"url":          safeString(row, 4),
			"ai_score":     safeString(row, 5),
			"ai_analysis":  safeString(row, 6),
			"your_rating":  safeString(row, 7),
			"notes":        safeString(row, 8),
			"use_for_post": safeString(row, 9),
			"status":       safeString(row, 10),
			"discovered":   safeString(row, 11),
		}
		topics = append(topics, topic)
	}
	return topics, nil
}

func safeString(row []interface{}, i int) string {
	if i < len(row) {
		return fmt.Sprintf("%v", row[i])
	}
	return ""
}
