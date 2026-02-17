# Built-in Skills

## Purpose

Gavryn ships built-in `SKILL.md` files so the model has an out-of-the-box reference for:

- valid tool names
- valid payload keys
- common command recipes used in this repository

This reduces malformed tool calls such as `browser.extract_text` and editor payloads using `file_path` instead of `path`.

## Seeded Built-ins

On control-plane startup, Gavryn seeds these skills if they do not already exist:

- `gavryn-tool-contracts`
- `gavryn-dev-recipes`
- `gavryn-browser-research`

Source files live in-repo under:

- `control-plane/internal/skills/builtin/gavryn-tool-contracts/SKILL.md`
- `control-plane/internal/skills/builtin/gavryn-dev-recipes/SKILL.md`
- `control-plane/internal/skills/builtin/gavryn-browser-research/SKILL.md`

## Runtime Surfacing Flow

1. Control plane starts.
2. Built-ins are seeded into the skills tables when missing.
3. Skills are materialized to `~/.config/opencode/skills/<skill-name>/SKILL.md`.
4. Runtime skill discovery reads from the skills directory as normal.

This preserves compatibility with user-managed skills because existing skill names are not overwritten.

## Canonical Tool Contracts

### Browser Tool Names

- `browser.navigate`
- `browser.snapshot`
- `browser.click`
- `browser.type`
- `browser.scroll`
- `browser.extract`
- `browser.evaluate`
- `browser.pdf`

### Editor Tool Names

- `editor.list`
- `editor.read`
- `editor.write`
- `editor.delete`
- `editor.stat`

Use `input.path` for editor file/directory targets.

### Process Tool Name

- `process.exec`

Required field: `input.command`.

## Command Recipes (from README/dev flow)

```bash
make dev
make smoke
make up
make down
```

Service-specific startup:

```bash
cd control-plane && go run ./cmd/control-plane
cd control-plane && go run ./cmd/worker
cd workers/browser && npm run dev
cd workers/tool-runner && npm run dev
cd frontend && npm run dev
```
