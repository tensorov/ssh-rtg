# Draft: rtg-ngrok-replacement

## Intent
Полноценная замена ngrok на базе reverse-ssh-gateway:
- TUI (gum) для управления
- Multi-VPS архитектура (A-оркестратор, B-G-воркеры)
- Keepalived VIP failover
- HTTP API Service Registry (Go, ~5MB)
- Edge-устройства от ESP32 до Linux
- Web UI — в отдельный план (v2)

## Exploration findings

### Repo structure
- `ansible/roles/ssh-tunnel-client/` — Ansible-роль для NAT-хоста
- `ansible/roles/ssh-tunnel-server/` — Ansible-роль для VPS
- `client-script/tunnel.sh` — standalone клиент (УЖЕ с reconnect/backoff/ControlMaster)
- `client-script/install.sh` — установщик
- `client-script/config.env` — конфиг

### Production setup
- `/usr/local/bin/tunnel.sh` — 60+ `-R *:port` флагов, БЕЗ reconnect (устаревшая версия)
- `/etc/systemd/system/ssh-tunnel.service` — простой systemd
- `/opt/services/traefik/` — Traefik с LE + Cloudflare + Authentik
- VPS: 2.56.89.78 root
- GatewaysPorts=yes уже включён вручную

### Key gaps (исправленные, после Metis)
1. **C0 исправлен**: Продовый скрипт — устаревшая версия. Репозиторный шаблон УЖЕ имеет reconnect. Надо деплоить шаблон на прод (через C7).
2. **GatewayPorts**: В server role не автоматизирован. Нужна задача Ansible (lineinfile + restart sshd).
3. **Traefik entryPoints**: Роль генерирует только dynamic config. Статические entryPoints — ручная предпосылка. Решение: генерация static config snippet или validation.
4. **UDP socat**: Клиент запускает UDP→TCP. Серверная socat TCP→UDP отсутствует. Добавить в server role.
5. **Web UI**: Вынесен в отдельный план v2.

## Decisions (Approved + Metis-corrected)

| Развилка | Решение | Почему |
|----------|---------|--------|
| TUI | gum (Charm) | Выбор пользователя |
| Web UI | **Вынесен в v2** | Scope creep, Metis recommendation |
| Service Registry | HTTP API (Go, CGO_ENABLED=0, ~5-8MB) | Легче Consul, health-check |
| Config sync (A↔H) | rsync + systemd timer (однонаправленный A→H) | 0 внешних сервисов |
| Клиент→Оркестратор | Keepalived VIP (VRID 51, priority 100/90, preempt) | failover 1-5s, полное восстановление 3-65s |
| ESP32 интеграция | Linux-шлюз (socat TCP-LISTEN→ESP32) | Не трогаем прошивку |
| GatewayPorts | Ansible task: lineinfile + restart sshd | Без этого fresh VPS не работает |
| Traefik entryPoints | Ansible task: **validation** + copy-paste snippet в README | Static config нельзя split, но можно проверить и подсказать |
| TLS | Не в роли — Traefik решает сам | Факт продакшена |
| Мониторинг v1 | TCP health-check (сонет→порт) | Достаточно для edge, v2 — HTTP с задержками |
| Rate limiting / multi-VPS | Не в первой итерации | Scope management |
| Molecule тесты | **Вынесен в v2** | Scope creep, Metis recommendation |

## Registry API (5 endpoints, финальная спецификация)

```
POST   /v1/services/register    {host, port, domain, proto, ttl?}
POST   /v1/services/heartbeat   {host}
GET    /v1/services             → [{host, port, domain, proto, status, last_seen}]
DELETE /v1/services/{id}        deregister
GET    /v1/health               → {status: "ok", uptime, services_count}
```

- Port: :8443 (проверка на конфликт с matrix-federation)
- Stateless: реестр пишет/читает JSON на диске, Traefik file provider читает
- Аутентификация: v1 без auth (на management network), v2 — API key

## HA story (после Metis)

- Keepalived: VIP 10.0.0.10, VRRP VRID 51, A=priority 100, H=priority 90, preempt
- rsync: A→H по SSH, каждые 60s, только dynamic config
- При failover: VIP переезжает на H за 1-5s. SSH-туннели рвутся. Клиентский backoff (1→60s) переподключается к VIP → новый мастер. Полное восстановление: 3-65s.
- Split-brain: Keepalived preempt + VRRP multicast исключает split-brain в нормальном режиме. При полной сетевой изоляции — обе VPS активны. Документировать как known limitation.

## Components ledger (финальный)

| ID | Component | Outcome | Wave |
|----|-----------|---------|------|
| C1 | TUI (gum) | `rtg-tui` — статус, порты, старт/стоп/рестарт, логи | Wave 0 |
| C2 | HTTP API Registry (Go) | `rtg-orchestrator` — 5 эндпоинтов, config gen, heartbeat | Wave 1 |
| C3 | Keepalived VIP | deploy/keepalived/keepalived.conf + инструкция | Wave 2 |
| C4 | rsync config sync | systemd service+timer A→H | Wave 2 |
| C5 | Linux-шлюз для ESP32 | socat bridge + systemd unit | Wave 2 |
| C6 | Server role улучшения | GatewayPorts, UDP socat, entryPoint validation, sysctl | Wave 3 |
| C7 | Client role улучшения | Деплой шаблона на прод, port sync assert, health check | Wave 3 |
| C8 | Web UI | **(v2 — вынесен)** | — |
| C9 | Документация | README с архитектурой, HA, примеры ESP32 | Wave 4 |
| C10 | CI для Go | Go build/test/lint в GitHub Actions | Wave 4 |

## Non-goals (explicit — Must-NOT-Have)
- Web UI (v2)
- Molecule Ansible тесты (v2)
- Rate limiting / IP whitelist на уровне туннеля (Traefik middleware)
- Prometheus/Grafana мониторинг (v2)
- Multi-DC, multi-cluster
- Агент на ESP32 (прошивка не меняется)

## Status: approved (Metis findings folded)
