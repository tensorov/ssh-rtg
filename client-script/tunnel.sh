#!/bin/bash
# reverse-ssh-gateway — standalone tunnel client script
# ======================================================
# Establishes reverse SSH tunnels with auto-reconnect and exponential
# backoff.  Reads configuration from config.env (sourced on startup).
# Supports UDP→TCP bridging via socat and optional UFW rules.
#
# Usage:
#   TUNNEL_CONFIG=/path/to/config.env ./tunnel.sh
#   ./tunnel.sh          # uses default config paths (see below)
#
# Signal handlers:
#   SIGTERM / SIGINT     graceful shutdown (kills socat processes first)
#
# Config search order:
#   1. $TUNNEL_CONFIG environment variable
#   2. /etc/reverse-ssh-gateway/config.env
#   3. ./config.env (relative to script directory)

set -euo pipefail

# ===========================================================================
# Config file discovery
# ===========================================================================
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

if [ -n "${TUNNEL_CONFIG:-}" ]; then
    CONFIG_FILE="${TUNNEL_CONFIG}"
elif [ -f /etc/reverse-ssh-gateway/config.env ]; then
    CONFIG_FILE=/etc/reverse-ssh-gateway/config.env
elif [ -f "${SCRIPT_DIR}/config.env" ]; then
    CONFIG_FILE="${SCRIPT_DIR}/config.env"
else
    echo "ERROR: No config.env found. Set TUNNEL_CONFIG or place one at:" >&2
    echo "  /etc/reverse-ssh-gateway/config.env" >&2
    echo "  ${SCRIPT_DIR}/config.env" >&2
    exit 1
fi

# shellcheck source=/dev/null
. "${CONFIG_FILE}"

# ===========================================================================
# Validate required settings
# ===========================================================================
REQUIRED_VARS="GATEWAY_HOST SSH_KEY_PATH"
for _var in ${REQUIRED_VARS}; do
    eval "_val=\"\${${_var}:-}\""
    if [ -z "${_val}" ]; then
        echo "ERROR: ${_var} is not set in ${CONFIG_FILE}" >&2
        exit 1
    fi
done

# ===========================================================================
# Defaults
# ===========================================================================
: "${LOCAL_HOST:=127.0.0.1}"
: "${GATEWAY_PORT:=22}"
: "${SERVER_ALIVE_INTERVAL:=30}"
: "${SERVER_ALIVE_COUNT_MAX:=3}"
: "${PORTS:=}"
: "${UDP_BRIDGES:=}"
: "${UFW_OPEN:=}"
: "${SSH_EXTRA_OPTS:=}"
: "${TUNNEL_SERVICE_NAME:=ssh-tunnel}"

# ===========================================================================
# Logging — uses logger when running as a daemon, stdout otherwise
# ===========================================================================
log() {
    if [ -t 1 ]; then
        echo "$(date '+%Y-%m-%d %H:%M:%S') tunnel[$$]: $*"
    else
        logger -t "tunnel[$$]" "$@"
    fi
}

# ===========================================================================
# Build SSH -R flags from the PORTS variable
# PORTS is a space-separated list of "remote=local" pairs.
# ===========================================================================
R_FLAGS=""
for _entry in ${PORTS}; do
    _remote="${_entry%=*}"
    _local="${_entry#*=}"
    if [ -n "${_remote}" ] && [ -n "${_local}" ]; then
        R_FLAGS="${R_FLAGS} -R ${_remote}:${LOCAL_HOST}:${_local}"
    fi
done

# ===========================================================================
# UFW setup (optional)
# ===========================================================================
if [ -n "${UFW_OPEN}" ]; then
    for _port in ${UFW_OPEN}; do
        if command -v ufw >/dev/null 2>&1; then
            ufw allow "${_port}" 2>/dev/null || true
            log "UFW: port ${_port} allowed"
        else
            log "WARNING: ufw not found — skipping port ${_port}"
        fi
    done
