# SRE Microservices Project - Assignment 4

A containerized microservices system written in **Go** with full observability,
ready for the incident-response simulation required by Assignment 4.

> Tech stack used: Go (`pgxpool`, `envconfig`, `godotenv`, `gRPC`, `RabbitMQ`),
> PostgreSQL, RabbitMQ, Prometheus, Grafana, Nginx, Docker Compose.

---

## 1. Architecture

```
                     ┌──────────────────────────────────────┐
   user ────► nginx ─┤  /api/auth   →  auth-service  (HTTP) │
                     │  /api/users  →  user-service  (HTTP) │
                     │  /api/products → product-service     │
                     │  /api/orders → order-service  (HTTP) │
                     │  /api/chat   →  chat-service  (HTTP) │
                     └──────────────────────────────────────┘
                                   │
                       ┌───────────┴────────────────────────┐
                       │            gRPC mesh               │
                       │   order-service ──► user-service   │
                       │   order-service ──► product-service│
                       └────────────────────────────────────┘
                                   │
            ┌───────────┐    ┌──────────┐    ┌──────────────┐
            │PostgreSQL │    │ RabbitMQ │    │ Prometheus + │
            │           │    │ events + │    │   Grafana    │
            └───────────┘    │  chat    │    └──────────────┘
                             └──────────┘
```

| Service          | Tech                                               | Purpose |
|------------------|----------------------------------------------------|---------|
| `auth-service`   | HTTP, JWT, bcrypt, pgxpool                         | Register / login / verify token |
| `user-service`   | HTTP + **gRPC server**, pgxpool                    | Look up users (called by Order) |
| `product-service`| HTTP + **gRPC server**, pgxpool                    | List/create products, atomic stock reserve |
| `order-service`  | HTTP, **gRPC client** (User+Product), **RabbitMQ producer** | Create/list orders, publish events |
| `chat-service`   | HTTP, **RabbitMQ producer + consumer**, pgxpool    | Send/receive messages between users |

All services expose `/metrics` (Prometheus) and `/health`.

---

## 2. Prerequisites

* Docker 24+
* Docker Compose v2 (`docker compose ...`)
* ~2 GB free RAM, ~3 GB free disk (for first-time image build)

---

## 3. Quick start

```bash
get this repo, but repo is private so you have only zip file, so unzip it and cd into the folder

# Optional: override defaults.
cp .env.example .env

# Bring everything up. First build takes ~3-5 min while Go compiles 5 services.
docker compose up -d --build

# Tail logs.
docker compose logs -f
```

Open:

* Frontend:   <http://localhost/>
* Grafana:    <http://localhost:3000> (admin / admin)
* Prometheus: <http://localhost:9090>
* RabbitMQ:   <http://localhost:15672> (guest / guest)

A pre-provisioned dashboard called **"SRE Microservices Overview"** is loaded
into Grafana automatically.

### Smoke test

```bash
./scripts/smoke.sh
```

This registers a user, lists products, places an order and sends a chat message,
which generates real traffic visible on the Grafana dashboard.

---

## 4. Incident-response simulation (Assignment 4)

The full incident write-up is in [`reports/incident_report.md`](reports/incident_report.md)
and the postmortem is in [`reports/postmortem.md`](reports/postmortem.md).
PDF versions are at `reports/incident_report.pdf` and `reports/postmortem.pdf`.

### Reproducing the incident

```bash
# 1. start a healthy stack (see section 3) and verify everything is green
#    on the Grafana dashboard.

# 2. inject the failure: misconfigure order-service's DB hostname
./scripts/break.sh

# 3. observe in Grafana / Prometheus:
#    - service_up{service="order-service"} drops to 0
#    - http_requests_total{service="order-service"} stops increasing
#    - 5xx rate on order-service spikes
#    Try placing an order from the frontend - it fails at the gateway with 502.

# 4. inspect logs to confirm root cause
docker compose logs --tail=30 order-service
# You'll see lines like:
#   "FATAL: order-service cannot connect to database"
#   db_host=postgres-typo ...

# 5. mitigate: restore the correct hostname
./scripts/fix.sh

# 6. confirm restoration in Grafana - service_up returns to 1, traffic resumes.
```

---

## 5. Project layout

```
.
├── cmd/                    # one main.go per service
│   ├── auth-service/
│   ├── user-service/
│   ├── product-service/
│   ├── order-service/
│   └── chat-service/
├── internal/               # service-private code + shared pkg
│   ├── auth/  user/  product/  order/  chat/
│   └── pkg/                # config, db, jwt, logger, metrics, rabbitmq
├── proto/                  # *.proto + generated *.pb.go
│   ├── userpb/
│   └── productpb/
├── frontend/               # static HTML/JS/CSS served by nginx
├── nginx/                  # reverse-proxy config + Dockerfile
├── monitoring/
│   ├── prometheus/
│   └── grafana/            # provisioning + dashboards
├── migrations/             # init.sql (loaded by postgres on first boot)
├── reports/                # Incident report + Postmortem (md + pdf)
├── scripts/                # break.sh / fix.sh / smoke.sh
├── docker-compose.yml      # healthy stack
├── docker-compose.broken.yml  # incident-injection override
├── Dockerfile              # generic Go service builder (ARG SERVICE)
├── go.mod / go.sum
└── README.md
```

---

## 6. Useful commands

```bash
# Rebuild a single service after code change.
docker compose up -d --build order-service

# Open a psql shell.
docker compose exec postgres psql -U app

# Tail one service's logs.
docker compose logs -f order-service

# Stop everything (keeps data).
docker compose down

# Stop and wipe all data.
docker compose down -v
```



