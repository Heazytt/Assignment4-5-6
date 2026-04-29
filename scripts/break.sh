#!/usr/bin/env bash


set -euo pipefail
cd "$(dirname "$0")/.."

echo "==> Injecting bad DB hostname into order-service..."
docker compose -f docker-compose.yml -f docker-compose.broken.yml up -d order-service

echo "==> Waiting 10s for the failure to surface..."
sleep 10

echo "==> Recent order-service logs (expect: 'cannot connect to database'):"
docker compose logs --tail=20 order-service

echo
echo "==> Container status:"
docker compose ps order-service

echo
echo "==> Prometheus query: service_up{service=\"order-service\"} (should be 0 or absent)"
echo "Open http://localhost:9090/graph and run that query to confirm."
