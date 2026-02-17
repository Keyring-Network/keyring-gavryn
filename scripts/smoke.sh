#!/usr/bin/env bash
set -euo pipefail
set +m

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
FRONTEND_PORT="${FRONTEND_PORT:-5173}"
TEMPORAL_ADDRESS="${TEMPORAL_ADDRESS:-localhost:7233}"
TEMPORAL_UI_PORT="${TEMPORAL_UI_PORT:-8088}"

if [ "$POSTGRES_HOST" = "postgres" ]; then
  POSTGRES_HOST="localhost"
fi

if [ "$TEMPORAL_ADDRESS" = "temporal:7233" ]; then
  TEMPORAL_ADDRESS="localhost:7233"
fi

if [ -z "${POSTGRES_URL:-}" ]; then
  POSTGRES_URL="postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@${POSTGRES_HOST}:${POSTGRES_PORT}/${POSTGRES_DB}?sslmode=disable"
fi

export POSTGRES_USER POSTGRES_PASSWORD POSTGRES_DB POSTGRES_HOST POSTGRES_PORT
export POSTGRES_URL TEMPORAL_ADDRESS

used_ports=""
pids=()

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
      return 0
    fi
    docker compose exec -T temporal tctl --address temporal:7233 namespace register default --retention 1 >/dev/null 2>&1
    sleep 1
  done
  set -e
  echo "Temporal namespace not ready" >&2
  docker compose logs temporal --tail=50
  return 1
}

wait_for_http() {
  local url="$1"
  local label="$2"
  for _ in $(seq 1 90); do
    if curl -sSf "$url" >/dev/null 2>&1; then
      return
    fi
    sleep 1
  done
  echo "${label} did not respond at ${url}" >&2
  return 1
}

