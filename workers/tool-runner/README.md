# Tool Runner Worker

## Purpose

Executes tool invocations and reports RunEvents back to the control plane. Supports:
- **Document generation**: PowerPoint, Word, PDF, CSV
- **Browser delegation**: Routes browser tools to the Playwright worker

## Available Tools

### Document Generation

#### `document.create_pptx`
Create PowerPoint presentations using pptxgenjs.

```json
{
  "filename": "presentation.pptx",
  "slides": [
    {
      "title": "Slide 1",
      "content": "Bullet point 1\nBullet point 2"
    }
  ]
}
```

#### `document.create_docx`
Create Word documents using the docx library.

```json
{
  "filename": "document.docx",
  "content": "Document content here...",
  "sections": [
    { "heading": "Section 1", "content": "..." }
  ]
}
```

#### `document.create_pdf`
Create PDF documents using pdf-lib.

```json
{
  "filename": "report.pdf",
  "content": "PDF content...",
  "pages": [
    { "text": "Page 1 content" }
  ]
}
```

#### `document.create_csv`
Create CSV files using papaparse.

```json
{
  "filename": "data.csv",
  "headers": ["Name", "Email", "Score"],
  "rows": [
    ["John", "john@example.com", 95],
    ["Jane", "jane@example.com", 87]
  ]
}
```

### Browser Delegation

Browser tools are forwarded to the browser worker at `BROWSER_WORKER_URL`:
- `browser.navigate`, `browser.snapshot`
- `browser.click`, `browser.type`, `browser.scroll`
- `browser.extract`, `browser.evaluate`, `browser.pdf`

## Transport
HTTP + JSON (initial). Easy to run locally and proxy. Future gRPC swap is possible without changing payloads.

## Endpoints

### POST /tools/execute
Run a tool invocation.

Request:
```json
{
  "run_id": "uuid",
  "invocation_id": "uuid",
  "tool_name": "browser.navigate",
  "input": { "url": "https://x.com" },
  "timeout_ms": 60000
}
```

Response:
```json
{
  "status": "completed",
  "output": { "result": "..." },
  "artifacts": [
    { "artifact_id": "uuid", "type": "screenshot", "uri": "..." }
  ]
}
```

### GET /health
Returns worker status.

## Event Reporting
Workers emit RunEvents to control plane via:
- `POST /runs/{id}/events`

## Allowlist
Tool execution must be allowlisted in configuration. `process.exec` only runs commands from `PROCESS_ALLOWLIST`.

## Environment

- `TOOL_RUNNER_PORT` (default `8081`)
- `CONTROL_PLANE_URL` (default `http://localhost:8080`)
- `BROWSER_WORKER_URL` (default `http://localhost:8082`)
- `ALLOWED_TOOLS` - Comma-separated list of enabled tools
- `PROCESS_ALLOWLIST` - Comma-separated commands allowed by `process.exec`

Default `ALLOWED_TOOLS` includes browser, document, editor, and process tools:
```
browser.navigate,browser.snapshot,browser.click,browser.type,browser.scroll,browser.extract,browser.evaluate,browser.pdf,document.create_pptx,document.create_docx,document.create_pdf,document.create_csv,editor.list,editor.read,editor.write,editor.delete,editor.stat,process.exec
```

Default `PROCESS_ALLOWLIST` is:
```
echo,ls,pwd,whoami,cat,cp,mv,rm,mkdir,touch,find,grep,sed,awk,git,node,npm,npx,pnpm,yarn,bun,go,python,python3,pip,pip3,uv,sh,bash
```

## Run

```sh
npm install
npm run dev
```
