#!/usr/bin/env bash


set -euo pipefail
cd "$(dirname "$0")/.."

echo "==> Recreating order-service with the healthy compose file only..."
docker compose -f docker-compose.yml up -d --force-recreate order-service

echo "==> Waiting 10s for the service to come up..."
sleep 10

echo "==> Recent order-service logs (expect: 'connected to database'):"
docker compose logs --tail=20 order-service

echo
echo "==> Container status:"
docker compose ps order-service

echo
echo "==> Service health endpoint:"
curl -fsS http://localhost/healthz/order || true
echo
