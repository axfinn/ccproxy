# ccproxy - Claude Code Proxy Service

A Golang-based proxy service for Claude that supports both Web mode (claude.ai) and API mode (Anthropic API) with JWT authentication.

## Features

- **Dual Mode Support**:
  - Web Mode: Proxy claude.ai using saved session cookies
  - API Mode: Proxy Anthropic API with key rotation
- **JWT Authentication**: Secure token-based access control
- **OpenAI-Compatible API**: Drop-in replacement for OpenAI API clients
- **Key Pool**: Load balancing across multiple API keys
- **SQLite Storage**: Persistent token and session management

## Quick Start

### 1. Build

```bash
go build -o ccproxy ./cmd/server
```

### 2. Configure

Copy the example environment file and edit it:

```bash
cp .env.example .env
# Edit .env with your settings
```

Required settings:
- `CCPROXY_JWT_SECRET`: A secure random string for JWT signing
- `CCPROXY_ADMIN_KEY`: Admin key for management operations

For API mode, add your API keys:
- `CCPROXY_CLAUDE_API_KEYS`: Comma-separated list of Anthropic API keys

### 3. Run

```bash
./ccproxy
```

Or with environment variables:

```bash
CCPROXY_JWT_SECRET=your-secret CCPROXY_ADMIN_KEY=admin123 ./ccproxy
```

## Docker Deployment

### Using Docker Compose (Recommended)

1. Create a `.env` file with required settings:

```bash
cp .env.example .env
# Edit .env with your JWT_SECRET and ADMIN_KEY
```

2. Start the service:

```bash
docker-compose up -d
```

3. View logs:

```bash
docker-compose logs -f
```

4. Stop the service:

```bash
docker-compose down
```

### Using Docker Directly

1. Build the image:

```bash
docker build -t ccproxy .
```

2. Run the container:

```bash
docker run -d \
  --name ccproxy \
  -p 8080:8080 \
  -v ccproxy-data:/app/data \
  -e CCPROXY_JWT_SECRET=your-secret \
  -e CCPROXY_ADMIN_KEY=your-admin-key \
  -e CCPROXY_CLAUDE_API_KEYS=sk-ant-api-xxx \
  ccproxy
```

### Using Makefile

```bash
# Build Docker image
make docker-build

# Start with docker-compose
make docker-run

# View logs
make docker-logs

# Stop
make docker-stop

# Push to registry
DOCKER_REGISTRY=your-registry.com make docker-push
```

### Docker Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `CCPROXY_JWT_SECRET` | Yes | - | JWT signing secret |
| `CCPROXY_ADMIN_KEY` | Yes | - | Admin API key |
| `CCPROXY_CLAUDE_API_KEYS` | No | - | Comma-separated API keys |
| `CCPROXY_SERVER_PORT` | No | 8080 | Server port |
| `CCPROXY_SERVER_MODE` | No | both | Mode: web, api, or both |
| `CCPROXY_STORAGE_DB_PATH` | No | /app/data/ccproxy.db | Database path |

## API Reference

### Token Management (Admin)

**Generate Token**
```bash
curl -X POST http://localhost:8080/api/token/generate \
  -H "X-Admin-Key: your-admin-key" \
  -H "Content-Type: application/json" \
  -d '{"name": "my-terminal", "expires_in": "720h", "mode": "both"}'
```

**List Tokens**
```bash
curl http://localhost:8080/api/token/list \
  -H "X-Admin-Key: your-admin-key"
```

**Revoke Token**
```bash
curl -X POST http://localhost:8080/api/token/revoke \
  -H "X-Admin-Key: your-admin-key" \
  -H "Content-Type: application/json" \
  -d '{"id": "token-id"}'
```

### Session Management (Admin, Web Mode)

**Add Session**
```bash
curl -X POST http://localhost:8080/api/session/add \
  -H "X-Admin-Key: your-admin-key" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "pro-account",
    "session_key": "sk-ant-sid01-xxx",
    "organization_id": "org-xxx"
  }'
```

