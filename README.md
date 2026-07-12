# reverse-ssh-gateway

[![CI](https://github.com/tensorov/reverse-ssh-gateway/actions/workflows/lint.yml/badge.svg)](https://github.com/tensorov/reverse-ssh-gateway/actions/workflows/lint.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Ansible](https://img.shields.io/badge/Ansible-core%3E%3D2.14-blue?logo=ansible)](https://www.ansible.com/)

Reverse SSH tunnel gateway for exposing NAT'd homelab services through a public VPS. Expose SSH, HTTP, and arbitrary TCP services behind NAT without port forwarding, dynamic DNS, or VPN overlays.

## Table of Contents

- [Architecture](#architecture)
- [Features](#features)
- [Quick Start](#quick-start)
  - [Standalone (no Ansible)](#option-a-standalone-no-ansible)
  - [Ansible (recommended)](#option-b-ansible-recommended)
- [Repository Structure](#repository-structure)
- [Ansible Roles](#ansible-roles)
  - [ssh-tunnel-client](#ssh-tunnel-client-nat-side)
  - [ssh-tunnel-server](#ssh-tunnel-server-vps-side)
  - [Playbooks](#playbooks)
  - [Sample Inventory](#sample-inventory)
- [Port Configuration](#port-configuration)
  - [TCP Port Mappings](#tcp-port-mappings)
  - [UDP-to-TCP Bridges](#udp-to-tcp-bridges)
- [Variables Reference](#variables-reference)
  - [Client Variables](#client-role-variables)
  - [Server Variables](#server-role-variables)
- [VPS Prerequisites](#vps-prerequisites)
- [Homelab Integration](#homelab-integration-git-submodule)
- [Tunnel Script Behavior](#tunnel-script-behavior)
- [Troubleshooting](#troubleshooting)
- [Development](#development)
- [License](#license)

## Architecture

```
    NAT SIDE (home network)                   PUBLIC VPS
    =======================                  =====================

    +------------------+                      +------------------+
    |  Homelab Host    |                      |  VPS             |
    |                  |    SSH tunnel        |                  |
    |  ssh-tunnel      |-- (outbound) ------->|  sshd            |
    |  (systemd svc)   |    -R <port>         |  +---------------+
    |                  |                      |  | Traefik       |
    +--------+---------+                      |  | (TCP router)  |
             |                                |  +-------+-------+
             |                                |          |
       local services                   entryPoints    public
       (SSH, HTTP, ...)                (tunnel-PORT)  internet
                                              |          |
                                              v          v
                                         127.0.0.1    Clients
                                         :<port>      reach
                                             |        services
                                             |        via VPS IP
                                         SSH / HTTP / TCP
```

**How it works:** The client on the NAT-side host opens an outbound SSH connection to the VPS. Through that single encrypted connection, SSH reverse-forward flags (`-R`) map remote ports on the VPS back to local ports behind NAT. Traefik on the VPS acts as a TCP router, accepting inbound traffic on the forwarded ports and proxying it through the tunnel to the homelab. The entire flow uses one outbound connection, so no inbound ports are opened on the NAT side.

## Features

- **No inbound ports required.** The client initiates all connections outbound. NAT stays closed.
- **Single encrypted tunnel.** All traffic flows through one SSH connection. No additional VPN or overlay network.
- **Auto-reconnect with backoff.** Exponential backoff (1s to 60s cap), reset on clean exit. Dead connections detected via SSH keepalive.
- **UDP-to-TCP bridging.** Forward UDP services (WireGuard, DNS) through the TCP tunnel using `socat(1)`.
- **Traefik TCP routing.** Server role generates Traefik dynamic config automatically for each tunneled port.
- **Two deployment paths.** Standalone shell script for single hosts, Ansible roles for multi-host fleets.
- **Systemd managed.** Both sides run as systemd services with structured syslog logging.
- **Firewall automation.** Server role opens UFW or nftables rules for each tunneled port.
- **Git submodule ready.** Designed to embed in an existing Ansible homelab repository.

## Quick Start

### Option A: Standalone (no Ansible)

The `client-script/` directory contains a self-contained tunnel manager for hosts where Ansible is not available or desired.

```bash
# 1. Copy the client script and config to your NAT host
scp client-script/tunnel.sh client-script/config.env user@nathost:~/tunnel/

# 2. On the NAT host, edit config.env with your VPS details
cd ~/tunnel
vim config.env

# 3. Deploy the SSH key to the VPS (one-time)
#    Copy the public key to the VPS tunnel user's authorized_keys:
#    ssh-copy-id -i ~/.ssh/id_reverse_tunnel tunnel@your-vps.example.com

# 4. Start the tunnel
./tunnel.sh start

# 5. Check status
./tunnel.sh status
```

**config.env reference:**

| Variable | Description | Example |
|---|---|---|
| `GATEWAY_HOST` | VPS hostname or IP | `vps.example.com` |
| `GATEWAY_PORT` | SSH port on VPS | `22` |
| `GATEWAY_USER` | SSH user on VPS | `tunnel` |
| `SSH_KEY_PATH` | Path to SSH private key | `$HOME/.ssh/id_reverse_tunnel` |
| `TUNNELS` | Comma-separated port mappings | `8080:80:localhost:80` |
| `POLL_INTERVAL` | Health check interval (seconds) | `30` |

**Tunnel format:** `<local_port>:<remote_port>:<target_host>:<target_port>` -- for example, `8080:80:localhost:80` maps VPS port 8080 through the tunnel to `localhost:80` on the NAT host.

### Option B: Ansible (recommended)

Better for managing multiple NAT hosts, shared tunnel configurations, or integrating into an existing Ansible homelab.

```bash
# 1. Clone the repository
git clone https://github.com/tensorov/reverse-ssh-gateway.git
cd reverse-ssh-gateway

# 2. Set up your inventory
cp -r ansible/inventories/sample ansible/inventories/production
vim ansible/inventories/production/hosts.yml

# 3. Define tunnel mappings
vim ansible/inventories/production/group_vars/all.yml

# 4. Deploy server (VPS)
ansible-playbook -i ansible/inventories/production/hosts.yml \
  ansible/playbooks/deploy-server.yml

# 5. Deploy client (NAT host)
ansible-playbook -i ansible/inventories/production/hosts.yml \
  ansible/playbooks/deploy-client.yml
```

## Repository Structure

```
reverse-ssh-gateway/
├── ansible/
│   ├── ansible.cfg
│   ├── inventories/
│   │   └── sample/
│   │       ├── hosts.yml
│   │       └── group_vars/all.yml
│   ├── playbooks/
│   │   ├── deploy-client.yml
│   │   └── deploy-server.yml
│   └── roles/
│       ├── ssh-tunnel-client/
│       │   ├── defaults/main.yml
│       │   ├── tasks/main.yml
│       │   ├── tasks/config.yml
│       │   ├── templates/
│       │   │   ├── tunnel.sh.j2
│       │   │   └── ssh-tunnel.service.j2
│       │   └── handlers/main.yml
│       └── ssh-tunnel-server/
│           ├── defaults/main.yml
│           ├── tasks/main.yml
│           ├── tasks/firewall.yml
│           ├── templates/
│           │   ├── traefik-tcp-routes.yml.j2
│           │   └── firewall-rules.sh.j2
│           └── handlers/main.yml
├── client-script/
│   ├── config.env
│   ├── tunnel.sh
│   └── install.sh
├── LICENSE
├── README.md
├── .ansible-lint
└── .yamllint
```

## Ansible Roles

### ssh-tunnel-client (NAT side)

Deployed to the host behind NAT. Creates a dedicated `tunnel` user, provisions an SSH key (Ed25519), templates the tunnel script with auto-reconnect and exponential backoff, and registers a systemd service.

**How it works:**

1. Creates a `tunnel` system user.
2. Either copies an SSH private key from the control node or generates a new Ed25519 pair.
3. Templates `/usr/local/bin/ssh-tunnel.sh` with your port mappings baked in.
4. Installs a systemd unit that runs the script as the tunnel user.
5. Writes a config summary to `/etc/ssh-tunnel/tunnel.conf` for debugging.

### ssh-tunnel-server (VPS side)

Deployed to the public VPS. Creates a `tunnel` user with `/usr/sbin/nologin`, templates Traefik TCP route configuration, opens firewall ports, and enables IP forwarding.

**How it works:**

1. Creates a `tunnel` system user (no login shell).
2. Templates Traefik dynamic config at `/etc/traefik/dynamic/tunnels/tcp-tunnels.yml`.
3. Opens firewall ports for each tunneled service (UFW or nftables).
4. Enables `net.ipv4.ip_forward` via sysctl.
5. Restarts Traefik and firewall services as needed.

### Playbooks

| Playbook | Target | Purpose |
|---|---|---|
| `deploy-client.yml` | `all` | Installs tunnel client on NAT-side hosts |
| `deploy-server.yml` | `gateway` | Configures VPS to accept and route tunnels |

### Sample Inventory

The sample inventory at `ansible/inventories/sample/` defines two host groups:

```yaml
gateway:
  hosts:
    vps01:
      ansible_host: YOUR_VPS_IP
      ansible_user: root
      ansible_python_interpreter: /usr/bin/python3

clients:
  hosts:
    homelab01:
      ansible_host: YOUR_NAT_HOST_IP
      ansible_user: root
      ansible_python_interpreter: /usr/bin/python3
```

- **`gateway`** group: VPS hosts that run the `ssh-tunnel-server` role. The `deploy-server.yml` playbook targets this group.
- **`clients`** group: NAT-side hosts that run the `ssh-tunnel-client` role. The `deploy-client.yml` playbook targets `all` (but you typically limit it to the `clients` group).

## Port Configuration

### TCP Port Mappings

Both roles share the same `ssh_tunnel_ports` list structure. Each entry is a map with three fields:

```yaml
ssh_tunnel_ports:
  - local_port: 8080      # Port on the NAT-side host
    remote_port: 8080      # Port on the VPS (tunnel endpoint)
    description: "Web service behind NAT"
  - local_port: 22
    remote_port: 2022
    description: "SSH into NAT'd host"
  - local_port: 3306
    remote_port: 3306
    description: "MySQL behind NAT"
```

| Field | Required | Description |
|---|---|---|
| `local_port` | yes | TCP port on the NAT-side host to forward from |
| `remote_port` | yes | TCP port on the VPS where Traefik listens and traffic arrives |
| `description` | no | Human-readable label for firewall rules and Traefik config |

**How port mapping works:**

1. On the VPS, Traefik binds to `:<remote_port>` and routes incoming traffic to `127.0.0.1:<remote_port>` via the tunnel.
2. SSH reverse-forward (`-R`) maps `:<remote_port>` on the VPS to `:<local_port>` on the NAT host.
3. Traffic flows: Internet -> VPS `:<remote_port>` -> Traefik -> tunnel -> NAT host `:<local_port>`.

**Important:** Each `remote_port` must also be defined as a Traefik entryPoint in your VPS static configuration:

```yaml
# In traefik.yml (static config)
entryPoints:
  tunnel-8080:
    address: ":8080"
  tunnel-2022:
    address: ":2022"
```

### UDP-to-TCP Bridges

For protocols that run over UDP but need TCP transport through the SSH tunnel, use `ssh_tunnel_udp_bridges`:

```yaml
ssh_tunnel_udp_bridges:
  - local_port: 51820     # UDP port on NAT host (WireGuard)
    remote_port: 51820     # TCP port on VPS (bridge endpoint)
    description: "WireGuard UDP-to-TCP bridge"
```

The client starts a `socat(1)` process for each entry that listens on UDP `local_port` and forwards the data over TCP through the SSH tunnel. A matching socat on the VPS side must convert TCP back to UDP at the final destination.

## Variables Reference

### Client Role Variables

| Variable | Default | Description |
|---|---|---|
| `ssh_tunnel_ports` | `[]` | List of TCP port mappings (see format above) |
| `ssh_tunnel_jump_host` | `""` | VPS connection string: `user@hostname` |
| `ssh_tunnel_local_host` | `"127.0.0.1"` | Local bind address for forwarded ports |
| `ssh_tunnel_udp_bridges` | `[]` | UDP-to-TCP bridge definitions |
| `ssh_tunnel_user` | `"tunnel"` | Dedicated system user for the tunnel |
| `ssh_tunnel_key_file` | `"/home/{{ ssh_tunnel_user }}/.ssh/id_ed25519"` | SSH private key path on target |
| `ssh_tunnel_private_key_src` | `""` | SSH key source path on Ansible control node |
| `ssh_tunnel_generate_key` | `false` | Generate Ed25519 key pair on target instead of copying |
| `ssh_tunnel_ssh_port` | `22` | SSH port on the VPS |
| `ssh_tunnel_server_alive_interval` | `30` | SSH keepalive interval (seconds) |
| `ssh_tunnel_server_alive_count_max` | `3` | Max missed keepalives before disconnect |
| `ssh_tunnel_service_name` | `"ssh-tunnel"` | Systemd service name |

**Key provisioning modes:**

- **Copy mode** (default): Set `ssh_tunnel_private_key_src` to a path on the Ansible control node. The role copies the private key to the tunnel user's home.
- **Generate mode**: Set `ssh_tunnel_generate_key: true`. The role creates a fresh Ed25519 key pair on the target. You must then copy the public key to the VPS `tunnel` user's `authorized_keys` manually or via another mechanism.

### Server Role Variables

| Variable | Default | Description |
|---|---|---|
| `ssh_tunnel_ports` | `[]` | List of TCP port mappings (same format as client) |
| `ssh_tunnel_server_user` | `"tunnel"` | System user created for tunnel SSH logins |
| `ssh_tunnel_traefik_routes_dir` | `"/etc/traefik/dynamic/tunnels/"` | Directory for Traefik dynamic config |
| `ssh_tunnel_traefik_config_file` | `"{{ ssh_tunnel_traefik_routes_dir }}tcp-tunnels.yml"` | Traefik config file path |
| `ssh_tunnel_firewall_type` | `"ufw"` | Firewall backend: `"ufw"` or `"nftables"` |
| `ssh_tunnel_enable_ip_forwarding` | `true` | Enable `net.ipv4.ip_forward` |
| `ssh_tunnel_allowed_remote_hosts` | `"0.0.0.0/0"` | Restrict tunnel listener to specific source CIDR |

## VPS Prerequisites

Before running the server playbook, the VPS must have the following configured.

### SSH Access

The `tunnel` user needs a login shell that accepts SSH connections from clients. The role creates this user with `/usr/sbin/nologin` by default, which still allows SSH reverse-forward connections but prevents interactive shell access.

If you need the tunnel user to also log in interactively, override:

```yaml
ssh_tunnel_server_user_shell: /bin/bash
```

### GatewayPorts Directive

For tunnel-forwarded ports to be accessible from outside the VPS, SSH must allow binding on non-loopback interfaces. Edit `/etc/ssh/sshd_config`:

```
GatewayPorts yes
```

Then restart sshd:

```bash
systemctl restart sshd
```

Without this, tunnel ports bind only to `127.0.0.1` on the VPS and are unreachable from the internet.

### Traefik Static Configuration

Each tunneled port requires a matching Traefik entryPoint. Add them to your Traefik static configuration:

```yaml
# /etc/traefik/traefik.yml
entryPoints:
  tunnel-8080:
    address: ":8080"
  tunnel-2022:
    address: ":2022"
```

The server role generates the dynamic TCP route configuration automatically. It writes to `/etc/traefik/dynamic/tunnels/tcp-tunnels.yml`, which Traefik picks up on restart or via file provider polling.

### Firewall

The server role opens firewall ports automatically (UFW or nftables). If you manage the firewall externally, ensure the following ports are open:

- Each `remote_port` from your `ssh_tunnel_ports` list.
- SSH port (22 by default) for client connections.

### IP Forwarding

The role enables `net.ipv4.ip_forward` by default. If you disable this, ensure your VPS can route traffic between the tunnel listener and the Traefik process (both run on localhost, so this is rarely an issue).

## Homelab Integration (Git Submodule)

To use this repo as a submodule in an existing Ansible homelab:

```bash
# Add as a submodule
cd ~/gits/homelab
git submodule add https://github.com/tensorov/reverse-ssh-gateway.git ansible/vendor/reverse-ssh-gateway

# Initialize after clone (on other machines)
git submodule update --init --recursive
```

### Extending Your Deploy Playbook

Reference the roles from the submodule in your own playbook:

```yaml
---
# homelab/deploy.yml
- name: Deploy homelab services
  hosts: all
  become: true

  roles:
    - role: ansible/vendor/reverse-ssh-gateway/ansible/roles/ssh-tunnel-client
      vars:
        ssh_tunnel_jump_host: "tunnel@YOUR_VPS_HOSTNAME"
        ssh_tunnel_generate_key: true
        ssh_tunnel_ports:
          - local_port: 22
            remote_port: 2022
            description: "SSH into homelab"
          - local_port: 80
            remote_port: 80
            description: "Homepage HTTP"
          - local_port: 443
            remote_port: 443
            description: "Homepage HTTPS"
```

### Setting Variables

You can set tunnel variables at multiple levels. From broadest to most specific:

1. **Role defaults** -- safest starting point; override in higher layers.
2. **group_vars/all.yml** -- shared across all hosts in your inventory.
3. **group_vars/<group>.yml** -- per-group overrides (e.g., different ports for different hosts).
4. **host_vars/<host>.yml** -- per-host overrides.
5. **Playbook vars** -- inline `vars:` block in your playbook.
6. **Extra vars** -- `-e "ssh_tunnel_jump_host=tunnel@vps.example.com"` on the command line.

### Updating the Submodule

```bash
# Pull latest changes
cd ~/gits/homelab
git submodule update --remote ansible/vendor/reverse-ssh-gateway

# Commit the update
git add ansible/vendor/reverse-ssh-gateway
git commit -m "Update reverse-ssh-gateway submodule"
```

## Tunnel Script Behavior

The generated tunnel script (`/usr/local/bin/ssh-tunnel.sh`) runs as a systemd service and provides:

- **Auto-reconnect** with exponential backoff: 1s, 2s, 4s, 8s, 16s, 30s, 60s (cap). A clean exit resets the backoff to 1s.
- **SSH keepalive** via `ServerAliveInterval` and `ServerAliveCountMax` to detect dead connections.
- **Multiplexed connections** via `ControlMaster` for faster reconnections.
- **UDP-to-TCP bridging** via `socat(1)` for each entry in `ssh_tunnel_udp_bridges`.
- **Structured logging** to syslog via `logger -t tunnel`.
- **Clean shutdown** on SIGTERM/SIGINT, killing any background socat processes.

## Troubleshooting

### Tunnel won't connect

**Check SSH key permissions:**

```bash
# On the NAT host
ls -la /home/tunnel/.ssh/
# id_ed25519 should be 0600, owned by tunnel:tunnel
# .ssh directory should be 0700

# On the VPS
ls -la /home/tunnel/.ssh/authorized_keys
# Should contain the client's public key
```

**Test the SSH connection manually:**

```bash
sudo -u tunnel ssh -i /home/tunnel/.ssh/id_ed25519 \
  -o StrictHostKeyChecking=accept-new \
  tunnel@YOUR_VPS_HOSTNAME
```

### GatewayPorts not working

If tunnel ports bind only to `127.0.0.1` on the VPS (unreachable from internet):

```bash
# Check current setting
grep -i gatewayports /etc/ssh/sshd_config

# Should show: GatewayPorts yes
# If missing or set to "no", edit the file and restart sshd:
systemctl restart sshd
```

### Firewall blocking traffic

```bash
# UFW
sudo ufw status | grep 8080
sudo ufw allow 8080/tcp

# nftables
sudo nft list ruleset | grep 8080
```

### Traefik not routing

Verify the dynamic config file exists and contains your routes:

```bash
cat /etc/traefik/dynamic/tunnels/tcp-tunnels.yml
```

Check Traefik logs for entryPoint mismatches:

```bash
journalctl -u traefik --since "5 minutes ago" | grep -i tunnel
```

### Service logs

```bash
# Client-side tunnel logs
journalctl -u ssh-tunnel -f

# Server-side sshd logs
journalctl -u ssh -f

# Traefik logs
journalctl -u traefik -f
```

### Common Issues

| Symptom | Cause | Fix |
|---|---|---|
| `Connection refused` | VPS firewall blocking port | Open the tunnel port in UFW/nftables |
| `Connection timed out` | VPS not listening on the port | Verify Traefik entryPoint matches `remote_port` |
| `Warning: remote port forwarding failed` | Port already in use on VPS | Check for conflicting services: `ss -tlnp \| grep <port>` |
| `ExitOnForwardFailure` | SSH could not bind remote port | Same as above, or check `GatewayPorts` setting |
| Tunnel connects but no traffic | Traefik routing misconfigured | Check `tcp-tunnels.yml` matches Traefik static entryPoints |
| Tunnel drops frequently | Keepalive not reaching VPS | Increase `ssh_tunnel_server_alive_interval` |
| `Permission denied (publickey)` | SSH key not deployed | Copy public key to VPS `authorized_keys` |

## Development

### Linting

The repo includes pre-configured linting rules:

```bash
# Ansible-lint (0 failures required)
ansible-lint ansible/

# yamllint
yamllint ansible/
```

Linting configuration:

- `.ansible-lint` -- skips FQCN, role-name, and casing rules; excludes `.git/` and `.github/`.
- `.yamllint` -- 200-char line limit, disables document-start and truthy checks.

### Testing

After making changes, verify with:

```bash
# Syntax check
ansible-playbook --syntax-check ansible/playbooks/deploy-server.yml
ansible-playbook --syntax-check ansible/playbooks/deploy-client.yml

# Dry run (check mode)
ansible-playbook --check -i ansible/inventories/sample/hosts.yml \
  ansible/playbooks/deploy-server.yml
```

## License

MIT License. Copyright (c) 2026 tensorov.

See [LICENSE](LICENSE) for the full text.
