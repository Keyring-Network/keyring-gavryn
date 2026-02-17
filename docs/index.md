# Gavryn Local Documentation

Welcome to the comprehensive documentation for **Gavryn Local** — a local-first control plane for running AI agents on your own infrastructure.

---

## Documentation Index

| Document | Purpose | Audience |
|----------|---------|----------|
| [Overview](./overview.md) | Project introduction, features, and goals | Everyone |
| [Architecture](./architecture.md) | System components, data flow, and tech stack | Developers, DevOps |
| [Local Development](./local-dev.md) | Setup, build, test, and development workflow | Contributors |
| [Runbook](./runbook.md) | Common operations and maintenance tasks | Operators |
| [API Reference](./api-reference.md) | HTTP endpoints, SSE events, and service contracts | Integrators |
| [Workflows & Events](./workflows.md) | Temporal workflows, activities, and event system | Backend developers |
| [Configuration](./configuration.md) | Environment variables and service configuration | DevOps, Users |
| [Built-in Skills](./skills-builtins.md) | Bootstrapped skill docs and canonical tool schemas | Developers, Contributors |
| [Troubleshooting](./troubleshooting.md) | Common issues and resolution steps | Everyone |
| [Data Model](./data-model.md) | Database schema and storage patterns | Backend developers |
| [Program Checklist](../PROGRAM_CHECKLIST.md) | Enterprise completion checklist and evidence tracker | Core engineering |

---

## Quick Links

### Getting Started
- [Prerequisites](./local-dev.md#prerequisites)
- [Quickstart Guide](./local-dev.md#quickstart)
- [Development Commands](./local-dev.md#development-commands)

### Reference
- [Environment Variables](./configuration.md#environment-variables)
- [API Endpoints](./api-reference.md#rest-api)
- [Database Schema](./data-model.md#schema-overview)
- [Supported LLM Providers](./configuration.md#llm-providers)

### Operations
- [Troubleshooting Common Issues](./troubleshooting.md)
- [Service Health Checks](./runbook.md#health-checks)
- [Log Locations](./runbook.md#logging)

---

## Documentation Maintenance

**Last Reviewed**: 2026-02-06  
**Maintained By**: Project contributors via PR review  
**Update Cadence**: Reviewed when features change; critical fixes within 48 hours

### Contributing to Docs

When modifying code that affects documented behavior:

1. Update relevant documentation pages
2. Include "last reviewed" date updates
3. Verify all links remain valid
4. Follow the [source-of-truth guidelines](./overview.md#source-of-truth-paths)

See [CONTRIBUTING.md](../CONTRIBUTING.md) for full contribution guidelines.

---

## Project Overview

Gavryn Local provides:

- **Chat Interface**: Full-featured chat with streaming responses
- **Browser Automation**: Navigate, interact, and extract data from web pages
- **Document Generation**: Create PowerPoint, Word, PDF, and CSV files
- **Skills System**: Manage reusable AI skills with filesystem sync
- **Context Management**: Attach files and folders to conversations
- **Memory**: Hybrid vector + full-text search for conversation history
- **Multi-Provider LLM**: Support for multiple AI providers

### Tech Stack

- **Control Plane**: Go + Temporal + Postgres
- **Streaming**: SSE for RunEvents
- **Workers**: Tool runner + Playwright browser worker
- **Frontend**: Vite + Tailwind + shadcn/ui + Lucide

---

## License

Licensed under the **MIT License**. See [LICENSE](../LICENSE).

---

*For the latest updates, visit the project repository or check the commit history in individual documentation pages.*