**List Sessions**
```bash
curl http://localhost:8080/api/session/list \
  -H "X-Admin-Key: your-admin-key"
```

**Delete Session**
```bash
curl -X DELETE http://localhost:8080/api/session/{id} \
  -H "X-Admin-Key: your-admin-key"
```

### Key Stats (Admin, API Mode)

```bash
curl http://localhost:8080/api/keys/stats \
  -H "X-Admin-Key: your-admin-key"
```

### Chat Completions (OpenAI-Compatible)

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer your-jwt-token" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-3-opus-20240229",
    "messages": [{"role": "user", "content": "Hello!"}],
    "stream": false
  }'
```

### List Models

```bash
curl http://localhost:8080/v1/models \
  -H "Authorization: Bearer your-jwt-token"
```

### Native Anthropic API

```bash
curl http://localhost:8080/v1/messages \
  -H "Authorization: Bearer your-jwt-token" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-3-opus-20240229",
    "max_tokens": 1024,
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

## Client Configuration

### For Claude Code (CLI)

```bash
export ANTHROPIC_AUTH_TOKEN="your-jwt-token"
export CLAUDE_API_BASE_URL="http://localhost:8080"
```

Or configure in `~/.claude/settings.json`:
```json
{
  "apiBaseUrl": "http://localhost:8080",
  "authToken": "your-jwt-token"
}
```

### For OpenAI-compatible clients

```bash
export OPENAI_API_BASE="http://localhost:8080/v1"
export OPENAI_API_KEY="your-jwt-token"
```

### For Anthropic SDK

```bash
export ANTHROPIC_BASE_URL="http://localhost:8080"
export ANTHROPIC_API_KEY="your-jwt-token"
```

## Modes

### API Mode
Uses Anthropic API keys for direct API access. Configure keys via:
- Environment: `CCPROXY_CLAUDE_API_KEYS=key1,key2,key3`
- Config file: `claude.api_keys: [key1, key2, key3]`

Features:
- Multiple key rotation (round-robin or random)
- Automatic unhealthy key detection
- Full Anthropic API compatibility

### Web Mode
Uses claude.ai session cookies for web-based access. Add sessions via the admin API.

To get a session cookie:
1. Log in to claude.ai in your browser
2. Open Developer Tools > Application > Cookies
3. Copy the `sessionKey` value
4. Add it using the session API

## Headers

| Header | Description |
|--------|-------------|
| `Authorization: Bearer <token>` | JWT authentication |
| `X-Admin-Key: <key>` | Admin authentication |
| `X-Proxy-Mode: web\|api` | Force specific mode (optional) |

## Token Modes

When generating tokens, you can specify the mode:
- `web`: Only allows Web mode access
- `api`: Only allows API mode access
- `both`: Allows both modes (default)

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        ccproxy                              │
├─────────────────────────────────────────────────────────────┤
│  ┌─────────┐    ┌──────────┐    ┌───────────────────────┐  │
│  │ Router  │───▶│ JWT Auth │───▶│    Mode Router        │  │
│  │  (Gin)  │    │Middleware│    └───────────┬───────────┘  │
│  └─────────┘    └──────────┘                │              │
│                                    ┌────────┴────────┐     │
│                                    ▼                 ▼     │
│                          ┌──────────────┐    ┌──────────┐  │
│                          │  Web Proxy   │    │API Proxy │  │
│                          │ (claude.ai)  │    │(API Key) │  │
│                          └──────┬───────┘    └────┬─────┘  │
│  ┌──────────┐  ┌─────────┐      │                 │        │
│  │  SQLite  │  │Key Pool │      ▼                 ▼        │
│  │  Store   │  │(轮询)   │   claude.ai       api.anthropic │
│  └──────────┘  └─────────┘                                 │
└─────────────────────────────────────────────────────────────┘
```

## License

MIT
