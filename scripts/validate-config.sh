#!/usr/bin/env bash
# Pre-deployment configuration validator.
# Catches the kind of misconfigurations that caused INC-2025-001
# (typo in DB_HOST that crashed order-service).
#
# Run BEFORE `docker compose up`:
#   ./scripts/validate-config.sh
#
# Exits non-zero if any check fails so it can be wired into CI.

set -uo pipefail

PASS=0
FAIL=0
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
NC='\033[0m'

ok()   { echo -e "${GREEN}[OK]${NC}   $1"; PASS=$((PASS+1)); }
fail() { echo -e "${RED}[FAIL]${NC} $1"; FAIL=$((FAIL+1)); }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }

echo "================================================================"
echo " Configuration validation"
echo "================================================================"

# 1. Required files exist.
echo
echo "── Required files"
for f in docker-compose.yml .env.example migrations/001_init.sql go.mod; do
  if [ -f "$f" ]; then ok "$f exists"; else fail "$f missing"; fi
done

# 2. .env present (or copy from example).
if [ ! -f .env ]; then
  warn ".env missing — copying from .env.example"
  cp .env.example .env
fi

# 3. Validate compose syntax.
echo
echo "── docker-compose syntax"
if docker compose config -q 2>/dev/null; then
  ok "docker-compose.yml is syntactically valid"
else
  fail "docker-compose.yml has YAML/schema errors"
fi

# 4. Validate that DB_HOST inside compose matches an actual service name.
# This is the exact check that would have prevented INC-2025-001.
echo
echo "── Cross-reference checks (DB_HOST must point to a real service)"
DB_HOST=$(docker compose config 2>/dev/null \
  | awk '/order-service:/,/^  [a-z]/' \
  | grep 'DB_HOST:' | head -1 | awk '{print $2}' | tr -d '"')

if [ -z "$DB_HOST" ]; then
  warn "Could not extract DB_HOST from order-service env (skipping)"
else
  SERVICES=$(docker compose config --services 2>/dev/null)
  if echo "$SERVICES" | grep -qx "$DB_HOST"; then
    ok "DB_HOST=$DB_HOST resolves to a defined service"
  else
    fail "DB_HOST=$DB_HOST does NOT match any service in compose. This is the failure mode from INC-2025-001."
    echo "      Defined services: $(echo "$SERVICES" | tr '\n' ' ')"
  fi
fi

# 5. JWT secret is not the default.
echo
echo "── Secrets"
if grep -E '^JWT_SECRET=' .env 2>/dev/null | grep -q 'change-me'; then
  warn "JWT_SECRET is still the default placeholder — change it for production"
else
  ok "JWT_SECRET has been customized"
fi

# 6. Postgres password is not "app" in production-like deployment.
if grep -E '^POSTGRES_PASSWORD=app$' .env 2>/dev/null; then
  warn "POSTGRES_PASSWORD is still the default 'app'"
fi

# 7. Required ports not already in use.
echo
echo "── Port availability"
for port in 80 3000 5432 9090; do
  if (echo > /dev/tcp/localhost/$port) 2>/dev/null; then
    fail "Port $port is already in use"
  else
    ok "Port $port is free"
  fi
done

# 8. Disk space.
echo
echo "── System resources"
AVAIL_GB=$(df --output=avail -BG / | tail -1 | tr -d 'G ')
if [ "$AVAIL_GB" -lt 3 ]; then
  fail "Less than 3 GB free disk — Docker build will likely fail"
else
  ok "Disk space: ${AVAIL_GB} GB free"
fi

MEM_MB=$(free -m | awk '/^Mem:/{print $2}')
if [ "$MEM_MB" -lt 1500 ]; then
  warn "Only ${MEM_MB} MB RAM — recommend >=2GB for full stack"
else
  ok "RAM: ${MEM_MB} MB"
fi

# Summary
echo
echo "================================================================"
echo " Summary: $PASS passed, $FAIL failed"
echo "================================================================"
exit $FAIL
