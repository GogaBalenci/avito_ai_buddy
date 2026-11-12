#!/bin/sh
# Smoke check of the running stack (service smoke):
#   docker compose --profile test run --rm smoke
# 1) web serves the SPA and proxies /healthz to api with both dependencies up;
# 2) scenarios S1-S4 and the mentor through the API: go test -tags smoke ./test/smoke
#    (it resets the demo data first).
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

echo "== scenarios S1-S4 and mentor (API)"
BASE_URL="$BASE_URL" go test -tags smoke -count=1 -v ./test/smoke

echo "smoke: OK"
