#!/usr/bin/env bash
# scripts/live-test.sh — end-to-end smoke test driven through the real
# install → start → probe → uninstall cycle.
#
# Runs in one of two modes, chosen automatically:
#   • systemd mode (preferred): installs to /usr/local/bin and uses a
#     uniquely-named systemd unit (workstation-probe-live-test.service). Requires
#     passwordless sudo for install/uninstall and systemd.
#   • direct mode (fallback): installs to $HOME/.local/bin and runs the
#     binary directly in the background. No sudo needed; systemd not used.
#     Cleanup kills the background process and removes the install dir.
#
# Selection: if `sudo -n true` succeeds, we use systemd mode; otherwise
# direct mode. Override with MODE=systemd or MODE=direct.
#
# Other overrides:
#   PORT=19191 ./scripts/live-test.sh
#   SKIP_INSTALL=1 ./scripts/live-test.sh      # don't touch files (probe only)

set -u

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PORT="${PORT:-18080}"
MODE_OVERRIDE="${MODE:-}"
SERVICE="workstation-probe-live-test"
SERVICE_TEMPLATE="${REPO_ROOT}/contrib/systemd/workstation-probe.service.in"
BIN_SRC="${REPO_ROOT}/workstation-probe"
LOG_PREFIX="[live-test]"

# State set once we know which mode we're in.
MODE=""
BIN_DEST=""
CONFIG_FILE=""
PID_FILE=""
SERVICE_FILE=""

# ---------------------------------------------------------------------------
# Mode selection
# ---------------------------------------------------------------------------
have_passless_sudo() {
    sudo -n true </dev/null >/dev/null 2>&1
}

if [ -n "$MODE_OVERRIDE" ]; then
    MODE="$MODE_OVERRIDE"
elif have_passless_sudo; then
    MODE=systemd
else
    MODE=direct
    echo "$LOG_PREFIX passwordless sudo not available → using direct mode (no systemd)"
fi

case "$MODE" in
    systemd)
        BIN_DEST="/usr/local/bin/${SERVICE}"
        CONFIG_FILE="/etc/workstation-probe-live-test/config.yaml"
        SERVICE_FILE="/etc/systemd/system/${SERVICE}.service"
        PID_FILE="/var/run/${SERVICE}.pid"
        SUDO="sudo"
        ;;
    direct)
        PREFIX="${HOME}/.local"
        BIN_DEST="${PREFIX}/bin/${SERVICE}"
        CONFIG_DIR="${PREFIX}/share/workstation-probe-live-test"
        CONFIG_FILE="${CONFIG_DIR}/config.yaml"
        PID_FILE="${CONFIG_DIR}/workstation-probe.pid"
        SUDO=""
        ;;
    *)
        echo "$LOG_PREFIX Unknown MODE=$MODE (use systemd or direct)"; exit 1 ;;
esac

# ---------------------------------------------------------------------------
# Cleanup — runs on any exit. Idempotent.
# ---------------------------------------------------------------------------
cleanup() {
    set +e
    echo
    echo "$LOG_PREFIX === Cleanup (mode=${MODE}) ==="
    case "$MODE" in
        systemd)
            sudo systemctl stop "${SERVICE}.service" 2>/dev/null
            sudo systemctl disable "${SERVICE}.service" 2>/dev/null
            sudo rm -f "${SERVICE_FILE}"
            sudo systemctl daemon-reload 2>/dev/null
            sudo rm -rf "/etc/workstation-probe-live-test"
            sudo rm -f "${BIN_DEST}"
            sudo pkill -f "${BIN_DEST}" 2>/dev/null
            ;;
        direct)
            if [ -f "${PID_FILE}" ]; then
                pid="$(cat "${PID_FILE}")"
                kill "${pid}" 2>/dev/null
                # give it up to 3s to exit cleanly
                for _ in 1 2 3; do
                    kill -0 "${pid}" 2>/dev/null || break
                    sleep 1
                done
                kill -9 "${pid}" 2>/dev/null
            fi
            rm -f "${BIN_DEST}" "${PID_FILE}"
            rm -rf "$(dirname "${CONFIG_FILE}")"
            ;;
    esac
    echo "$LOG_PREFIX Cleanup done."
}
trap cleanup EXIT INT TERM

# ---------------------------------------------------------------------------
# Preflight
# ---------------------------------------------------------------------------
need() { command -v "$1" >/dev/null 2>&1 || { echo "$LOG_PREFIX Missing: $1"; exit 1; }; }
need make
need curl
need python3

if [ -z "${SKIP_INSTALL:-}" ] && ss -lnt 2>/dev/null | awk '{print $4}' | grep -q ":${PORT}\$"; then
    echo "$LOG_PREFIX Port ${PORT} is already in use; pick another via PORT="
    exit 1
fi

# ---------------------------------------------------------------------------
# 1. Build
# ---------------------------------------------------------------------------
echo "$LOG_PREFIX Building…"
( cd "${REPO_ROOT}" && make build )

