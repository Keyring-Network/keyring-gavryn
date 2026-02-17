# Configuration

**Last Reviewed**: 2026-02-06  
**Source of Truth Paths**:
- Config package: `control-plane/internal/config/`
- Environment template: `.env.example`
- Dev script: `scripts/dev.sh`

---

## Overview

Gavryn Local uses environment variables for configuration. The `scripts/dev.sh` script orchestrates defaults and port selection, while individual services read their specific configurations.

---

## Environment Files

### File Locations

| File | Purpose | In Git |
|------|---------|--------|
| `.env.example` | Template with all options | Yes |
| `.env` | Your local configuration | **No** |

### Setup

```bash
# Copy the template
cp .env.example .env

# Edit with your values
nano .env
```

**Warning**: Never commit `.env` files! The `.gitignore` excludes them.

---

## Core Configuration

### Database

Postgres connection settings.

| Variable | Default | Required | Description |
|----------|---------|----------|-------------|
| `POSTGRES_USER` | `gavryn` | No | Database username |
| `POSTGRES_PASSWORD` | `gavryn` | No | Database password |
| `POSTGRES_DB` | `gavryn` | No | Database name |
| `POSTGRES_HOST` | `localhost` | No | Database host |
| `POSTGRES_PORT` | `5432` | No | Database port |
| `POSTGRES_URL` | Auto-built | No | Full connection string |

**URL Building**:

If `POSTGRES_URL` is not set, it's automatically constructed:

```
postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@${POSTGRES_HOST}:${POSTGRES_PORT}/${POSTGRES_DB}?sslmode=disable
```

**Recent Change (Feb 2026)**: `scripts/dev.sh` re-reads the actual Docker-mapped port after startup to prevent stale references.

### Service Ports

| Variable | Default | Description |
|----------|---------|-------------|
| `CONTROL_PLANE_PORT` | `8080` | Control plane API |
| `TOOL_RUNNER_PORT` | `8081` | Tool runner worker |
| `BROWSER_WORKER_PORT` | `8082` | Browser worker |
| `TEMPORAL_UI_PORT` | `8088` | Temporal web UI |

**Port Selection**:
- `make dev` automatically finds available ports
- Override with explicit values: `CONTROL_PLANE_PORT=8090 make dev`
- Reserved ports are tracked to prevent conflicts

### Service URLs

| Variable | Default | Description |
|----------|---------|-------------|
| `CONTROL_PLANE_URL` | `http://localhost:8080` | Control plane base URL |
| `TOOL_RUNNER_URL` | `http://localhost:8081` | Tool runner base URL |
| `BROWSER_WORKER_URL` | `http://localhost:8082` | Browser worker base URL |
| `VITE_API_BASE_URL` | `http://localhost:8080` | Frontend API base URL |
| `TEMPORAL_ADDRESS` | `localhost:7233` | Temporal server address |

**Note**: These are automatically set by `scripts/dev.sh` based on selected ports.

---

## LLM Configuration

### Required Settings

| Variable | Required | Description |
|----------|----------|-------------|
| `LLM_SECRETS_KEY` | **Yes** | 32-byte base64 key for API key encryption |

**Generate**:
```bash
openssl rand -base64 32
```

**Without this key**: "Save settings" will fail with 500 error even if "Test connection" succeeds.

### Provider Settings

| Variable | Default | Description |
|----------|---------|-------------|
| `LLM_MODE` | `remote` | LLM mode (`remote` or `local`) |
| `LLM_PROVIDER` | `codex` | Provider identifier |
| `LLM_MODEL` | `gpt-5.2-codex` | Model name |
| `LLM_BASE_URL` | Provider default | Custom API base URL |

### Provider Identifiers

| Provider | Identifier | Default Base URL |
|----------|------------|------------------|
| Codex | `codex` | Uses CLI auth |
| OpenAI | `openai` | `https://api.openai.com/v1` |
| OpenRouter | `openrouter` | `https://openrouter.ai/api/v1` |
| OpenCode Zen | `opencode-zen` | `https://opencode.ai/zen/v1` |
| Kimi for Coding | `kimi-for-coding` | `https://api.kimi.com/coding/v1` |
| Moonshot AI | `moonshot-ai` | `https://api.moonshot.ai/v1` |

### API Keys

Provider-specific API keys (optional if using setup wizard):

| Variable | Provider | Example |
|----------|----------|---------|
| `OPENAI_API_KEY` | OpenAI | `sk-...` |
| `OPENROUTER_API_KEY` | OpenRouter | `sk-or-...` |
| `OPENCODE_API_KEY` | OpenCode Zen | `sk-...` |

