# PROJECT KNOWLEDGE BASE

**Generated:** 2026-01-28 22:10 UTC
**Commit:** 499dc70
**Branch:** main

## OVERVIEW
Gavryn is a local-first control plane with Go + Temporal backend, Node workers, and a Vite React frontend.

## STRUCTURE
```
./
├── control-plane/        # Go API + Temporal worker
├── frontend/             # Vite React UI
├── workers/              # Browser + tool-runner services
├── infra/migrations/     # SQL schema changes
├── scripts/              # dev/smoke orchestration
├── docker-compose.yml    # Postgres + Temporal
└── Makefile              # dev shortcuts
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| API handlers | control-plane/internal/api | HTTP endpoints + SSE |
| Workflows | control-plane/internal/workflows | Temporal orchestration |
| Storage | control-plane/internal/store | Store interface + postgres/memory |
| UI | frontend/src/App.tsx | Main UI component |
| UI components | frontend/src/components/ui | shadcn/ui pattern |
| Workers | workers/*/src/server.js | Express servers |
| Migrations | infra/migrations | psql-applied SQL |
| Dev bootstrap | scripts/dev.sh | Docker + migrations + services |

## CONVENTIONS
- Go: `cmd/` entry points, `internal/` packages, pgx for Postgres, Temporal SDK.
- Frontend: Vite + TS strict, alias `@/*` -> `src/*`, Tailwind + shadcn/ui.
- Workers: plain JS (CommonJS), single-file Express servers.
- Tests: Go `*_test.go`, frontend `*.test.ts` (Vitest), `e2e/*.spec.ts` (Playwright).

## ANTI-PATTERNS (THIS PROJECT)
- None documented in-code.

## UNIQUE STYLES
- `scripts/dev.sh` bootstraps DB roles and runs migrations inside Docker.
- Auto port selection when defaults are busy.
- No monorepo workspace at root; each service is standalone.

## COMMANDS
```bash
make dev
make smoke
make up
make down
```

## NOTES
- Postgres must have pgvector installed for memory features (`CREATE EXTENSION vector`).
- Temporal namespace is auto-created by scripts.
