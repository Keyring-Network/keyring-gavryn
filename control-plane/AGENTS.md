# CONTROL-PLANE KNOWLEDGE BASE

**Generated:** 2026-01-28 22:10 UTC
**Commit:** 499dc70
**Branch:** main

## OVERVIEW
Go control plane providing HTTP API, SSE streaming, Temporal workflows, and storage implementations.

## STRUCTURE
```
control-plane/
├── cmd/
│   ├── control-plane/   # API server entry
│   └── worker/          # Temporal worker entry
└── internal/
    ├── api/             # HTTP handlers + SSE
    ├── config/          # Env config + defaults
    ├── events/          # SSE broker
    ├── llm/             # Provider adapters
    ├── secrets/         # Secret storage
    ├── skills/          # Skill materialization
    ├── store/           # Interface + implementations
    └── workflows/       # Temporal workflows
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| API routes | control-plane/internal/api | Chi router + handlers |
| SSE events | control-plane/internal/events | RunEvent broker |
| Store interface | control-plane/internal/store/store.go | Cross-impl contract |
| Postgres store | control-plane/internal/store/postgres | pgx queries |
| Memory store | control-plane/internal/store/memory | dev/test impl |
| Workflows | control-plane/internal/workflows | Temporal orchestration |
| LLM providers | control-plane/internal/llm | Codex/OpenAI/OpenRouter |

## CONVENTIONS
- Entry points live under `cmd/` and are started with `go run ./cmd/<name>`.
- All internal packages take `context.Context` and return typed errors.
- Store implementations conform to a single interface in `internal/store`.
- Temporal workflow functions use `workflow.Context` and explicit timeouts.

## ANTI-PATTERNS
- None documented in-code.

## NOTES
- `go.mod` sets Go 1.25.5 and uses pgx + Temporal SDK.
