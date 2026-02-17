#!/usr/bin/env bash
set -euo pipefail

CONTROL_PLANE_URL="${CONTROL_PLANE_URL:-http://localhost:8080}"
WAIT_FOR_COMPLETION="${WAIT_FOR_COMPLETION:-true}"
if [[ -n "${WAIT_TIMEOUT_SECONDS:-}" && -z "${AUTOMATION_TIMEOUT_MS:-}" ]]; then
  AUTOMATION_TIMEOUT_MS="$((WAIT_TIMEOUT_SECONDS * 1000))"
else
  AUTOMATION_TIMEOUT_MS="${AUTOMATION_TIMEOUT_MS:-180000}"
fi
AUTOMATION_POLL_INTERVAL_MS="${AUTOMATION_POLL_INTERVAL_MS:-1200}"

if [[ $# -gt 0 ]]; then
  PROMPT="$*"
else
  PROMPT="$(cat)"
fi

if [[ -z "${PROMPT// }" ]]; then
  echo "Usage: scripts/run-automation.sh \"<prompt>\"" >&2
  echo "You can also pipe a prompt via stdin." >&2
  exit 1
fi

PAYLOAD="$(python3 - "$PROMPT" "$WAIT_FOR_COMPLETION" "$AUTOMATION_TIMEOUT_MS" "$AUTOMATION_POLL_INTERVAL_MS" "${MODEL_ROUTE:-}" "${BROWSER_MODE:-}" "${BROWSER_INTERACTION:-}" "${BROWSER_DOMAIN_ALLOWLIST:-}" "${BROWSER_PREFERRED_BROWSER:-}" "${BROWSER_USER_AGENT:-}" <<'PY'
import json
import sys

prompt, wait_for_completion, timeout_ms, poll_interval_ms, model_route, browser_mode, browser_interaction, browser_domain_allowlist, browser_preferred_browser, browser_user_agent = sys.argv[1:]

def parse_bool(raw: str) -> bool:
    return str(raw or "").strip().lower() in {"1", "true", "yes", "y", "on"}

metadata = {}
if model_route.strip():
    metadata["model_route"] = model_route.strip()
if browser_mode.strip():
    metadata["browser_mode"] = browser_mode.strip()
if browser_interaction.strip():
    metadata["browser_interaction"] = browser_interaction.strip()

payload = {
    "prompt": prompt,
    "wait_for_completion": parse_bool(wait_for_completion),
    "timeout_ms": int(timeout_ms),
    "poll_interval_ms": int(poll_interval_ms),
}
if metadata:
    payload["metadata"] = metadata

if browser_preferred_browser.strip():
    payload["browser_preferred_browser"] = browser_preferred_browser.strip()

if browser_user_agent.strip():
    payload["browser_user_agent"] = browser_user_agent.strip()

allowlist = [item.strip() for item in str(browser_domain_allowlist or "").split(",") if item.strip()]
if allowlist:
    payload["browser_domain_allowlist"] = allowlist

print(json.dumps(payload))
PY
)"

RESPONSE="$(curl -fsS \
  -H "Content-Type: application/json" \
  -X POST \
  "${CONTROL_PLANE_URL%/}/automation/execute" \
  -d "$PAYLOAD")"

python3 - "$RESPONSE" <<'PY'
import json
import sys

response = json.loads(sys.argv[1])
status = str(response.get("status", "")).strip() or "unknown"
phase = str(response.get("phase", "")).strip()
run_id = str(response.get("run_id", "")).strip() or "unknown"
final_response = str(response.get("final_response", "")).strip()
diagnostics = response.get("diagnostics") or {}
sources = diagnostics.get("sources") or []

usable = diagnostics.get("usable_sources", 0)
low_quality = diagnostics.get("low_quality", 0)
blocked = diagnostics.get("blocked_sources", 0)
terminal_reason = diagnostics.get("terminal_reason", "")

print(f"Run: {run_id}")
print(f"Status: {status}{f' ({phase})' if phase else ''}")
if terminal_reason:
    print(f"Terminal reason: {terminal_reason}")
print(f"Diagnostics: usable={usable}, low_quality={low_quality}, blocked={blocked}")

if sources:
    print("\nPer-source diagnostics:")
    for index, source in enumerate(sources, 1):
        url = str(source.get("url", "")).strip()
        title = str(source.get("title", "")).strip() or "Source"
        reason = str(source.get("reason_code", "")).strip() or "ok"
        detail = str(source.get("reason_detail", "")).strip()
        print(f"{index}. {title} | {url}")
        if detail:
            print(f"   reason={reason} detail={detail}")
        else:
            print(f"   reason={reason}")

print("\nFinal response:\n")
print(final_response or "(empty)")

if status not in {"completed", "partial"}:
    sys.exit(1)
PY
