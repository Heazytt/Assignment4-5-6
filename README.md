# Assignment 6 — Automation & Capacity Planning

This folder contains the additions made on top of the Assignment 4/5 project.
Drop these files into your existing project directory (replacing where they
overlap) and rebuild:

```
docker compose down
docker compose up -d --build
```

## What's new

### Files added
- `monitoring/prometheus/alerts.yml` — alert rules (service down, high error rate, latency, CPU, memory, restart loops)
- `monitoring/alertmanager/alertmanager.yml` — Alertmanager routing config
- `scripts/loadtest.sh` — concurrent load generator
- `scripts/validate-config.sh` — pre-deployment config validator
- `scripts/log-inspect.sh` — automated log pattern detection

### Files updated
- `docker-compose.yml`
  - `healthcheck` added to all 5 Go services
  - `deploy.resources.limits` (CPU + memory caps) on every service
  - New containers: `alertmanager`, `cadvisor`, `node-exporter`
- `monitoring/prometheus/prometheus.yml` — loads alerts, points to Alertmanager, scrapes cAdvisor + node-exporter
- `monitoring/grafana/dashboards/sre-overview.json` — adds capacity panels (CPU/memory/network per container, host CPU/RAM)

## New endpoints

| URL | Purpose |
|-----|---------|
| http://localhost:9093 | Alertmanager UI |
| http://localhost:8081 | cAdvisor (container metrics) |
| http://localhost:9100 | node-exporter (host metrics) |
| http://localhost:9090/alerts | Prometheus alerts page |

## Workflows

### Validate before deploy
```bash
./scripts/validate-config.sh
```

### Generate load and watch the dashboard
```bash
./scripts/loadtest.sh http://localhost 60 20    # 60s, 20 concurrent workers
```

### Inspect logs for known failure patterns
```bash
./scripts/log-inspect.sh                         # all services
./scripts/log-inspect.sh order-service           # one service
SINCE=10m ./scripts/log-inspect.sh               # last 10 minutes only
```

### Trigger an alert to test the pipeline
```bash
./scripts/break.sh                # the same script from Assignment 4
# Wait ~30s — `ServiceDown` alert appears in Alertmanager (http://localhost:9093)
./scripts/fix.sh                  # restores service, alert auto-resolves
```
