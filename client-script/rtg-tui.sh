#!/bin/bash
# rtg-tui.sh — gum-based TUI for reverse SSH tunnel management
# ============================================================
# Provides a terminal UI for checking status, viewing port mappings,
# starting/stopping the tunnel service, and browsing logs.
#
# Requirements: gum (charmbracelet/gum) — https://github.com/charmbracelet/gum
#
# Usage:
#   bash rtg-tui.sh              Open the TUI menu
#   bash rtg-tui.sh --install    Run install.sh first if service missing
#   bash rtg-tui.sh --help       Show this help message
#
# Navigation:
#   Up/Down arrow keys to select · Enter to confirm · q to quit pager
#   Ctrl+C to exit · Escape to cancel a selection

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CONFIG_DIR="/etc/reverse-ssh-gateway"
SERVICE_NAME="ssh-tunnel"
SERVICE_UNIT="${SERVICE_NAME}.service"
INSTALL_SCRIPT="${SCRIPT_DIR}/install.sh"

# Gum style presets (arrays for safe word splitting)
GUM_ERR=(--foreground 9)
GUM_WARN=(--foreground 11)
GUM_OK=(--foreground 10)

# ────────────────────────────────────────────────────────────
# Prerequisite checks
# ────────────────────────────────────────────────────────────

check_gum() {
    if ! command -v gum >/dev/null 2>&1; then
        cat >&2 <<'GUM_EOF'
ERROR: gum (charmbracelet/gum) is required but not found.

  Install: https://github.com/charmbracelet/gum/releases

  Debian/Ubuntu:
    echo 'deb [signed-by=/usr/share/keyrings/charm.gpg] https://repo.charm.sh/apt/ * *' | \
      sudo tee /etc/apt/sources.list.d/charm.list
    sudo apt update && sudo apt install gum

  macOS:
    brew install gum
GUM_EOF
        exit 1
    fi
}

service_exists() {
    systemctl cat "${SERVICE_UNIT}" >/dev/null 2>&1
}

# ────────────────────────────────────────────────────────────
# Config discovery (same search order as tunnel.sh)
# ────────────────────────────────────────────────────────────

find_config() {
    if [ -n "${TUNNEL_CONFIG:-}" ]; then
        printf '%s' "${TUNNEL_CONFIG}"
    elif [ -f "${CONFIG_DIR}/config.env" ]; then
        printf '%s' "${CONFIG_DIR}/config.env"
    elif [ -f "${SCRIPT_DIR}/config.env" ]; then
        printf '%s' "${SCRIPT_DIR}/config.env"
    fi
}

# Parse PORTS variable from config (space-separated "remote=local" pairs)
parse_ports() {
    local cfg
    cfg="$(find_config)"
    [ -z "${cfg}" ] && return 0

    grep '^PORTS=' "${cfg}" 2>/dev/null | head -1 | cut -d= -f2- | tr -d '"' || true
}

# ────────────────────────────────────────────────────────────
# Screen: Status dashboard
# ────────────────────────────────────────────────────────────

show_status() {
    clear
    gum style --border double --padding "1 2" "Tunnel Status"
    echo ""

    gum spin --spinner dot --title "Checking tunnel..." -- sleep 0.5

    local status
    status="$(systemctl is-active "${SERVICE_NAME}" 2>/dev/null || echo "unknown")"

    case "${status}" in
        active)
            gum style "${GUM_OK[@]}" "● ACTIVE"
            echo ""

            # Uptime from systemd property
            local uptime_raw uptime
            uptime_raw="$(systemctl show "${SERVICE_NAME}" -p ActiveEnterTimestamp 2>/dev/null)"
            uptime="${uptime_raw#ActiveEnterTimestamp=}"
            if [ -n "${uptime}" ]; then
                echo "  Uptime:   ${uptime}"
            fi

            # PID of the tunnel process
            local pid_raw pid
            pid_raw="$(systemctl show "${SERVICE_NAME}" -p MainPID 2>/dev/null)"
            pid="${pid_raw#MainPID=}"
            if [ -n "${pid}" ] && [ "${pid}" -gt 1 ] 2>/dev/null; then
                echo "  PID:      ${pid}"
            fi

            echo ""
            echo "  Forwarded Ports:"

            local ports
            ports="$(parse_ports)"
            if [ -n "${ports}" ]; then
                # shellcheck disable=SC2086
                local entry remote local_p
                for entry in ${ports}; do
                    remote="${entry%=*}"
                    local_p="${entry#*=}"
                    echo "    :${remote} → 127.0.0.1:${local_p}"
                done
            else
                echo "    (none configured)"
            fi

            echo ""
            gum style "────────────────────────────"
            echo "  Service: ${SERVICE_NAME}"
            ;;

        inactive)
            gum style "${GUM_WARN[@]}" "● INACTIVE"
            echo ""
            echo "  The tunnel is stopped."
            echo "  Use 'Start/Stop' from the main menu to start it."
            ;;

        unknown)
            gum style "${GUM_ERR[@]}" "● NOT FOUND"
            echo ""
            echo "  Service '${SERVICE_NAME}' is not installed."
            echo "  Deploy it first with:"
            echo "    ${INSTALL_SCRIPT}"
            echo ""
            echo "  Or re-run with the --install flag."
            ;;

        *)
            gum style "${GUM_ERR[@]}" "● ${status}"
            ;;
    esac

    echo ""
    gum style --faint "Press any key to return to menu..."
    read -rs -n 1 2>/dev/null || true
}

