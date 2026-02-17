# Local Development Guide

**Last Reviewed**: 2026-02-06  
**Source of Truth Paths**:
- Dev script: `scripts/dev.sh`
- Environment template: `.env.example`
- Makefile: `Makefile`

---

## Prerequisites

### Required Software

| Tool | Version | Purpose | Install Command |
|------|---------|---------|-----------------|
| Go | 1.22+ | Control plane | `brew install go` |
| Node.js | 18+ | Workers + Frontend | `brew install node` |
| Docker | Latest | Postgres + Temporal | [Docker Desktop](https://www.docker.com/products/docker-desktop) |
| Make | Any | Build automation | Built-in (macOS/Linux) |

### Verify Installation

```bash
go version        # Should show 1.22 or higher
node --version    # Should show v18 or higher
npm --version     # Should show 9 or higher
docker --version  # Should be installed and running
docker compose version  # Docker Compose plugin
```

### macOS Setup Tips

1. **Homebrew** (recommended package manager):
   ```bash
   /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
   ```

2. **Docker Desktop**: Ensure it's running in the menu bar

3. **Git**: `brew install git` (if not present)

---

## Quickstart

### 1. Clone and Setup

```bash
# Navigate to project directory
cd /Volumes/ExtSSD/Repos/webapp/gavryn-local

# Copy environment template
cp .env.example .env
```

### 2. Generate Required Secrets

The `LLM_SECRETS_KEY` is **required** for storing encrypted API keys:

```bash
# Generate encryption key
openssl rand -base64 32

# Add to .env
# LLM_SECRETS_KEY=<paste-output-here>
```

### 3. Start Everything

```bash
make dev
```

This command:
- Starts Docker containers (Postgres + Temporal)
- Runs database migrations
- Starts control plane + Temporal worker
- Starts Node.js workers (browser + tool-runner)
- Starts Vite frontend dev server
- Auto-detects and handles port conflicts

### 4. Access the Application

Once running, you'll see URLs in the terminal:

| Service | Default URL | Notes |
|---------|-------------|-------|
| Frontend | `http://localhost:5173` | Main UI |
| Control Plane API | `http://localhost:8080` | REST API |
| Temporal UI | `http://localhost:8088` | Workflow visualization |

### 5. Configure LLM Provider

1. Open the frontend URL in your browser
2. Click **Settings** (gear icon)
3. Click **Run Setup Wizard**
4. Follow the steps:
   - Choose provider (Codex, OpenAI, OpenRouter, etc.)
   - Configure authentication
   - Test connection
   - Save settings

**Note**: Without `LLM_SECRETS_KEY`, "Save settings" will fail even if "Test connection" succeeds.

---

## Development Commands

### Makefile Commands

| Command | Description |
|---------|-------------|
| `make dev` | Start all services (recommended) |
| `make smoke` | Run health check tests |
| `make up` | Start Docker dependencies only |
| `make down` | Stop Docker containers |
| `make logs` | View Docker container logs |
| `make ps` | Show running Docker containers |
| `make coverage` | Run all coverage reports |

### Running Components Individually

If you need more control, start components separately:

#### Docker Dependencies

```bash
docker compose up -d

# Run migrations manually
for migration in infra/migrations/*.sql; do
  psql "$POSTGRES_URL" -f "$migration"
done
```

#### Control Plane

Terminal 1 - API Server:
```bash
cd control-plane
go run ./cmd/control-plane
```

Terminal 2 - Temporal Worker:
```bash
cd control-plane
go run ./cmd/worker
```

#### Workers

Terminal 3 - Browser Worker:
```bash
cd workers/browser
npm install
npm run dev
```

Terminal 4 - Tool Runner:
```bash
cd workers/tool-runner
npm install
npm run dev
```

#### Frontend

Terminal 5 - Frontend:
```bash
cd frontend
npm install
npm run dev
```

---

## Testing

### Run All Tests

```bash
# Control plane tests
cd control-plane && make test

# Frontend tests
cd frontend && npm test -- --run

# Browser worker tests
cd workers/browser && npm test

# Tool runner tests
cd workers/tool-runner && npm test
```

### Coverage Reports

```bash
make coverage
```

Generates coverage reports for all components:
- Control plane: HTML report at `control-plane/coverage.html`
- Frontend: Console output + files

### Smoke Tests

```bash
make smoke
```

Validates the entire stack:
- Docker containers are running
- All services respond on health endpoints
- Database connectivity
- LLM connectivity (if configured)

### E2E Tests

```bash
cd frontend
npm run e2e
```

Playwright end-to-end tests.

---

## Development Workflow

### Making Changes

1. **Create a branch**:
   ```bash
   git checkout -b feature/your-feature-name
   # or
   git checkout -b fix/issue-description
   ```

2. **Make your changes** with tests

3. **Run tests**:
   ```bash
   make smoke
   cd control-plane && make test
   cd frontend && npm test -- --run
   ```

4. **Update documentation** if behavior changed

5. **Commit**:
   ```bash
   git add .
   git commit -m "type: description"
   # Types: feature, fix, docs, refactor, test, chore
   ```

### Branch Naming

Format: `type/short-description`

| Type | Use For |
|------|---------|
| `feature/` | New functionality |
| `fix/` | Bug fixes |
| `docs/` | Documentation changes |
| `refactor/` | Code restructuring |
| `test/` | Test additions/changes |
| `chore/` | Maintenance tasks |

Examples:
- `feature/llm-providers`
- `fix/settings-save`
- `docs/update-readme`

---

## Environment Configuration

### Required Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `LLM_SECRETS_KEY` | ✅ | 32-byte base64 key for encrypting API keys |
| `POSTGRES_URL` | Auto | Database connection (auto-built if not set) |

### Optional Variables

See [Configuration](./configuration.md) for complete reference.

### Environment Files

| File | Purpose |
|------|---------|
| `.env` | Your local configuration (gitignored) |
| `.env.example` | Template with all options |

**Never commit `.env` files!**

---

## Common Development Tasks

### Reset Database

```bash
# Stop Docker
make down

# Remove volume
docker volume rm gavryn-local_postgres_data

# Restart
make dev
```

### View Logs

```bash
# All Docker logs
make logs

# Specific container
docker compose logs -f temporal

# Service logs (in separate terminals)
```

### Port Conflicts

`make dev` automatically handles port conflicts by finding available ports. You'll see messages like:

```
Control plane port 8080 in use, switching to 8084
```

### Rebuild After Dependency Changes

```bash
# Go
rm -rf control-plane/go.sum
cd control-plane && go mod tidy

# Node
cd frontend && rm -rf node_modules && npm install
cd workers/browser && rm -rf node_modules && npm install
cd workers/tool-runner && rm -rf node_modules && npm install
```

### Run Single Test

```bash
# Go
cd control-plane
go test -run TestSpecificFunction ./internal/api/

# Node/Vitest
cd frontend
npm test -- --run -t "test name pattern"
```

---

## Troubleshooting Development Issues

### "Port already in use"

```bash
# Find process using port
lsof -i :8080

# Kill process
kill -9 <PID>

# Or use make dev (auto-port selection)
make dev
```

### "Database connection refused"

1. Check Docker is running:
   ```bash
   docker compose ps
   ```

2. Verify Postgres container is healthy:
   ```bash
   docker compose logs postgres
   ```

3. Check migrations ran:
   ```bash
   docker compose exec postgres psql -U gavryn -d gavryn -c "\dt"
   ```

### "Cannot find module"

```bash
# Reinstall dependencies
cd <service-directory>
rm -rf node_modules package-lock.json
npm install
```

### "Worker not responding"

Check worker health:
```bash
curl http://localhost:8081/health  # Tool runner
curl http://localhost:8082/health  # Browser worker
```

### Frontend build errors

```bash
cd frontend
rm -rf node_modules dist
npm install
npm run build
```

### "LLM_SECRETS_KEY is required" on save

You must set `LLM_SECRETS_KEY` in `.env`:

```bash
# Generate key
openssl rand -base64 32

# Add to .env
# LLM_SECRETS_KEY=<output>

# Restart make dev
```

See [Troubleshooting](./troubleshooting.md) for more issues.

---

## IDE Setup

### VS Code

Recommended extensions:
- Go (golang.go)
- ES7+ React/Redux/React-Native snippets
- Tailwind CSS IntelliSense
- Prettier
- ESLint

### Go Development

Ensure `GOPATH` and `GOROOT` are configured:

```bash
go env GOPATH
go env GOROOT
```

### TypeScript/React

The frontend uses:
- TypeScript strict mode
- Path alias `@/*` → `src/*`
- Tailwind CSS with HSL variables

---

## Performance Tips

### Faster Docker Builds

Enable BuildKit:
```bash
export DOCKER_BUILDKIT=1
```

### Parallel Testing

```bash
# Run tests in parallel (different terminals)
```

### Exclude from Spotlight (macOS)

Add `node_modules` to Spotlight exclusions to improve performance.

---

## Contributing Checklist

Before submitting a PR:

- [ ] `make smoke` passes
- [ ] Relevant component tests pass
- [ ] Documentation updated if behavior changed
- [ ] No secrets or `.env` files committed
- [ ] Branch follows naming convention
- [ ] Commit messages are descriptive

See [CONTRIBUTING.md](../CONTRIBUTING.md) for full guidelines.

---

## Next Steps

- Learn about [Configuration](./configuration.md)
- Understand the [Architecture](./architecture.md)
- Review the [API Reference](./api-reference.md)
- Check [Troubleshooting](./troubleshooting.md) for issues
