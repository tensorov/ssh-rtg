# rtg-ngrok-replacement - Work Plan

## TL;DR (For humans)
<!-- Fill this LAST, after the detailed plan below is written. -->

**What you'll get:** Полноценная замена ngrok для хомлаба. TUI (gum) для управления туннелями, реестр сервисов на Go (5MB), автоматический failover между VPS (Keepalived), ESP32 через Linux-шлюз.

**Why this approach:** Reverse SSH tunnel — самая лёгкая и надёжная основа. Go-бинарник без зависимостей (не Consul). Keepalived — failover за секунды. Работает на Raspberry Pi и edge-устройствах.

**What it will NOT do:** Web UI (v2), Molecule тесты (v2), rate limiting, не менять прошивку ESP32.

**Effort:** Large (15 задач в 5 волнах)
**Risk:** Medium — интеграция Go-компонентов с bash/Ansible
**Decisions to sanity-check:** Registry API 5 endpoints без auth, Keepalived priority 100/90 preempt, rsync A→H однонаправленный

## Scope
### Must have
- TUI (gum) для управления туннелями: статус, список портов, старт/стоп/рестарт, просмотр логов
- HTTP API Service Registry (Go, CGO_ENABLED=0, ~5-8MB): 5 endpoints, Traefik dynamic config generation
- Keepalived VIP между VPS A (primary) и H (backup): VRID 51, priority 100/90, preempt
- rsync config sync: systemd timer, A→H, каждые 60s, только Traefik dynamic config
- Linux-шлюз для ESP32: socat TCP-LISTEN → ESP32 + systemd unit
- Server role: GatewayPorts automation (lineinfile + restart sshd), UDP socat counterpart, Traefik entryPoint validation
- Client role: деплой существующего robust шаблона на прод, port sync assert, TCP health check
- CI: Go build/test/lint в GitHub Actions
- README: обновление с архитектурой, HA, примерами ESP32

### Must NOT have (guardrails, anti-slop, scope boundaries)
- NOT Web UI (отдельный план v2)
- NOT Molecule Ansible тесты (отдельный план v2)
- NOT rate limiting / IP whitelist
- NOT Prometheus/Grafana мониторинг
- NOT прошивка ESP32 (не меняется)
- NOT multi-DC / multi-cluster
- NOT аутентификация в Registry API (v1 без auth, в management сети)

## Testing & Chaos Engineering

### TDD (Test-Driven Development) — strict
- **Go (W1, W4):** test-first. Написать `*_test.go` ДО реализации. `go test ./...` должен упасть → потом имплементация → тест зелёный.
- **Bash/Ansible (W0, W2, W3):** test-alongside. После написания логики — сразу тест (shellcheck + integration). Для Ansible — `--check` dry-run как тест. Для скриптов — assert на exit code + output.
- **QA сценарии пишутся ДО или одновременно с кодом**, а не постфактум.

### Chaos Engineering — hard mode
Каждый компонент проходит минимум 3 хаос-эксперимента. Эксперименты должны быть воспроизводимы одной bash-командой.

| Эксперимент | Команда | Что проверяем |
|---|---|---|
| Kill process | `pkill -9 socat; pkill -9 rtg-orchestrator` | systemd Restart=always, backoff |
| Network partition | `iptables -A INPUT -s {vps_ip} -j DROP; sleep 30; iptables -D INPUT...` | reconnect после разрыва |
| Resource pressure | `dd if=/dev/zero of=/tmp/fill bs=1M count=1024 2>/dev/null; sleep 5; rm /tmp/fill` | graceful degradation, not OOM |
| Race condition | параллельно 10x register + 10x delete + heartbeat каждые 100ms | race detector, file corruption |
| Zombie cleanup | register → убить процесс → проверить cleanup | TTL-based garbage collection |
| Disk full | заполнить data-dir → register → проверить response | error handling (HTTP 500 + log) |
| Concurrent config gen | 5 параллельных register → проверить rtg-services.yml | atomic write, no partial YAML |

- **Evidence-led:** каждый эксперимент — одна команда с assert. fail = компонент не готов.
- **Go-компоненты:** `go test -race -count=1` обязателен. `go test -fuzz` рекомендуется.

### Verification strategy
> Zero human intervention - all verification is agent-executed.
- Test decision: **TDD-first** (Go: test-first, Bash/Ansible: test-alongside, Chaos: hard-mode experiments)
- Evidence: `.omo/evidence/task-<N>-rtg-ngrok-replacement.<ext>`

## Execution strategy
### Parallel execution waves
- Wave 0: TUI (gum) + standalone improvements
- Wave 1: HTTP API Service Registry (Go)
- Wave 2: Multi-VPS infra (Keepalived, rsync, ESP32 gateway)
- Wave 3: Ansible role improvements
- Wave 4: Documentation + CI

### Dependency matrix
| Todo | Depends on | Blocks | Can parallelize with |
| --- | --- | --- | --- |
| W0-T1 gum dependency | — | W0-T2 | — |
| W0-T2 TUI creation | W0-T1 | W0-T3 | — |
| W0-T3 TUI integration | W0-T2 | — | — |
| W1-T1 Go scaffold | — | W1-T2, W1-T3, W4-T1 | W0-T1 |
| W1-T2 Registry endpoints | W1-T1 | W1-T3 | — |
| W1-T3 Config generator | W1-T2 | — | — |
| W2-T1 Keepalived | — | — | W2-T2, W2-T3 |
| W2-T2 rsync sync | — | — | W2-T1, W2-T3 |
| W2-T3 ESP32 gateway | — | — | W2-T1, W2-T2 |
| W3-T1 GatewayPorts | — | — | W3-T2, W3-T3, W3-T4 |
| W3-T2 UDP socat | — | — | W3-T1, W3-T3, W3-T4 |
| W3-T3 entryPoint validation | — | — | W3-T1, W3-T2, W3-T4 |
| W3-T4 Client improvements | — | — | W3-T1, W3-T2, W3-T3 |
| W4-T1 Go CI | W1-T1 | — | W4-T2 |
| W4-T2 README | — | — | W4-T1 |

