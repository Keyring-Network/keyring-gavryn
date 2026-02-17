# Troubleshooting

**Last Reviewed**: 2026-02-06  
**Source of Truth Paths**:
- Dev script: `scripts/dev.sh`
- Smoke tests: `scripts/smoke.sh`
- Known issues: Project knowledge base

---

## Quick Diagnosis

Run the smoke test first:

```bash
make smoke
```

This validates the entire stack and identifies most common issues.

---

## Common Issues

### "LLM_SECRETS_KEY is required" Error

**Symptoms**:
- Setup wizard shows "Test connection succeeded"
- "Save settings" fails with 500 error
- Logs show: `LLM_SECRETS_KEY is required`

**Cause**: Backend cannot encrypt API keys without encryption key.

**Solution**:

1. Generate a key:
   ```bash
   openssl rand -base64 32
   ```

2. Add to `.env`:
   ```bash
   LLM_SECRETS_KEY=<paste-output-here>
   ```

3. Restart services:
   ```bash
   # Stop current session (Ctrl+C)
   make dev
   ```

**Verification**: After restart, "Save settings" should succeed.

---

### Database Connection Refused

**Symptoms**:
- Errors on startup: `connection refused localhost:5432` or `localhost:5433`
- Cannot save settings
- Chat doesn't work

**Causes & Solutions**:

#### 1. Docker Not Running

**Check**:
```bash
docker info
```

**Fix**: Start Docker Desktop

#### 2. Postgres Container Not Started

**Check**:
```bash
docker compose ps
```

**Fix**:
```bash
make up
# or
make dev
```

#### 3. Migrations Not Applied

**Check**:
```bash
docker compose exec postgres psql -U gavryn -d gavryn -c "\dt"
# Should show tables: runs, messages, llm_settings, etc.
```

**Fix**:
```bash
for migration in infra/migrations/*.sql; do
  psql "$POSTGRES_URL" -f "$migration"
done
```

#### 4. Port Conflict with Local Postgres

**Check**:
```bash
lsof -i :5432
# If you see native Postgres, not Docker
```

**Fix** (Feb 2026): `scripts/dev.sh` now re-reads the actual Docker port after startup. Just use `make dev`.

**Workaround**:
```bash
# Stop native Postgres
brew services stop postgresql

# Or use different port
POSTGRES_PORT=5433 make dev
```

#### 5. Stale Port Reference

**Symptoms**: Port changed but code still references old port.

**Fix** (Feb 2026): Fixed in recent dev script. Ensure you're on latest:
```bash
git pull origin main
```

---

### "Model not appearing in dropdown"

**Symptoms**:
- Provider selected but no models load
- Dropdown shows empty or stuck loading

**Causes & Solutions**:

#### 1. API Key Invalid

**Check**: Browser console for fetch errors.

**Fix**: Verify API key in settings or `.env`.

#### 2. CORS Issues

**Symptoms**: Console shows CORS errors.

**Fix**: Usually not applicable for local dev. If using custom base URL, ensure it allows `localhost`.

#### 3. API Rate Limiting

**Symptoms**: Models load slowly or intermittently fail.

**Fix**: Wait a moment and retry. Check provider status page.

#### 4. Fallback Behavior

**Note**: If API fetching fails, backend uses hardcoded fallback lists. This is expected behavior.

---

### Workers Not Responding

**Symptoms**:
- Browser tools don't execute
- Document creation fails
- "Worker not found" errors in logs

**Diagnosis**:

```bash
# Check worker health
curl http://localhost:8081/health  # Tool runner
curl http://localhost:8082/health  # Browser worker
```

**Causes & Solutions**:

#### 1. Workers Not Started

**Fix**: Run `make dev` or start workers manually.

#### 2. Port Conflicts

**Symptoms**: Workers started but on different ports.

**Fix**:
```bash
# Check what ports are actually in use
lsof -iTCP -sTCP:LISTEN -n -P | grep -E "(8081|8082)"

# Restart with make dev for auto-port selection
```

#### 3. Control Plane URL Wrong

**Symptoms**: Workers can't emit events.

