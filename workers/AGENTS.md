# WORKERS KNOWLEDGE BASE

**Generated:** 2026-01-28 22:10 UTC
**Commit:** 499dc70
**Branch:** main

## OVERVIEW
Node workers providing tool execution and browser automation over HTTP.

## STRUCTURE
```
workers/
├── browser/          # Playwright worker
├── tool-runner/      # Tool execution worker
└── contracts/        # JSON API contracts
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Browser worker | workers/browser/src/server.js | Playwright + artifacts |
| Tool runner | workers/tool-runner/src/server.js | Allowlisted tools |
| API contracts | workers/contracts | Request/response schemas |

## CONVENTIONS
- Plain JavaScript (CommonJS), minimal dependencies.
- Express servers started via `npm run dev`.
- Ports and URLs come from environment variables with defaults.

## ANTI-PATTERNS
- None documented in-code.

## NOTES
- Browser worker handles `browser.navigate` and `browser.snapshot`.