# ---------------------------------------------------------------------------
# 2-4. Install + (service | launch)
# ---------------------------------------------------------------------------
if [ -z "${SKIP_INSTALL:-}" ]; then
    echo "$LOG_PREFIX Installing binary to ${BIN_DEST}"
    ${SUDO} install -m 0755 -D "${BIN_SRC}" "${BIN_DEST}"

    echo "$LOG_PREFIX Writing config to ${CONFIG_FILE}"
    if [ "$MODE" = systemd ]; then
        ${SUDO} mkdir -p "$(dirname "${CONFIG_FILE}")"
        ${SUDO} tee "${CONFIG_FILE}" >/dev/null <<EOF
server:
  host: 127.0.0.1
  port: ${PORT}
sampler:
  interval: 1s
  history_capacity: 30
modules:
  cpu:    {enabled: true}
  memory: {enabled: true}
  gpu:    {enabled: true}
  storage:
    enabled: true
    mount_points:
      - {path: /, alias: root}
security:
  rate_limit:
    enabled: true
    requests_per_second: 10
    burst: 20
    trust_proxy_headers: false
    exempt_paths: [/health]
logging:
  level: info
  format: json
EOF
        echo "$LOG_PREFIX Rendering ${SERVICE_FILE}"
        ${SUDO} sed -e "s/@USER@/${USER:-$(id -un)}/g" \
                    -e "s|/usr/local/bin/workstation-probe|${BIN_DEST}|g" \
                    -e "s|/etc/workstation-probe/config.yaml|${CONFIG_FILE}|g" \
                    "${SERVICE_TEMPLATE}" | ${SUDO} tee "${SERVICE_FILE}" >/dev/null
        ${SUDO} systemctl daemon-reload
        ${SUDO} systemctl enable --now "${SERVICE}.service"
    else
        mkdir -p "$(dirname "${CONFIG_FILE}")"
        cat > "${CONFIG_FILE}" <<EOF
server:
  host: 127.0.0.1
  port: ${PORT}
sampler:
  interval: 1s
  history_capacity: 30
modules:
  cpu:    {enabled: true}
  memory: {enabled: true}
  gpu:    {enabled: true}
  storage:
    enabled: true
    mount_points:
      - {path: /, alias: root}
security:
  rate_limit:
    enabled: true
    requests_per_second: 10
    burst: 20
    trust_proxy_headers: false
    exempt_paths: [/health]
logging:
  level: info
  format: json
EOF
        echo "$LOG_PREFIX Launching ${BIN_DEST} directly (PID file ${PID_FILE})"
        nohup "${BIN_DEST}" -config "${CONFIG_FILE}" >/dev/null 2>&1 &
        echo $! > "${PID_FILE}"
    fi
fi

# ---------------------------------------------------------------------------
# 5. Wait for readiness
# ---------------------------------------------------------------------------
URL="http://localhost:${PORT}"
echo "$LOG_PREFIX Waiting for ${URL}/health …"
ready=0
for i in $(seq 1 30); do
    if curl -sf -o /dev/null "${URL}/health"; then
        ready=1
        echo "$LOG_PREFIX Ready after ${i}s"
        break
    fi
    sleep 1
done
if [ "$ready" -ne 1 ]; then
    echo "$LOG_PREFIX Service did not become ready in 30s"
    if [ "$MODE" = systemd ]; then
        ${SUDO} journalctl -u "${SERVICE}.service" -n 30 --no-pager 2>/dev/null || true
    else
        echo "$LOG_PREFIX PID file:"
        cat "${PID_FILE}" 2>/dev/null || true
    fi
    exit 1
fi

# ---------------------------------------------------------------------------
# 6. Probe + human-readable summary
# ---------------------------------------------------------------------------
echo
echo "==================  /profile (raw JSON)  =================="
curl -sf "${URL}/profile" | python3 -m json.tool

echo
echo "==================  Human-readable status  =================="
python3 <<EOF
import json, urllib.request

URL = "${URL}"

def get(path):
    return json.loads(urllib.request.urlopen(URL + path, timeout=3).read())

profile = get("/profile")
metrics = get("/metrics")
health  = get("/health")

print(f"Hostname: {profile.get('hostname', '?')}")
print(f"Go:       {profile.get('go_version', '?')}")
print(f"Sampler:  {profile['sampler']['interval_ms']} ms interval, "
      f"{profile['sampler']['history_capacity']} samples history")
print(f"Health:   {health.get('status', '?')}")
print()

def gb(n): return n / (1024.0 ** 3)

