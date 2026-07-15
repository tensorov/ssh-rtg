# Traefik Config Sync (rsync)

One-way rsync replication of Traefik dynamic config from the **primary VPS** (A,
where `rtg-orchestrator` runs) to the **backup VPS** (H).  Runs every 60 seconds
via a systemd timer.  On failover, H already has the latest config and Traefik
continues serving without interruption.

## Prerequisites

- `rsync` on both VPSes (`apt install rsync`)
- SSH key pair on the **destination** (backup) VPS
- The public key installed in `authorized_keys` on the **source** (primary) VPS

## Installation

### 1. Generate an SSH key on the backup VPS (destination)

```bash
ssh-keygen -t ed25519 -f /root/.ssh/sync-key -N ""
```

### 2. Copy the public key to the primary VPS (source)

```bash
ssh-copy-id -i /root/.ssh/sync-key.pub root@<PRIMARY_VPS_IP>
```

Verify passwordless login works:

```bash
ssh -i /root/.ssh/sync-key -o StrictHostKeyChecking=accept-new root@<PRIMARY_VPS_IP> "echo OK"
```

### 3. Set the source host via EnvironmentFile

Create `/etc/default/sync-traefik-config`:

```bash
SYNC_SRC_HOST=<PRIMARY_VPS_IP_OR_HOSTNAME>
```

Optional overrides (defaults shown):

```bash
SYNC_SRC_DIR=/etc/traefik/dynamic/
SYNC_DEST_DIR=/etc/traefik/dynamic/
SYNC_SSH_KEY=/root/.ssh/sync-key
```

### 4. Install systemd units

```bash
cp sync-traefik-config.service /etc/systemd/system/
cp sync-traefik-config.timer   /etc/systemd/system/
cp sync-traefik-config.sh      /usr/local/bin/sync-traefik-config.sh
chmod 755 /usr/local/bin/sync-traefik-config.sh
systemctl daemon-reload
systemctl enable --now sync-traefik-config.timer
```

### 5. Verify

```bash
systemctl status sync-traefik-config.timer
journalctl -u sync-traefik-config.service --since "1 minute ago"
```

The timer should show `active (waiting)` and the service should have exited
cleanly at least once.

## Testing

### Manual sync

```bash
/usr/local/bin/sync-traefik-config.sh
echo $?   # 0 = success
```

### Force a sync cycle

```bash
systemctl start sync-traefik-config.service
journalctl -u sync-traefik-config.service -n 20 --no-pager
```

## Recovery

If the primary VPS is rebuilt from scratch and needs to pull config **back** from
the backup, run rsync manually in the **opposite** direction once:

```bash
rsync -az --delete -e "ssh -i /root/.ssh/sync-key" \
  root@<BACKUP_VPS_IP>:/etc/traefik/dynamic/ /etc/traefik/dynamic/
systemctl reload traefik
```

## File layout

```
deploy/sync/
├── sync-traefik-config.sh      # rsync wrapper script
├── sync-traefik-config.service  # systemd oneshot service
├── sync-traefik-config.timer    # systemd timer (every 1 min)
└── README.md                    # this file
```
