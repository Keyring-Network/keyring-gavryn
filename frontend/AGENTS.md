# FRONTEND KNOWLEDGE BASE

**Generated:** 2026-01-28 22:10 UTC
**Commit:** 499dc70
**Branch:** main

## OVERVIEW
Vite + React + TypeScript frontend with Tailwind and shadcn/ui components.

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| App shell | frontend/src/App.tsx | Main UI and state |
| Entry point | frontend/src/main.tsx | React root |
| UI primitives | frontend/src/components/ui | shadcn/ui style |
| Shared utilities | frontend/src/lib | `cn` + event helpers |
| E2E tests | frontend/e2e | Playwright specs |
| Unit tests | frontend/src/**/*.test.ts | Vitest |

## CONVENTIONS
- TypeScript strict mode with ES2020 target.
- Path alias `@/*` maps to `src/*` (tsconfig + Vite).
- Tailwind CSS with HSL variables; dark mode via `class`.
- Unit tests use Vitest in `vite.config.ts`; E2E uses Playwright.

## ANTI-PATTERNS
- None documented in-code.

## NOTES
- `npm run dev` starts Vite on port 5173.
