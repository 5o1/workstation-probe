#!/usr/bin/env bash
# scripts/install.sh — root/system install of workstation-probe.
#
# Builds the binary, installs to /usr/local/bin/workstation-probe, creates config at
# /etc/workstation-probe/config.yaml, and installs + starts a systemd service.
#
# Usage:
#   sudo ./scripts/install.sh              # full install + start
#   sudo ./scripts/install.sh --no-start   # install without starting
#   sudo ./scripts/install.sh --port 9090  # install with custom port
#   ./scripts/install.sh -h                # show usage

set -euo pipefail

HERE="$(cd "$(dirname "$0")/.." && pwd)"
cd "$HERE"

# ---------------------------------------------------------------------------
# Defaults
# ---------------------------------------------------------------------------
BIN_NAME="workstation-probe"
INSTALL_BIN="/usr/local/bin/${BIN_NAME}"
CONFIG_DIR="/etc/workstation-probe"
CONFIG_FILE="${CONFIG_DIR}/config.yaml"
SERVICE_FILE="/etc/systemd/system/${BIN_NAME}.service"
SERVICE_TEMPLATE="${HERE}/contrib/systemd/workstation-probe.service.in"
CONFIG_EXAMPLE="${HERE}/config.example.yaml"
MONITOR_USER="root"
DO_START=1
PORT=""

# ---------------------------------------------------------------------------
# Colors
# ---------------------------------------------------------------------------
RED='\033[1;31m'
GREEN='\033[1;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

info()  { echo -e "${GREEN}[INFO]${NC}  $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $*" >&2; }
err()   { echo -e "${RED}[ERROR]${NC} $*" >&2; }
fatal() { err "$@"; exit 1; }

# ---------------------------------------------------------------------------
# Usage
# ---------------------------------------------------------------------------
usage() {
    cat <<EOF
Usage: sudo $0 [OPTIONS]

Build and install workstation-probe (monitor) as a system service.
Requires root privileges.

Options:
  -h            Show this help and exit.
  --port N      Set the server listen port (default: 19090 from config.example.yaml).
  --no-start    Install the binary, config, and service, but do not start the service.

Installation paths:
  Binary:  ${INSTALL_BIN}
  Config:  ${CONFIG_FILE}
  Service: ${SERVICE_FILE}
EOF
    exit 0
}

# ---------------------------------------------------------------------------
# Root check
# ---------------------------------------------------------------------------
if [ "${EUID:-$(id -u)}" -ne 0 ]; then
    fatal "This script must be run as root. Use: sudo $0"
fi

# ---------------------------------------------------------------------------
# Parse flags
# ---------------------------------------------------------------------------
while [ $# -gt 0 ]; do
    case "$1" in
        -h) usage ;;
        --port)
            shift
            if [ $# -eq 0 ]; then
                fatal "--port requires a numeric argument."
            fi
            PORT="$1"
            ;;
        --no-start) DO_START=0 ;;
        *)
            fatal "Unknown option: $1 (use -h for help)"
            ;;
    esac
    shift
done

# Validate --port value
if [ -n "$PORT" ]; then
    if ! [[ "$PORT" =~ ^[0-9]+$ ]] || [ "$PORT" -lt 1 ] || [ "$PORT" -gt 65535 ]; then
        fatal "--port must be an integer between 1 and 65535, got: $PORT"
    fi
fi

# ---------------------------------------------------------------------------
# Preflight: check for existing installation
# ---------------------------------------------------------------------------
if [ -x "$INSTALL_BIN" ]; then
    warn "Binary already exists at ${INSTALL_BIN} — will be overwritten."
fi
if [ -f "$CONFIG_FILE" ]; then
    warn "Config already exists at ${CONFIG_FILE} — will be preserved."
    if [ -n "$PORT" ]; then
        warn "--port is ignored because config already exists; edit ${CONFIG_FILE} manually to change the port."
    fi
elif [ -f "$SERVICE_FILE" ]; then
    warn "Service file already exists at ${SERVICE_FILE} — will be overwritten."
fi

# ---------------------------------------------------------------------------
# Build
# ---------------------------------------------------------------------------
info "Building binary..."
if ! make build; then
    fatal "Build failed. Check the Go toolchain and try again."
fi
info "Build succeeded."

# ---------------------------------------------------------------------------
# Install binary
# ---------------------------------------------------------------------------
info "Installing binary to ${INSTALL_BIN}..."
install -m 0755 -D "${HERE}/${BIN_NAME}" "${INSTALL_BIN}"
info "Binary installed."

# ---------------------------------------------------------------------------
# Config
# ---------------------------------------------------------------------------
info "Setting up config at ${CONFIG_DIR}..."
mkdir -p "${CONFIG_DIR}"
if [ ! -f "$CONFIG_FILE" ]; then
    install -m 0644 "${CONFIG_EXAMPLE}" "${CONFIG_FILE}"
    info "Config created from ${CONFIG_EXAMPLE}."
    if [ -n "$PORT" ]; then
        info "Setting port to ${PORT} in config..."
        sed -i "s/^  port: [0-9]\+/  port: ${PORT}/" "${CONFIG_FILE}"
    fi
else
    info "Config already exists, preserving."
fi

# ---------------------------------------------------------------------------
# Systemd service
# ---------------------------------------------------------------------------
info "Creating systemd service at ${SERVICE_FILE}..."
sed "s/@USER@/${MONITOR_USER}/" "${SERVICE_TEMPLATE}" > "${SERVICE_FILE}"
systemctl daemon-reload
info "Service file created."

# ---------------------------------------------------------------------------
# Port check (before starting)
# ---------------------------------------------------------------------------
if [ "$DO_START" -eq 1 ]; then
    LISTEN_PORT="${PORT:-$(awk '/^  port: / {print $2; exit}' "${CONFIG_FILE}")}"
    if ss -lnt 2>/dev/null | awk '{print $4}' | grep -qE ":${LISTEN_PORT}\$"; then
        pid="$(ss -lntp 2>/dev/null | awk -v p=":${LISTEN_PORT}\$" '$4 ~ p {print $0}' | grep -oP 'pid=\K[0-9]+' | head -1 || true)"
        proc="$(ss -lntp 2>/dev/null | awk -v p=":${LISTEN_PORT}\$" '$4 ~ p {print $0}' | grep -oP '\("([^"]+)",pid=' | sed 's/.*"\([^"]*\)".*/\1/' | head -1 || true)"
        fatal "Port ${LISTEN_PORT} is already in use.${pid:+ pid=${pid}${proc:+  process=${proc}}} Stop the existing listener and retry, or use --port to choose a different port."
    fi
fi

# ---------------------------------------------------------------------------
# Enable and start
# ---------------------------------------------------------------------------
if [ "$DO_START" -eq 1 ]; then
    info "Enabling and starting service..."
    systemctl enable --now "${BIN_NAME}.service"
    info "Service enabled and started."
    info "Check status with: systemctl status ${BIN_NAME}"
    info "View logs with:    journalctl -u ${BIN_NAME} -f"
else
    info "Skipping service start (--no-start)."
    info "Start manually with: systemctl enable --now ${BIN_NAME}"
fi

info "Installation complete."
