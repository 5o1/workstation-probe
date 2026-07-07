#!/usr/bin/env bash
# stability-check.sh — drives a long-running soak of the monitor binary
# and validates the acceptance criteria in docs/STABILITY.md.
#
# Usage: STABILITY_DURATION=5m MODE=direct ./scripts/stability-check.sh
#
# Environment:
#   STABILITY_DURATION  total run time (default: 5m). Accepts Go duration.
#   MODE                direct | systemd (default: direct)
#   MONITOR_BIN         path to monitor binary (default: ./monitor)
#   MONITOR_PORT        port to probe (default: 19090)
#   RACE_BUILD          1 = rebuild with -race before running (default: 0)
#   ARTIFACT_DIR        where to write logs (default: build/stability/<ts>/)

set -euo pipefail

DURATION="${STABILITY_DURATION:-5m}"
MODE="${MODE:-direct}"
BIN="${MONITOR_BIN:-./monitor}"
PORT="${MONITOR_PORT:-19090}"
RACE_BUILD="${RACE_BUILD:-0}"

if ! command -v curl >/dev/null; then
  echo "curl not found" >&2
  exit 2
fi

ts() { date -u +%Y%m%dT%H%M%SZ; }
ARTIFACT_DIR="${ARTIFACT_DIR:-build/stability/$(ts)}"
mkdir -p "$ARTIFACT_DIR"

echo "=== stability-check ==="
echo "duration  : $DURATION"
echo "mode      : $MODE"
echo "binary    : $BIN"
echo "port      : $PORT"
echo "artifacts : $ARTIFACT_DIR"
echo

# Build (optionally with -race) — silence the noise but capture failure.
build() {
  if [ "$RACE_BUILD" = "1" ]; then
    echo ">> rebuilding with -race"
    CGO_ENABLED=1 go build -race -o monitor.race ./cmd/monitor
    BIN=./monitor.race
  else
    if [ ! -x "$BIN" ]; then
      echo ">> building monitor"
      CGO_ENABLED=1 go build -o monitor ./cmd/monitor
    fi
  fi
}

start_monitor() {
  case "$MODE" in
    direct)
      "$BIN" --port "$PORT" >"$ARTIFACT_DIR/monitor.log" 2>&1 &
      echo $! >"$ARTIFACT_DIR/monitor.pid"
      ;;
    systemd)
      sudo systemctl restart monitor
      ;;
    *)
      echo "unknown MODE=$MODE" >&2
      exit 2
      ;;
  esac
}

stop_monitor() {
  case "$MODE" in
    direct)
      if [ -f "$ARTIFACT_DIR/monitor.pid" ]; then
        pid=$(cat "$ARTIFACT_DIR/monitor.pid")
        kill "$pid" 2>/dev/null || true
        wait "$pid" 2>/dev/null || true
      fi
      ;;
    systemd)
      sudo systemctl stop monitor
      ;;
  esac
}

trap stop_monitor EXIT

build
start_monitor

# Wait for the port to come up.
echo ">> waiting for /health"
for _ in $(seq 1 50); do
  if curl -fsS "http://127.0.0.1:$PORT/health" >/dev/null 2>&1; then
    break
  fi
  sleep 0.1
done

# Periodic sampler.
(
  while true; do
    curl -fsS -o /dev/null -w "metrics_http=%{http_code} t=%{time_total}\n" \
      "http://127.0.0.1:$PORT/metrics" || echo "metrics_http=ERR"
    sleep 30
  done
) >"$ARTIFACT_DIR/sample.log" 2>&1 &
SAMPLER=$!

# Goroutine sampler (only meaningful for -race build; harmless either way).
(
  while true; do
    echo "ts=$(ts) num_goroutine=$(curl -fsS http://127.0.0.1:$PORT/health | head -c 200)"
    sleep 60
  done
) >"$ARTIFACT_DIR/goroutine.log" 2>&1 &
GOROUTINE_SAMPLER=$!

# HUP cycle.
(
  sleep "$(echo "$DURATION" | awk -F'm|h' '{print $1*60-30}')s" 2>/dev/null || true
) || true

# Optional HUP drill half-way through.
mid="${DURATION%m*}"
if [[ "$DURATION" == *m ]]; then
  mins="${DURATION%m}"
  if [ -n "$mins" ] && [ "$mins" -ge 2 ] 2>/dev/null; then
    half=$((mins / 2))
    ( sleep "${half}m"; kill -HUP "$(cat "$ARTIFACT_DIR/monitor.pid" 2>/dev/null)" 2>/dev/null ) &
  fi
fi

echo ">> soaking for $DURATION"
case "$DURATION" in
  *m) sleep "${DURATION%m}m" ;;
  *h) sleep "${DURATION%h}h" ;;
  *s) sleep "${DURATION%s}s" ;;
  *) sleep "$DURATION" ;;
esac

kill "$SAMPLER" 2>/dev/null || true
kill "$GOROUTINE_SAMPLER" 2>/dev/null || true

echo ">> generating summary"
{
  echo "{\"duration\":\"$DURATION\",\"mode\":\"$MODE\",\"port\":$PORT}"
  echo "\"samples_total\":$(wc -l <"$ARTIFACT_DIR/sample.log")"
  echo "\"errors\":$(grep -c ERR "$ARTIFACT_DIR/sample.log" || true)"
} >"$ARTIFACT_DIR/summary.json"

stop_monitor

if grep -q ERR "$ARTIFACT_DIR/sample.log"; then
  echo "STABILITY FAIL: errors observed in sample.log"
  exit 1
fi
echo "STABILITY PASS"
