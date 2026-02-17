#!/usr/bin/env bash
set -euo pipefail
set -m

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [ -f "$ROOT_DIR/.env" ]; then
  set -a
  # shellcheck source=/dev/null
  . "$ROOT_DIR/.env"
  set +a
fi

POSTGRES_USER="${POSTGRES_USER:-gavryn}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-gavryn}"
POSTGRES_DB="${POSTGRES_DB:-gavryn}"
POSTGRES_HOST="${POSTGRES_HOST:-localhost}"
POSTGRES_PORT="${POSTGRES_PORT:-5432}"
CONTROL_PLANE_PORT="${CONTROL_PLANE_PORT:-8080}"
TOOL_RUNNER_PORT="${TOOL_RUNNER_PORT:-8081}"
BROWSER_WORKER_PORT="${BROWSER_WORKER_PORT:-8082}"
BROWSER_USER_TAB_CDP_URL="${BROWSER_USER_TAB_CDP_URL:-${BROWSER_CDP_URL:-}}"
TEMPORAL_ADDRESS="${TEMPORAL_ADDRESS:-localhost:7233}"
TEMPORAL_UI_PORT="${TEMPORAL_UI_PORT:-8088}"

export POSTGRES_USER POSTGRES_PASSWORD POSTGRES_DB POSTGRES_HOST POSTGRES_PORT

if [ "$POSTGRES_HOST" = "postgres" ]; then
  POSTGRES_HOST="localhost"
fi

if [ "$TEMPORAL_ADDRESS" = "temporal:7233" ]; then
  TEMPORAL_ADDRESS="localhost:7233"
fi

USE_ENV_POSTGRES_URL="${USE_ENV_POSTGRES_URL:-}"

export TEMPORAL_ADDRESS

used_ports=""

port_reserved() {
  local port="$1"
  for reserved in $used_ports; do
    if [ "$reserved" = "$port" ]; then
      return 0
    fi
  done
  return 1
}

reserve_port() {
  used_ports="$used_ports $1"
}

port_in_use() {
  local port="$1"
  if command -v lsof >/dev/null 2>&1; then
    lsof -iTCP:"$port" -sTCP:LISTEN -n -P >/dev/null 2>&1
    return $?
  fi
  if command -v nc >/dev/null 2>&1; then
    nc -z 127.0.0.1 "$port" >/dev/null 2>&1
    return $?
  fi
  return 1
}

choose_port() {
  local var="$1"
  local label="$2"
  local port="${!var}"
  local base="$port"
  for _ in $(seq 1 50); do
    if port_reserved "$port"; then
      port=$((port + 1))
      continue
    fi
    if ! port_in_use "$port"; then
      if [ "$port" != "$base" ]; then
        echo "${label} port ${base} in use, switching to ${port}" >&2
      fi
      reserve_port "$port"
      printf -v "$var" "%s" "$port"
      return
    fi
    port=$((port + 1))
  done
  echo "${label} port unavailable starting at ${base}" >&2
  exit 1
}

reserve_port "$TEMPORAL_UI_PORT"
choose_port POSTGRES_PORT "Postgres"

wait_for_port() {
  local port="$1"
  local label="$2"
  for _ in $(seq 1 30); do
    if port_in_use "$port"; then
      return
    fi
    sleep 1
  done
  echo "${label} did not start on port ${port}" >&2
  return 1
}

ensure_temporal_namespace() {
  set +e
  for _ in $(seq 1 60); do
    docker compose exec -T temporal tctl --address temporal:7233 namespace describe default >/dev/null 2>&1
    if [ $? -eq 0 ]; then
      set -e
      return
    fi
    docker compose exec -T temporal tctl --address temporal:7233 namespace register default --retention 1 >/dev/null 2>&1
    sleep 1
  done
  set -e
  echo "Temporal namespace not ready" >&2
  docker compose logs temporal --tail=50
  exit 1
}

