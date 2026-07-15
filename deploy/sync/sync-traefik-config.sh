#!/bin/bash
set -euo pipefail

# rsync Traefik dynamic config from the primary VPS (source) to the
# backup VPS (destination).  The source host runs `rtg-orchestrator` which
# writes rtg-services.yml; this script pulls that file (and any other
# *.yml in the dynamic dir) so both VPSes serve the same routes.
#
# On a full config-drift recovery (e.g. after primary rebuild), run
# rsync in the OPPOSITE direction manually once:
#   rsync -az --delete -e "ssh -i /root/.ssh/sync-key" root@<H>:<SRC_DIR>/ <DEST_DIR>
#   systemctl reload traefik

readonly SRC_HOST="${SYNC_SRC_HOST:?SYNC_SRC_HOST not set}"
readonly SRC_DIR="${SYNC_SRC_DIR:-/etc/traefik/dynamic/}"
readonly DEST_DIR="${SYNC_DEST_DIR:-/etc/traefik/dynamic/}"
readonly SSH_KEY="${SYNC_SSH_KEY:-/root/.ssh/sync-key}"

rsync -az --delete -e \
  "ssh -i ${SSH_KEY} -o StrictHostKeyChecking=accept-new -o BatchMode=yes" \
  "root@${SRC_HOST}:${SRC_DIR}" "${DEST_DIR}"

systemctl reload traefik
