#!/bin/bash
# reverse-ssh-gateway — standalone client installer
# ==================================================
# Copies tunnel.sh to /usr/local/bin/, installs a systemd service unit,
# and optionally writes config with provided CLI parameters.
#
# Usage:
#   ./install.sh                                                # interactive (prompts for missing values)
#   ./install.sh --jump-host tunnel@vps.example.com             # minimal, uses default PORTS
#   ./install.sh --jump-host tunnel@vps.example.com --ports 2022:22,8080:8080
#   ./install.sh --config /path/to/custom.conf                  # use existing config
#   ./install.sh --help                                         # this message
#
# Flags:
#   --jump-host USER@HOST       VPS jump host (sets GATEWAY_HOST)
#   --ports  REMOTE:LOCAL,...   Comma-separated port mappings
#   --config PATH               Path to config.env to install
#   --service-name NAME         Systemd service name (default: ssh-tunnel)
#   --skip-gum-check            Skip the gum dependency check (for CI)
#   --uninstall                 Remove the service and all installed files
#   --help                      Show this message

set -euo pipefail

# ===========================================================================
# Defaults
# ===========================================================================
BIN_DIR="/usr/local/bin"
CONFIG_DIR="/etc/reverse-ssh-gateway"
SERVICE_NAME="ssh-tunnel"
JUMP_HOST=""
PORTS_STR=""
CONFIG_SRC=""
DO_UNINSTALL=0
SKIP_GUM_CHECK=0

# ===========================================================================
# Parse CLI flags
# ===========================================================================
while [ $# -gt 0 ]; do
    case "$1" in
        --jump-host)
            shift
            JUMP_HOST="$1"
            ;;
        --ports)
            shift
            PORTS_STR="$1"
            ;;
        --config)
            shift
            CONFIG_SRC="$1"
            ;;
        --service-name)
            shift
            SERVICE_NAME="$1"
            ;;
        --uninstall)
            DO_UNINSTALL=1
            ;;
        --skip-gum-check)
            SKIP_GUM_CHECK=1
            ;;
        --help|-h)
            sed -n '/^# Usage:/,/^$/p' "$0" | sed 's/^# //; s/^#$//'
            exit 0
            ;;
        *)
            echo "ERROR: Unknown flag '$1'. Use --help for usage." >&2
            exit 1
            ;;
    esac
    shift
done

# ===========================================================================
# Uninstall mode
# ===========================================================================
if [ "${DO_UNINSTALL}" -eq 1 ]; then
    echo "=== Uninstalling reverse-ssh-gateway client ==="

    if systemctl is-enabled "${SERVICE_NAME}" >/dev/null 2>&1; then
        echo "  Stopping and disabling ${SERVICE_NAME} service..."
        systemctl stop "${SERVICE_NAME}" 2>/dev/null || true
        systemctl disable "${SERVICE_NAME}" 2>/dev/null || true
    fi

    rm -f "${BIN_DIR}/tunnel.sh"
    rm -f "/etc/systemd/system/${SERVICE_NAME}.service"
    systemctl daemon-reload 2>/dev/null || true

    if [ -d "${CONFIG_DIR}" ]; then
        rm -rf "${CONFIG_DIR}"
    fi

    echo "  Done. Removed:"
    echo "    ${BIN_DIR}/tunnel.sh"
    echo "    /etc/systemd/system/${SERVICE_NAME}.service"
    echo "    ${CONFIG_DIR}/"
    exit 0
fi

# ===========================================================================
# Resolve config source
# ===========================================================================
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

if [ -z "${CONFIG_SRC}" ]; then
    CONFIG_SRC="${SCRIPT_DIR}/config.env"
fi

# Build a temporary config if CLI flags are provided
TMP_CONFIG=""
if [ -n "${JUMP_HOST}" ] || [ -n "${PORTS_STR}" ]; then
    TMP_CONFIG="$(mktemp)"

    # Start with the stock config as base
    if [ -f "${CONFIG_SRC}" ]; then
        cp "${CONFIG_SRC}" "${TMP_CONFIG}"
    fi

    # Set jump host
    if [ -n "${JUMP_HOST}" ]; then
        if grep -q '^GATEWAY_HOST=' "${TMP_CONFIG}" 2>/dev/null; then
            sed -i "s|^GATEWAY_HOST=.*|GATEWAY_HOST=\"${JUMP_HOST}\"|" "${TMP_CONFIG}"
        else
            echo "GATEWAY_HOST=\"${JUMP_HOST}\"" >> "${TMP_CONFIG}"
        fi
    fi

    # Set port mappings: convert comma-separated "remote:local" -> "remote=local"
    if [ -n "${PORTS_STR}" ]; then
        PARSED=""
        _IFS_SAVE="${IFS}"
        IFS=','
        for _pair in ${PORTS_STR}; do
            _remote="${_pair%:*}"
            _local="${_pair#*:}"
            if [ -n "${_remote}" ] && [ -n "${_local}" ]; then
                if [ -n "${PARSED}" ]; then
                    PARSED="${PARSED} ${_remote}=${_local}"
                else
                    PARSED="${_remote}=${_local}"
                fi
            fi
        done
        IFS="${_IFS_SAVE}"

        if [ -n "${PARSED}" ]; then
            if grep -q '^PORTS=' "${TMP_CONFIG}" 2>/dev/null; then
                sed -i "s|^PORTS=.*|PORTS=\"${PARSED}\"|" "${TMP_CONFIG}"
            else
                echo "PORTS=\"${PARSED}\"" >> "${TMP_CONFIG}"
            fi
        fi
    fi

    CONFIG_SRC="${TMP_CONFIG}"
