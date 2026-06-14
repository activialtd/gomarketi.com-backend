#!/usr/bin/env bash
# Run all GoMarket services locally.
# Usage: ./dev.sh
# Stop: Ctrl-C (kills all child processes)

set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT"

# Load shared env
set -a; source .env; set +a

# Port map  (gateway = 8080 public-facing, each service gets its own)
#   gateway   :8080  ← what the frontend hits
#   auth      :8081
#   identity  :8082
#   storefront:8083
#   catalogue :8084
#   orders    :8085

pids=()

start_service() {
  local name=$1
  local port=$2
  local pkg=$3
  echo "→ starting $name on :$port"
  PORT=$port go run "./$pkg/cmd/server" &
  pids+=($!)
}

cleanup() {
  echo ""
  echo "→ stopping all services..."
  for pid in "${pids[@]}"; do
    kill "$pid" 2>/dev/null || true
  done
  wait
  echo "→ done"
}
trap cleanup SIGINT SIGTERM EXIT

# Start upstream services first
start_service "auth"       8081 "services/auth"
start_service "identity"   8082 "services/identity"
start_service "storefront" 8083 "services/storefront"
start_service "catalogue"  8084 "services/catalogue"
start_service "orders"     8085 "services/orders"

# Give upstreams a moment to bind
sleep 1

# Gateway last — it proxies to all the above
echo "→ starting gateway on :8080"
PORT=8080 go run "./services/gateway/cmd/server" &
pids+=($!)

echo ""
echo "✓ All services running"
echo "  Gateway  → http://localhost:8080"
echo "  Auth     → http://localhost:8081"
echo "  Storefront → http://localhost:8083"
echo ""
echo "Press Ctrl-C to stop all."

wait
