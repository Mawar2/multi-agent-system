# GitHub MCP Integration Setup

The supervisor uses the Model Context Protocol (MCP) to interact with GitHub. This provides a standardized way to access GitHub APIs with proper authentication.

## Environment Variables

The supervisor requires one environment variable:

```bash
# Required: Your GitHub Personal Access Token
export GITHUB_TOKEN="ghp_your_token_here"

# Optional: Custom MCP server URL (defaults to GitHub Copilot MCP endpoint)
export MCP_SERVER_URL="https://api.githubcopilot.com/mcp/"
```

### Getting a GitHub Personal Access Token

1. Go to GitHub Settings → Developer settings → Personal access tokens → Tokens (classic)
2. Click "Generate new token (classic)"
3. Give it a descriptive name (e.g., "Multi-Agent Supervisor")
4. Select scopes:
   - ✅ `repo` (Full control of private repositories)
   - ✅ `read:org` (Read org and team membership)
   - ✅ `workflow` (Update GitHub Action workflows)
5. Click "Generate token"
6. Copy the token immediately (you won't see it again!)
7. Add to your environment:
   ```bash
   export GITHUB_TOKEN="ghp_xxxxxxxxxxxxxxxxxxxxx"
   ```

### Permanent Setup

**Linux/Mac (bash/zsh):**
Add to `~/.bashrc` or `~/.zshrc`:
```bash
export GITHUB_TOKEN="ghp_xxxxxxxxxxxxxxxxxxxxx"
```

**Windows (PowerShell):**
Add to PowerShell profile:
```powershell
$env:GITHUB_TOKEN = "ghp_xxxxxxxxxxxxxxxxxxxxx"
```

Or set system environment variable:
```
System Properties → Environment Variables → New
Variable name: GITHUB_TOKEN
Variable value: ghp_xxxxxxxxxxxxxxxxxxxxx
```

## MCP Architecture

```
┌──────────────────────────────────────────────────────────┐
│                   Supervisor (Go)                        │
└───────────────────────┬──────────────────────────────────┘
                        │
                        ▼
              ┌─────────────────────┐
              │  HTTPMCPClient      │  (internal/ticket/mcp_client.go)
              │  - JSON-RPC style   │
              │  - HTTP POST        │
              └──────────┬──────────┘
                         │
                         │ HTTP + Bearer Token
                         ▼
              ┌─────────────────────┐
              │   MCP Server        │  (GitHub Copilot MCP)
              │   api.githubcopilot │
              └──────────┬──────────┘
                         │
                         │ GitHub API calls
                         ▼
              ┌─────────────────────┐
              │   GitHub REST API   │
              └─────────────────────┘
```

## How It Works

1. **Supervisor starts** - Initializes `HTTPMCPClient` with `GITHUB_TOKEN`
2. **GitHubClient calls MCP tool** - e.g., `mcp__github__list_issues`
3. **HTTPMCPClient sends HTTP POST** - To MCP server with tool name + params
4. **MCP server authenticates** - Uses your GitHub token
5. **MCP server calls GitHub API** - Makes appropriate REST API call
6. **Response flows back** - MCP server → HTTPMCPClient → GitHubClient → Supervisor

## Available MCP Tools

The supervisor uses these GitHub MCP tools:

- **`mcp__github__list_issues`** - Fetch open issues with optional label filtering
- **`mcp__github__issue_read`** - Get details of a specific issue
- **`mcp__github__search_pull_requests`** - Search for PRs linked to issues

## Testing MCP Connection

Test the MCP client works:

```bash
# Set token
export GITHUB_TOKEN="ghp_xxxxxxxxxxxxxxxxxxxxx"

# Run ticket client tests (they mock the MCP server)
cd multi-agent-system
go test ./internal/ticket/ -v

# All tests should pass
```

## Troubleshooting

### "GITHUB_TOKEN environment variable is required"
- Make sure you've exported `GITHUB_TOKEN`
- Verify it's in your environment: `echo $GITHUB_TOKEN` (Linux/Mac) or `echo %GITHUB_TOKEN%` (Windows)

### "MCP server returned status 401"
- Your token is invalid or expired
- Generate a new token with correct scopes

### "MCP server returned status 404"
- Repository doesn't exist or you don't have access
- Check repo owner/name in `orchestrator.yml`

### "MCP error 403: Resource not accessible"
- Token doesn't have required scopes
- Regenerate token with `repo` and `read:org` scopes

## Security Notes

⚠️ **Never commit your GitHub token to version control!**

- Use environment variables (not config files)
- Add `.env` to `.gitignore` if you use dotenv files
- Rotate tokens regularly
- Use fine-grained tokens with minimum required permissions (when available)

## Alternative: Direct GitHub API (Future)

The architecture supports swapping the MCP client for direct GitHub API calls:

```go
// Instead of HTTPMCPClient, use:
githubClient := github.NewClient(oauth2Client)
// Implement MCPClient interface wrapping go-github library
```

This is not implemented yet, but the abstraction layer (`MCPClient` interface) makes it straightforward to add.
