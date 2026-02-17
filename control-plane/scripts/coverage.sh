#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

COVER_PROFILE="coverage.out"

go test ./... -parallel=2 -covermode=atomic -coverprofile="$COVER_PROFILE" -coverpkg=./...

package_list="$(go list ./...)"
package_file="$(mktemp)"
trap 'rm -f "$package_file"' EXIT
printf "%s\n" "$package_list" > "$package_file"

cover_func_output="$(go tool cover -func="$COVER_PROFILE")"

echo ""
echo "Coverage per package:"

package_summary="$(
  awk -v pkg_file="$package_file" '
    BEGIN {
      while ((getline line < pkg_file) > 0) {
        if (line != "") packages[line] = 1
      }
      close(pkg_file)
    }
    /^mode:/ { next }
    {
      split($1, loc, ":")
      file = loc[1]
      sub(/\/[^\/]+$/, "", file)
      pkg = file
      stmts = $2
      count = $3
      total[pkg] += stmts
      if (count > 0) covered[pkg] += stmts
      packages[pkg] = 1
    }
    END {
      for (pkg in packages) {
        pct = 0
        if (total[pkg] > 0) pct = covered[pkg] * 100 / total[pkg]
        printf "%s %.1f%%\n", pkg, pct
      }
    }
  ' "$COVER_PROFILE"
)"

printf "%s\n" "$package_summary" | sort

echo ""
echo "Total coverage:"
printf "%s\n" "$cover_func_output" | awk 'END { print }'

failed=0
while read -r pkg pct; do
  if [ -z "$pkg" ]; then
    continue
  fi
  pct_num="${pct%%%}"
  if awk -v pct="$pct_num" 'BEGIN { exit (pct < 100) ? 1 : 0 }'; then
    continue
  fi
  failed=1
done <<< "$package_summary"

if [ "$failed" -ne 0 ]; then
  echo ""
  echo "Coverage check failed: at least one package below 100.0%"
  exit 1
fi
