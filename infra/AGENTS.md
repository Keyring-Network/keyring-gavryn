# INFRA KNOWLEDGE BASE

**Generated:** 2026-01-28 22:10 UTC
**Commit:** 499dc70
**Branch:** main

## OVERVIEW
SQL migrations applied directly via `psql` from dev scripts.

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Schema migrations | infra/migrations | Ordered SQL files |
| Memory tables | infra/migrations/005_memory.sql | pgvector + FTS |

## CONVENTIONS
- Migrations are plain SQL files executed in filename order.
- Extensions are created in migrations (pgcrypto, vector).

## ANTI-PATTERNS
- None documented in-code.

## NOTES
- `scripts/dev.sh` runs all migrations inside the Postgres container.
