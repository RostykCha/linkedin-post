# OAuth Authentication Skill

Authenticate with LinkedIn using OAuth 2.0.

## Usage

```
/oauth [login|status|refresh]
```

## Initial Setup

### 1. Configure LinkedIn App Credentials

Edit `configs/config.local.yaml`:
```yaml
linkedin:
  client_id: "your-linkedin-app-client-id"
  client_secret: "your-linkedin-app-client-secret"
  redirect_uri: "http://localhost:8080/callback"
```

Or set environment variables:
```bash
export LINKEDIN_LINKEDIN_CLIENT_ID="your-client-id"
export LINKEDIN_LINKEDIN_CLIENT_SECRET="your-client-secret"
```

### 2. Run OAuth Login

```bash
cd c:\Users\rosti\linkedin-post
./bin/linkedin-agent oauth login
```

This will:
1. Start local server on port 8080
2. Open browser to LinkedIn authorization page
3. Handle callback and store tokens in database

### 3. Verify Authentication

```bash
./bin/linkedin-agent oauth status
```

## Headless/Docker Deployment

For environments without a browser, inject tokens via environment:

```bash
export LINKEDIN_LINKEDIN_ACCESS_TOKEN="your-access-token"
export LINKEDIN_LINKEDIN_REFRESH_TOKEN="your-refresh-token"
export LINKEDIN_LINKEDIN_TOKEN_EXPIRES_AT="2024-12-31T23:59:59Z"
```

The scheduler will automatically import these on startup.

## Token Management

Tokens are stored in the SQLite database (`./data/linkedin.db` in `oauth_tokens` table).

### Check Token Status

```bash
./bin/linkedin-agent oauth status
```

### Force Token Refresh

```bash
./bin/linkedin-agent oauth refresh
```

## LinkedIn App Setup

1. Go to [LinkedIn Developer Portal](https://www.linkedin.com/developers/apps)
2. Create a new app
3. Add OAuth 2.0 settings:
   - Redirect URL: `http://localhost:8080/callback`
4. Request these scopes:
   - `w_member_social` (post on behalf of user)
   - `r_liteprofile` (read basic profile)
5. Copy Client ID and Client Secret to config

## Troubleshooting

- **Port 8080 in use**: Change `redirect_uri` port in both config and LinkedIn app
- **Invalid redirect**: Ensure `redirect_uri` matches exactly in config and LinkedIn app
- **Token expired**: Run `oauth refresh` or `oauth login` again
- **Scope denied**: Check LinkedIn app has required permissions