## Todos

> Implementation + Test = ONE todo. Never separate.

---

### Wave 0 — TUI (gum) + standalone improvements

- [x] 1. Добавить проверку/установку gum в install.sh
  What to do / Must NOT do: В `client-script/install.sh` добавить:
  - Проверку наличия `gum` (`command -v gum`)
  - Если gum не найден — предложить установить: показать URL `https://github.com/charmbracelet/gum/releases` и инструкцию для apt/brew
  - НЕ скачивать gum автоматически (политика минимальных привилегий)
  - Добавить флаг `--skip-gum-check` для CI/автоматизации
  Parallelization: Wave 0 | Blocked by: — | Blocks: W0-T2
  References: `client-script/install.sh:22-244` (весь установщик), `client-script/config.env:1-83`
  Acceptance criteria:
  - `./install.sh --skip-gum-check` → не проверяет gum
  - `command -v gum` найден → install проходит без сообщения
  - `command -v gum` найден → install проходит без сообщения
  QA scenarios:
  - Happy: хост с gum → установка без предупреждения. Evidence: terminal output (grep "gum not found" → отсутствует)
  - Happy: --skip-gum-check без gum → установка проходит. Evidence: exit 0
  - Failure: нет gum, нет --skip-gum-check → напечатано "gum не найден". Evidence: grep "gum" в output
  Commit: Y | `feat(install): add gum dependency check`

- [x] 2. Создать TUI-скрипт `client-script/rtg-tui.sh`
  What to do / Must NOT do: Создать TUI на gum с экранами:
  1. **Status dashboard** (главный экран):
     - Статус туннеля: `gum spin --spinner dot --title "Checking tunnel..." -- sleep 1 && systemctl is-active ssh-tunnel`
     - Список проброшенных портов: парсинг `systemctl show ssh-tunnel -p ExecStart` → извлечение `-R` флагов
     - Uptime: `systemctl show ssh-tunnel -p ActiveEnterTimestamp`
  2. **Port list**: `gum table` с колонками PORT | LOCAL | STATUS
  3. **Start/Stop/Restart**: `gum confirm "Start tunnel?"` → `systemctl start ssh-tunnel` → `gum spin`
  4. **Logs**: `journalctl -fu ssh-tunnel --no-pager -n 50 | gum pager`
  5. **Exit**: `gum confirm "Exit TUI?"`
  - Навигация: `gum choose "Status" "Ports" "Start/Stop" "Logs" "Exit"` с бесконечным циклом
  - Флаг `--install` автоматически запускает install.sh если tunnel.sh не найден
  - Must NOT: не использовать Dialog/Whiptail — только gum
  - Must NOT: не менять tunnel.sh, только вызывать systemctl
  Parallelization: Wave 0 | Blocked by: W0-T1 | Blocks: W0-T3
  References: `client-script/tunnel.sh:1-196` (текущий standalone), `ansible/.../tunnel.sh.j2:1-102` (шаблон с reconnect)
  Acceptance criteria:
  - `bash client-script/rtg-tui.sh` открывает TUI с 5 пунктами меню
  - Выбор "Status" показывает статус туннеля (active/inactive)
  - Выбор "Ports" показывает таблицу портов
  - Выбор "Start/Stop" предлагает confirm и выполняет systemctl
  - Выбор "Logs" показывает логи через gum pager
  - Выход по "Exit" без ошибок
  QA scenarios:
  - Happy: tunnel работает → Status показывает "active". Evidence: скриншот TUI
  - Happy: tunnel остановлен → Ports показывает пустую таблицу. Evidence: скриншот
  - Happy: Start → `systemctl start` выполнен, статус changed. Evidence: systemctl status
  - Failure: gum не установлен → `command -v gum` проверка в начале, выход с сообщением
  - Failure: нет systemd юнита → сообщение "Service not found"
  Commit: Y | `feat(tui): add gum-based TUI for tunnel management`

- [x] 3. Интегрировать TUI в install.sh
  What to do / Must NOT do:
  - В `client-script/install.sh` скопировать `rtg-tui.sh` в `/usr/local/bin/rtg-tui` (chmod 755)
  - Добавить флаг `--with-tui` для установки TUI (по умолчанию — нет, чтобы не менять поведение существующих пользователей)
  - В конце установки спросить: `gum confirm "Install TUI?"` если gum найден
  - НЕ создавать systemd unit для TUI — это интерактивный инструмент
  - НЕ добавлять TUI в автозапуск
  Parallelization: Wave 0 | Blocked by: W0-T2 | Blocks: —
  References: `client-script/install.sh:22-244`, `client-script/rtg-tui.sh`
  Acceptance criteria:
  - `./install.sh --with-tui` → `/usr/local/bin/rtg-tui` создан, executable
  - `./install.sh` (без флага) → rtg-tui не скопирован
  - `/usr/local/bin/rtg-tui` запускается без ошибок
  QA scenarios:
  - Happy: --with-tui → rtg-tui exists, --help работает. Evidence: `ls -la /usr/local/bin/rtg-tui`
  - Failure: без --with-tui → rtg-tui не существует. Evidence: `ls /usr/local/bin/rtg-tui` → No such file
  Commit: Y | `feat(install): integrate TUI installation`

