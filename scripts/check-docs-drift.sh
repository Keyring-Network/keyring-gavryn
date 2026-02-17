#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ROUTER_FILE="$ROOT_DIR/control-plane/internal/api/server.go"
DOC_FILE="$ROOT_DIR/docs/api-reference.md"

if [ ! -f "$ROUTER_FILE" ]; then
  echo "missing router file: $ROUTER_FILE" >&2
  exit 1
fi
if [ ! -f "$DOC_FILE" ]; then
  echo "missing docs file: $DOC_FILE" >&2
  exit 1
fi

routes_tmp="$(mktemp)"
docs_tmp="$(mktemp)"
missing_tmp="$(mktemp)"
extra_tmp="$(mktemp)"
trap 'rm -f "$routes_tmp" "$docs_tmp" "$missing_tmp" "$extra_tmp"' EXIT

perl -ne 'if(/^\s*r\.(Get|Post|Put|Delete)\("([^"]+)"/){print uc($1)." $2\n"}' "$ROUTER_FILE" \
  | sort -u > "$routes_tmp"

perl -ne '
  if (/<!-- CONTROL_PLANE_ROUTES_START -->/) { $in = 1; next; }
  if (/<!-- CONTROL_PLANE_ROUTES_END -->/) { $in = 0; next; }
  if ($in && /^\s*(GET|POST|PUT|DELETE)\s+([^\s`]+)/) {
    $route = $2;
    $route =~ s/\?.*$//;
    print uc($1)." $route\n";
  }
' "$DOC_FILE" | sort -u > "$docs_tmp"

comm -23 "$routes_tmp" "$docs_tmp" > "$missing_tmp"
comm -13 "$routes_tmp" "$docs_tmp" > "$extra_tmp"

if [ -s "$missing_tmp" ] || [ -s "$extra_tmp" ]; then
  echo "API docs drift detected." >&2
  if [ -s "$missing_tmp" ]; then
    echo >&2
    echo "Missing from docs/api-reference.md:" >&2
    cat "$missing_tmp" >&2
  fi
  if [ -s "$extra_tmp" ]; then
    echo >&2
    echo "Documented but not registered in router:" >&2
    cat "$extra_tmp" >&2
  fi
  exit 1
fi

echo "API docs are in sync with router endpoints."
