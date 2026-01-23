# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Development Commands

```bash
# Build the binary (requires CGO for SQLite)
make build                    # or: CGO_ENABLED=1 go build -o ccproxy ./cmd/server

# Run locally (builds first)
make run

# Run tests
make test                     # or: go test -v ./...

# Run a single test
go test -v ./internal/handler -run TestTokenGenerate

# Format and lint
make fmt                      # or: go fmt ./...
make lint                     # requires golangci-lint

# Manage dependencies
make deps                     # go mod download && go mod tidy
```

## Docker Commands

```bash
make docker-build             # Build image
make docker-run               # Start with docker-compose
make docker-stop              # Stop
make docker-logs              # View logs
```

## Architecture

ccproxy is a Go proxy service for Claude with dual-mode support (Web mode via claude.ai sessions, API mode via Anthropic API keys) and JWT authentication.

**Core Components:**

- `cmd/server/main.go` - Entry point, sets up Gin router with all routes and middleware
- `internal/config/` - Viper-based configuration (config.yaml + CCPROXY_* env vars)
- `internal/handler/` - HTTP handlers:
  - `token.go` - JWT token generation/revocation (admin endpoints)
  - `session.go` - Legacy session management (admin endpoints, kept for backward compatibility)
  - `account.go` - OAuth account management (admin endpoints)
  - `proxy.go` - OpenAI-compatible /v1/chat/completions router
  - `web_proxy.go` - claude.ai proxy for web mode
  - `api_proxy.go` - Anthropic API proxy with key pool
- `internal/middleware/` - JWT auth and admin key middleware
- `internal/loadbalancer/` - API key pool with round-robin/random strategies
- `internal/store/` - SQLite storage for tokens, accounts (replaces sessions)
- `internal/service/` - Business logic layer:
  - `oauth.go` - OAuth login flow and token refresh
- `pkg/jwt/` - JWT token generation/validation

**Request Flow:**

1. Request hits Gin router
2. JWT middleware validates token and extracts mode permissions
3. Mode router directs to Web or API proxy based on token mode and X-Proxy-Mode header
4. Proxy handler forwards to claude.ai (with session cookie) or api.anthropic.com (with pooled API key)

**Configuration:** Uses Viper with `CCPROXY_` prefix for env vars. Required: `CCPROXY_JWT_SECRET`, `CCPROXY_ADMIN_KEY`. For API mode: `CCPROXY_CLAUDE_API_KEYS`.

## Claude Code Client Configuration

```bash
# Required environment variables
export ANTHROPIC_AUTH_TOKEN="your-jwt-token"
export CLAUDE_API_BASE_URL="http://localhost:8080"   # No /v1 suffix!
```

Or in `~/.claude/settings.json`:
```json
{
  "apiBaseUrl": "http://localhost:8080",
  "authToken": "your-jwt-token"
}
```

**Important:** `CLAUDE_API_BASE_URL` should NOT include `/v1` - the proxy adds it automatically.

## Recent Updates (2026-01-23)

### OAuth with Chrome TLS Fingerprint

Updated OAuth service to use `imroc/req/v3` library with `ImpersonateChrome()` for bypassing Cloudflare detection.

**Key Changes:**
- Uses correct OAuth constants (ClientID, TokenURL, RedirectURI) from official Claude implementation
- Automatic system proxy detection from environment variables (`HTTPS_PROXY`, `HTTP_PROXY`, `ALL_PROXY`)
- Support for manual proxy URL in OAuth requests

**OAuth Request with Proxy:**
```bash
curl -X POST http://localhost:8080/api/account/oauth \
  -H "X-Admin-Key: your-key" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "my-account",
    "session_key": "sk-ant-sid01-xxx",
    "proxy_url": "http://127.0.0.1:7890"
  }'
```

**Files Modified:**
- `internal/service/oauth.go` - Rewritten with imroc/req and correct OAuth flow
- `cmd/server/main.go` - Added event_logging endpoint, fixed double /v1 path handling

---

## Recent Updates (2026-01-22)

### OAuth Account Management (Latest)

**Major Architecture Update:** Unified account management system with full OAuth support.