---

### Wave 1 — HTTP API Service Registry (Go)

- [x] 4. Создать Go модуль `cmd/rtg-orchestrator/`
  What to do / Must NOT do:
  - Инициализировать `go.mod` в корне репозитория: `go 1.22`, module `github.com/tensorov/reverse-ssh-gateway`
  - Создать `cmd/rtg-orchestrator/main.go`:
    - Флаги: `--port` (default 8443), `--config-dir` (default `/etc/traefik/dynamic/tunnels/`), `--data-dir` (default `/var/lib/rtg-orchestrator/`)
    - HTTP server на `net/http` (без внешних зависимостей, только stdlib)
    - `/v1/health` → `{"status":"ok","uptime":"...","services_count":0}`
    - Graceful shutdown через `signal.NotifyContext(ctx, SIGTERM, SIGINT)`
    - Middleware: `loggingMiddleware` (method, path, duration, remote_addr)
  - `Makefile` в корне:
    ```makefile
    build-orchestrator:
    	CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/rtg-orchestrator ./cmd/rtg-orchestrator/
    ```
  - Must NOT: не использовать chi/gin/echo — только stdlib `net/http`
  - Must NOT: не включать CGO
  Parallelization: Wave 1 | Blocked by: — | Blocks: W1-T2, W1-T3, W4-T1
  References: Go 1.22 stdlib `net/http`, `encoding/json`, `log/slog`
  Acceptance criteria:
  - `make build-orchestrator` → `bin/rtg-orchestrator` (~5-8MB), ELF binary
  - `bin/rtg-orchestrator --port 9999 & curl localhost:9999/v1/health` → `{"status":"ok","services_count":0}`
  - SIGTERM → graceful shutdown (exit 0)
  - `bin/rtg-orchestrator --help` → показывает все флаги
  - `go test ./cmd/rtg-orchestrator/...` — минимум 1 smoke test (health endpoint returns 200)
  QA scenarios:
  - Happy: build + run + health endpoint → 200 + JSON. Evidence: `curl -v localhost:9999/v1/health`
  - Happy: --help → флаги перечислены. Evidence: output содержит "port", "config-dir", "data-dir"
  - Failure: build with CGO_ENABLED=0 → нет ссылок на libc. Evidence: `ldd bin/rtg-orchestrator` → "not a dynamic executable"
  - Failure: port занят → log message "address already in use", exit 1. Evidence: stderr
  Commit: Y | `feat(orchestrator): scaffold Go module with health endpoint`