# ────────────────────────────────────────────────────────────
# Screen: Port list table
# ────────────────────────────────────────────────────────────

show_ports() {
    clear
    gum style --border double --padding "1 2" "Port Forwarding"
    echo ""

    if ! service_exists; then
        gum style "${GUM_ERR[@]}" "Service '${SERVICE_NAME}' not found."
        echo ""
        gum style --faint "Press any key to return..."
        read -rs -n 1 2>/dev/null || true
        return
    fi

    local ports
    ports="$(parse_ports)"

    if [ -z "${ports}" ]; then
        gum style "${GUM_WARN[@]}" "No port mappings configured."
        echo ""
        gum style --faint "Press any key to return..."
        read -rs -n 1 2>/dev/null || true
        return
    fi

    local status_label
    status_label="$(systemctl is-active "${SERVICE_NAME}" 2>/dev/null || echo "inactive")"
    [ "${status_label}" = "active" ] && status_label="forwarded" || status_label="stopped"

    # Build and render table: gum table reads tab-separated stdin
    {
        printf 'PORT\tLOCAL\tSTATUS\n'
        # shellcheck disable=SC2086
        local entry remote local_p
        for entry in ${ports}; do
            remote="${entry%=*}"
            local_p="${entry#*=}"
            printf '%s\t127.0.0.1:%s\t%s\n' "${remote}" "${local_p}" "${status_label}"
        done
    } | gum table

    echo ""
    gum style --faint "Press any key to return..."
    read -rs -n 1 2>/dev/null || true
}

# ────────────────────────────────────────────────────────────
# Screen: Start / Stop / Restart
# ────────────────────────────────────────────────────────────

manage_tunnel() {
    clear
    gum style --border double --padding "1 2" "Tunnel Control"
    echo ""

    if ! service_exists; then
        gum style "${GUM_ERR[@]}" "Service '${SERVICE_NAME}' not found."
        echo ""
        gum style --faint "Press any key to return..."
        read -rs -n 1 2>/dev/null || true
        return
    fi

    local action
    action="$(gum choose "Start" "Stop" "Restart" "Cancel")"
    echo ""

    case "${action}" in
        Start)
            if gum confirm "Start the tunnel service?"; then
                gum spin --spinner dot --title "Starting..." -- systemctl start "${SERVICE_NAME}"
                local s
                s="$(systemctl is-active "${SERVICE_NAME}" 2>/dev/null || echo "failed")"
                if [ "${s}" = "active" ]; then
                    gum style "${GUM_OK[@]}" "Tunnel started."
                else
                    gum style "${GUM_ERR[@]}" "Failed to start tunnel."
                fi
            fi
            ;;

        Stop)
            if gum confirm "Stop the tunnel service?"; then
                gum spin --spinner dot --title "Stopping..." -- systemctl stop "${SERVICE_NAME}"
                local s
                s="$(systemctl is-active "${SERVICE_NAME}" 2>/dev/null || echo "inactive")"
                if [ "${s}" = "inactive" ]; then
                    gum style "${GUM_WARN[@]}" "Tunnel stopped."
                else
                    gum style "${GUM_ERR[@]}" "Failed to stop tunnel."
                fi
            fi
            ;;

        Restart)
            if gum confirm "Restart the tunnel service?"; then
                gum spin --spinner dot --title "Restarting..." -- systemctl restart "${SERVICE_NAME}"
                local s
                s="$(systemctl is-active "${SERVICE_NAME}" 2>/dev/null || echo "failed")"
                if [ "${s}" = "active" ]; then
                    gum style "${GUM_OK[@]}" "Tunnel restarted."
                else
                    gum style "${GUM_ERR[@]}" "Failed to restart tunnel."
                fi
            fi
            ;;

        Cancel|"")
            # User pressed Escape or selected Cancel
            return
            ;;
    esac

    echo ""
    gum style --faint "Press any key to return to menu..."
    read -rs -n 1 2>/dev/null || true
}

