#!/bin/bash
set -e

echo "=== Gavryn Coverage Runner ==="
echo ""

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

FAILED=0

# Function to run coverage and check result
run_coverage() {
    local name=$1
    local dir=$2
    local cmd=$3

    echo "Running $name coverage..."
    cd "$dir"

    if eval "$cmd"; then
        echo -e "${GREEN}✓ $name coverage passed${NC}"
    else
        echo -e "${RED}✗ $name coverage failed${NC}"
        FAILED=1
    fi
    echo ""
}

# Get the root directory
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# 1. Control Plane (Go)
cd "$ROOT_DIR"
run_coverage "Control Plane" "./control-plane" \
    "go test ./... -parallel=2 -covermode=atomic -coverprofile=coverage.out -coverpkg=./... && go tool cover -func=coverage.out | grep total | awk '{if (\$3+0 < 100) exit 1}'"

# 2. Frontend
cd "$ROOT_DIR"
run_coverage "Frontend" "./frontend" \
    "npm run test:coverage"

# 3. Browser Worker
cd "$ROOT_DIR"
run_coverage "Browser Worker" "./workers/browser" \
    "npm run test:coverage"

# 4. Tool Runner Worker
cd "$ROOT_DIR"
run_coverage "Tool Runner Worker" "./workers/tool-runner" \
    "npm run test:coverage"

# Summary
echo "=== Coverage Summary ==="
if [ $FAILED -eq 0 ]; then
    echo -e "${GREEN}All coverage checks passed!${NC}"
    exit 0
else
    echo -e "${RED}Some coverage checks failed!${NC}"
    exit 1
fi