- [x] 5. Реализовать Registry API (5 endpoints)
  What to do / Must NOT do: В `cmd/rtg-orchestrator/main.go` (или `internal/registry/` если >300 строк):
  1. **`POST /v1/services/register`**
     - Body: `{"host":"vps-b","port":8080,"domain":"myapp.zeitoven.ru","proto":"http","ttl":30}`
     - Валидация: host обязателен, port 1-65535, domain опционален (если proto=http), proto="http"|"tcp", ttl=default 30
     - Сохранить в `{data-dir}/services/{host}-{port}.json`
           - Ответ: `{"id":"vps-b-8080","status":"registered"}` (opaque — файл `{data-dir}/services/vps-b-8080.json`)`
  2. **`POST /v1/services/heartbeat`**
     - Body: `{"host":"vps-b"}`
     - Обновить `last_seen` для всех сервисов этого хоста
     - Если сервис не найден — создать (lazy register)
     - Ответ: `{"status":"ok","services_updated":N}`
  3. **`GET /v1/services`**
     - Параметры: `?status=alive` (фильтр: alive = last_seen < TTL*2 секунд назад)
     - Ответ: `[{"host":"vps-b","port":8080,"domain":"...","proto":"http","status":"alive","last_seen":"RFC3339"}]`
   4. **`DELETE /v1/services/{id}`**
      - `{id}` = `{host}-{port}` (opaque ID, не парсится — удаляем файл напрямую)
      - Удалить файл `{data-dir}/services/{id}.json`
      - Ответ: 204 No Content
  5. **`GET /v1/health`** (уже из W1-T1)
  - Фоновый процесс: каждые 30s сканировать `{data-dir}/services/`, удалять сервисы с `last_seen > TTL*3`
  - Регенерация Traefik dynamic config (см. W1-T3) только при register, delete, или cleanup удалил сервис. Heartbeat не триггерит регенерацию (конфиг не меняется)
  - Must NOT: не хранить состояние в памяти (stateless — читаем с диска)
  - Must NOT: не добавлять auth в v1
  Parallelization: Wave 1 | Blocked by: W1-T1 | Blocks: W1-T3
  References: `net/http`, `encoding/json`, `os.ReadFile/WriteFile`, `path/filepath`
  Acceptance criteria:
  - Register: `curl -X POST localhost:9999/v1/services/register -d '{"host":"vps-b","port":8080}'` → 200 + JSON
  - Heartbeat: `curl -X POST localhost:9999/v1/services/heartbeat -d '{"host":"vps-b"}'` → 200
  - List: `curl localhost:9999/v1/services` → массив с сервисом
  - Delete: `curl -X DELETE localhost:9999/v1/services/vps-b-8080` → 204
  - Health: `curl localhost:9999/v1/health` → services_count=1
  QA scenarios:
  - Happy: register → list → сервис в списке. Evidence: curl + jq
  - Happy: register → heartbeat через 5s → list → status="alive". Evidence: jq `.[0].status`
  - Happy: delete → list → пустой массив. Evidence: `curl localhost:9999/v1/services` → `[]`
  - Failure: DELETE несуществующего ID → 404 Not Found. Evidence: body
  - Failure: register с port=0 → 400 Bad Request + error message. Evidence: body содержит "port"
  - Failure: register с invalid JSON → 400 + parse error. Evidence: body
  - Failure: TTL expired → сервис исчезает из списка. Evidence: register TTL=1 → wait 4s → list empty
  Commit: Y | `feat(orchestrator): implement service registry CRUD + heartbeat`

- [x] 6. Реализовать генерацию Traefik dynamic config
  What to do / Must NOT do: В `internal/configgen/traefik.go`:
  - При каждом изменении реестра (register/heartbeat/delete/cleanup):
    1. Прочитать все живые сервисы из `{data-dir}/services/`
    2. Сгенерировать `{config-dir}/rtg-services.yml` (Traefik file provider format):
       ```yaml
       # Auto-generated by rtg-orchestrator — DO NOT EDIT
       http:
         routers:
           myapp-zeitoven-ru:
             rule: "Host(`myapp.zeitoven.ru`)"
             entryPoints: ["web-secure"]
             tls: { certResolver: default }
             service: myapp-zeitoven-ru-svc
         services:
           myapp-zeitoven-ru-svc:
             loadBalancer:
               servers: [{ url: "http://vps-b:8080" }]
       tcp:
         routers:
           db-5432:
             entryPoints: ["tunnel-5432"]
             rule: "HostSNI(`*`)"
             service: db-5432-svc
         services:
           db-5432-svc:
             loadBalancer:
               servers: [{ address: "vps-c:5432" }]
       ```
    3. Записать атомарно: write to `.tmp` → rename → удалить старый файл
  - Для proto=http: генерировать HTTP router с TLS
  - Для proto=tcp: генерировать TCP router
  - Для сервисов без domain: не генерировать HTTP router (только TCP)
  - Must NOT: не менять существующие файлы в `{config-dir}`, только `rtg-services.yml`
  - Must NOT: не удалять другие файлы из config-dir
  Parallelization: Wave 1 | Blocked by: W1-T2 | Blocks: —
  References: `/opt/services/traefik/dynamic/config.yml:1-60` (существующий формат), `/opt/services/traefik/dynamic/onlybridge.yml:1-78` (пример с TLS)
  Acceptance criteria:
  - Register http-сервис с domain → `{config-dir}/rtg-services.yml` создан, содержит router с Host rule + TLS
  - Register tcp-сервис без domain → `rtg-services.yml` содержит TCP router
  - Delete сервис → router удалён из `rtg-services.yml`
  - Атомарность: сымитировать crash во время записи → config файл не повреждён
  QA scenarios:
  - Happy: register http → config содержит правильный router. Evidence: `grep -r "myapp" {config-dir}/`
  - Happy: register tcp → config содержит TCP section. Evidence: grep "tcp:" в config
  - Happy: delete → router исчез. Evidence: grep после delete → пусто
  - Failure: register без domain для http → config не создан (недостаточно данных)
  Commit: Y | `feat(orchestrator): implement Traefik dynamic config generator`

---

### Wave 2 — Multi-VPS инфраструктура

- [x] 7. Создать Keepalived конфигурацию (A primary, H backup)
  What to do / Must NOT do: Создать `deploy/keepalived/keepalived-vps-a.conf` и `deploy/keepalived/keepalived-vps-h.conf`:
  - VPS A (primary): `priority 100`, `state MASTER`
  - VPS H (backup): `priority 90`, `state BACKUP`
  - VRID: `51` (уникальный в сети), виртуальный IP: `10.0.0.10/24`
  - Интерфейс: `eth0` (с пометкой "change to your interface")
  - `preempt` (primary при восстановлении забирает VIP обратно)
  - `advert_int 1` (проверка каждую секунду)
  - Аутентификация VRRP: `auth_type PASS`, пароль в отдельном файле `keepalived.auth`
  - Script для проверки Traefik: `vrrp_script chk_traefik { script "/usr/bin/systemctl is-active traefik"; interval 2; fall 2; rise 2 }`
  - README с инструкцией:
    - Установка keepalived (apt/brew/yum)
    - Копирование конфига
    - Настройка интерфейса
    - Проверка: `systemctl status keepalived`
    - Тест failover: выключить Traefik на A → VIP должен переехать на H
  - Must NOT: не включать фактические пароли/интерфейсы — использовать плейсхолдеры
  - Must NOT: не управлять keepalived через Ansible (v1 — ручная настройка)
  Parallelization: Wave 2 | Blocked by: — | Blocks: —
  References: Keepalived documentation, `man keepalived.conf`
  Acceptance criteria:
  - `keepalived-vps-a.conf` содержит `priority 100`, `state MASTER`, `virtual_ipaddress 10.0.0.10`
  - `keepalived-vps-h.conf` содержит `priority 90`, `state BACKUP`
  - `vrrp_script chk_traefik` присутствует
  - README описывает 5 шагов установки
  QA scenarios:
  - Happy: конфиги синтаксически валидны. Evidence: `keepalived --config-test -f deploy/keepalived/keepalived-vps-a.conf`
  - Happy: README содержит все шаги. Evidence: grep "Install", "Configure", "Test" в README
  - Failure: файл `keepalived.auth` защищён (chmod 600). Evidence: `stat -c %a deploy/keepalived/keepalived.auth`
  Commit: Y | `feat(ha): add Keepalived config for VPS A (primary) and H (backup)`

- [x] 8. Создать rsync config sync (systemd timer)
  What to do / Must NOT do: Создать `deploy/sync/sync-traefik-config.sh`:
  ```bash
  #!/bin/bash
  set -euo pipefail
  SOURCE_HOST="backup-vps"  # или IP
  SOURCE_DIR="/etc/traefik/dynamic/"
  DEST_DIR="/etc/traefik/dynamic/"
  SSH_KEY="/root/.ssh/sync-key"
  rsync -az --delete -e "ssh -i ${SSH_KEY} -o StrictHostKeyChecking=accept-new" \
    "root@${SOURCE_HOST}:${SOURCE_DIR}" "${DEST_DIR}"
  systemctl reload traefik
  ```
  - Создать `deploy/sync/sync-traefik-config.service` и `sync-traefik-config.timer`:
    ```ini
    [Timer]
    OnCalendar=*:0/1  # каждую минуту
    Persistent=true
    ```
  - Создать `deploy/sync/README.md`:
    - Установка rsync
    - Генерация SSH-ключа: `ssh-keygen -t ed25519 -f /root/.ssh/sync-key -N ""`
    - Копирование публичного ключа на H: `ssh-copy-id -i /root/.ssh/sync-key.pub root@H`
    - Установка systemd timer: `cp ...service ...timer /etc/systemd/system/ && systemctl daemon-reload && systemctl enable --now sync-traefik-config.timer`
    - Проверка: `systemctl status sync-traefik-config.timer && journalctl -u sync-traefik-config.service`
  - Must NOT: agent-forwarding (использовать ключ)
  - Must NOT: PasswordAuthentication
  Parallelization: Wave 2 | Blocked by: — | Blocks: —
  References: `man rsync`, `man systemd.timer`
  Acceptance criteria:
  - sync-traefik-config.sh содержит rsync с --delete
  - service unit вызывает ExecStart со скриптом
  - timer активируется каждую минуту
  - README содержит 5 шагов установки
  QA scenarios:
  - Happy: `shellcheck deploy/sync/sync-traefik-config.sh` → no errors. Evidence: exit 0
  - Happy: systemd units парсятся: `systemd-analyze verify deploy/sync/sync-traefik-config.service`. Evidence: exit 0
  - Failure: SSH ключ не найден → exit 1, лог. Evidence: запуск без ключа
  Commit: Y | `feat(ha): add rsync config sync with systemd timer`

- [x] 9. Создать Linux-шлюз для ESP32
  What to do / Must NOT do: Создать `deploy/gateway/esp32-gateway@.service` (systemd template unit):
  ```ini
  [Unit]
  Description=ESP32 Gateway: %i (socat reverse-proxy)
  After=network-online.target
  PartOf=esp32-gateway.target
  
  [Service]
  Type=simple
  ExecStart=/usr/bin/socat TCP-LISTEN:%i,fork,reuseaddr TCP:{{ESP32_HOST}}:{{ESP32_PORT}}
  Restart=always
  RestartSec=5
  User=nobody
  AmbientCapabilities=CAP_NET_BIND_SERVICE
  StandardOutput=journal
  StandardError=journal
  
  [Install]
  WantedBy=multi-user.target
  ```
  - `{{ESP32_HOST}}` и `{{ESP32_PORT}}` — заменяются при создании инстанса через `@` модификатор или env var
  - Создать `deploy/gateway/esp32-gateway.target` (group unit):
    ```ini
    [Unit]
    Description=ESP32 Gateway — all proxy instances
    Wants=esp32-gateway@3000.service esp32-gateway@3001.service
    
    [Install]
    WantedBy=multi-user.target
    ```
  - Создать `deploy/gateway/README.md` с инструкцией:
    - Установка socat: `apt install socat`
    - Активация инстанса: `systemctl enable --now esp32-gateway@3000.service`
    - Параметры: `%i` = gateway_port (порт на Linux-шлюзе). ESP32_HOST и ESP32_PORT через `/etc/systemd/system/esp32-gateway@.service.d/override.conf`:
      ```ini
      [Service]
      Environment=ESP32_HOST=192.168.1.42 ESP32_PORT=80
      ```
    - Или через /etc/default/esp32-gateway (EnvironmentFile)
  - Сценарий: ESP32 с HTTP сервером (DHT22 температура). Linux-шлюз поднимает socat на :3000→ESP32:80. SSH туннель форвардит VPS:8080→gateway:3000. Через VPS клиент получает температуру.
  - Must NOT: не использовать `Type=oneshot` или bash-скрипт-обёртку (каждый socat — отдельный `Type=simple` unit)
  - Must NOT: не устанавливать socat автоматически (документировать как prerequisite)
  - Must NOT: не запускать от root — `User=nobody` + `AmbientCapabilities=CAP_NET_BIND_SERVICE` для портов <1024
  Parallelization: Wave 2 | Blocked by: — | Blocks: —
  References: `man socat`, `man systemd.service`, `man systemd.target`
  Acceptance criteria:
  - `systemctl start esp32-gateway@3000.service` → socat слушает :3000
  - `socat` crash → `Restart=always` перезапускает в течение 5s
  - `esp32-gateway.target` включает все инстансы
  - README описывает установку, override.conf, EnvironmentFile
  QA scenarios:
  - Happy: socat слушает на gateway_port. Evidence: `ss -tlnp | grep 3000` → показывает socat
  - Happy: TCP соединение к gateway_port доходит до ESP32. Evidence: `echo "GET /" | nc localhost 3000` получает ответ
  - Happy: socat убит → `systemctl status` показывает "active (running)". Evidence: `watch -n1 systemctl is-active esp32-gateway@3000`
  - Failure: порт занят → journalctl содержит "Address already in use", Restart=5s retry. Evidence: journalctl
  - Failure: ESP32 недоступен → socat запущен, но connection refused клиенту. Evidence: `nc -v localhost 3000` → "Connection refused"
  Commit: Y | `feat(gateway): add ESP32 gateway with systemd template unit + target`

---

### Wave 3 — Ansible role improvements

- [x] 10. Server role: GatewayPorts automation
  What to do / Must NOT do: В `ansible/roles/ssh-tunnel-server/tasks/main.yml` добавить:
  ```yaml
  - name: Ensure GatewayPorts is enabled in sshd_config
    ansible.builtin.lineinfile:
      path: /etc/ssh/sshd_config
      regexp: '^#?GatewayPorts\s'
      line: 'GatewayPorts yes'
      state: present
    notify: restart sshd
  
  - name: Ensure GatewayPorts is not commented after the line
    ansible.builtin.lineinfile:
      path: /etc/ssh/sshd_config
      regexp: '^\s*#\s*GatewayPorts'
      line: 'GatewayPorts yes'
      state: present
    notify: restart sshd
  ```
  - Добавить handler `restart sshd` в `handlers/main.yml`:
    ```yaml
    - name: restart sshd
      ansible.builtin.systemd:
        name: sshd
        state: restarted
    ```
  - Must NOT: комментировать/удалять другие настройки sshd
  - Must NOT: менять ssh_port (порт SSH не трогаем)
  Parallelization: Wave 3 | Blocked by: — | Blocks: —
  References: `ansible/roles/ssh-tunnel-server/tasks/main.yml:1-45`, `ansible/roles/ssh-tunnel-server/handlers/main.yml:1-22`
  Acceptance criteria:
  - `ansible-playbook deploy-server.yml` → `/etc/ssh/sshd_config` содержит `GatewayPorts yes` (не закомментировано)
  - Handler `restart sshd` вызывается при изменении sshd_config
  - Idempotent: повторный запуск не меняет sshd_config
  QA scenarios:
  - Happy: `ansible-playbook --check deploy-server.yml` → reports changed=1 (GatewayPorts). Evidence: play recap
  - Happy: idempotent — второй запуск → changed=0. Evidence: play recap
  - Failure: sshd_config не существует → Ansible ошибка. Evidence: play recap = failed
  Commit: Y | `feat(ansible): automate GatewayPorts yes in sshd_config`

- [x] 11. Server role: UDP socat counterpart
  What to do / Must NOT do: 
  - В `ansible/roles/ssh-tunnel-server/defaults/main.yml` добавить:
    ```yaml
    ssh_tunnel_server_udp_bridges: []
    # Формат:
    # - name: "wireguard"     # уникальное имя для systemd unit (буквы+цифры+дефис)
    #   local_port: 51820     # UDP порт на VPS (конечный слушатель)
    #   remote_port: 51820    # TCP порт из SSH туннеля
    #   description: "WireGuard UDP bridge"
    ```
  - В `ansible/roles/ssh-tunnel-server/tasks/main.yml` добавить:
    ```yaml
    - name: Install socat for UDP bridges
      ansible.builtin.package:
        name: socat
        state: present
      when: ssh_tunnel_server_udp_bridges | length > 0
    
    - name: Template UDP bridge systemd service
      ansible.builtin.template:
        src: udp-bridge.service.j2
        dest: "/etc/systemd/system/udp-bridge-{{ item.name }}.service"
        mode: "0644"
      loop: "{{ ssh_tunnel_server_udp_bridges }}"
      when: ssh_tunnel_server_udp_bridges | length > 0
      notify: restart udp bridges
    ```
  - Создать `templates/udp-bridge.service.j2` (no `@` — каждый bridge = отдельный unit через loop):
    ```ini
    [Unit]
    Description=UDP Bridge {{ item.name }} (TCP→UDP)
    After=network.target
    
    [Service]
    Type=simple
    ExecStart=/usr/bin/socat TCP4-LISTEN:{{ item.remote_port }},fork,reuseaddr UDP4:127.0.0.1:{{ item.local_port }}
    Restart=always
    RestartSec=5
    StandardOutput=journal
    StandardError=journal
    
    [Install]
    WantedBy=multi-user.target
    ```
  - Must NOT: путать local_port (UDP на VPS) и remote_port (TCP из туннеля)
  - Must NOT: стартовать socat без проверки наличия socat
  Parallelization: Wave 3 | Blocked by: — | Blocks: —
  References: `ansible/.../tunnel.sh.j2:56-61` (клиентская сторона UDP→TCP)
  Acceptance criteria:
  - `ssh_tunnel_server_udp_bridges` с 1 entry → socat установлен, systemd unit создан
  - UDP bridge template корректно генерирует socat команду TCP-LISTEN → UDP
  - Idempotent: повторный запуск не меняет
  QA scenarios:
  - Happy: после деплоя socat слушает TCP на remote_port. Evidence: `ss -tlnp | grep {remote_port}`
  - Happy: systemd unit активен. Evidence: `systemctl is-active udp-bridge-{name}`
  - Failure: socat не установлен → playbook устанавливает. Evidence: pre/post install test
  Commit: Y | `feat(ansible): add server-side UDP socat bridge`

- [x] 12. Server role: Traefik entryPoint validation
  What to do / Must NOT do: В `ansible/roles/ssh-tunnel-server/tasks/main.yml` добавить:
  ```yaml
  - name: Validate Traefik entryPoints exist for each tunnel port
    ansible.builtin.shell:
      cmd: |
        for port in {{ ssh_tunnel_ports | map(attribute='remote_port') | join(' ') }}; do
          if ! grep -q "tunnel-${port}:" /etc/traefik/traefik.yml 2>/dev/null; then
            echo "MISSING: entryPoint tunnel-${port} not found in /etc/traefik/traefik.yml"
          fi
        done
      warn: false
    changed_when: false
    register: _entrypoint_check
    failed_when: _entrypoint_check.stdout_lines | length > 0
  ```
  - Добавить comment-блок в шаблон `traefik-tcp-routes.yml.j2` с copy-paste секцией:
    ```yaml
    # === REQUIRED STATIC ENTRYPOINTS (add to traefik.yml) ===
    # entryPoints:
    # {% for item in ssh_tunnel_ports %}
    #   tunnel-{{ item.remote_port }}:
    #     address: ":{{ item.remote_port }}"
    # {% endfor %}
    ```
  - Must NOT: изменять /etc/traefik/traefik.yml (пользователь может управлять им отдельно)
  - Must NOT: добавлять entryPoints через Traefik dynamic config (entryPoints — только static)
  Parallelization: Wave 3 | Blocked by: — | Blocks: —
  References: `/opt/services/traefik/traefik.yml:13-21` (формат entryPoints), `ansible/.../traefik-tcp-routes.yml.j2:1-38`
  Acceptance criteria:
  - Если entryPoint отсутствует в traefik.yml → playbook FAILS с сообщением о missing port
  - Если все entryPoints присутствуют → playbook passes
  - Шаблон tcp-tunnels.yml содержит comment с copy-paste entryPoints
  QA scenarios:
  - Happy: все entryPoints есть → pass. Evidence: play recap `ok=* failed=0`
  - Failure: entryPoint missing → fail с сообщением "MISSING: entryPoint tunnel-8080". Evidence: stderr
  - Happy: comment блок в сгенерированном файле. Evidence: `grep "REQUIRED STATIC" {config_file}`
  Commit: Y | `feat(ansible): add Traefik entryPoint validation + docs`

- [x] 13. Client role: деплой robust шаблона + port sync assert + health check
  What to do / Must NOT do:
  **A. Деплой robust tunnel.sh на прод:**
  - Убедиться, что `ansible/roles/ssh-tunnel-client/templates/tunnel.sh.j2` уже содержит:
    - Exponential backoff (1s→60s) ✅ (уже есть, строки 68-102)
    - ControlMaster / ControlPersist ✅ (уже есть, строки 83-84)
    - SSH keepalive ✅ (уже есть, строки 79-80)
    - Cleanup handler ✅ (уже есть, строки 39-49)
    - `set -euo pipefail` ✅ (уже есть, строка 7)
  - Подтвердить тестами, что шаблон корректен

  **B. Port sync validation:**
  - В `ansible/roles/ssh-tunnel-client/tasks/main.yml` добавить pre-task:
    ```yaml
    - name: Validate ssh_tunnel_ports is not empty
      ansible.builtin.assert:
        that: ssh_tunnel_ports | length > 0
        fail_msg: "ssh_tunnel_ports is empty — no tunnels configured"
        quiet: true
    
    - name: Validate port ranges
      ansible.builtin.assert:
        that:
          - item.local_port | int > 0
          - item.local_port | int < 65536
          - item.remote_port | int > 0
          - item.remote_port | int < 65536
        fail_msg: "Invalid port in {{ item.description | default('unknown') }}: local_port={{ item.local_port }}, remote_port={{ item.remote_port }}"
      loop: "{{ ssh_tunnel_ports }}"
    ```

  **C. TCP health check (для standalone + systemd):**
  - Добавить в `ansible/roles/ssh-tunnel-client/templates/tunnel.sh.j2` опциональный health check:
    ```bash
    # Health check loop (optional, enabled when HEALTH_CHECK_PORTS is set)
    HEALTH_CHECK_PORTS="{{ ssh_tunnel_health_check_ports | default([]) | join(' ') }}"
    if [ -n "${HEALTH_CHECK_PORTS}" ]; then
      while true; do
        for port in ${HEALTH_CHECK_PORTS}; do
          if ! timeout 3 bash -c "echo >/dev/tcp/127.0.0.1/${port}" 2>/dev/null; then
            log "WARNING: health check failed for port ${port}"
          fi
        done
        sleep "${HEALTH_CHECK_INTERVAL:-30}"
      done &
    fi
    ```
  - Переменная `ssh_tunnel_health_check_ports` в defaults/main.yml
  - Must NOT: health check не блокирует запуск туннеля (только warning)
  - Must NOT: не добавлять health check в продовый tunnel.sh (только Ansible-управляемый)
  Parallelization: Wave 3 | Blocked by: — | Blocks: —
  References: `ansible/roles/ssh-tunnel-client/tasks/main.yml:1-68`, `ansible/.../tunnel.sh.j2:1-102`
  Acceptance criteria:
  - `ssh_tunnel_ports = []` → playbook fails с "ssh_tunnel_ports is empty"
  - `ssh_tunnel_ports = [{local_port: 0, remote_port: 80}]` → fail с "Invalid port"
  - `ssh_tunnel_ports = [{local_port: 8080, remote_port: 8080}]` → pass
  - `ssh_tunnel_health_check_ports: [80]` → в сгенерированном tunnel.sh есть health check loop
  - `ssh_tunnel_health_check_ports: []` → health check loop отсутствует
  QA scenarios:
  - Happy: порты валидны → playbook pass. Evidence: play recap
  - Failure: порт 0 → fail. Evidence: stderr содержит "Invalid port"
  - Happy: health check порт 80 (nginx) → health check success, нет warning. Evidence: journalctl
  - Failure: health check порт 9999 (нет сервиса) → warning в логе. Evidence: journalctl grep "WARNING"
  Commit: Y | `feat(ansible): add port validation and health check`

---

### Wave 4 — Documentation + CI

- [x] 14. Добавить CI для Go компонентов
  What to do / Must NOT do: В `.github/workflows/lint.yml`:
  **A. Обновить path filters для push и pull_request:**
  Добавить Go-пути к существующим triggers:
  ```yaml
  paths:
    - "ansible/**"
    - "cmd/**"
    - "internal/**"
    - "go.mod"
    - "go.sum"
    - "Makefile"
    - ".github/workflows/lint.yml"
    - ".ansible-lint"
    - ".yamllint"
  ```
  **B. Добавить новый job `build-go`:**
  ```yaml
  build-go:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
      - name: Lint
        run: |
          go vet ./...
      - name: Build
        run: CGO_ENABLED=0 go build -ldflags="-s -w" -o /dev/null ./cmd/rtg-orchestrator/
      - name: Test
        run: go test ./... -v -count=1
  ```
  - Must NOT: дублировать существующие шаги ansible-lint/yamllint (добавить новый job)
  - Must NOT: публиковать артефакты (только build + test)
  Parallelization: Wave 4 | Blocked by: W1-T1 | Blocks: —
  References: `.github/workflows/lint.yml`
  Acceptance criteria:
  - `push` Go-файлов (`cmd/**`, `internal/**`, `go.mod`) → триггерит `build-go` job
  - `pull_request` с Go-изменениями → CI запускает `go vet`, `go build`, `go test`
  - Все три шага проходят
  QA scenarios:
  - Happy: PR с Go-изменениями → build-go job запущен. Evidence: GitHub Actions UI (check runs)
  - Happy: `go vet ./...` без ошибок → job pass
  - Happy: `go build` без ошибок → binary created
  - Failure: syntax error → CI fails, указана строка ошибки
  Commit: Y | `ci: add Go build/test/lint workflow (fix path triggers)`

- [x] 15. Обновить README с архитектурой и примерами
  What to do / Must NOT do: Обновить `/home/izyabretatel/gits/rtg/README.md`:
  - Добавить секцию "Multi-VPS Architecture":
    - ASCII-диаграмма с A (primary orchestator), H (backup), B-G (workers), Edge clients
    - Описание: Keepalived VIP, rsync sync, heartbeat
  - Добавить секцию "Service Registry (rtg-orchestrator)":
    - Быстрый старт: `bin/rtg-orchestrator --port 8443 --config-dir /etc/traefik/dynamic/tunnels/`
    - Пример регистрации: `curl -X POST ...`
  - Добавить секцию "ESP32 & Edge Devices":
    - Схема: ESP32 ⇢ socat gateway ⇢ SSH tunnel ⇢ VPS
    - Шаги: установить socat, настроить services.yml, запустить esp32-gateway.service
    - Пример: сенсор температуры через туннель
  - Добавить секцию "HA / Failover":
    - Keepalived установка и настройка
    - rsync config sync
    - Ожидаемое время восстановления
  - Обновить диаграмму архитектуры в README (текущая односторонняя → multi-VPS)
  - Must NOT: менять существующие секции (только добавлять новые)
  Parallelization: Wave 4 | Blocked by: — | Blocks: —
  References: `README.md:1-581`
  Acceptance criteria:
  - README содержит 4 новые секции: Multi-VPS, Service Registry, ESP32, HA/Failover
  - Все ссылки в ToC работают
  - Диаграмма архитектуры отражает multi-VPS
  QA scenarios:
  - Happy: `grep "Multi-VPS" README.md` → найдено
  - Happy: `grep "rtg-orchestrator" README.md` → найдено
  - Happy: `grep "Keepalived" README.md` → найдено
  - Happy: `grep "ESP32" README.md` → найдено
  Commit: Y | `docs: add multi-VPS architecture, service registry, ESP32, HA docs`

---

## Final verification wave
> Runs in parallel after ALL todos. ALL must APPROVE. Surface results and wait for the user's explicit okay before declaring complete.
- [x] F1. Plan compliance audit — каждый Todo выполнен? Acceptance criteria пройдены? Commit message соответствует?
- [x] F2. Code quality review — Go vet/lint, shellcheck на .sh, Ansible-lint на ролях, yamllint
- [ ] F3. Real manual QA — на продакшене: TUI открывается, registry отвечает, туннели работают после деплоя (skipped — requires production access)
- [x] F4. Scope fidelity — NOT в scope не реализованы? ESP32 NOT прошивка не тронута? WebUI NOT в этом плане?
- [x] F5. Chaos Engineering — минимум 3 эксперимента выполнились:
  - Kill + systemd restart loop (socat, tunnel, registry)
  - Network partition (iptables DROP) + reconnect
  - Go race detector: `go test -race -count=1 ./...` (0 races)

## Commit strategy
- Каждый Todo = отдельный коммит
- Формат: `type(scope): description`
- Типы: `feat`, `fix`, `ci`, `docs`
- Примеры: `feat(tui): add gum-based TUI for tunnel management`, `feat(orchestrator): scaffold Go module with health endpoint`

## Success criteria
1. TUI запускается и показывает статус туннеля (порты, активность)
2. Go registry (5MB) отвечает на 5 endpoints, генерирует Traefik config
3. Keepalived конфигурация готова к деплою (A primary, H backup)
4. rsync config sync работает (A→H, каждые 60s)
5. ESP32 gateway шаблон готов (socat + systemd + документация)
6. Ansible server role: GatewayPorts включён, UDP socat на VPS, entryPoint validation
7. Ansible client role: port validation, TCP health check, deploy robust шаблона
8. CI проходит Go build+vet+test
9. README обновлён с multi-VPS архитектурой и примерами