# ────────────────────────────────────────────────────────────
# Screen: Log viewer
# ────────────────────────────────────────────────────────────

show_logs() {
    clear
    gum style --border double --padding "1 2" "Tunnel Logs"
    echo ""

    if ! service_exists; then
        gum style "${GUM_ERR[@]}" "Service '${SERVICE_NAME}' not found."
        echo ""
        gum style --faint "Press any key to return..."
        read -rs -n 1 2>/dev/null || true
        return
    fi

    # Quick check for existing journal entries
    local sample
    sample="$(journalctl -u "${SERVICE_NAME}" --no-pager -n 1 2>&1 || true)"
    if echo "${sample}" | grep -qi "no entries"; then
        gum style "${GUM_WARN[@]}" "No logs found for '${SERVICE_NAME}'."
        echo ""
        gum style --faint "Press any key to return..."
        read -rs -n 1 2>/dev/null || true
        return
    fi

    # Display last 50 lines in gum pager — press 'q' to exit pager
    journalctl -u "${SERVICE_NAME}" --no-pager -n 50 2>/dev/null | gum pager
}

# ────────────────────────────────────────────────────────────
# CLI flag parsing
# ────────────────────────────────────────────────────────────

INSTALL_MODE=0

while [ $# -gt 0 ]; do
    case "$1" in
        --install)
            INSTALL_MODE=1
            shift
            ;;
        --help|-h)
            cat <<'HELP'
Usage: bash rtg-tui.sh [OPTIONS]

A gum-based terminal UI for managing the reverse SSH tunnel service.

Options:
  --install   Run install.sh if the tunnel service is not installed
  --help, -h  Show this help message

Navigation:
  Up/Down arrow keys  Select menu item
  Enter               Confirm selection
  q                   Quit the log pager
  Ctrl+C              Exit the TUI
  Escape              Cancel a selection

Menu items:
  Status      Live status dashboard (active / inactive / not found)
  Ports       Table view of forwarded port mappings
  Start/Stop  Start, stop, or restart the tunnel service
  Logs        View recent service logs (last 50 lines)
  Exit        Cleanly exit the TUI
HELP
            exit 0
            ;;
        *)
            echo "Unknown option: $1" >&2
            echo "Use --help for usage." >&2
            exit 1
            ;;
    esac
done

# ────────────────────────────────────────────────────────────
# Main
# ────────────────────────────────────────────────────────────

check_gum

# --install mode: bootstrap the tunnel service if missing
if [ "${INSTALL_MODE}" -eq 1 ]; then
    if service_exists 2>/dev/null; then
        gum style "${GUM_OK[@]}" "Service '${SERVICE_NAME}' already installed."
        echo ""
    else
        if [ -f "${INSTALL_SCRIPT}" ]; then
            if gum confirm "Tunnel service not found. Run install.sh now?"; then
                bash "${INSTALL_SCRIPT}"
            fi
        else
            gum style "${GUM_ERR[@]}" "install.sh not found at ${INSTALL_SCRIPT}"
            echo ""
            gum style --faint "Press any key to exit..."
            read -rs -n 1 2>/dev/null || true
            exit 1
        fi
    fi
fi

# Main navigation loop
while true; do
    clear
    gum style \
        --border thick \
        --padding "1 2" \
        --margin "1" \
        --align center \
        "Reverse SSH Gateway TUI"
    echo ""

    choice="$(gum choose "Status" "Ports" "Start/Stop" "Logs" "Exit")"
    echo ""

    case "${choice}" in
        "Status")
            show_status
            ;;
        "Ports")
            show_ports
            ;;
        "Start/Stop")
            manage_tunnel
            ;;
        "Logs")
            show_logs
            ;;
        "Exit")
            clear
            if gum confirm "Exit TUI?"; then
                clear
                exit 0
            fi
            ;;
    esac
done
