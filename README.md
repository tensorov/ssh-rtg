# reverse-ssh-gateway

Reverse SSH tunnel gateway for exposing NAT'd homelab services via a public VPS.

## Overview

This repository provides Ansible roles and standalone scripts to deploy and manage
reverse SSH tunnels. A lightweight client on the NAT-side host establishes an
outbound SSH connection to a public VPS, which forwards traffic to services
behind NAT.

## Components

- **Ansible roles** — `ssh-tunnel-client` (NAT side) and `ssh-tunnel-server` (VPS side)
- **Client scripts** — standalone bash tunnel manager for non-Ansible environments
- **Playbooks** — deploy-client.yml, deploy-server.yml

## License

MIT
