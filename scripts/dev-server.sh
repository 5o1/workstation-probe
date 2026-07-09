#!/usr/bin/env bash
# scripts/dev-server.sh — start workstation-probe for webview development.
#
# Generates a self-contained config with CORS enabled for the Hugo dev
# server, builds the binary if needed, pre-checks that the listen port
# is free, and runs the server in the foreground. Press Ctrl+C to stop.
#
# Environment variables:
#   HOST             - listen host (default: 127.0.0.1)
#   PORT             - listen port (default: 19090)
#   WEBVIEW_ORIGIN   - origin(s) allowed by CORS
#                      (default: http://localhost:1313, http://127.0.0.1:1313)
#   REFRESH          - sampler interval as Go duration (default: 1s)
#   GPU              - GPU build mode: auto, nvml, or stub (default: auto)
#                      auto uses NVML when libnvidia-ml is available, otherwise stub
#   REBUILD          - rebuild the local development binary before starting:
#                      1 or 0 (default: 1)

set -euo pipefail

HERE="$(cd "$(dirname "$0")/.." && pwd)"
cd "$HERE"

HOST="${HOST:-127.0.0.1}"
PORT="${PORT:-19090}"
WEBVIEW_ORIGIN="${WEBVIEW_ORIGIN:-http://localhost:1313,http://127.0.0.1:1313}"
REFRESH="${REFRESH:-1s}"
GPU="${GPU:-auto}"
REBUILD="${REBUILD:-1}"
CONFIG="/tmp/workstation-probe-dev-config.yaml"
BIN="$HERE/workstation-probe"

cleanup() {
  rm -f "$CONFIG"
}
trap cleanup EXIT

nvml_library_available() {
  if ldconfig -p 2>/dev/null | grep -qE 'libnvidia-ml\.so(\.1)?'; then
    return 0
  fi
  for p in \
    /lib/x86_64-linux-gnu/libnvidia-ml.so \
    /usr/lib/x86_64-linux-gnu/libnvidia-ml.so \
    /usr/lib64/libnvidia-ml.so \
    /usr/local/nvidia/lib64/libnvidia-ml.so; do
    [ -e "$p" ] && return 0
  done
  return 1
}

build_monitor() {
  case "$GPU" in
    auto)
      if nvml_library_available; then
        echo ">> building workstation-probe with NVML support (GPU=auto)"
        if make build-nvml; then
          echo "   gpu build  : nvml"
          return 0
        fi
        echo ">> WARN: NVML build failed; falling back to stub GPU collector" >&2
      else
        echo ">> NVML library not found; building stub GPU collector (GPU=auto)"
      fi
      make build
      echo "   gpu build  : stub"
      ;;
    nvml|1|true|yes)
      echo ">> building workstation-probe with NVML support (GPU=${GPU})"
      make build-nvml
      echo "   gpu build  : nvml"
      ;;
    stub|0|false|no)
      echo ">> building workstation-probe with stub GPU collector (GPU=${GPU})"
      make build
      echo "   gpu build  : stub"
      ;;
    *)
      echo ">> ERROR: invalid GPU mode ${GPU}; expected auto, nvml, or stub" >&2
      exit 2
      ;;
  esac
}

# Pre-check: if anything is already listening on $PORT, give an actionable
# error rather than letting the binary fail with a cryptic bind error.
# ss(8) on Linux is the most reliable way to find the listener PID.
if ss -lnt 2>/dev/null | awk '{print $4}' | grep -qE ":${PORT}\$"; then
  pid="$(ss -lntp 2>/dev/null | awk -v p=":${PORT}\$" '$4 ~ p {print $0}' | grep -oP 'pid=\K[0-9]+' | head -1 || true)"
  proc="$(ss -lntp 2>/dev/null | awk -v p=":${PORT}\$" '$4 ~ p {print $0}' | grep -oP '\("([^"]+)",pid=' | sed 's/.*"\([^"]*\)".*/\1/' | head -1 || true)"
  echo ">> ERROR: port ${PORT} is already in use" >&2
  if [ -n "$pid" ]; then
    echo "   pid=${pid}${proc:+  process=${proc}}" >&2
    echo >&2
    echo "   options:" >&2
    echo "     kill ${pid}              # stop the existing process and retry" >&2
    echo "     PORT=19191 $0           # use a different port instead" >&2
  else
    echo "   (could not identify the owning process; check 'ss -lntp | grep :${PORT}')" >&2
  fi
  exit 1
fi

# Build the origins list, one per line, in YAML list syntax.
origins_yaml=""
for o in $(echo "$WEBVIEW_ORIGIN" | tr ',' ' '); do
  origins_yaml="${origins_yaml}      - ${o}
"
done

cat > "$CONFIG" <<EOF
server:
  host: ${HOST}
  port: ${PORT}
sampler:
  interval: ${REFRESH}
  history_capacity: 60
modules:
  cpu:    {enabled: true}
  memory: {enabled: true}
  gpu:    {enabled: true}
  storage:
    enabled: true
    mount_points:
      - {path: /, alias: root}
security:
  cors:
    enabled: true
    allowed_origins:
${origins_yaml}  rate_limit:
    enabled: true
    requests_per_second: 10
    burst: 20
    trust_proxy_headers: false
    exempt_paths: [/health]
logging:
  level: info
  format: json
EOF

if [ "$REBUILD" = "0" ]; then
  if [ ! -x "$BIN" ]; then
    echo ">> ERROR: development binary not found and REBUILD=0" >&2
    echo "   run without REBUILD=0, or build manually with 'make build-nvml'" >&2
    exit 1
  fi
else
  build_monitor
fi

echo ">> workstation-probe starting"
echo "   listen     : ${HOST}:${PORT}"
echo "   CORS       : ${WEBVIEW_ORIGIN}"
echo "   config     : ${CONFIG}"
echo "   refresh    : ${REFRESH}"
echo "   GPU mode   : ${GPU}"
echo
echo ">> test it:"
echo "   curl http://localhost:${PORT}/metrics"
echo
echo ">> press Ctrl+C to stop"
echo

# Run without exec so the trap fires on any exit (binary crash included).
"$BIN" -config "$CONFIG"
