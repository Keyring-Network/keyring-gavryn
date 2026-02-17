# Contributing

Thanks for helping improve Gavryn Local! This guide keeps contributions consistent and easy to review.

## Setup

- Install prerequisites (Go, Node, Docker) listed in the [README](./README.md).
- Create your env file: `cp .env.example .env`.
- Start the stack: `make dev`.
- Read the [Local Development Guide](./docs/local-dev.md) for detailed setup.

## Branch naming

Use the pattern: `type/short-description`.

Examples: `feature/llm-providers`, `fix/settings-save`, `chore/update-docs`.

## Tests

Run smoke tests and the relevant component test suites before opening a PR:

```bash
make smoke

cd control-plane && make test
cd frontend && npm test -- --run
cd workers/browser && npm test
cd workers/tool-runner && npm test
```

## Documentation

When making changes, update relevant documentation:

- **Code changes**: Update docs in `docs/` if behavior changes
- **API changes**: Update [API Reference](./docs/api-reference.md)
- **New features**: Add to [Overview](./docs/overview.md) and relevant guide
- **Bug fixes**: Check if [Troubleshooting](./docs/troubleshooting.md) needs updating

### Documentation Maintenance

Each docs page includes:
- **Last Reviewed** date (update when changing)
- **Source of Truth Paths** (list key implementation files)

See [Documentation Index](./docs/index.md) for the full documentation map.

## PR checklist

- [ ] `make smoke` passes
- [ ] Relevant component tests pass
- [ ] Documentation updated if behavior changed (see [docs/](./docs/))
- [ ] No secrets or `.env` files committed
- [ ] Documentation "last reviewed" dates updated if applicable

## Getting Help

- Check [Troubleshooting](./docs/troubleshooting.md) for common issues
- Review [Architecture](./docs/architecture.md) to understand the system
- See [Runbook](./docs/runbook.md) for operational guidance
