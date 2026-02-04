# Publish Content Skill

Generate LinkedIn posts from topics and publish them.

## Usage

```
/publish [generate|now|schedule] [--topic-id=<id>] [--post-id=<id>]
```

## Workflow

### Generate Content

1. **List available topics**
   ```bash
   cd c:\Users\rosti\linkedin-post
   ./bin/linkedin-agent topics list --status=approved --min-score=70
   ```

2. **Generate post from topic**
   ```bash
   ./bin/linkedin-agent publish generate <topic-id>
   ```

3. **Review generated post**
   ```bash
   ./bin/linkedin-agent posts list --status=draft
   ./bin/linkedin-agent posts show <post-id>
   ```

### Publish Immediately

```bash
./bin/linkedin-agent publish now <post-id>
```

### Schedule for Later

```bash
./bin/linkedin-agent publish schedule <post-id> --time="2024-01-15T09:00:00"
```

## Content Generation

The AI generates posts following these rules:
- Professional, insightful tone
- Emotion triggers (inspiration, curiosity, fear/urgency, validation)
- Email capture factor + practical value
- Optimized for LinkedIn algorithm (engagement hooks)

## Configuration

Edit `configs/config.local.yaml`:
```yaml
publishing:
  auto_approve: false          # Set true to auto-publish high-score topics
  max_posts_per_day: 3         # Daily limit
  min_score_threshold: 70.0    # Minimum AI score for publishing
  brand_voice: "Professional, insightful, conversational"
```

## Troubleshooting

- **OAuth error**: Run `./bin/linkedin-agent oauth login` first
- **Post failed**: Check `./bin/linkedin-agent posts list --status=failed`
- **Rate limited**: LinkedIn allows ~100 posts/day, check limits
