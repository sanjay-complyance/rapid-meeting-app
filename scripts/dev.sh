#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
API_PID=""
WORKER_PID=""
FRONTEND_PID=""
SHUTTING_DOWN=0

cleanup() {
  if [[ "${SHUTTING_DOWN}" -eq 1 ]]; then
    return
  fi
  SHUTTING_DOWN=1

  trap - EXIT INT TERM

  local pid=""
  local child=""

  for pid in "$FRONTEND_PID" "$WORKER_PID" "$API_PID"; do
    if [[ -n "${pid}" ]] && kill -0 "${pid}" 2>/dev/null; then
      if command -v pgrep >/dev/null 2>&1; then
        while read -r child; do
          [[ -n "${child}" ]] && kill -TERM "${child}" 2>/dev/null || true
        done < <(pgrep -P "${pid}" 2>/dev/null || true)
      fi
      kill -TERM "${pid}" 2>/dev/null || true
    fi
  done

  sleep 1

  for pid in "$FRONTEND_PID" "$WORKER_PID" "$API_PID"; do
    if [[ -n "${pid}" ]] && kill -0 "${pid}" 2>/dev/null; then
      if command -v pgrep >/dev/null 2>&1; then
        while read -r child; do
          [[ -n "${child}" ]] && kill -KILL "${child}" 2>/dev/null || true
        done < <(pgrep -P "${pid}" 2>/dev/null || true)
      fi
      kill -KILL "${pid}" 2>/dev/null || true
    fi
  done

  for pid in "$FRONTEND_PID" "$WORKER_PID" "$API_PID"; do
    [[ -n "${pid}" ]] && wait "${pid}" 2>/dev/null || true
  done
}

trap cleanup EXIT INT TERM

wait_for_first_exit() {
  while true; do
    for pid in "$API_PID" "$WORKER_PID" "$FRONTEND_PID"; do
      if [[ -n "${pid}" ]] && ! kill -0 "${pid}" 2>/dev/null; then
        wait "${pid}" 2>/dev/null || true
        return 0
      fi
    done
    sleep 1
  done
}

if [[ ! -f "${ROOT}/.env" ]]; then
  echo "Missing ${ROOT}/.env"
  echo "Copy .env.example to .env and set DATABASE_URL to your Neon connection string."
  exit 1
fi

mkdir -p "${ROOT}/data/uploads" "${ROOT}/data/artifacts"

set -a
source "${ROOT}/.env"
set +a

if [[ -z "${STORAGE_ROOT:-}" ]]; then
  STORAGE_ROOT="${ROOT}/data"
elif [[ "${STORAGE_ROOT}" != /* ]]; then
  STORAGE_ROOT="${ROOT}/${STORAGE_ROOT#./}"
fi
export STORAGE_ROOT

echo "Starting Go API on ${API_ADDR:-:8080}"
(
  cd "${ROOT}/backend"
  exec go run ./cmd/api
) &
API_PID=$!

echo "Starting Python worker"
(
  cd "${ROOT}/worker"
  exec uv run python -m app.main
) &
WORKER_PID=$!

echo "Starting SvelteKit frontend"
(
  cd "${ROOT}/frontend"
  exec bun run dev --host 0.0.0.0
) &
FRONTEND_PID=$!

echo "Dev stack is running. Frontend: http://localhost:5173"
wait_for_first_exit
