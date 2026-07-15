# ESP32 TCP Bridge Gateway

Forward TCP connections from a Linux gateway host to an ESP32 device behind NAT using `socat(1)` and systemd template units.

## Architecture

```
External client            Linux gateway                   ESP32
  (SSH tunnel,     ──>    socat TCP-LISTEN    ──>         HTTP/TCP server
   local network,          :3000, fork,                    :80 (DHT22 sensor,
   etc.)                   reuseaddr                       relay, etc.)
```

The gateway runs one socat instance per ESP32 service. Each instance listens on a local TCP port and forwards all connections to the ESP32's target host and port.

## Prerequisites

- **socat** must be installed on the gateway host:

```bash
sudo apt install socat       # Debian / Ubuntu
sudo yum install socat       # RHEL / CentOS
sudo pacman -S socat         # Arch Linux
```

**Important:** socat is a prerequisite. It is NOT installed automatically by the systemd unit.

## Quick Start

### 1. Configure ESP32 target via override.conf

Create a drop-in override file to set the ESP32 host and port:

```bash
sudo mkdir -p /etc/systemd/system/esp32-gateway@.service.d
```

```ini
# /etc/systemd/system/esp32-gateway@.service.d/override.conf
[Service]
Environment=ESP32_HOST=192.168.1.42 ESP32_PORT=80
```

### 2. Activate a gateway instance

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now esp32-gateway@3000.service
```

This starts `socat TCP-LISTEN:3000,fork,reuseaddr TCP:192.168.1.42:80`.

### 3. Verify the gateway is listening

```bash
ss -tlnp | grep 3000
```

You should see socat listening on port 3000.

### 4. Test end-to-end connectivity

```bash
echo "GET /" | nc localhost 3000
```

If the ESP32 is running an HTTP server, you should receive its response.

## Configuration Methods

### Method A: Drop-in override.conf (recommended)

Create `/etc/systemd/system/esp32-gateway@.service.d/override.conf`:

```ini
[Service]
Environment=ESP32_HOST=192.168.1.42 ESP32_PORT=80
```

The `%i` parameter is the **local gateway listen port** (e.g., 3000). The `ESP32_HOST` and `ESP32_PORT` environment variables point to the ESP32 target.

### Method B: EnvironmentFile

Create `/etc/default/esp32-gateway`:

```bash
# /etc/default/esp32-gateway
ESP32_HOST=192.168.1.42
ESP32_PORT=80
```

Then create an override to point to this file:

```ini
# /etc/systemd/system/esp32-gateway@.service.d/envfile.conf
[Service]
EnvironmentFile=/etc/default/esp32-gateway
```

## Managing Multiple Instances

Each ESP32 service or port gets its own instance of the template unit:

```bash
sudo systemctl enable --now esp32-gateway@3000.service   # ESP32 HTTP (port 80)
sudo systemctl enable --now esp32-gateway@3001.service   # ESP32 custom service (port 8080)
```

### Group Target

The `esp32-gateway.target` groups all gateway instances. Enable it to start all configured instances:

```bash
sudo systemctl enable --now esp32-gateway.target
```

By default, the target includes instances for ports 3000 and 3001. Edit `deploy/gateway/esp32-gateway.target` to add or remove instances.

### Managing individual instances

```bash
sudo systemctl status esp32-gateway@3000.service
sudo systemctl restart esp32-gateway@3001.service
sudo systemctl stop esp32-gateway@3000.service
```

## Full Scenario: Temperature Sensor via SSH Tunnel

This example shows how to expose an ESP32 DHT22 temperature sensor through a VPS via SSH tunnel and the gateway.

### Components

| Component | Role | Address |
|-----------|------|---------|
| ESP32 | HTTP server with DHT22 sensor | `192.168.1.42:80` |
| Linux gateway | socat reverse-proxy | `:3000` → `192.168.1.42:80` |
| VPS | SSH tunnel endpoint + Traefik | `:8080` → `gateway:3000` |
| External client | Query via VPS | `vps.example.com:8080` |

### Setup

1. **Flash ESP32** with an HTTP server (e.g., Arduino IDE / ESP-IDF) that serves temperature data on port 80 at `/temperature`.

2. **On Linux gateway**, override the ESP32 target:

```ini
# /etc/systemd/system/esp32-gateway@.service.d/override.conf
[Service]
Environment=ESP32_HOST=192.168.1.42 ESP32_PORT=80
```

3. **Start the gateway instance:**

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now esp32-gateway@3000.service
```

4. **On the VPS**, create an SSH reverse tunnel from `:8080` to `gateway:3000`:

```bash
ssh -R 8080:localhost:3000 user@gateway-host
```

5. **Query from anywhere:**

```bash
curl http://vps.example.com:8080/temperature
```

The request flows: VPS:8080 → SSH tunnel → gateway:3000 → socat → ESP32:80 → DHT22 sensor.

## Logs

```bash
# Watch gateway logs
journalctl -u esp32-gateway@3000.service -f

# View last 50 lines
journalctl -u esp32-gateway@3000.service --no-pager -n 50
```

## Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| `socat: Address already in use` | Port already occupied | Check with `ss -tlnp \| grep <port>` |
| `Connection refused` | ESP32 unreachable | Verify `ESP32_HOST` and `ESP32_PORT`; ping ESP32 from gateway |
| socat exits immediately | `ESP32_HOST` or `ESP32_PORT` not set | Check override.conf or EnvironmentFile; `systemctl show esp32-gateway@3000.service \| grep ESP32` |
| socat not found | socat not installed | `apt install socat` (see prerequisites) |
| Port < 1024 binding fails | Missing capability | Ensure `AmbientCapabilities=CAP_NET_BIND_SERVICE` is in the unit |

## Verify socat is properly configured

```bash
# Check environment variables are set
systemctl show esp32-gateway@3000.service -p Environment | grep ESP32

# Check socat is listening
ss -tlnp | grep "$(systemctl show esp32-gateway@3000.service -p MainPID | cut -d= -f2)"
```

## Security Notes

- The unit runs as `User=nobody` by default (not root).
- For ports below 1024, `AmbientCapabilities=CAP_NET_BIND_SERVICE` allows binding without root.
- The ESP32 target (`ESP32_HOST`) should be on a trusted local network.
- Consider firewall rules to restrict access to the gateway ports if needed.
