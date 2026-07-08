#!/usr/bin/env bash
# scripts/rootless-install.sh — rootless/user install of workstation-probe.
#
# Builds the binary, installs to ~/.local/bin/workstation-probe, creates config at
# ~/.config/workstation-probe/config.yaml, and installs + starts a systemd user service.
# No root required.
#
# Usage:
#   ./scripts/rootless-install.sh              # full install + start
#   ./scripts/rootless-install.sh --no-start   # install without starting
#   ./scripts/rootless-install.sh --port 9090  # install with custom port
#   ./scripts/rootless-install.sh -h           # show usage

set -euo pipefail

HERE="$(cd "$(dirname "$0")/.." && pwd)"
cd "$HERE"

# ---------------------------------------------------------------------------
# Defaults
# ---------------------------------------------------------------------------
BIN_NAME="workstation-probe"
INSTALL_BIN="${HOME}/.local/bin/${BIN_NAME}"
CONFIG_DIR="${HOME}/.config/workstation-probe"
CONFIG_FILE="${CONFIG_DIR}/config.yaml"
SYSTEMD_USER_DIR="${HOME}/.config/systemd/user"
SERVICE_FILE="${SYSTEMD_USER_DIR}/${BIN_NAME}.service"
CONFIG_EXAMPLE="${HERE}/config.example.yaml"
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
Usage: $0 [OPTIONS]

Build and install workstation-probe (monitor) as a user service (rootless).
No root privileges required.

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
fi
if [ -f "$SERVICE_FILE" ]; then
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
mkdir -p "$(dirname "${INSTALL_BIN}")"
install -m 0755 "${HERE}/${BIN_NAME}" "${INSTALL_BIN}"
info "Binary installed."

# Ensure ~/.local/bin is in PATH for this session
case ":$PATH:" in
    *:"${HOME}/.local/bin":*) ;;
    *) export PATH="${HOME}/.local/bin:${PATH}" ;;
esac

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
# Systemd user service
# ---------------------------------------------------------------------------
info "Creating systemd user service at ${SERVICE_FILE}..."
mkdir -p "${SYSTEMD_USER_DIR}"
cat > "${SERVICE_FILE}" <<SERVICEEOF
[Unit]
Description=System Load Monitor (CPU/GPU/Memory/Storage) — User
After=network-online.target
Wants=network-online.target
StartLimitIntervalSec=60
StartLimitBurst=3

[Service]
Type=simple
ExecStart=${INSTALL_BIN} -config ${CONFIG_FILE}
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal

# Sandbox: limit what a compromised process can do. ProtectSystem=strict keeps
# /etc, /usr, and /boot read-only; this service only needs to read its config
# and writes logs to journald. MemoryDenyWriteExecute is deliberately omitted:
# the optional NVML-backed GPU module dlopen()s libnvidia-ml and some
# driver-side code paths can produce transient W^X mappings.
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=read-only
PrivateTmp=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
RestrictNamespaces=true
LockPersonality=true

[Install]
WantedBy=default.target
SERVICEEOF
systemctl --user daemon-reload
info "Service file created."

# ---------------------------------------------------------------------------
# loginctl enable-linger
# ---------------------------------------------------------------------------
if command -v loginctl >/dev/null 2>&1; then
    if ! loginctl enable-linger 2>/dev/null; then
        warn "loginctl enable-linger failed. User services may stop when you log out."
    else
        info "Linger enabled for user ${USER}."
    fi
else
    warn "loginctl not found. User services may stop when you log out."
fi

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
    info "Enabling and starting user service..."
    systemctl --user enable --now "${BIN_NAME}.service"
    info "User service enabled and started."
    info "Check status with: systemctl --user status ${BIN_NAME}"
    info "View logs with:    journalctl --user -u ${BIN_NAME} -f"
else
    info "Skipping service start (--no-start)."
    info "Start manually with: systemctl --user enable --now ${BIN_NAME}"
fi

info "Installation complete."
