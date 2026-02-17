# Gavryn Tool Contracts

Use this skill when generating tool calls. It provides canonical tool names and payload keys accepted by Gavryn's workers.

## Canonical Browser Tools

Supported `tool_name` values:

- `browser.navigate`
- `browser.snapshot`
- `browser.click`
- `browser.type`
- `browser.scroll`
- `browser.extract`
- `browser.evaluate`
- `browser.pdf`

Use these names exactly. Do not emit `browser.extract_text` as the primary tool name.

## Canonical Editor Tools

Supported `tool_name` values:

- `editor.list`
- `editor.read`
- `editor.write`
- `editor.delete`
- `editor.stat`

All editor calls should use `input.path` for file/directory targets.

Do not use:

- `input.file_path`
- `input.filepath`

## Process Tool

- `process.exec`

Required payload keys:

- `input.command` string
- Optional `input.args` string[]
- Optional `input.cwd` (workspace-relative path)
- Optional `timeout_ms` number

Note: command execution is allowlisted by runtime config. If `command not allowlisted` is returned, switch to a read-only plan or ask the user to run it locally.

## Valid Tool Block Shape

```tool
{"tool_calls":[{"tool_name":"editor.write","input":{"path":"README.md","content":"# Project\n"}}]}
```

## Browser Payload Examples

```tool
{"tool_calls":[
  {"tool_name":"browser.navigate","input":{"url":"https://example.com"}},
  {"tool_name":"browser.extract","input":{"selector":"h1","attribute":"textContent"}},
  {"tool_name":"browser.snapshot","input":{}}
]}
```

## Editor Payload Examples

```tool
{"tool_calls":[
  {"tool_name":"editor.list","input":{"path":"."}},
  {"tool_name":"editor.read","input":{"path":"package.json"}},
  {"tool_name":"editor.write","input":{"path":"src/main.ts","content":"console.log('hi')\n"}},
  {"tool_name":"editor.stat","input":{"path":"src/main.ts"}},
  {"tool_name":"editor.delete","input":{"path":"tmp.txt"}}
]}
```
