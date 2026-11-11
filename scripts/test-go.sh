#!/bin/sh
# Go tests and coverage (service test-go). Same command in CI and for the expert:
#   docker compose --profile test run --rm test-go
# C0: two reports (app / mocks) without a gate; the 80% gate (scripts/coverage-check.sh)
# is switched on at C1 together with business code (12 §11.2).
set -eu
OUT=${OUT:-/out}
mkdir -p "$OUT"

APP_PKGS=$(go list ./... | grep -v -e '/internal/mcpmocks' -e '/cmd/mcp-mocks' -e '/internal/gen/' -e '/test/smoke')
MOCK_PKGS=$(go list ./internal/mcpmocks/... ./cmd/mcp-mocks/...)
COVERPKG=$(echo "$APP_PKGS" | paste -sd, -)

echo "== go vet"
go vet ./...

echo "== app packages (threshold report)"
# shellcheck disable=SC2086
go test -race -count=1 -coverpkg="$COVERPKG" -coverprofile="$OUT/go-app.out" $APP_PKGS
go tool cover -func="$OUT/go-app.out" > "$OUT/go-app.txt"
go tool cover -html="$OUT/go-app.out" -o "$OUT/go-app.html"

echo "== mocks (reference report, outside the threshold)"
# shellcheck disable=SC2086
go test -race -count=1 -coverprofile="$OUT/go-mocks.out" $MOCK_PKGS
go tool cover -func="$OUT/go-mocks.out" > "$OUT/go-mocks.txt"
go tool cover -html="$OUT/go-mocks.out" -o "$OUT/go-mocks.html"

echo "== coverage"
printf 'app:   %s\n' "$(tail -1 "$OUT/go-app.txt" | awk '{print $NF}')"
printf 'mocks: %s\n' "$(tail -1 "$OUT/go-mocks.txt" | awk '{print $NF}')"
echo "reports: coverage/go-app.html, coverage/go-mocks.html"