**Check**:
```bash
echo $CONTROL_PLANE_URL
```

**Fix**: Ensure `CONTROL_PLANE_URL` matches actual control plane port.

#### 4. Tool Not in Allowlist

**Symptoms**: "Tool not allowlisted" error.

**Fix**: Add tool to `ALLOWED_TOOLS` in `.env`.

---

### Chat History Not Reloading

**Symptoms**:
- Refresh page, task list is empty
- Previously created runs don't appear

**Cause** (Feb 2026 - Fixed): Frontend was not rehydrating task list on startup.

**Solution**: Ensure you're on latest version:

```bash
git pull origin main
make dev
```

**Verification**:
1. Create a run
2. Send a message
3. Refresh page
4. Run should appear in task list

---

### Orphan Worker Processes

**Symptoms**:
- After `Ctrl+C`, processes still running
- Port conflicts on next start
- Multiple instances of same service

**Cause**: Previous fix incomplete.

**Solution** (Feb 2026): `scripts/dev.sh` now has robust shutdown handling.

**Immediate Fix**:
```bash
# Kill all related processes
pkill -f node
pkill -f "go run"
pkill -f chromium  # Browser workers

# Verify
ps aux | grep -E "(node|go run)" | grep -v grep
```

---

### "Cross-talk" Between Dev Instances

**Symptoms**:
- One dev instance receives tasks from another
- Unexpected DB connection errors
- Messages appear in wrong runs

**Cause** (Feb 2026 - Fixed): Multiple control planes consuming from same Temporal queue.

**Solution**: Task queues are now isolated per port:

```bash
TEMPORAL_TASK_QUEUE=gavryn-runs-8084
```

**Verification**: Check logs for queue name:
```
Worker starting on queue: gavryn-runs-8084
```

---

### Setup Wizard Issues

#### "Test connection" works but "Save" fails

**Cause**: Infrastructure down (Postgres/Temporal).

**Symptoms**: Logs show connection refused to localhost:5433 or 7233.

**Fix**: Ensure `make dev` is running successfully before using wizard.

#### Provider Base URL Not Prefilled

**Symptoms**: OpenCode Zen or other providers show empty Base URL field.

**Fix** (Feb 2026): Fixed in recent update. Provider defaults now include:
- `opencode-zen`: `https://opencode.ai/zen/v1`
- `kimi-for-coding`: `https://api.kimi.com/coding/v1`
- `moonshot-ai`: `https://api.moonshot.ai/v1`

#### Wizard Step 7: "Saved" but Next Disabled

**Symptoms**: On final step, shows "Saved" but Next button grayed out.

**Fix** (Feb 2026): Added `llmAction` state to track per-action loading. Next button now correctly enabled when `hasLLMConfig` is true.

---

### Browser Automation Issues

#### Screenshot/Click Not Working

**Symptoms**:
- `browser.snapshot` returns error
- Elements not found

**Causes & Solutions**:

1. **Page not loaded**:
   - Ensure `browser.navigate` completed before other actions
   - Check `browser.evaluate` for `document.readyState`

2. **Selector issues**:
   - Use browser DevTools to verify selectors
   - Try more specific selectors (class + attribute)

3. **Headless mode**:
   - Set `BROWSER_HEADLESS=false` to see browser
   - Watch automation in real-time

#### Chrome/Chromium Not Found

**Symptoms**: Playwright can't launch browser.

**Fix**:
```bash
cd workers/browser
npx playwright install chromium
```

---

### Document Generation Issues

#### "Cannot create file"

**Symptoms**: Document tools fail with file system errors.

**Check**: 
- Artifacts directory exists: `workers/browser/artifacts/`
- Write permissions

**Fix**:
```bash
mkdir -p workers/browser/artifacts
chmod 755 workers/browser/artifacts
```

#### PDF Generation Fails

**Symptoms**: `browser.pdf` returns error.

**Cause**: Page not fully loaded or network resource blocked.

**Fix**: 
- Wait for page load: Add delay after `browser.navigate`
- Use `fullPage: true` option

---

