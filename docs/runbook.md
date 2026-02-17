# Operations Runbook

**Last Reviewed**: 2026-02-06  
**Source of Truth Paths**:
- Dev script: `scripts/dev.sh`
- Smoke tests: `scripts/smoke.sh`
- Docker compose: `docker-compose.yml`

---

## Overview

This runbook covers common operational tasks for running Gavryn Local in development and testing environments. For production deployments, additional considerations apply.

---

## Health Checks

### Service Health Endpoints

All services expose a `/health` endpoint:

```bash
# Control plane
curl http://localhost:8080/health
# Expected: {"status":"ok"}

# Tool runner
curl http://localhost:8081/health
# Expected: {"status":"ok"}

# Browser worker
curl http://localhost:8082/health
# Expected: {"status":"ok"}
```

### Database Health

```bash
# Check Postgres connectivity
docker compose exec postgres pg_isready -U gavryn

# List tables
docker compose exec postgres psql -U gavryn -d gavryn -c "\dt"

# Check migration status (look for expected tables)
docker compose exec postgres psql -U gavryn -d gavryn -c "
SELECT table_name 
FROM information_schema.tables 
WHERE table_schema = 'public';
"
```

### Temporal Health

```bash
# Check Temporal namespace
docker compose exec temporal tctl --address temporal:7233 namespace describe default

# Check workflow executions
docker compose exec temporal tctl --address temporal:7233 workflow list
```

### Full Stack Smoke Test

```bash
make smoke
```

This runs comprehensive health checks across all services.

---

## Starting Services

### Development Mode (Recommended)

```bash
make dev
```

This starts:
- Docker containers (Postgres, Temporal, Temporal UI)
- Database migrations
- Control plane API server
- Temporal worker process
- Browser worker
- Tool runner
- Vite frontend dev server

### Docker Only

```bash
make up
```

Starts only Postgres and Temporal containers. Useful when running services manually.

### Manual Service Start

