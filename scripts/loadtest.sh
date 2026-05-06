#!/usr/bin/env bash
# Capacity / load test script.
# Generates concurrent traffic against the API to populate Prometheus and
# observe behavior under load. Watch the Grafana dashboard while it runs.
#
# Usage:  ./scripts/loadtest.sh [BASE_URL] [DURATION_SECONDS] [CONCURRENCY]
# Example: ./scripts/loadtest.sh http://localhost 60 20

set -euo pipefail

BASE=${1:-${BASE:-http://localhost}}
DURATION=${2:-${DURATION:-60}}
CONCURRENCY=${3:-${CONCURRENCY:-20}}

echo "================================================================"
echo " Load test starting"
echo "   target:       $BASE"
echo "   duration:     ${DURATION}s"
echo "   concurrency:  $CONCURRENCY"
echo "================================================================"

# Register / login a test user up front to get a token.
TS=$(date +%s)
EMAIL="loadtest-${TS}@example.com"
RESP=$(curl -fsS -X POST "$BASE/api/auth/register" \
  -H "Content-Type: application/json" \
  -d "{\"email\":\"$EMAIL\",\"name\":\"Load\",\"password\":\"hunter22\"}" || true)

if ! echo "$RESP" | grep -q token; then
  echo "Registration failed, aborting."
  echo "$RESP"
  exit 1
fi
TOKEN=$(echo "$RESP" | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')
echo "Got token: ${TOKEN:0:20}..."
echo

# Worker function: hammers the API with a mix of read/write requests.
worker() {
  local id=$1
  local end=$(( $(date +%s) + DURATION ))
  local count=0
  while [ "$(date +%s)" -lt "$end" ]; do
    # 70% reads (cheap), 30% writes (full pipeline: gRPC + RabbitMQ + DB).
    if [ $((RANDOM % 10)) -lt 7 ]; then
      curl -fsS -o /dev/null "$BASE/api/products" || true
    else
      curl -fsS -o /dev/null -X POST "$BASE/api/orders" \
        -H "Authorization: Bearer $TOKEN" \
        -H "Content-Type: application/json" \
        -d '{"items":[{"product_id":1,"quantity":1}]}' || true
    fi
    count=$((count + 1))
  done
  echo "Worker $id finished: $count requests"
}

# Start workers in parallel.
for i in $(seq 1 "$CONCURRENCY"); do
  worker "$i" &
done

# Print system stats every 5s while workers run.
(
  for i in $(seq 1 $((DURATION / 5))); do
    sleep 5
    echo
    echo "--- $(date +%H:%M:%S) container stats ---"
    docker stats --no-stream --format \
      "table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}" \
      2>/dev/null | head -15
  done
) &
STATS_PID=$!

wait $(jobs -p | grep -v "$STATS_PID") 2>/dev/null || wait
kill "$STATS_PID" 2>/dev/null || true

echo
echo "================================================================"
echo " Load test finished"
echo " Check Grafana dashboard for resulting graphs:"
echo "   - HTTP requests/s by service"
echo "   - p95 latency"
echo "   - Container CPU and memory usage"
echo "================================================================"