### Frontend Build Issues

#### "Cannot find module"

**Symptoms**: Import errors, TypeScript compilation fails.

**Fix**:
```bash
cd frontend
rm -rf node_modules
npm install
```

#### Vite Dev Server Won't Start

**Symptoms**: Port 5173 in use or build errors.

**Fix**:
```bash
# Use different port
npm run dev -- --port 5174

# Or kill existing Vite
lsof -i :5173
kill -9 <PID>
```

#### Hot Reload Not Working

**Symptoms**: Changes not reflected in browser.

**Fix**:
1. Check browser console for errors
2. Hard refresh: `Cmd+Shift+R` (macOS) or `Ctrl+F5` (Windows)
3. Restart Vite: `Ctrl+C`, then `npm run dev`

---

### Test Failures

#### "Port already in use" in tests

**Symptoms**: Tests fail with EADDRINUSE.

**Fix**:
```bash
# Kill lingering test processes
pkill -f vitest
pkill -f "go test"

# Or use different ports for tests
```

#### Go Tests Timeout

**Symptoms**: Tests hang indefinitely.

**Cause**: Temporal/Postgres not available.

**Fix**: Ensure Docker is running:
```bash
docker compose up -d
```

#### Frontend Tests Fail

**Symptoms**: Component tests fail with rendering errors.

**Fix**:
```bash
cd frontend
npm test -- --run
```

Use `--run` for non-watch mode (CI-friendly).

---

### Git Issues

#### "Permission denied" on hooks

**Symptoms**: Can't commit.

**Fix**:
```bash
chmod +x .git/hooks/*
```

#### Large Files in History

**Symptoms**: Repo is huge, slow operations.

**Check**:
```bash
git rev-list --objects --all | git cat-file --batch-check='%(objecttype) %(objectname) %(objectsize) %(rest)' | awk '/^blob/ {print substr($0,6)}' | sort -rnk3 | head -20
```

**Note**: Coverage files and build artifacts should be in `.gitignore`.

---

## Debugging Tips

### Enable Debug Logging

**Control Plane**:
- Uses standard Go logging
- Check terminal output

**Workers**:
- Add `console.log()` in `server.js`
- Restart worker to see output

**Frontend**:
- Use browser DevTools console
- React DevTools extension helpful

### Check Event Flow

1. **Browser DevTools → Network → EventStream**: Verify SSE connection
2. **Temporal UI** (`localhost:8088`): Check workflow execution
3. **Postgres**: Query recent events
   ```bash
   docker compose exec postgres psql -U gavryn -d gavryn -c "SELECT * FROM run_events ORDER BY timestamp DESC LIMIT 10;"
   ```

### Trace Request Flow

```bash
# 1. Check API receives request
curl -v http://localhost:8080/runs

# 2. Check workflow signalled
docker compose exec temporal tctl workflow list

# 3. Check events emitted
curl http://localhost:8080/runs/{id}/events
```

### Common Log Messages

| Message | Meaning | Action |
|---------|---------|--------|
| `connection refused` | Service not running | Start service |
| `tool not allowlisted` | Security block | Add to `ALLOWED_TOOLS` |
| `worker failed to emit event` | Network issue | Check `CONTROL_PLANE_URL` |
| `LLM provider error` | API key/connection | Verify LLM settings |
| `max attempts reached` | Temporal retry exhausted | Check activity logs |

---

## Getting Help

### Information to Provide

When reporting issues, include:

1. **Error message** (exact text)
2. **Steps to reproduce**
3. **Environment**:
   - OS and version
   - Go version: `go version`
   - Node version: `node --version`
   - Docker version: `docker --version`
4. **Logs** (relevant excerpts)
5. **Recent changes** (git status, recent commits)

### Diagnostic Commands

```bash
# Run these and share output:
make smoke
docker compose ps
git status
go version
node --version
```

---

## See Also

- [Runbook](./runbook.md) - Operations guide
- [Local Development](./local-dev.md) - Setup instructions
- [Configuration](./configuration.md) - Environment variables
- [Architecture](./architecture.md) - System design