See [Local Development](./local-dev.md#running-components-individually) for starting services separately.

---

## Stopping Services

### Graceful Shutdown

```bash
# Stop all services (started with make dev)
Ctrl+C in the terminal running make dev

# Stop Docker only
make down
```

### Force Stop

If services don't stop cleanly:

```bash
# Kill all node processes
pkill -f node

# Kill all Go processes
pkill -f "go run"

# Stop Docker forcibly
docker compose down -v
```

---

## Logging

### Log Locations

| Service | Log Output |
|---------|------------|
| Control plane | Terminal stdout (colored) |
| Temporal worker | Terminal stdout |
| Tool runner | Terminal stdout |
| Browser worker | Terminal stdout |
| Frontend (Vite) | Terminal stdout |
| Postgres | `docker compose logs postgres` |
| Temporal | `docker compose logs temporal` |

### Viewing Logs

```bash
# All Docker containers
make logs

# Specific container (follow mode)
docker compose logs -f temporal

# Last 100 lines
docker compose logs --tail=100 postgres
```

### Log Levels

Control plane uses standard Go logging with Chi middleware:
- HTTP requests are logged automatically
- Use `fmt.Println()` or structured logging in code

---

## Database Operations

### Running Migrations

Migrations run automatically on `make dev`. To run manually:

```bash
# Apply all migrations
for migration in infra/migrations/*.sql; do
  psql "$POSTGRES_URL" -f "$migration"
done

# Or using Docker
docker compose exec -T postgres psql -U gavryn -d gavryn < infra/migrations/001_init.sql
```

### Migration Files

| File | Purpose |
|------|---------|
| `001_init.sql` | Core tables (runs, messages, events) |
| `002_llm_settings.sql` | LLM configuration storage |
| `003_skills.sql` | Skills system tables |
| `004_context.sql` | Context management tables |
| `005_memory.sql` | Memory system (pgvector + FTS) |
| `006_personality.sql` | Personality settings |

### Database Backup

```bash
# Backup
docker compose exec postgres pg_dump -U gavryn gavryn > backup.sql

# Restore
docker compose exec -T postgres psql -U gavryn gavryn < backup.sql
```

### Reset Database

**Warning**: Destroys all data!

```bash
# Stop containers
make down

# Remove volume
docker volume rm gavryn-local_postgres_data

# Restart
make dev
```

### Query Examples

```bash
# List runs
docker compose exec postgres psql -U gavryn -d gavryn -c "SELECT * FROM runs;"

# Count messages per run
docker compose exec postgres psql -U gavryn -d gavryn -c "
SELECT r.id, r.title, COUNT(m.id) as message_count
FROM runs r
LEFT JOIN messages m ON r.id = m.run_id
GROUP BY r.id, r.title;
"

# Check LLM settings
docker compose exec postgres psql -U gavryn -d gavryn -c "SELECT provider, model FROM llm_settings;"
```

---

## Service Monitoring

### Process Status

```bash
# Check running processes
ps aux | grep -E "(go|node|docker)" | grep -v grep

# Check ports in use
lsof -iTCP -sTCP:LISTEN -n -P
```

### Resource Usage

```bash
# Docker stats
docker stats

# Container resource usage
docker compose ps
```

### Temporal Monitoring

Access Temporal UI at `http://localhost:8088` (or auto-selected port).

View:
- Running workflows
- Workflow history
- Activity executions
- Task queues

---

## Common Operations

### Restart Single Service

```bash
# If running with make dev:
# 1. Stop make dev (Ctrl+C)
# 2. Start service manually in new terminal

# Example: Restart just the browser worker
cd workers/browser
npm run dev
```

### Update Dependencies

```bash
# Go
cd control-plane
go get -u ./...
go mod tidy

# Node
cd frontend
npm update

# Or reinstall
cd workers/browser
rm -rf node_modules
npm install
```

### Clean Build Artifacts

```bash
# Frontend
cd frontend
rm -rf dist node_modules/.vite

# Go (removes build cache)
go clean -cache

# Docker
docker system prune -f
```

### Port Management

When ports are in use, `make dev` automatically finds alternatives:

```
Control plane port 8080 in use, switching to 8084
Tool runner port 8081 in use, switching to 8085
```

To force specific ports:

```bash
CONTROL_PLANE_PORT=8090 TOOL_RUNNER_PORT=8091 make dev
```

---

## Troubleshooting Operations

### Service Won't Start

1. Check port availability:
   ```bash
   lsof -i :8080
   ```

2. Check Docker is running:
   ```bash
   docker info
   ```

3. Review logs for specific errors

### Database Connection Issues

1. Verify Postgres is running:
   ```bash
   docker compose ps postgres
   ```

2. Check environment variables:
   ```bash
   echo $POSTGRES_URL
   ```

3. Test connection:
   ```bash
   psql "$POSTGRES_URL" -c "SELECT 1;"
   ```

### Worker Connection Issues

Workers report connection failures in logs. Check:

```bash
# Control plane URL accessibility
curl http://localhost:8080/health

# Worker can reach control plane
# (from worker logs: "failed to emit event")
```

### Memory Issues

If Docker containers use excessive memory:

```bash
# Restart containers
make down && make up

# Or limit resources in docker-compose.yml
```

---

## Security Operations

### Rotate Encryption Key

**Warning**: Rotating `LLM_SECRETS_KEY` invalidates stored API keys!

1. Note current API keys
2. Generate new key: `openssl rand -base64 32`
3. Update `.env`
4. Reconfigure LLM settings in UI

### Audit API Keys

Check which providers are configured:

```bash
docker compose exec postgres psql -U gavryn -d gavryn -c "
SELECT provider, model, 
       CASE WHEN api_key_enc IS NOT NULL THEN 'configured' ELSE 'missing' END as key_status
FROM llm_settings;
"
```

---

## Performance Tuning

### Docker Performance

On macOS, Docker can be slow. Optimize with:

1. **Docker Desktop Resources**: Allocate more CPU/RAM
2. **Volume mounts**: Use `:delegated` flag for mounts
3. **Exclude from Spotlight**: Add Docker to exclusions

### Database Performance

For large datasets:

```sql
-- Add indexes (if not present)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_messages_run_id ON messages(run_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_run_events_run_seq ON run_events(run_id, seq);
```

### Worker Scaling

Currently single-instance. For testing:

```bash
# Run multiple browser workers (different ports)
BROWSER_WORKER_PORT=8083 npm run dev
```

---

## Disaster Recovery

### Complete Reset

```bash
# 1. Stop everything
make down
pkill -f node
pkill -f "go run"

# 2. Remove all data
docker volume rm gavryn-local_postgres_data

# 3. Clean build artifacts
find . -name "node_modules" -type d -exec rm -rf {} + 2>/dev/null || true
find . -name "dist" -type d -exec rm -rf {} + 2>/dev/null || true

# 4. Reinstall and restart
cd frontend && npm install
cd ../workers/browser && npm install
cd ../workers/tool-runner && npm install
make dev
```

### Recover from Backup

```bash
# Restore from backup file
docker compose exec -T postgres psql -U gavryn gavryn < backup.sql
```

---

## Maintenance Windows

### Scheduled Tasks

None required for development use. For extended operation:

1. **Log rotation**: Docker handles container logs
2. **Database cleanup**: Remove old runs if needed
3. **Dependency updates**: Monthly `npm update` / `go get -u`

### Cleanup Commands

```bash
# Remove old Docker images
docker image prune -a

# Clean npm cache
npm cache clean --force

# Clean Go cache
go clean -cache
```

---

## See Also

- [Troubleshooting](./troubleshooting.md) - Common issues and fixes
- [Configuration](./configuration.md) - Environment variables
- [Local Development](./local-dev.md) - Development setup
- [Architecture](./architecture.md) - System architecture
