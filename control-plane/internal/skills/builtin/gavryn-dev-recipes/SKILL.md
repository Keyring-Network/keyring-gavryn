# Gavryn Dev Recipes

Use this skill for common local workflows: scaffolding, dev server startup, and preview.

## Project Scaffolding Recipe

1. Create files with `editor.write` (always use `input.path`).
2. Verify structure with `editor.list` and `editor.stat`.
3. If allowlisted, run install/build with `process.exec`.

Example tool calls:

```tool
{"tool_calls":[
  {"tool_name":"editor.write","input":{"path":"frontend/src/pages/landing.tsx","content":"export default function Landing(){return <main>Landing</main>}\n"}},
  {"tool_name":"editor.list","input":{"path":"frontend/src/pages"}}
]}
```

## Start Full Stack Dev Environment

Primary repo recipe:

```bash
make dev
```

Health/smoke:

```bash
make smoke
```

Docker-only dependencies:

```bash
make up
make down
```

## Service-by-Service Recipe

Control plane:

```bash
cd control-plane
go run ./cmd/control-plane
```

Worker:

```bash
cd control-plane
go run ./cmd/worker
```

Browser worker:

```bash
cd workers/browser
npm install
npm run dev
```

Tool runner:

```bash
cd workers/tool-runner
npm install
npm run dev
```

Frontend:

```bash
cd frontend
npm install
npm run dev
```

Preview URL is usually `http://localhost:5173` unless Vite selects a different free port.

## Tool-Based Preview Pattern

If `process.exec` allows `npm`:

```tool
{"tool_calls":[
  {"tool_name":"process.exec","input":{"command":"npm","args":["run","dev"],"cwd":"frontend"}}
]}
```

Then parse stdout/stderr for `http://localhost:<port>` and return that URL.
