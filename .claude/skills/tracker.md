# Google Sheets Tracker Skill

Sync topics and posts to Google Sheets for tracking and review.

## Usage

```
/tracker [sync-topics|sync-posts|setup]
```

## Setup

### 1. Create Google Cloud Service Account

1. Go to [Google Cloud Console](https://console.cloud.google.com)
2. Create a new project or select existing
3. Enable Google Sheets API
4. Create Service Account with Sheets access
5. Download JSON credentials

### 2. Configure Credentials

Save credentials to `configs/google-credentials.json`:
```json
{
  "type": "service_account",
  "project_id": "your-project",
  "private_key_id": "...",
  "private_key": "-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----\n",
  "client_email": "your-service-account@your-project.iam.gserviceaccount.com",
  ...
}
```

### 3. Create Google Sheet

1. Create a new Google Spreadsheet
2. Share it with the service account email (Editor access)
3. Copy the Spreadsheet ID from URL

### 4. Update Configuration

Edit `configs/config.local.yaml`:
```yaml
tracker:
  enabled: true
  credentials_file: "configs/google-credentials.json"
  spreadsheet_id: "your-spreadsheet-id-from-url"
  topics_sheet: "Topics"
  posts_sheet: "Posts"
```

## Commands

### Sync Topics to Sheet

```bash
cd c:\Users\rosti\linkedin-post
./bin/linkedin-agent tracker sync-topics
```

This exports all discovered topics with:
- ID, Title, Source, AI Score
- Status, Keywords
- Discovery timestamp

### Sync Posts to Sheet

```bash
./bin/linkedin-agent tracker sync-posts
```

This exports all generated posts with:
- ID, Topic ID, Content preview
- Status, LinkedIn URL (if published)
- Scheduled/Published timestamps

## Sheet Structure

### Topics Sheet

| ID | Title | Source | AI Score | Status | Keywords | Discovered At |
|----|-------|--------|----------|--------|----------|---------------|

### Posts Sheet

| ID | Topic | Content | Status | LinkedIn URL | Scheduled | Published |
|----|-------|---------|--------|--------------|-----------|-----------|

## Workflow Integration

When tracker is enabled:
1. New topics are automatically added to Topics sheet on discovery
2. Generated posts are tracked in Posts sheet
3. Published posts are updated with LinkedIn URLs

## Troubleshooting

- **Auth error**: Verify service account has Editor access to spreadsheet
- **Sheet not found**: Check spreadsheet_id and sheet names match config
- **API quota**: Google Sheets API has rate limits, add delays between bulk operations
