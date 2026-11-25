# patrolbox

**A zero-config watchdog for your self-hosted server.** One binary, no database, no web panel, no agent to keep running.

patrolbox finds the sites you host by reading your Caddy, nginx and Docker setup, checks them, and messages you when something breaks — and again when it recovers.

🌍 [Türkçe](docs/README.tr.md)

```console
$ patrolbox
Config: none (built-in defaults)

CRIT  Certificate  shop.example.com  expires in only 3 days (2026-01-04), issued by R11
WARN  Disk         /                 getting full: 84% used, 12.3 GB free
OK    Backup       database          latest backup db-2026-01-01.sql.gz is 5h old, 48.2 MB
OK    Container    app               running, up 3d
OK    Container    postgres          running, up 12d
OK    Domain       example.com       registration valid, expires in 212 days (2026-08-01)
OK    Website      example.com       HTTP 200 in 142 ms
OK    Website      shop.example.com  HTTP 200 in 310 ms
OK    Certificate  example.com       certificate valid, expires in 61 days (2026-03-03), issued by R10

7 ok, 1 warning, 1 critical, 0 unknown
```

## What it checks

| Check | Problem it catches |
|---|---|
| **Website** | Site down, error status, slow response, expected text missing |
| **Certificate** | TLS certificate expiring, expired, or not valid for the host |
| **Domain** | Domain registration about to lapse (via RDAP) |
| **Container** | Docker containers crash-looping, exited, unhealthy or OOM-killed |
| **Disk** | Filesystems running out of space or inodes |
| **Backup** | Newest backup file too old, empty or suspiciously small |

Sites are discovered from:

- **Caddy** — Caddyfiles (including imports) and the admin API
- **nginx** — `server_name` of every TLS server block, following `include`
- **Docker** — caddy-docker-proxy and Traefik labels on running containers

Run `patrolbox discover` to see what it finds. Anything it cannot see goes under `sites:` in the config.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/ferhalemre/patrolbox/main/install.sh | sh
```

This downloads the latest release binary for your platform into `/usr/local/bin` and verifies its checksum. Or build it yourself:

```bash
go install github.com/ferhalemre/patrolbox/cmd/patrolbox@latest
```

## Use

```bash
patrolbox              # run every check once
patrolbox discover     # list the sites it found
patrolbox init         # write patrolbox.yml with every option explained
patrolbox test         # send a test notification
```

Then let cron run it. Every five minutes is a good default:

```cron
*/5 * * * * /usr/local/bin/patrolbox --quiet --config /etc/patrolbox/patrolbox.yml
```

patrolbox remembers the previous result of every check, so you get **one message when something breaks and one when it recovers** — not one every five minutes. Set `notify.repeat` to be reminded about problems that are still open.

Flags: `--config PATH`, `--lang CODE`, `--json`, `--quiet` (problems only), `--no-notify`.

Exit codes follow the monitoring-plugin convention: `0` ok, `1` warning, `2` critical, `3` unknown — so patrolbox also works as a check inside other tools.

## Notifications

Telegram, Slack, Discord, ntfy, email (SMTP) and a generic JSON webhook. Configure any number of them:

```yaml
notify:
  repeat: 24h
  telegram:
    bot_token: "${TELEGRAM_TOKEN}"
    chat_id: "123456789"
```

`${NAME}` is read from the environment, so secrets can stay out of the file.

## Configuration

Everything is optional; with no config file patrolbox uses sensible defaults. `patrolbox init` writes a commented file with every option. A short example:

```yaml
lang: en
exclude: ["staging.*"]

tls:
  warn_days: 14
  crit_days: 5

backups:
  - name: database
    path: /var/backups/db/*.sql.gz
    max_age: 26h

docker:
  require: [postgres, "app-*"]
```

The config file is looked up at `./patrolbox.yml`, then `/etc/patrolbox/patrolbox.yml`.

## Languages

Output and notifications are available in English, Turkish, German, Spanish, French, Portuguese, Russian and Chinese. The language is taken from your system (`LANG`), or set it with `lang:` in the config or `--lang`.

Adding a language is one folder of JSON: copy `internal/i18n/locales/en` to your language code and translate the values. `go test ./...` checks that no key or placeholder is missing.

## How it compares

Tools like Uptime Kuma and Gatus are excellent when you want dashboards, history and status pages. patrolbox is for the case where you only want to be told when something is wrong: nothing to host, nothing to click through, and no list of sites to maintain by hand. It also looks at the server from the inside (containers, disks, backups), which an external uptime checker cannot do.

## Requirements

Linux for the container and disk checks. Website, certificate, domain and backup checks also work on macOS and Windows. Reading the Docker socket needs root or membership of the `docker` group.

## Contributing

Issues and pull requests are welcome. `go test ./...` must pass; tests never touch the real network.

## License

[MIT](LICENSE)
