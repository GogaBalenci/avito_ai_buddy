#!/bin/sh
# Fails when total coverage in a `go tool cover -func` report is below the threshold.
#   scripts/coverage-check.sh coverage/go-app.txt 80
set -eu
REPORT=$1
MIN=$2
TOTAL=$(tail -1 "$REPORT" | awk '{print $NF}' | tr -d '%')
if awk -v t="$TOTAL" -v m="$MIN" 'BEGIN { exit !(t + 0 < m + 0) }'; then
  echo "coverage gate: FAIL — application coverage ${TOTAL}% < ${MIN}%" >&2
  exit 1
fi
echo "coverage gate: OK — application coverage ${TOTAL}% >= ${MIN}% (target 85%)"