choose_port CONTROL_PLANE_PORT "Control plane"
choose_port TOOL_RUNNER_PORT "Tool runner"
choose_port BROWSER_WORKER_PORT "Browser worker"
TEMPORAL_TASK_QUEUE="${TEMPORAL_TASK_QUEUE:-gavryn-runs-${CONTROL_PLANE_PORT}}"
CONTROL_PLANE_URL="http://localhost:${CONTROL_PLANE_PORT}"
TOOL_RUNNER_URL="http://localhost:${TOOL_RUNNER_PORT}"
BROWSER_WORKER_URL="http://localhost:${BROWSER_WORKER_PORT}"
VITE_API_BASE_URL="$CONTROL_PLANE_URL"

if [ -n "${POSTGRES_URL:-}" ] && [ "$USE_ENV_POSTGRES_URL" != "1" ]; then
  echo "POSTGRES_URL is set in the environment; overriding to use the Docker Postgres instance." >&2
  echo "Set USE_ENV_POSTGRES_URL=1 to keep the existing POSTGRES_URL." >&2
fi
if [ "$USE_ENV_POSTGRES_URL" = "1" ] && [ -n "${POSTGRES_URL:-}" ]; then
  :
else
  POSTGRES_URL="postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@${POSTGRES_HOST}:${POSTGRES_PORT}/${POSTGRES_DB}?sslmode=disable"
fi

export POSTGRES_URL

export CONTROL_PLANE_PORT TOOL_RUNNER_PORT BROWSER_WORKER_PORT TEMPORAL_TASK_QUEUE
export CONTROL_PLANE_URL TOOL_RUNNER_URL BROWSER_WORKER_URL
export VITE_API_BASE_URL
export BROWSER_USER_TAB_CDP_URL

check_user_tab_cdp() {
  if [ -z "${BROWSER_USER_TAB_CDP_URL:-}" ]; then
    return
  fi

  local cdp_url="${BROWSER_USER_TAB_CDP_URL%/}"
  local cdp_version_url="${cdp_url}/json/version"

  if command -v curl >/dev/null 2>&1; then
    if curl --max-time 2 -fsS "$cdp_version_url" >/dev/null 2>&1; then
      echo "User-tab browser mode enabled via ${BROWSER_USER_TAB_CDP_URL}"
      return
    fi
  fi

  echo "Warning: BROWSER_USER_TAB_CDP_URL is set but CDP endpoint is unreachable (${cdp_version_url})." >&2
  echo "User-tab mode will fail until a browser is started with remote debugging enabled." >&2
  if [ "$(uname -s)" = "Darwin" ]; then
    echo "Example: open -na 'Google Chrome' --args --remote-debugging-port=9222 --user-data-dir=/tmp/gavryn-user-tab" >&2
  fi
}