sql_literal() {
  local value="$1"
  value=${value//"'"/"''"}
  printf "'%s'" "$value"
}

cleanup() {
  if [ "${#pids[@]}" -gt 0 ]; then
    {
      for pid in "${pids[@]}"; do
        kill -INT "$pid" 2>/dev/null || true
      done
      local deadline=$((SECONDS + 6))
      while [ "$SECONDS" -lt "$deadline" ]; do
        local still_running=()
        for pid in "${pids[@]}"; do
          if kill -0 "$pid" 2>/dev/null; then
            still_running+=("$pid")
          fi
        done
        if [ "${#still_running[@]}" -eq 0 ]; then
          break
        fi
        sleep 0.2
      done
      for pid in "${pids[@]}"; do
        if kill -0 "$pid" 2>/dev/null; then
          kill -TERM "$pid" 2>/dev/null || true
        fi
      done
      deadline=$((SECONDS + 2))
      while [ "$SECONDS" -lt "$deadline" ]; do
        local still_running=()
        for pid in "${pids[@]}"; do
          if kill -0 "$pid" 2>/dev/null; then
            still_running+=("$pid")
          fi
        done
        if [ "${#still_running[@]}" -eq 0 ]; then
          break
        fi
        sleep 0.2
      done
      for pid in "${pids[@]}"; do
        if kill -0 "$pid" 2>/dev/null; then
          kill -KILL "$pid" 2>/dev/null || true
        fi
      done
      for pid in "${pids[@]}"; do
        wait "$pid" 2>/dev/null || true
      done
    } 2>/dev/null
  fi
  if [ "${KEEP_DOCKER_UP:-}" != "1" ]; then
    (cd "$ROOT_DIR" && docker compose down)
  fi
}

run_once() {
  used_ports=""
  reserve_port "$TEMPORAL_UI_PORT"
  choose_port CONTROL_PLANE_PORT "Control plane"
  choose_port TOOL_RUNNER_PORT "Tool runner"
  choose_port BROWSER_WORKER_PORT "Browser worker"
  choose_port FRONTEND_PORT "Frontend"

  CONTROL_PLANE_URL="http://localhost:${CONTROL_PLANE_PORT}"
  TOOL_RUNNER_URL="http://localhost:${TOOL_RUNNER_PORT}"
  BROWSER_WORKER_URL="http://localhost:${BROWSER_WORKER_PORT}"
  VITE_API_BASE_URL="$CONTROL_PLANE_URL"

  export CONTROL_PLANE_PORT TOOL_RUNNER_PORT BROWSER_WORKER_PORT FRONTEND_PORT
  export CONTROL_PLANE_URL TOOL_RUNNER_URL BROWSER_WORKER_URL VITE_API_BASE_URL

  echo "Starting docker compose..."
  (cd "$ROOT_DIR" && docker compose up -d)

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
    echo "Postgres did not become ready or no admin user available." >&2
    return 1
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
    return 1
  fi
  sleep 5
  if ! ensure_temporal_namespace; then
    return 1
  fi

  pids=()

  run_bg() {
    local name="$1"
    shift
    echo "Starting $name..."
    (
      local child_pid=""
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
        kill "-${sig}" "$pid" 2>/dev/null || true
      }
      shutdown_child() {
        if [ -z "$child_pid" ]; then
          return
        fi
        if ! kill -0 "$child_pid" 2>/dev/null; then
          return
        fi
        signal_tree "$child_pid" INT
        local shutdown_deadline=$((SECONDS + 4))
        while [ "$SECONDS" -lt "$shutdown_deadline" ]; do
          if ! kill -0 "$child_pid" 2>/dev/null; then
            return
          fi
          sleep 0.2
        done
        signal_tree "$child_pid" TERM
        sleep 0.5
        signal_tree "$child_pid" KILL
      }
      trap 'shutdown_child; exit 0' INT TERM
      "$@" &
      child_pid=$!
      wait "$child_pid"
    ) &
    pids+=("$!")
  }

  run_bg_retry() {
    local name="$1"
    shift
    local cmd="$*"
    echo "Starting $name..."
    (
      local child_pid=""
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
        kill "-${sig}" "$pid" 2>/dev/null || true
      }
      shutdown_child() {
        if [ -z "$child_pid" ]; then
          return
        fi
        if ! kill -0 "$child_pid" 2>/dev/null; then
          return
        fi
        signal_tree "$child_pid" INT
        local shutdown_deadline=$((SECONDS + 4))
        while [ "$SECONDS" -lt "$shutdown_deadline" ]; do
          if ! kill -0 "$child_pid" 2>/dev/null; then
            return
          fi
          sleep 0.2
        done
        signal_tree "$child_pid" TERM
        sleep 0.5
        signal_tree "$child_pid" KILL
      }
      trap 'shutdown_child; exit 0' INT TERM
      while true; do
        bash -lc "$cmd" &
        child_pid=$!
        wait "$child_pid"
        echo "${name} exited, retrying..."
        sleep 1
      done
    ) &
    pids+=("$!")
  }

  run_bg_retry "control-plane" "cd '$ROOT_DIR/control-plane' && exec go run ./cmd/control-plane"
  run_bg_retry "temporal-worker" "cd '$ROOT_DIR/control-plane' && exec go run ./cmd/worker"

  run_bg "browser-worker" bash -lc "cd '$ROOT_DIR/workers/browser' && if [ ! -d node_modules ]; then npm install; fi && BROWSER_WORKER_PORT=$BROWSER_WORKER_PORT CONTROL_PLANE_URL=$CONTROL_PLANE_URL BROWSER_WORKER_URL=$BROWSER_WORKER_URL exec npm run dev"
  run_bg "tool-runner" bash -lc "cd '$ROOT_DIR/workers/tool-runner' && if [ ! -d node_modules ]; then npm install; fi && TOOL_RUNNER_PORT=$TOOL_RUNNER_PORT CONTROL_PLANE_URL=$CONTROL_PLANE_URL BROWSER_WORKER_URL=$BROWSER_WORKER_URL exec npm run dev"

  run_bg "frontend" bash -lc "cd '$ROOT_DIR/frontend' && if [ ! -d node_modules ]; then npm install; fi && VITE_API_BASE_URL=$VITE_API_BASE_URL exec npm run dev -- --port $FRONTEND_PORT"

  echo "Waiting for services..."
  wait_for_http "$CONTROL_PLANE_URL/health" "Control plane"
  wait_for_http "$TOOL_RUNNER_URL/health" "Tool runner"
  wait_for_http "$BROWSER_WORKER_URL/health" "Browser worker"
  wait_for_http "http://localhost:${FRONTEND_PORT}" "Frontend"

  echo "Smoke test passed."
}

ITERATIONS="${SMOKE_ITERATIONS:-1}"
if [ "${1:-}" = "--loop" ]; then
  ITERATIONS=0
fi

trap 'cleanup' EXIT

if [ "$ITERATIONS" -eq 0 ]; then
  while true; do
    run_once
    cleanup
    sleep 1
  done
else
  for _ in $(seq 1 "$ITERATIONS"); do
    run_once
    cleanup
  done
fi
