# Security fixes for rtg-orchestrator + module path

## Context

External code review flagged two security issues (security-by-default: 4/10):

1. **`rtg-orchestrator` listens on `:8443` (all interfaces)** with zero auth — anyone on the network can register/delete services and rewrite Traefik config.
2. **`tunnel.sh` uses `StrictHostKeyChecking=accept-new`** — trusts unknown host keys on first connect (MITM window).

Plus an inconsistency: the Go module path is still `tensorov/reverse-ssh-gateway` while the repo moved to `tensorov/ssh-rtg`.

## Tasks

### Task 1: Lock `rtg-orchestrator` to localhost by default

**File:** `cmd/rtg-orchestrator/main.go`

- Add `--bind` flag, default `"127.0.0.1"`
- Change `addr := fmt.Sprintf(":%d", *port)` → `addr := fmt.Sprintf("%s:%d", *bind, *port)`
- Update `slog.Info("starting server", ...)` line to reflect the actual addr
- Help text on `--bind`: `"Bind address (set to 0.0.0.0 to expose publicly — ensure firewall or reverse-proxy auth is in place)"`

**Why:** The single biggest complaint in both reviews. With `127.0.0.1` by default, orchestrator is unreachable from outside even if the port is accidentally left open in the firewall. Users who need external access consciously opt in via `-bind 0.0.0.0`.

### Task 2: Document the `--bind` flag in README

**File:** `README.md` (rtg-orchestrator section, around existing `go build` / `Run with` examples)

- Add `-bind 127.0.0.1` to the example invocations
- Add a security callout: "Orchestrator binds to localhost by default. If you must expose it, put a reverse proxy with auth in front."

### Task 3: Document `StrictHostKeyChecking` trade-off in `tunnel.sh`

**File:** `client-script/tunnel.sh`, line 174

- Add inline comment above the `-o StrictHostKeyChecking=accept-new \` line:
  ```bash
  # accept-new: accept unknown host keys on first connect, then pin.
  # This is a deliberate trade-off for unattended operation — the alternative
  # (StrictHostKeyChecking=no) would accept any key every time.
  # For higher security, pre-provision the known_hosts file and switch to
  # StrictHostKeyChecking=yes.
  ```

**Why:** Reviewers flagged this but the trade-off is reasonable. A comment makes the decision explicit and auditable.

### Task 4: Update Go module path from `reverse-ssh-gateway` to `ssh-rtg`

**Files:**
- `go.mod` — `go mod edit -module github.com/tensorov/ssh-rtg`
- `internal/configgen/traefik.go` — update import
- `internal/configgen/traefik_test.go` — update import
- `deploy/sync/sync-traefik-config.timer` — update Documentation URL
- `deploy/sync/sync-traefik-config.service` — update Documentation URL
- `ansible/roles/ssh-tunnel-server/templates/udp-bridge.service.j2` — update Documentation URL

**Verify:** `go build ./cmd/rtg-orchestrator/` succeeds after changes.

## Order

1. Task 1 (bind address — highest impact)
2. Task 2 (README docs)
3. Task 3 (tunnel.sh comment)
4. Task 4 (module path — touches Go imports, verify with build)

## Verification

- `go build ./cmd/rtg-orchestrator/` — must succeed
- `go vet ./cmd/rtg-orchestrator/` — no warnings
- `go test ./internal/...` — all tests pass
- Manual: `./rtg-orchestrator -h` shows `--bind` flag with `127.0.0.1` default
- Manual: start with no flags → `ss -tlnp | grep 8443` shows `127.0.0.1:8443`