sql_literal() {
  local value="$1"
  value=${value//"'"/"''"}
  printf "'%s'" "$value"
}

echo "Starting docker compose..."
(cd "$ROOT_DIR" && docker compose up -d)
check_user_tab_cdp

compose_port_line="$(cd "$ROOT_DIR" && docker compose port postgres 5432 2>/dev/null || true)"
compose_port="${compose_port_line##*:}"
if [ -n "$compose_port" ] && [ "$compose_port" != "$POSTGRES_PORT" ]; then
  echo "Postgres port mismatch (compose ${compose_port} vs ${POSTGRES_PORT}); recreating postgres container." >&2
  (cd "$ROOT_DIR" && docker compose up -d --force-recreate postgres)
fi

if [ "$USE_ENV_POSTGRES_URL" != "1" ]; then
  compose_port_line="$(cd "$ROOT_DIR" && docker compose port postgres 5432 2>/dev/null || true)"
  compose_port="${compose_port_line##*:}"
  if [ -n "$compose_port" ] && [ "$compose_port" != "$POSTGRES_PORT" ]; then
    echo "Using Postgres port ${compose_port} from docker compose." >&2
    POSTGRES_PORT="$compose_port"
    POSTGRES_URL="postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@${POSTGRES_HOST}:${POSTGRES_PORT}/${POSTGRES_DB}?sslmode=disable"
    export POSTGRES_PORT POSTGRES_URL
  fi
fi

echo "Waiting for Postgres..."
ADMIN_USER=""
for attempt in $(seq 1 30); do
  if docker compose exec -T postgres pg_isready -U "$POSTGRES_USER" -d postgres >/dev/null 2>&1; then
    ADMIN_USER="$POSTGRES_USER"
    break
  fi
  if docker compose exec -T postgres pg_isready -U postgres -d postgres >/dev/null 2>&1; then
    ADMIN_USER="postgres"
    break
  fi
  sleep 1
done

if [ -z "$ADMIN_USER" ]; then
  echo "Postgres did not become ready or no admin user available."
  echo "If this is a fresh setup, try: docker compose down -v && make dev"
  exit 1
fi

echo "Ensuring database user and schema exist..."
db_user_literal=$(sql_literal "$POSTGRES_USER")
db_pass_literal=$(sql_literal "$POSTGRES_PASSWORD")
db_name_literal=$(sql_literal "$POSTGRES_DB")

docker compose exec -T postgres psql -U "$ADMIN_USER" -d postgres -X -v ON_ERROR_STOP=1 -f /dev/stdin <<SQL
SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', ${db_user_literal}, ${db_pass_literal})
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = ${db_user_literal});
\gexec
SQL

docker compose exec -T postgres psql -U "$ADMIN_USER" -d postgres -X -v ON_ERROR_STOP=1 -f /dev/stdin <<SQL
SELECT format('CREATE DATABASE %I OWNER %I', ${db_name_literal}, ${db_user_literal})
WHERE NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = ${db_name_literal});
\gexec
SQL