# CPU
cpu_p = profile['modules']['cpu']
cpu_m = metrics.get('cpu', {})
print("=== CPU ===")
if cpu_p.get('enabled'):
    print(f"  Model:   {cpu_p.get('model_name', '?')}")
    print(f"  Vendor:  {cpu_p.get('vendor', '?')}")
    print(f"  Cores:   {cpu_p.get('core_count', '?')}")
    if cpu_m.get('error'):
        print(f"  Error:   {cpu_m['error']}")
    else:
        print(f"  Overall: {cpu_m.get('overall_percent', 0):.1f}%")
        per = cpu_m.get('per_core_percent') or []
        if per:
            preview = ' '.join(f"{v:5.1f}%" for v in per[:10])
            if len(per) > 10:
                preview += f" … ({len(per)} total)"
            print(f"  Per-core: {preview}")
else:
    print(f"  Disabled: {cpu_p.get('disabled_reason', '?')}")
print()

# Memory
mem_p = profile['modules']['memory']
mem_m = metrics.get('memory', {})
print("=== Memory ===")
if mem_p.get('enabled'):
    if mem_m.get('error'):
        print(f"  Error: {mem_m['error']}")
    else:
        print(f"  Total:     {gb(mem_p.get('total_bytes', 0)):6.1f} GiB")
        print(f"  Used:      {gb(mem_m.get('used_bytes', 0)):6.1f} GiB "
              f"({mem_m.get('used_percent', 0):.1f}%)")
        print(f"  Available: {gb(mem_m.get('available_bytes', 0)):6.1f} GiB")
        sw_total = mem_m.get('swap_total_bytes', 0)
        if sw_total > 0:
            print(f"  Swap:      {gb(mem_m.get('swap_used_bytes', 0)):6.1f} / "
                  f"{gb(sw_total):.1f} GiB used")
else:
    print(f"  Disabled: {mem_p.get('disabled_reason', '?')}")
print()

# GPU
gpu_p = profile['modules']['gpu']
gpu_m = metrics.get('gpu')
print("=== GPU ===")
if gpu_p.get('enabled'):
    for d in gpu_p.get('devices', []):
        print(f"  [{d.get('index', '?')}] {d.get('name', '?')}  "
              f"{gb(d.get('memory_total_bytes', 0)):.1f} GiB")
    if gpu_m:
        for d in gpu_m.get('devices', []):
            err = d.get('error')
            if err:
                print(f"    [{d.get('index', '?')}] error: {err}")
                continue
            print(f"    [{d.get('index', '?')}] util={d.get('utilization_gpu_percent', 0):.0f}%  "
                  f"mem={d.get('utilization_memory_percent', 0):.0f}%  "
                  f"temp={d.get('temperature_c', 0):.0f}°C  "
                  f"power={d.get('power_draw_watts', 0):.0f}W  "
                  f"vram={gb(d.get('memory_used_bytes', 0)):.1f}/"
                  f"{gb(d.get('memory_total_bytes', 0)):.1f} GiB")
else:
    print(f"  Disabled: {gpu_p.get('disabled_reason', '?')}")
print()

# Storage
st_p = profile['modules']['storage']
st_m = metrics.get('storage', {})
print("=== Storage ===")
if st_p.get('enabled'):
    by_path = {d.get('path', ''): d for d in st_m.get('disks', [])}
    for mp in st_p.get('mount_points', []):
        path = mp.get('path', '?')
        alias = mp.get('alias', '')
        device = mp.get('device', '?')
        fstype = mp.get('fstype', '?')
        d = by_path.get(path, {})
        head = f"  {path}"
        if alias:
            head += f" ({alias})"
        head += f"  [{fstype} on {device}]"
        print(head)
        if d.get('error'):
            print(f"    Error: {d['error']}")
        else:
            print(f"    Total: {gb(d.get('total_bytes', 0)):6.1f} GiB  "
                  f"Used: {gb(d.get('used_bytes', 0)):6.1f} GiB "
                  f"({d.get('used_percent', 0):.1f}%)  "
                  f"Free: {gb(d.get('free_bytes', 0)):6.1f} GiB")
else:
    print(f"  Disabled: {st_p.get('disabled_reason', '?')}")

print()
print("--- /health per-module freshness ---")
for name, m in health.get('modules', {}).items():
    if m.get('enabled'):
        print(f"  {name:8s} ok   last sample {m.get('last_sample_age_ms', 0):>5d} ms ago")
    else:
        print(f"  {name:8s} disabled ({m.get('disabled_reason', '?')})")
EOF

echo
echo "==================  /metrics/cpu/history?duration=10s  =================="
curl -sf "${URL}/metrics/cpu/history?duration=10s&limit=5" | python3 -c "
import json, sys
d = json.loads(sys.stdin.read())
print(f'  count={d[\"count\"]} window={d[\"window_seconds\"]}s')
for s in d.get('samples', []):
    print(f'    {s[\"timestamp\"]}  overall={s[\"overall_percent\"]:5.1f}%')
"

echo
echo "==================  Done  =================="
echo "$LOG_PREFIX All probes succeeded. Cleanup will run on exit."

exit 0
