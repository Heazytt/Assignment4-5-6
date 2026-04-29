#!/usr/bin/env bash


set -euo pipefail
BASE=${BASE:-http://localhost}

echo "==> Registering test user..."
RESP=$(curl -fsS -X POST "$BASE/api/auth/register" \
  -H "Content-Type: application/json" \
  -d '{"email":"alice@example.com","name":"Alice","password":"hunter22"}' || true)

# If user already exists, log in instead.
if [[ -z "$RESP" ]] || ! echo "$RESP" | grep -q token; then
  echo "==> User exists, logging in..."
  RESP=$(curl -fsS -X POST "$BASE/api/auth/login" \
    -H "Content-Type: application/json" \
    -d '{"email":"alice@example.com","password":"hunter22"}')
fi
TOKEN=$(echo "$RESP" | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')
echo "Got token: ${TOKEN:0:20}..."

echo "==> Listing products..."
curl -fsS "$BASE/api/products" | head -c 400; echo

echo "==> Placing order for product 1, qty 2..."
curl -fsS -X POST "$BASE/api/orders" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"items":[{"product_id":1,"quantity":2}]}'
echo

echo "==> Listing my orders..."
curl -fsS "$BASE/api/orders" -H "Authorization: Bearer $TOKEN"
echo

echo "==> Sending chat message to user 1..."
curl -fsS -X POST "$BASE/api/chat/send" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"receiver_id":1,"content":"Hello from smoke test"}'
echo

echo "==> Done."