fi

# Verify config exists
if [ ! -f "${CONFIG_SRC}" ]; then
    echo "ERROR: config file not found: ${CONFIG_SRC}" >&2
    echo "  Provide one with --config, or run from the client-script/ directory." >&2
    echo "  Use --help for usage." >&2
    exit 1
fi

# ===========================================================================
# Check prerequisites
# ===========================================================================
if ! command -v ssh >/dev/null 2>&1; then
    echo "ERROR: ssh (OpenSSH client) is required but not found." >&2
    exit 1
fi

if [ "${SKIP_GUM_CHECK}" -eq 0 ]; then
    if ! command -v gum >/dev/null 2>&1; then
        echo "ERROR: gum is required but not found." >&2
        echo "  Install it from https://github.com/charmbracelet/gum/releases" >&2
        echo "" >&2
        echo "  Debian/Ubuntu:" >&2
        echo "    echo 'deb [signed-by=/usr/share/keyrings/charm.gpg] https://repo.charm.sh/apt/ * *' | sudo tee /etc/apt/sources.list.d/charm.list" >&2
        echo "    sudo apt update && sudo apt install gum" >&2
        echo "" >&2
        echo "  macOS:" >&2
        echo "    brew install gum" >&2
        echo "" >&2
        echo "  Or skip this check with --skip-gum-check." >&2
        exit 1
    fi
fi

if [ ! -d "${BIN_DIR}" ]; then
    mkdir -p "${BIN_DIR}"
fi

if [ ! -d "${CONFIG_DIR}" ]; then
    mkdir -p "${CONFIG_DIR}"
fi

# ===========================================================================
# Install files
# ===========================================================================
echo "=== Installing reverse-ssh-gateway client ==="

INSTALLED_SCRIPT="${BIN_DIR}/tunnel.sh"
cp "${SCRIPT_DIR}/tunnel.sh" "${INSTALLED_SCRIPT}"
chmod 755 "${INSTALLED_SCRIPT}"
echo "  Script: ${INSTALLED_SCRIPT}"

INSTALLED_CONFIG="${CONFIG_DIR}/config.env"
cp "${CONFIG_SRC}" "${INSTALLED_CONFIG}"
chmod 600 "${INSTALLED_CONFIG}"
echo "  Config: ${INSTALLED_CONFIG}"

# ===========================================================================
# Install systemd service unit
# ===========================================================================
UNIT_FILE="/etc/systemd/system/${SERVICE_NAME}.service"

cat > "${UNIT_FILE}" << UNITEOF
# reverse-ssh-gateway — systemd service unit for tunnel client.
# Installed by install.sh -- manual changes will be overwritten on reinstall.

[Unit]
Description=Reverse SSH Tunnel Client (${SERVICE_NAME})
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=${INSTALLED_SCRIPT}
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal
Environment=TUNNEL_CONFIG=${INSTALLED_CONFIG}

[Install]
WantedBy=multi-user.target
UNITEOF

echo "  Unit:   ${UNIT_FILE}"

systemctl daemon-reload
echo "  systemd daemon reloaded"

systemctl enable "${SERVICE_NAME}"
echo "  Service enabled: ${SERVICE_NAME}"

# Offer to start the service
echo ""
echo "=== Installation complete ==="
echo "  Service:  ${SERVICE_NAME}"
echo "  Script:   ${INSTALLED_SCRIPT}"
echo "  Config:   ${INSTALLED_CONFIG}"
echo "  Control:  systemctl {start|stop|restart|status} ${SERVICE_NAME}"
echo "  Logs:     journalctl -fu ${SERVICE_NAME}"
echo ""
echo "To start the tunnel now:  systemctl start ${SERVICE_NAME}"

# Clean up temp file
if [ -n "${TMP_CONFIG}" ]; then
    rm -f "${TMP_CONFIG}"
fi
