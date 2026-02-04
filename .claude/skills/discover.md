# Discover Topics Skill

Run the topic discovery workflow to find trending content from configured sources.

## Usage

```
/discover [--source=<source>] [--dry-run]
```

## Workflow

1. **Build the CLI** (if needed)
   ```bash
   cd c:\Users\rosti\linkedin-post && go build -o bin/linkedin-agent.exe ./cmd/cli
   ```

2. **Run discovery**
   ```bash
   ./bin/linkedin-agent discover run
   ```

3. **Check results**
   ```bash
   ./bin/linkedin-agent topics list --status=pending --limit=10
   ```

## Options

- `--source=rss` - Only fetch from RSS sources
- `--source=custom` - Only fetch from custom keywords
- `--dry-run` - Fetch and rank topics without saving

## What It Does

1. Fetches topics from all enabled sources (RSS feeds, custom keywords)
2. Deduplicates against existing topics in database
3. Ranks topics using Claude AI (0-100 score)
4. Saves high-quality topics to database
5. Reports results (found, ranked, saved, skipped)

## Post-Discovery

After discovery, review topics:
```bash
./bin/linkedin-agent topics list --status=pending --min-score=70
```

Approve high-quality topics for publishing:
```bash
./bin/linkedin-agent topics approve <topic-id>
```

## Troubleshooting

- **No topics found**: Check source configuration in `configs/config.local.yaml`
- **AI ranking fails**: Verify `LINKEDIN_ANTHROPIC_API_KEY` is set
- **Rate limit errors**: Wait and retry, or adjust `rate_limit.anthropic_requests_per_minute`
