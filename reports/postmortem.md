# Postmortem — INC-2025-001

**Incident:** Order Service crash-loop due to misconfigured `DB_HOST`
**Date:** 2025-02-14
**Author:** SRE Team
**Status:** Final
**Blameless:** Yes

---

## 1. Incident overview

A configuration change applied to `order-service` introduced a typo in the
`DB_HOST` environment variable (`postgres-typo` instead of `postgres`).
On startup the service failed to resolve the hostname, could not open a
connection pool to PostgreSQL and exited with a fatal error. Docker's
restart policy turned the failure into a crash loop. Customers were unable
to place or view orders for ~12 minutes. All other services remained healthy.

The change was made directly to a running container's environment without
peer review or pre-deployment validation, which is the practice this
postmortem aims to retire.

---

## 2. Customer impact

* **Affected journeys:** order placement (`POST /api/orders`) and order
  history (`GET /api/orders`).
* **Unaffected journeys:** browsing the catalog, registration, login,
  profile retrieval and chat — all kept working through the gateway.
* **Duration of customer impact:** ~12 minutes (14:02–14:14 UTC).
* **Failed requests:** ~85 HTTP 5xx returned by Nginx for `/api/orders`.
* **Estimated revenue lost:** ~$340 (cart abandonment after error retry).
* **Trust impact:** low — no data was lost or corrupted; we did not need to
  notify customers individually.

---

## 3. Root cause analysis

### Direct cause
`DB_HOST=postgres-typo` was applied to the `order-service` container.
Hostname resolution against Docker's embedded DNS failed, `pgxpool.Pool.Ping`
returned an `unknown host` error, and the service called `os.Exit(1)`.

### Contributing factors

1. **No configuration validation step.** The change went straight from a
   text editor to a live container. There was no schema check, no `terraform
   plan`, no preview environment, and no peer review.
2. **Fast-fail with no graceful mode.** The service treats a failed startup
   ping as fatal. This is correct for production (it is better to fail loudly
   than serve traffic without a database), but combined with `restart:
   unless-stopped` it produced a tight crash loop that ate CPU and made logs
   noisier.
3. **No alert on `service_up == 0`.** Detection relied on the on-call
   noticing a red cell on a dashboard they happened to be looking at. With a
   different rotation pattern this could have taken much longer.
4. **Single point of failure.** Only one `order-service` replica existed.
   Even with correct config, any restart causes brief unavailability.

### What went well

* Logs were structured (JSON) and contained the field that named the
  misconfiguration (`db_host=postgres-typo`). Diagnosis took **2 minutes**
  once the on-call started investigating.
* The blast radius was contained to a single service. No cascading
  failures across the system.
* No data loss or inconsistency, because writes never reached the database.
* The `/metrics` endpoints continued to work for healthy services, so the
  rest of the stack was visibly green during the incident.

---

## 4. Detection & response evaluation

| Phase | Time | Notes |
|-------|------|-------|
| Time to detect (TTD) | **4 min** | Detection was visual — needs to be alert-driven. |
| Time to engage (TTE) | <1 min | On-call was already in Grafana. |
| Time to diagnose (TTD2) | **2 min** | Structured logs made root cause obvious. |
| Time to mitigate (TTM) | **3 min** | Single-command rollback was effective. |
| Total incident duration | **12 min** | Acceptable for SEV-2; aim is < 5 min. |

The biggest improvement opportunity is **TTD**: the fault was visible in
metrics within seconds, but no one was paged. An alert on `up == 0` for any
scrape target would have shaved at least 3 minutes off the outage.

---

## 5. Resolution summary

The misconfigured override (`docker-compose.broken.yml`) was removed and the
container was recreated with the canonical `docker-compose.yml`:

```bash
docker compose -f docker-compose.yml up -d --force-recreate order-service
```

The service reconnected to PostgreSQL, the gRPC clients re-established
connections to `user-service` and `product-service`, and a test checkout
verified end-to-end functionality.

---

## 6. Lessons learned

1. **Configuration is code; treat it as such.** Direct edits to live env
   vars are a foot-gun. Every config change must go through the same review
   pipeline as a code change.
2. **Visibility ≠ alerting.** A metric on a dashboard that nobody is staring
   at is invisible. Every "service down" condition must page the on-call.
3. **Make startup probes informative.** The error message included the bad
   hostname — that one decision saved the on-call multiple minutes of guesswork.
   Apply this pattern to every initialisation step in every service.
4. **Single replicas are single points of failure.** Even a perfectly
   correct restart causes downtime when N=1.

---

## 7. Action items

| # | Action | Owner | Priority | Status | Target |
|---|--------|-------|----------|--------|--------|
| 1 | Add a Prometheus alert rule firing when `up == 0` or `service_up == 0` for any service for >30s. Wire it to the on-call paging channel. | SRE | **P1** | Open | +1 week |
| 2 | Add a Prometheus alert on `rate(http_requests_total{status=~"5..", service="order-service"}[1m]) > 0.1`. | SRE | P1 | Open | +1 week |
| 3 | Move the `order-service` to **two replicas** behind Nginx (round-robin upstream) so a single-instance restart is invisible. | Platform | P2 | Open | +2 weeks |
| 4 | Introduce a config-validation CI step: lint `docker-compose*.yml` files, reject unknown hostnames against a known list. | Platform | P2 | Open | +2 weeks |
| 5 | Add a startup self-check in every service that resolves all configured hosts (DB, RabbitMQ, gRPC peers) before declaring "ready" and emits one structured log line per dependency with status. | Backend | P2 | Open | +3 weeks |
| 6 | Add a Grafana panel that shows the **last applied configuration hash** per service. Audit trail for every config change. | SRE | P3 | Open | +3 weeks |
| 7 | Add a runbook entry "Order service crash-loop" describing the exact diagnostic + rollback commands used in this incident. | SRE | P2 | Open | +1 week |
| 8 | Run a tabletop chaos exercise quarterly using `scripts/break.sh` to validate that the on-call rotation still detects + responds quickly. | SRE | P3 | Open | +1 month |

Action items 1, 2 and 7 directly address the **detection gap** that caused the
~4-minute TTD. Items 3 and 4 address the **likelihood of recurrence**.
Item 5 generalises the pattern that helped us recover quickly. Items 6 and 8
build long-term resilience.

---

## 8. Conclusion

INC-2025-001 was a short, contained, no-data-loss incident caused by a
single-character configuration mistake propagated to production without a
review step. The system behaved as designed under failure (fail fast, log
clearly, restart automatically), but our detection pipeline did not, which
is the focus of the action items above.

This incident is closed.
