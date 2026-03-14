#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [[ ! -f "${ROOT}/.env" ]]; then
  echo "Missing ${ROOT}/.env"
  echo "Copy .env.example to .env before starting the tunnel."
  exit 1
fi

if ! command -v cloudflared >/dev/null 2>&1; then
  echo "cloudflared is not installed."
  echo "Install it with: brew install cloudflared"
  exit 1
fi

set -a
source "${ROOT}/.env"
set +a

API_ADDR="${API_ADDR:-:8080}"
API_HOST="127.0.0.1"
API_PORT="${API_ADDR##*:}"

if [[ -z "${API_PORT}" || "${API_PORT}" == "${API_ADDR}" ]]; then
  echo "Could not parse API_ADDR=${API_ADDR}"
  echo "Set API_ADDR to a value like :8080 or 127.0.0.1:8080 in .env."
  exit 1
fi

PUBLIC_HOST="${PUBLIC_API_BASE_URL:-}"

cat <<EOF
Starting Cloudflare Tunnel for the local API:
  http://${API_HOST}:${API_PORT}

After cloudflared prints a public URL, use this Fathom webhook path:
  <public-url>/v1/integrations/fathom/webhook

Current frontend:
  http://localhost:5173

Press Ctrl+C to stop the tunnel.
EOF

if [[ -n "${PUBLIC_HOST}" ]]; then
  echo
  echo "Local frontend is configured to call: ${PUBLIC_HOST}"
  echo "For local UI testing, keep PUBLIC_API_BASE_URL=http://localhost:8080 in .env."
fi

echo
exec cloudflared tunnel --url "http://${API_HOST}:${API_PORT}" --no-autoupdate