**Note**: API keys are encrypted before storage. The `LLM_SECRETS_KEY` is required for encryption/decryption.

### Codex CLI Configuration

For Codex provider (uses local CLI authentication):

| Variable | Default | Description |
|----------|---------|-------------|
| `CODEX_AUTH_PATH` | Auto-detected | Path to `auth.json` |
| `CODEX_HOME` | `~/.codex` | Codex CLI home directory |

**Setup**:
```bash
codex login
# Creates ~/.codex/auth.json
```

---

## Worker Configuration

### Tool Runner

| Variable | Default | Description |
|----------|---------|-------------|
| `ALLOWED_TOOLS` | See below | Comma-separated allowlist |

**Default Allowlist**:
```
browser.navigate,browser.snapshot,browser.click,browser.type,browser.scroll,browser.extract,browser.evaluate,browser.pdf,document.create_pptx,document.create_docx,document.create_pdf,document.create_csv
```

### Browser Worker

| Variable | Default | Description |
|----------|---------|-------------|
| `BROWSER_HEADLESS` | `true` | Run browser in headless mode |

**Headless Mode**:
- `true`: No visible browser window (faster, CI-friendly)
- `false`: Visible browser window (useful for debugging)

---

## Temporal Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `TEMPORAL_ADDRESS` | `localhost:7233` | Temporal server gRPC address |
| `TEMPORAL_TASK_QUEUE` | `gavryn-runs` | Task queue name |

**Task Queue Isolation** (Feb 2026):

When running multiple dev instances, each gets an isolated queue:

```bash
TEMPORAL_TASK_QUEUE=gavryn-runs-8084
```

This prevents cross-talk between instances.

---

## Complete Configuration Examples

### OpenAI Configuration

```bash
# .env
LLM_PROVIDER=openai
LLM_MODEL=gpt-4o
OPENAI_API_KEY=sk-your-key-here
LLM_SECRETS_KEY=your-encryption-key-here
```

### OpenRouter Configuration

```bash
# .env
LLM_PROVIDER=openrouter
LLM_MODEL=anthropic/claude-3.5-sonnet
OPENROUTER_API_KEY=sk-or-your-key-here
LLM_SECRETS_KEY=your-encryption-key-here
```

### Codex Configuration

```bash
# .env
LLM_PROVIDER=codex
LLM_MODEL=gpt-5.2-codex
CODEX_AUTH_PATH=/Users/YOUR_USERNAME/.codex/auth.json
LLM_SECRETS_KEY=your-encryption-key-here
```

### OpenCode Zen Configuration

```bash
# .env
LLM_PROVIDER=opencode-zen
LLM_MODEL=opencode/gpt-5.2-codex
OPENCODE_API_KEY=sk-your-key-here
LLM_SECRETS_KEY=your-encryption-key-here
```

### Kimi for Coding Configuration

```bash
# .env
LLM_PROVIDER=kimi-for-coding
LLM_MODEL=kimi-k2-2024-12-11
LLM_SECRETS_KEY=your-encryption-key-here
```

### Moonshot AI Configuration

```bash
# .env
LLM_PROVIDER=moonshot-ai
LLM_MODEL=moonshot-v1-8k
LLM_SECRETS_KEY=your-encryption-key-here
```

---

## Configuration Precedence

Configuration is loaded in this order (later values override earlier):

1. **Hardcoded defaults** (in code)
2. **`.env` file** (if present)
3. **Environment variables** (shell exports)
4. **Runtime assignment** (`scripts/dev.sh` dynamic values)

### Example Override Chain

```bash
# 1. Default: CONTROL_PLANE_PORT=8080
# 2. .env: CONTROL_PLANE_PORT=8081
# 3. Shell: export CONTROL_PLANE_PORT=8082
# 4. dev.sh: Detects port 8082 in use, assigns 8084

# Final value: 8084
```

---

## Dynamic Configuration

### Dev Script Behavior

`scripts/dev.sh` performs these configuration steps:

1. **Load `.env`** if present
2. **Select ports** - Find available ports starting from defaults
3. **Build URLs** - Construct service URLs from selected ports
4. **Set task queue** - Isolate queue per control-plane port
5. **Export all** - Make variables available to child processes

### Port Selection Algorithm

```bash
choose_port() {
  local port=$base_port
  while port_in_use $port; do
    port=$((port + 1))
  done
  echo $port
}
```

**Example**:
- Port 8080 in use → Try 8081 → Try 8082 → Assign 8082

---

## Configuration Validation

### At Startup

| Check | Behavior if Failing |
|-------|---------------------|
| Postgres connectivity | Log error, retry |
| Temporal connectivity | Log error, retry |
| LLM_SECRETS_KEY | Allow startup, fail on save |