echo "Applying migrations..."
for migration in "$ROOT_DIR/infra/migrations"/*.sql; do
  docker compose exec -T postgres psql -U "$ADMIN_USER" -d "$POSTGRES_DB" -X -v ON_ERROR_STOP=1 \
    -f /dev/stdin < "$migration"
done

echo "Waiting for Temporal..."
if ! wait_for_port 7233 "Temporal"; then
  docker compose logs temporal --tail=50
  exit 1
fi
sleep 5
ensure_temporal_namespace

pids=()
pgids=()
cleanup_done=0
shutdown_flag="${TMPDIR:-/tmp}/gavryn-dev-shutdown.$$"
rm -f "$shutdown_flag" 2>/dev/null || true
self_pgid=""
if command -v ps >/dev/null 2>&1; then
  self_pgid=$(ps -o pgid= -p $$ 2>/dev/null | tr -d ' ')
fi

record_process() {
  local pid="$1"
  local pgid=""
  if command -v ps >/dev/null 2>&1; then
    pgid=$(ps -o pgid= -p "$pid" 2>/dev/null | tr -d ' ')
  fi
  pids+=("$pid")
  if [ -n "$pgid" ]; then
    pgids+=("$pgid")
  fi
}

child_pids() {
  if command -v pgrep >/dev/null 2>&1; then
    pgrep -P "$1" 2>/dev/null || true
  fi
}

signal_tree() {
  local pid="$1"
  local sig="$2"
  local child
  for child in $(child_pids "$pid"); do
    signal_tree "$child" "$sig"
  done
  kill "-$sig" "$pid" 2>/dev/null || true
}

run_bg() {
  local name="$1"
  shift
  echo "Starting $name..."
  "$@" &
  record_process "$!"
}

run_bg_retry() {
  local name="$1"
  shift
  local cmd="$*"
  echo "Starting $name..."
  local loop_cmd="trap 'exit 0' INT TERM; while true; do if [ -f '$shutdown_flag' ]; then exit 0; fi; if $cmd; then exit 0; fi; echo '${name} exited, retrying...'; sleep 1; done"
  bash -c "$loop_cmd" &
  record_process "$!"
}

cleanup() {
  if [ "$cleanup_done" -eq 1 ]; then
    return
  fi
  cleanup_done=1
  trap - INT TERM EXIT
  echo "Stopping..."
  touch "$shutdown_flag" 2>/dev/null || true
  if [ "${#pgids[@]}" -gt 0 ]; then
    for pgid in "${pgids[@]}"; do
      if [ -n "$pgid" ] && [ "$pgid" != "$self_pgid" ]; then
        kill -INT -- "-$pgid" 2>/dev/null || true
      fi
    done
  fi
  if [ "${#pids[@]}" -gt 0 ]; then
    for pid in "${pids[@]}"; do
      signal_tree "$pid" INT
    done
  fi
  local deadline=$((SECONDS + 6))
  while [ "$SECONDS" -lt "$deadline" ]; do
    local still_running=0
    for pgid in "${pgids[@]}"; do
      if [ -n "$pgid" ] && [ "$pgid" != "$self_pgid" ] && kill -0 -- "-$pgid" 2>/dev/null; then
        still_running=1
        break
      fi
    done
    if [ "$still_running" -eq 0 ]; then
      for pid in "${pids[@]}"; do
        if kill -0 "$pid" 2>/dev/null; then
          still_running=1
          break
        fi
      done
    fi
    if [ "$still_running" -eq 0 ]; then
      break
    fi
    sleep 0.2
  done
  if [ "${#pgids[@]}" -gt 0 ]; then
    for pgid in "${pgids[@]}"; do
      if [ -n "$pgid" ] && [ "$pgid" != "$self_pgid" ]; then
        kill -TERM -- "-$pgid" 2>/dev/null || true
      fi
    done
  fi
  if [ "${#pids[@]}" -gt 0 ]; then
    for pid in "${pids[@]}"; do
      signal_tree "$pid" TERM
    done
  fi
  deadline=$((SECONDS + 2))
  while [ "$SECONDS" -lt "$deadline" ]; do
    local still_running=0
    for pgid in "${pgids[@]}"; do
      if [ -n "$pgid" ] && [ "$pgid" != "$self_pgid" ] && kill -0 -- "-$pgid" 2>/dev/null; then
        still_running=1
        break
      fi
    done
    if [ "$still_running" -eq 0 ]; then
      for pid in "${pids[@]}"; do
        if kill -0 "$pid" 2>/dev/null; then
          still_running=1
          break
        fi
      done
    fi
    if [ "$still_running" -eq 0 ]; then
      break
    fi
    sleep 0.2
  done
  if [ "${#pgids[@]}" -gt 0 ]; then
    for pgid in "${pgids[@]}"; do
      if [ -n "$pgid" ] && [ "$pgid" != "$self_pgid" ]; then
        kill -KILL -- "-$pgid" 2>/dev/null || true
      fi
    done
  fi
  if [ "${#pids[@]}" -gt 0 ]; then
    for pid in "${pids[@]}"; do
      signal_tree "$pid" KILL
    done
  fi
  for pid in "${pids[@]}"; do
    wait "$pid" 2>/dev/null || true
  done
  rm -f "$shutdown_flag" 2>/dev/null || true
}

run_bg_retry "control-plane" "cd '$ROOT_DIR/control-plane' && go run ./cmd/control-plane"
run_bg_retry "temporal-worker" "cd '$ROOT_DIR/control-plane' && go run ./cmd/worker"

run_bg "browser-worker" bash -c "cd '$ROOT_DIR/workers/browser' && if [ ! -d node_modules ]; then npm install; fi && npm run dev"
run_bg "tool-runner" bash -c "cd '$ROOT_DIR/workers/tool-runner' && if [ ! -d node_modules ]; then npm install; fi && npm run dev"

run_bg "frontend" bash -c "cd '$ROOT_DIR/frontend' && if [ ! -d node_modules ]; then npm install; fi && npm run dev"

echo "All services started. Press Ctrl+C to stop."

trap 'cleanup; exit 0' INT TERM
trap 'cleanup' EXIT
wait
