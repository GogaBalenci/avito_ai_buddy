#!/bin/sh
# Smoke check of the running stack (service smoke):
#   docker compose --profile test run --rm smoke
# C0 stub: web serves the SPA and proxies /healthz to api with both dependencies up.
# At C3 it is replaced by `go test -tags smoke ./test/smoke` (main scenario via API).
set -eu
BASE_URL=${BASE_URL:-http://web}

echo "== GET $BASE_URL/"
curl -fsS "$BASE_URL/" | grep -q '<div id="root">' && echo "ok: SPA served"

echo "== GET $BASE_URL/healthz"
body=$(curl -fsS "$BASE_URL/healthz")
echo "$body"
echo "$body" | grep -q '"status":"ok"'
echo "$body" | grep -q '"postgres":true'
echo "$body" | grep -q '"redis":true'

echo "== headers"
curl -fsSI "$BASE_URL/" | grep -qi '^content-security-policy:' && echo "ok: CSP present"

echo "smoke: OK"