**Key Features:**
- **3-Step OAuth Login**: Automatically converts sessionKey to OAuth access token
  - Step 1: Get organization UUID from claude.ai
  - Step 2: Obtain authorization code with PKCE
  - Step 3: Exchange for access token and refresh token
- **Automatic Token Refresh**: Monitors token expiration and auto-refreshes
- **Health Monitoring**: Built-in health checks for account validation
- **Account Types**:
  - `oauth` - OAuth accounts with automatic token refresh
  - `session_key` - Legacy sessionKey support (backward compatible)
  - `api_key` - Direct API key (future support)
- **Database Migration**: Automatic migration from `sessions` to `accounts` table

**New API Endpoints:**
```
POST   /api/account/oauth           # OAuth login (sessionKey → OAuth token)
POST   /api/account/sessionkey      # Create session key account (legacy)
GET    /api/account/list            # List all accounts
GET    /api/account/:id             # Get account details
PUT    /api/account/:id             # Update account
DELETE /api/account/:id             # Delete account
POST   /api/account/:id/deactivate  # Deactivate account
POST   /api/account/:id/refresh     # Manual token refresh
POST   /api/account/:id/check       # Health check
```

**Anti-Detection Features:**
- OAuth Bearer token authentication (better than Cookie)
- Browser fingerprint simulation (Chrome 131)
- Identity isolation between accounts

**Files Added/Modified:**
- `internal/store/account.go` - New unified account data model
- `internal/service/oauth.go` - OAuth login and token refresh service
- `internal/handler/account.go` - Account management API handlers
- `internal/store/sqlite.go` - Database migration for accounts table
- `cmd/server/main.go` - Registered account management routes
- `internal/handler/web_proxy.go` - Updated to use Account instead of Session
- `internal/handler/proxy.go` - Updated to use Account instead of Session
- `docs/OAUTH_GUIDE.md` - Complete OAuth usage documentation
- `test-oauth-account.sh` - OAuth account management test script

**Usage:**
See [OAuth Account Management Guide](./docs/OAUTH_GUIDE.md) for detailed usage instructions.

### Anti-Detection Optimizations

To prevent triggering Claude's risk control systems, the proxy now uses realistic browser fingerprints:

**Web Mode (web_proxy.go, proxy.go):**
- Updated User-Agent to Chrome 131 (latest 2026 version)
- Added modern Client Hints headers (Sec-Ch-Ua-*)
- Complete Sec-Fetch-* security headers
- Full Accept-Encoding support (gzip, deflate, br, zstd)
- Multi-language Accept-Language header
- Proper Cache-Control and Pragma headers

**API Mode (api_proxy.go):**
- Standard SDK User-Agent (anthropic-sdk-go)
- Accept-Encoding for compression support
- Complete Anthropic API headers

### Admin UI Enhancements

**1. Configuration Documentation (`/admin/docs`)**
- Embedded Claude Code setup guide
- Environment variable reference
- Mode selection explanation (web/api/both)
- Streaming configuration examples
- Troubleshooting guide

**2. Token Management Improvements**
- **View Token Info**: Display token metadata (ID, mode, timestamps)
- **Test Token**: Test tokens by inputting saved token, sends test message to verify functionality
- Token ID preview in list (first 8 chars)
- Security note: Token plaintext is not stored for security reasons

**3. Session Management Enhancements**
- **Test Session**: Test sessions directly from admin UI with live chat test
- Real-time test results display
- Retry capability for failed tests

### Testing Tools

**Test Script (`test-proxy.sh`):**
```bash
./test-proxy.sh YOUR_TOKEN http://localhost:8080
```
- Automated testing of models, chat, streaming
- Web/API mode switching tests
- Claude Code configuration examples
- Colored output for easy reading

### Files Modified

- `internal/handler/web_proxy.go` - Enhanced request headers for web mode
- `internal/handler/api_proxy.go` - Enhanced request headers for API mode
- `internal/handler/proxy.go` - Enhanced request headers in unified proxy
- `web/src/pages/Tokens.tsx` - Added token view and test features
- `web/src/pages/Sessions.tsx` - Added session test feature
- `web/src/pages/Docs.tsx` - New documentation page
- `web/src/App.tsx` - Added docs route
- `web/src/components/Layout.tsx` - Added docs navigation
- `test-proxy.sh` - New testing script
