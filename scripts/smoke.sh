#!/usr/bin/env bash
# End-to-end smoke test: exercises the whole platform through the gateway only,
# proving the auth -> service -> event flow works. Run `make run` first.
set -euo pipefail

GW="${GW:-http://localhost:8080}"

echo "1) Issue an admin token (OAuth2 client-credentials)"
TOKEN=$(curl -s -X POST "$GW/api/v1/auth/token" \
  -H 'Content-Type: application/json' \
  -d '{"client_id":"admin-client","client_secret":"admin-secret"}' \
  | sed -n 's/.*"access_token":"\([^"]*\)".*/\1/p')
test -n "$TOKEN" && echo "   got token (${#TOKEN} chars)"

echo "2) Register a user (publishes user.created -> notification service logs it)"
curl -s -X POST "$GW/api/v1/users" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"email":"alice@example.com"}'
echo

echo "3) Create a wallet (publishes wallet.created)"
curl -s -X POST "$GW/api/v1/wallets" -H "Authorization: Bearer $TOKEN"
echo

echo "4) List wallets (admin-only RBAC route)"
curl -s "$GW/api/v1/wallets" -H "Authorization: Bearer $TOKEN"
echo

echo "5) Verify RBAC: a user-role token is rejected from the admin list route"
USER_TOKEN=$(curl -s -X POST "$GW/api/v1/auth/token" \
  -H 'Content-Type: application/json' \
  -d '{"client_id":"user-client","client_secret":"user-secret"}' \
  | sed -n 's/.*"access_token":"\([^"]*\)".*/\1/p')
echo "   user listing wallets ->"
curl -s -o /dev/null -w '   HTTP %{http_code} (expect 403)\n' \
  "$GW/api/v1/wallets" -H "Authorization: Bearer $USER_TOKEN"

echo
echo "Smoke test complete. Check 'make logs' for the notification service"
echo "logging the user.created / wallet.created events off the bus."
