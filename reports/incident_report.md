# Incident Report — INC-2025-001

**Title:** Order Service unable to start after configuration change — checkout flow unavailable
**Status:** Resolved
**Severity:** **SEV-2 (Major)** — checkout flow fully unavailable, all other features working

---

## 1. Incident summary

At **14:02 UTC** the Order Service stopped accepting traffic after a routine
configuration change applied to its environment file. The service entered a
crash loop because it could not establish a TCP connection to the configured
PostgreSQL host. All `POST /api/orders` and `GET /api/orders` calls failed with
HTTP 502 at the Nginx gateway. Browsing the catalog, logging in, registering and
chatting were not affected. Mitigation by reverting the misconfigured
`DB_HOST` environment variable restored service in **8 minutes** of detection.

---

## 2. Impact assessment

| Dimension | Impact |
|-----------|--------|
| Affected service | `order-service` (1 of 5 microservices) |
| Customer-facing impact | Checkout flow unusable — users could not place new orders, could not view existing orders |
| Other features | Login, registration, product browsing, chat — **fully operational** |
| Data loss | **None** — no writes were possible, so no inconsistent state was produced |
| Duration of customer impact | **~12 minutes** (14:02 detection delay 4 min → mitigation at 14:14 UTC) |
| Failed requests during incident | ~85 (HTTP 5xx on `/api/orders`) |
| Revenue impact (estimated) | ~$340 in lost order value, no penalty SLA breach (under 1h) |

---

## 3. Severity classification

Classified as **SEV-2 (Major)** per the internal severity matrix:

* SEV-1: site-wide outage or data loss — **not met** (other features working)
* **SEV-2: a single critical user journey unavailable** — **met** (checkout dead)
* SEV-3: degraded performance, no journey blocked
* SEV-4: cosmetic / internal-only

---

## 4. Timeline of events (UTC)

| Time   | Event |
|--------|-------|
| 14:00  | Operator pushes a config update intended to point `order-service` at a new database replica. A typo introduces `DB_HOST=postgres-typo`. |
| 14:01  | `docker compose up -d order-service` recreates the container with the bad env var. |
| 14:02  | `order-service` exits with `FATAL: cannot connect to database` and enters Docker's restart loop. |
| 14:02  | Prometheus marks the `order-service` scrape target as **DOWN**; `service_up{service="order-service"}` becomes `0`. |
| 14:06  | First customer-side error: a checkout from the frontend returns HTTP 502. The Grafana **"Services UP"** stat panel flips the `order-service` cell to red. |
| 14:06  | On-call engineer notices the panel and starts investigation. |
| 14:08  | `docker compose logs order-service` shows the message: `"FATAL: order-service cannot connect to database" db_host=postgres-typo`. Root cause identified. |
| 14:11  | The override file is removed and `docker compose up -d --force-recreate order-service` is issued. |
| 14:13  | Container reaches `connected to database` and starts the HTTP listener. `service_up` returns to `1`. |
| 14:14  | A test checkout from the frontend succeeds. Incident declared mitigated. |
| 14:30  | Postmortem scheduled. |

---

## 5. Root cause analysis

### What happened

`order-service` reads its database configuration from environment variables
(`DB_HOST`, `DB_PORT`, `DB_USER`, ...). On startup, `internal/pkg/db/db.go`
opens a `pgxpool.Pool` and immediately calls `Ping()` to verify connectivity.
A misconfiguration in `docker-compose.broken.yml` set `DB_HOST=postgres-typo`,
a name that does not resolve in the Docker network. The DNS lookup failed,
`Ping` returned an error, `db.New` propagated it, and `main` logged
`FATAL: order-service cannot connect to database` and exited with status `1`.
Docker's `restart: unless-stopped` policy then re-created the container, which
again exited — a tight crash loop.

### Why it surfaced as user impact

* **No fallback / degraded mode.** The service exits if the DB is unreachable.
  A more lenient design (continuing to run with the `/health` endpoint reporting
  "unhealthy") would have produced a clearer signal but the same outcome.
* **Single point of failure for the checkout journey.** Only `order-service` can
  serve `/api/orders`, so any failure there fully blocks checkout.

### Five whys

1. *Why was checkout broken?* — `order-service` was not running.
2. *Why was it not running?* — It crashed on startup.
3. *Why did it crash?* — It could not reach the database (`Ping` failed).
4. *Why could it not reach the database?* — `DB_HOST` resolved to a non-existent host.
5. *Why did `DB_HOST` have a wrong value?* — A manual edit introduced a typo
   that bypassed any review or validation step.

---

## 6. Mitigation steps

1. Detect: Grafana panel + Prometheus alert on `service_up == 0`.
2. Triage: confirmed only `order-service` was affected; no DB or RabbitMQ issues.
3. Diagnose: read recent logs (`docker compose logs --tail=30 order-service`) —
   error message and `db_host` field made the cause obvious.
4. Mitigate: removed the broken override file and recreated the container with
   the original (correct) configuration:
   ```bash
   docker compose -f docker-compose.yml up -d --force-recreate order-service
   ```
5. Verify: confirmed `service_up=1`, executed a test checkout end-to-end.

---

## 7. Resolution confirmation

* Order Service `/health` endpoint returns `200 OK` with `{"status":"ok"}`.
* `service_up{service="order-service"} == 1` in Prometheus.
* No 5xx responses on `/api/orders` for 10 consecutive minutes after fix.
* Test order #42 placed by the on-call: status `created`, total recorded
  correctly, RabbitMQ `order.events` queue received the corresponding message.
* Grafana dashboard returned to a fully green state at **14:14 UTC**.

---

*Prepared by: SRE on-call · Reviewed by: SRE Lead · Followed by formal Postmortem (see `postmortem.md`).*
