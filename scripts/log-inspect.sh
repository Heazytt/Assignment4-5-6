#!/usr/bin/env bash
# Automated log inspection.
# Scans container logs for known failure patterns (the same patterns that
# would have shortened INC-2025-001 if we'd had this script during the
# incident). Outputs a triage summary with recommended actions.
#
# Usage:
#   ./scripts/log-inspect.sh                    # last 1000 lines, all services
#   ./scripts/log-inspect.sh order-service      # only order-service
#   SINCE=10m ./scripts/log-inspect.sh           # last 10 minutes

set -u

SERVICES=${1:-"auth-service user-service product-service order-service chat-service"}
SINCE=${SINCE:-"30m"}

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Pattern -> diagnosis -> recommended action
declare -A PATTERNS=(
  ["cannot connect to database"]="Database unreachable|Check DB_HOST env var (typo?), check that postgres is healthy: docker compose ps postgres"
  ["pq: password authentication failed"]="Wrong DB password|Check POSTGRES_PASSWORD in .env matches what the service expects"
  ["dial tcp.*connect: connection refused"]="Network unreachable|Target service is not running. Run: docker compose ps"
  ["no such host"]="DNS resolution failed|Hostname does not resolve. This is exactly the INC-2025-001 failure mode."
  ["context deadline exceeded"]="Timeout|Downstream service is slow or hanging. Check its logs and CPU usage."
  ["FATAL"]="Fatal startup error|Service crashed on init. Read full log to find root cause."
  ["panic:"]="Go panic|Unhandled error in code. Capture full stack trace before restart."
  ["bcrypt:"]="Auth library error|Usually password too long or hash format mismatch."
  ["too many open files"]="Resource exhaustion|Bump ulimit on the host or reduce connection pool size."
  ["out of memory"]="OOM|Container exceeded memory limit. Increase deploy.resources.limits.memory."
  ["amqp.*Exception"]="RabbitMQ error|Check that rabbitmq is healthy and the exchange/queue exists."
)

echo "================================================================"
echo " Log inspection (since: $SINCE)"
echo "================================================================"

TOTAL_HITS=0

for svc in $SERVICES; do
  echo
  echo -e "${BLUE}── $svc ──${NC}"

  LOG=$(docker compose logs --since="$SINCE" --no-color "$svc" 2>/dev/null || true)
  if [ -z "$LOG" ]; then
    echo "  (no logs available)"
    continue
  fi

  HITS=0
  for pattern in "${!PATTERNS[@]}"; do
    MATCHES=$(echo "$LOG" | grep -ciE "$pattern" || true)
    if [ "$MATCHES" -gt 0 ]; then
      DIAG="${PATTERNS[$pattern]%%|*}"
      ACTION="${PATTERNS[$pattern]##*|}"
      echo -e "  ${RED}[!]${NC} pattern: ${YELLOW}$pattern${NC} — $MATCHES hits"
      echo -e "      diagnosis: $DIAG"
      echo -e "      action:    $ACTION"
      # Show one example line
      EXAMPLE=$(echo "$LOG" | grep -iE "$pattern" | head -1 | cut -c1-160)
      echo -e "      example:   ${EXAMPLE}"
      HITS=$((HITS + MATCHES))
      TOTAL_HITS=$((TOTAL_HITS + MATCHES))
    fi
  done

  if [ "$HITS" -eq 0 ]; then
    echo -e "  ${GREEN}✓${NC} no known issue patterns"
  fi

  # Container status
  STATE=$(docker inspect --format '{{.State.Status}} (restarts: {{.RestartCount}})' "$svc" 2>/dev/null || echo "unknown")
  echo -e "  state: $STATE"
done

echo
echo "================================================================"
if [ "$TOTAL_HITS" -eq 0 ]; then
  echo -e " ${GREEN}No issues detected.${NC} All scanned logs look clean."
else
  echo -e " ${RED}$TOTAL_HITS issue patterns detected.${NC} Review actions above."
fi
echo "================================================================"
