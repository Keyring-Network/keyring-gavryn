# Overview

**Last Reviewed**: 2026-02-06  
**Source of Truth Paths**:
- Root README: `/README.md`
- This document: `/docs/overview.md`
- Architecture spec: `/docs/architecture.md`

---

## What is Gavryn Local?

Gavryn Local is a **local-first control plane** for running AI agents on your own infrastructure. Unlike cloud-based solutions, everything runs locally — your data stays on your machine, and you maintain full control over the AI workflows.

### Key Principles

1. **Privacy-First**: All data remains local; no external API calls except to your chosen LLM provider
2. **Extensible**: Plugin-style skill system for custom capabilities
3. **Observable**: Full event streaming and audit logging
4. **Developer-Friendly**: Clean APIs, comprehensive tests, and clear documentation

---

## Features

### Core Capabilities

| Feature | Description | Status |
|---------|-------------|--------|
| **Chat Interface** | Full-featured chat with streaming responses | ✅ Stable |
| **Browser Automation** | Navigate, click, type, scroll, extract, evaluate, PDF capture | ✅ Stable |
| **Document Generation** | Create PPTX, DOCX, PDF, CSV files programmatically | ✅ Stable |
| **Skills System** | Reusable AI skills with filesystem sync | ✅ Stable |
| **Context Management** | Attach files and folders to conversations | ✅ Stable |
| **Memory System** | Hybrid vector + full-text search for history | ✅ Stable |
| **Multi-Provider LLM** | OpenAI, Anthropic, OpenRouter, OpenCode Zen, Kimi, Moonshot AI | ✅ Stable |

### LLM Provider Support

| Provider | Authentication | Notes |
|----------|----------------|-------|
| **Codex** | Local CLI auth (`~/.codex/auth.json`) | Uses OpenAI Codex CLI |
| **OpenAI** | API key | Standard OpenAI API |
| **OpenRouter** | API key | Unified API for multiple models |
| **OpenCode Zen** | API key | OpenCode platform |
| **Kimi for Coding** | API key | Moonshot AI coding models |
| **Moonshot AI** | API key | General-purpose models |

---

## System Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        FRONTEND                                  │
│              Vite + React + Tailwind + shadcn/ui              │
│                         Port 5173                               │
└─────────────────────────────────────────────────────────────────┘
                              │
                              │ HTTP + SSE
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                     CONTROL PLANE (Go)                           │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────────┐  │
│  │   API       │  │  Workflows  │  │   Event Broker (SSE)   │  │
│  │  Server     │  │  (Temporal) │  │                         │  │
│  └─────────────┘  └─────────────┘  └─────────────────────────┘  │
│                         Port 8080                               │
└─────────────────────────────────────────────────────────────────┘
                              │
              ┌───────────────┼───────────────┐
              │               │               │
              ▼               ▼               ▼
