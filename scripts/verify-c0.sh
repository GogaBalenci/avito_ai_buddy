#!/bin/sh
# Checks the scaffold the way an expert would: clean start, timed build,
# all healthchecks green, smoke and Go tests. Writes the report to .c0-report.txt.
#
#   sh scripts/verify-c0.sh          # build without build cache (base images may be cached)
#   COLD=1 sh scripts/verify-c0.sh   # truly first build: also removes our base images
#                                    # and prunes the Docker build cache (affects other projects' cache)
set -u
cd "$(dirname "$0")/.." || exit 1
REPORT=.c0-report.txt
: > "$REPORT"

log() { printf '%s\n' "$*" | tee -a "$REPORT"; }
run() { log "\$ $*"; "$@" >> "$REPORT" 2>&1; rc=$?; [ $rc -eq 0 ] || log "(exit $rc)"; return $rc; }
now() { date +%s; }

BASE_IMAGES="library/golang:1.26-alpine library/golang:1.26-bookworm library/alpine:3.22 library/node:24-alpine library/nginx:1.28-alpine library/postgres:18-alpine library/redis:8-alpine"

log "# C0 verification — $(date '+%Y-%m-%d %H:%M:%S %z')"
log "host: $(uname -sm)"
log "docker: $(docker version --format '{{.Server.Version}} ({{.Server.Os}}/{{.Server.Arch}})' 2>/dev/null || echo 'NOT RUNNING')"
log "compose: $(docker compose version --short 2>/dev/null || echo 'missing')"

log ""
log "## 1. Registry"
PREFIX=""
for p in docker.io/ mirror.gcr.io/; do
  if docker pull -q "${p}library/alpine:3.22" >/dev/null 2>&1; then PREFIX=$p; break; fi
  log "registry ${p}: NOT reachable"
done
if [ -z "$PREFIX" ]; then log "FAIL: neither Docker Hub nor mirror.gcr.io is reachable"; exit 1; fi
log "registry: using REGISTRY_PREFIX=$PREFIX"
export REGISTRY_PREFIX=$PREFIX

log ""
log "## 2. Clean state"
run docker compose --profile test down -v --remove-orphans
if [ "${COLD:-0}" = "1" ]; then
  for img in $BASE_IMAGES; do docker image rm -f "${PREFIX}${img}" "$img" >/dev/null 2>&1; done
  docker image rm -f avito-ai-buddy/api:local avito-ai-buddy/mcp-mocks:local avito-ai-buddy/web:local avito-ai-buddy/test-go:local >/dev/null 2>&1
  run docker builder prune -af
  log "cold mode: base images and build cache removed"
fi

log ""
log "## 3. Build (no cache)"
t0=$(now)
run docker compose build --no-cache --pull || { log "FAIL: build"; exit 1; }
t1=$(now)
log "build time: $((t1 - t0)) s"

log ""
log "## 4. Up and wait for healthchecks"
run docker compose up -d --wait --wait-timeout 180 || { docker compose logs --no-color >> "$REPORT" 2>&1; log "FAIL: up"; exit 1; }
t2=$(now)
log "up+healthy time: $((t2 - t1)) s; total from clean: $((t2 - t0)) s"
log ""
docker compose ps --format 'table {{.Service}}\t{{.Status}}\t{{.Ports}}' | tee -a "$REPORT"

log ""
log "## 5. Checks"
log "GET http://localhost:${WEB_PORT:-8380}/healthz → $(curl -sS "http://localhost:${WEB_PORT:-8380}/healthz" 2>&1)"
run docker compose --profile test run --rm smoke && log "smoke: OK" || log "smoke: FAIL"
run docker compose --profile test run --rm test-go && log "test-go: OK" || log "test-go: FAIL"
run docker compose --profile test run --rm test-web && log "test-web: OK" || log "test-web: FAIL"

log ""
log "Stack is left running: http://localhost:${WEB_PORT:-8380} (stop: docker compose down)"
log "Report: $REPORT"
