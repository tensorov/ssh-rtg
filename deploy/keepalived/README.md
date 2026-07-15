# Keepalived VIP Failover Configuration

Auto-failover for Traefik between VPS A (primary) and VPS H (backup) using
Keepalived VRRP. A virtual IP (`10.0.0.10/24`) floats between the two VPSes
based on Traefik health.

## Architecture

```
        10.0.0.10/24 (VIP)
               |
    +----------+----------+
    |                     |
  VPS A                 VPS H
  (MASTER)              (BACKUP)
  priority 100          priority 90
  preempt               no preempt
    |                     |
    +--- VRID 51 ---------+
          VRRP advert
          every 1s
```

- Normal: VIP on VPS A. Traefik serves through the VIP.
- Failure: Traefik dies on VPS A → `chk_traefik` script fails for 4s → VPS H
  becomes MASTER, takes the VIP.
- Recovery: Traefik comes back on VPS A → `chk_traefik` passes for 4s →
  VPS A preempts, VIP moves back (primary recovery).

## Prerequisites

- **OS:** Debian 11+ (or any Keepalived-supported Linux)
- **Network:** Two VPSes on the same L2 segment (same broadcast domain).
  The virtual IP must be unused in your network.
- **VRID:** `51` — must be unique on the network segment. Change if 51 is
  already in use by another VRRP group.

## Installation

### 1. Install Keepalived

**Debian/Ubuntu:**
```bash
apt update && apt install -y keepalived
```

**macOS (testing only — not for production failover):**
```bash
brew install keepalived
```

**RHEL/CentOS/Fedora:**
```bash
dnf install -y keepalived
```

### 2. Copy configuration files

On **VPS A (primary):**
```bash
cp deploy/keepalived/keepalived-vps-a.conf /etc/keepalived/keepalived.conf
cp deploy/keepalived/keepalived.auth /etc/keepalived/keepalived.auth
chmod 600 /etc/keepalived/keepalived.auth
```

On **VPS H (backup):**
```bash
cp deploy/keepalived/keepalived-vps-h.conf /etc/keepalived/keepalived.conf
cp deploy/keepalived/keepalived.auth /etc/keepalived/keepalived.auth
chmod 600 /etc/keepalived/keepalived.auth
```

### 3. Set the VRRP password

Generate a secure 8-character password (VRRP protocol limit):

```bash
openssl rand -base64 12 | cut -c1-8
```

Then:

1. Edit `/etc/keepalived/keepalived.auth` — replace `REPLACE_WITH_SECURE_PASSWORD`
   with the generated password.
2. Edit `/etc/keepalived/keepalived.conf` — replace `REPLACE_WITH_SECURE_PASSWORD`
   with the **same** password.
3. Use the **same password on both VPS A and VPS H** — they must match.

> The `keepalived.auth` file exists as a secure reference so you never need
> to commit the password to version control. Both files must contain the
> same password.

### 4. Adjust interface (if needed)

The config uses `eth0` by default. Check your actual interface:

```bash
ip route show default
```

If your default route uses a different interface (e.g., `ens3`, `enp0s3`),
edit `/etc/keepalived/keepalived.conf` and change `interface eth0` accordingly.

### 5. Enable and start Keepalived

```bash
systemctl enable --now keepalived
```

Verify it is running:

```bash
systemctl status keepalived
```

Check the VIP is assigned:

```bash
ip addr show eth0 | grep 10.0.0.10
```

On VPS A (primary), you should see the VIP. On VPS H (backup), you should NOT
(unless failover has occurred).

## Testing Failover

### Test 1: Normal state

Before testing, verify the VIP is on VPS A:

```bash
# On VPS A — should show 10.0.0.10
ip addr show eth0 | grep 10.0.0.10

# On VPS H — should NOT show 10.0.0.10
ip addr show eth0 | grep 10.0.0.10
```

### Test 2: Simulate Traefik failure on primary

On **VPS A**, stop Traefik:

```bash
systemctl stop traefik
```

Wait up to 4 seconds (fall 2 × interval 2). Then verify:

```bash
# On VPS H — should now show 10.0.0.10 (VIP moved)
ip addr show eth0 | grep 10.0.0.10

# On VPS A — should NOT show 10.0.0.10 anymore
ip addr show eth0 | grep 10.0.0.10
```

Check Keepalived logs:

```bash
journalctl -u keepalived --since "1 minute ago" | grep -i "transition\|master\|backup"
```

### Test 3: Recovery — Traefik back on primary

On **VPS A**, start Traefik again:

```bash
systemctl start traefik
```

Wait up to 4 seconds (rise 2 × interval 2). Then verify the VIP moves back
(preempt is enabled on VPS A):

```bash
# On VPS A — should see 10.0.0.10 again
ip addr show eth0 | grep 10.0.0.10

# On VPS H — VIP should be released
ip addr show eth0 | grep 10.0.0.10
```

### Test 4: Full ping test

From a third machine on the same network segment:

```bash
# During normal operation — pings VPS A
ping -c 4 10.0.0.10

# During failover — pings VPS H (zero or one dropped packet during transition)
# Stop Traefik on VPS A and immediately ping:
ping 10.0.0.10
# Expect 1-3 missed pings during transition, then continued responses from VPS H
```

## Monitoring

### Check Keepalived status

```bash
systemctl status keepalived
```

### Watch VRRP transitions

```bash
journalctl -u keepalived -f
```

### Verify VIP ownership

```bash
ip addr show eth0 | grep 10.0.0.10
```

## Troubleshooting

### VIP not appearing

1. Check Keepalived is running:
   ```bash
   systemctl is-active keepalived
   ```

2. Check for configuration errors:
   ```bash
   keepalived --config-test -f /etc/keepalived/keepalived.conf
   ```

3. Verify VRRP traffic is not blocked by firewall:
   ```bash
   # Keepalived uses VRRP protocol (IP protocol 112)
   # Ensure these are NOT blocked:
   # - 224.0.0.18 (VRRP multicast)
   # - IP protocol 112
   ```

4. Check logs:
   ```bash
   journalctl -u keepalived -n 50 --no-pager
   ```

### Auth password mismatch

If VRRP peers cannot authenticate, check:

```bash
journalctl -u keepalived | grep -i auth
# Expected: "VRRP_Instance(VI_51) - ignoring received advertisement..."
# Fix: ensure the same password is set in /etc/keepalived/keepalived.auth
```

### Both VPSes claim the VIP (split-brain)

This should not happen with VRRP authentication. If it does:

1. Verify the firewall is not blocking VRRP multicast (`224.0.0.18`)
2. Check both configs have the same VRID (`51`), same auth_pass
3. Verify both VPSes are on the same L2 segment

## Files

| File | Purpose |
|---|---|
| `keepalived-vps-a.conf` | Primary config — MASTER, priority 100, preempt |
| `keepalived-vps-h.conf` | Backup config — BACKUP, priority 90, no preempt |
| `keepalived.auth` | VRRP auth password (chmod 600 required) |
| `README.md` | This file |