┌─────────────────┐ ┌─────────────────┐ ┌─────────────────┐
│     Postgres     │ │    Temporal     │ │     WORKERS      │
│   (Data store)   │ │  (Orchestration)│ │                  │
│    Port 5432     │ │    Port 7233    │ │ ┌─────────────┐ │
│                  │ │                 │ │ │ Tool Runner │ │
│ pgvector + FTS   │ │   Port 8088     │ │ │  Port 8081  │ │
│                  │ │    (UI)         │ │ ├─────────────┤ │
│                  │ │                 │ │ │   Browser   │ │
│                  │ │                 │ │ │  Port 8082  │ │
└─────────────────┘ └─────────────────┘ └─────────────────┘
```

For detailed architecture information, see [Architecture](./architecture.md).

---

## Project Structure

```
gavryn-local/
├── control-plane/        # Go API + Temporal worker
│   ├── cmd/              # Entry points (control-plane, worker)
│   ├── internal/         # Internal packages
│   │   ├── api/          # HTTP handlers + SSE
│   │   ├── workflows/    # Temporal orchestration
│   │   ├── store/        # Storage interface
│   │   ├── llm/          # LLM provider adapters
│   │   └── ...
│   └── Makefile          # Build commands
├── frontend/             # Vite React UI
│   ├── src/              # Source code
│   │   ├── App.tsx       # Main UI component
│   │   ├── components/   # UI components (shadcn/ui)
│   │   └── lib/          # Utilities
│   └── package.json      # Dependencies
├── workers/              # Node.js workers
│   ├── browser/          # Playwright automation
│   └── tool-runner/      # Tool execution
├── infra/                # Infrastructure
│   └── migrations/       # SQL schema changes
├── scripts/              # Dev/smoke orchestration
│   ├── dev.sh            # Start everything
│   └── smoke.sh          # Health check tests
├── docs/                 # This documentation
├── docker-compose.yml    # Postgres + Temporal
├── Makefile              # Dev shortcuts
└── README.md             # Quick reference
```

---

## Source of Truth Paths

### Code Organization

| Component | Path | Description |
|-----------|------|-------------|
| API Handlers | `control-plane/internal/api/` | HTTP endpoints + SSE |
| Workflows | `control-plane/internal/workflows/` | Temporal orchestration |
| Storage | `control-plane/internal/store/` | Store interface + postgres/memory |
| UI | `frontend/src/App.tsx` | Main UI component |
| UI Components | `frontend/src/components/ui/` | shadcn/ui pattern |
| Workers | `workers/*/src/server.js` | Express servers |
| Migrations | `infra/migrations/` | psql-applied SQL |
| Dev Bootstrap | `scripts/dev.sh` | Docker + migrations + services |

### Key Files

| File | Purpose |
|------|---------|
| `README.md` | Quick start and feature overview |
| `CONTRIBUTING.md` | Contribution guidelines |
| `LICENSE` | MIT License |
| `.env.example` | Environment variable template |
| `docker-compose.yml` | Infrastructure services |
| `Makefile` | Common dev commands |

---

## Quickstart

### Prerequisites

- Go 1.22+
- Node 18+
- Docker (for Postgres + Temporal)

### 1. Start Dependencies

```bash
cp .env.example .env
docker compose up -d
```

### 2. Run Everything (Recommended)

```bash
make dev
```

This starts all services with auto port selection and proper environment setup.

### 3. Access the UI

Open your browser to the Vite URL shown in the output (default `http://localhost:5173`).

### 4. Configure LLM

Use the built-in setup wizard (Settings → Run Setup Wizard) to configure your LLM provider.

**Required**: Set `LLM_SECRETS_KEY` in `.env` before saving settings:

```bash
openssl rand -base64 32
```

For detailed setup instructions, see [Local Development](./local-dev.md).

---

## Development Philosophy

### Design Principles

1. **Local-First**: All data stays on your machine by default
2. **Minimal Dependencies**: Only what's necessary; no bloat
3. **Clear Boundaries**: Control plane, workers, and frontend are distinct
4. **Event-Driven**: SSE for real-time updates; workflows for async operations
5. **Test Coverage**: All components target 100% test coverage

### Code Conventions

- **Go**: `cmd/` entry points, `internal/` packages, pgx for Postgres, Temporal SDK
- **Frontend**: Vite + TS strict, alias `@/*` → `src/*`, Tailwind + shadcn/ui
- **Workers**: Plain JS (CommonJS), single-file Express servers
- **Tests**: Go `*_test.go`, frontend `*.test.ts` (Vitest), `e2e/*.spec.ts` (Playwright)

---

## Roadmap

Current focus areas:

- ✅ **Stability**: Reliable dev shutdown, task queue isolation, connection resilience
- ✅ **UX**: Collapsible sidebar, cohesive chat layout, setup wizard improvements
- 🔄 **Future**: Additional LLM providers, enhanced memory features, skill marketplace

See [Architecture](./architecture.md#recent-improvements) for recent technical improvements.

---

## Support

- **Issues**: Open a GitHub issue for bugs or feature requests
- **Documentation**: Check [Troubleshooting](./troubleshooting.md) for common problems
- **Contributing**: See [CONTRIBUTING.md](../CONTRIBUTING.md)

---

## License

Licensed under the **MIT License**. See [LICENSE](../LICENSE) for full text.

Contributions are welcome — please read [CONTRIBUTING.md](../CONTRIBUTING.md) first.
