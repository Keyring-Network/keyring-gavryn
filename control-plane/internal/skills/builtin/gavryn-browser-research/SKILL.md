# Gavryn Browser Research

Use this skill for web research tasks that must complete with supported browser tools.

## Reliable Research Flow

1. `browser.navigate` to a direct target URL.
2. `browser.snapshot` for visual checkpoint.
3. `browser.extract` for structured text.
4. Optional `browser.scroll` and repeat extract on additional sections.
5. Return concise findings with source URLs.

Prefer known pages over search-engine homepages to avoid captcha/rate-limit dead ends.

## Supported Browser Inputs

- `browser.navigate`: `input.url`
- `browser.click`: `input.selector`
- `browser.type`: `input.selector`, `input.text`, optional `input.clear`
- `browser.scroll`: `input.direction` (`up|down|top|bottom`), optional `input.amount`
- `browser.extract`: optional `input.selector`, optional `input.attribute`
- `browser.evaluate`: `input.script`
- `browser.pdf`: optional `input.filename`, optional `input.format` (`A4|Letter`)

## Example: Multi-Source News Check

```tool
{"tool_calls":[
  {"tool_name":"browser.navigate","input":{"url":"https://news.ycombinator.com"}},
  {"tool_name":"browser.extract","input":{"selector":".titleline > a","attribute":"textContent"}},
  {"tool_name":"browser.navigate","input":{"url":"https://techcrunch.com"}},
  {"tool_name":"browser.extract","input":{"selector":"article h2 a","attribute":"textContent"}}
]}
```

## Anti-Patterns To Avoid

- Using unsupported `browser.extract_text` as the canonical tool name.
- Returning raw tool JSON as final user-facing output.
- Claiming browser execution succeeded without checking tool response status.