### API Validation

Configuration is validated when saving via API:

```http
POST /settings/llm/test
```

Tests provider connectivity without persisting.

```http
POST /settings/llm
```

Validates and persists (requires `LLM_SECRETS_KEY`).

---

## Security Considerations

### Secrets Management

| Secret | Storage | Encryption |
|--------|---------|------------|
| API keys | Postgres | AES-256-GCM (via `LLM_SECRETS_KEY`) |
| LLM_SECRETS_KEY | Environment | None (protect this!) |
| Codex auth | Filesystem | None (relies on OS permissions) |

### Best Practices

1. **Generate strong keys**: Use `openssl rand -base64 32`
2. **Protect `.env`**: Set restrictive permissions: `chmod 600 .env`
3. **Rotate periodically**: Change `LLM_SECRETS_KEY` (requires re-configuration)
4. **Never commit secrets**: Verify `.env` is in `.gitignore`

### Key Rotation

To rotate `LLM_SECRETS_KEY`:

1. Note current API keys from UI
2. Generate new key: `openssl rand -base64 32`
3. Update `.env`
4. Restart services
5. Re-enter API keys in setup wizard

---

## Configuration Reference Tables

### All Environment Variables

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `POSTGRES_USER` | string | `gavryn` | DB username |
| `POSTGRES_PASSWORD` | string | `gavryn` | DB password |
| `POSTGRES_DB` | string | `gavryn` | DB name |
| `POSTGRES_HOST` | string | `localhost` | DB host |
| `POSTGRES_PORT` | int | `5432` | DB port |
| `POSTGRES_URL` | string | Auto | Full DB URL |
| `CONTROL_PLANE_PORT` | int | `8080` | API port |
| `CONTROL_PLANE_URL` | string | `http://localhost:8080` | API URL |
| `TOOL_RUNNER_PORT` | int | `8081` | Tool runner port |
| `TOOL_RUNNER_URL` | string | `http://localhost:8081` | Tool runner URL |
| `BROWSER_WORKER_PORT` | int | `8082` | Browser worker port |
| `BROWSER_WORKER_URL` | string | `http://localhost:8082` | Browser worker URL |
| `TEMPORAL_UI_PORT` | int | `8088` | Temporal UI port |
| `TEMPORAL_ADDRESS` | string | `localhost:7233` | Temporal server |
| `TEMPORAL_TASK_QUEUE` | string | `gavryn-runs` | Task queue |
| `VITE_API_BASE_URL` | string | `http://localhost:8080` | Frontend API URL |
| `LLM_MODE` | enum | `remote` | LLM mode |
| `LLM_PROVIDER` | string | `codex` | Provider ID |
| `LLM_MODEL` | string | `gpt-5.2-codex` | Model name |
| `LLM_BASE_URL` | string | Provider default | Custom URL |
| `LLM_SECRETS_KEY` | base64 | - | Encryption key |
| `OPENAI_API_KEY` | string | - | OpenAI key |
| `OPENROUTER_API_KEY` | string | - | OpenRouter key |
| `OPENCODE_API_KEY` | string | - | OpenCode key |
| `CODEX_AUTH_PATH` | path | Auto | Codex auth file |
| `CODEX_HOME` | path | `~/.codex` | Codex home |
| `ALLOWED_TOOLS` | CSV | See defaults | Tool allowlist |
| `BROWSER_HEADLESS` | bool | `true` | Headless browser |

---

## Troubleshooting Configuration

### "LLM_SECRETS_KEY is required"

**Cause**: Missing encryption key  
**Fix**:
```bash
openssl rand -base64 32
# Add to .env
```

### "Connection refused" to services

**Cause**: Wrong port configuration  
**Fix**:
1. Check actual ports: `lsof -iTCP -sTCP:LISTEN`
2. Use `make dev` for auto-selection
3. Or explicitly set ports in `.env`

### "Database connection refused"

**Cause**: Postgres not running or wrong URL  
**Fix**:
```bash
# Check Docker
docker compose ps

# Verify URL
echo $POSTGRES_URL

# Test connection
psql "$POSTGRES_URL" -c "SELECT 1"
```

### Changes Not Applied

**Cause**: Services need restart  
**Fix**:
```bash
# Stop make dev (Ctrl+C)
# Restart
make dev
```

---

## See Also

- [Local Development](./local-dev.md) - Setup guide
- [Troubleshooting](./troubleshooting.md) - Common issues
- [Runbook](./runbook.md) - Operations guide
- [Architecture](./architecture.md) - System design