fi

# ===========================================================================
# Cleanup handler — killed background socat and UFW teardown
# ===========================================================================
_caught_signal=0

cleanup() {
    _caught_signal=1
    log "Shutting down tunnel client (signal ${1:-TERM})"

    # Kill any running socat bridge processes we started
    if [ -n "${UDP_BRIDGES}" ]; then
        for _entry in ${UDP_BRIDGES}; do
            _local="${_entry%=*}"
            if [ -n "${_local}" ]; then
                pkill -f "socat.*UDP-RECVFROM:${_local}" 2>/dev/null || true
            fi
        done
    fi

    # Close the SSH control master socket
    rm -f "/tmp/ssh-tunnel-${TUNNEL_SERVICE_NAME}.sock" 2>/dev/null || true

    exit 0
}

trap 'cleanup TERM' SIGTERM
trap 'cleanup INT' SIGINT

# ===========================================================================
# UDP → TCP socat bridges (optional)
# ===========================================================================
if [ -n "${UDP_BRIDGES}" ]; then
    if ! command -v socat >/dev/null 2>&1; then
        log "WARNING: socat not found — UDP bridges will not start"
    else
        for _entry in ${UDP_BRIDGES}; do
            _local="${_entry%=*}"
            _remote="${_entry#*=}"
            if [ -n "${_local}" ] && [ -n "${_remote}" ]; then
                socat "UDP4-RECVFROM:${_local},fork" "TCP4:127.0.0.1:${_remote}" &
                log "UDP bridge started: :${_local}/udp -> tunnel :${_remote}/tcp"
            fi
        done
    fi
fi

# ===========================================================================
# Auto-reconnect SSH loop with exponential backoff
# Backoff: 1s -> 2s -> 4s -> 8s -> 16s -> 30s -> 60s (cap)
# On clean exit backoff resets to 1s.
# ===========================================================================
BACKOFF=1
MAX_BACKOFF=60

# shellcheck disable=SC2153
log "Starting reverse SSH tunnel to ${GATEWAY_HOST}:${GATEWAY_PORT}"
log "Ports:${R_FLAGS}"

while [ "${_caught_signal}" -eq 0 ]; do
    log "Connecting to ${GATEWAY_HOST} ..."

    # shellcheck disable=SC2086,SC2153
    # accept-new: accept unknown host keys on first connect, then pin.
    # This is a deliberate trade-off for unattended operation — the alternative
    # (StrictHostKeyChecking=no) would accept any key every time.
    # For higher security, pre-provision the known_hosts file and switch to
    # StrictHostKeyChecking=yes.
    if ssh \
        -i "${SSH_KEY_PATH}" \
        -p "${GATEWAY_PORT}" \
        -o ServerAliveInterval="${SERVER_ALIVE_INTERVAL}" \
        -o ServerAliveCountMax="${SERVER_ALIVE_COUNT_MAX}" \
        -o ExitOnForwardFailure=yes \
        -o StrictHostKeyChecking=accept-new \
        -o ControlMaster=auto \
        -o ControlPath="/tmp/ssh-tunnel-${TUNNEL_SERVICE_NAME}.sock" \
        -o ControlPersist=300 \
        ${SSH_EXTRA_OPTS} \
        -N \
        ${R_FLAGS} \
        "${GATEWAY_HOST}"; then

        log "Connection exited cleanly — reconnecting immediately"
        BACKOFF=1
    else
        _exit_code=$?
        log "Connection failed or dropped (exit=${_exit_code}). Reconnecting in ${BACKOFF}s ..."
        sleep "${BACKOFF}"

        # Exponential backoff with ceiling
        BACKOFF=$((BACKOFF * 2))
        if [ "${BACKOFF}" -gt "${MAX_BACKOFF}" ]; then
            BACKOFF=${MAX_BACKOFF}
        fi
    fi
done
