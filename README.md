
# AuctionBidGo – Quick Start

A minimal Redis‑backed live‑auction service written in Go – complete with REST & WebSocket APIs, Swagger docs, and a tiny browser UI.

---

## 1. Prerequisites

| Tool | Purpose | Tested with |
|------|---------|------------|
| **Go** | build/run the service | 1.24 |
| **Docker & Compose** | spin‑up Redis & Postgres | 24.0 |
| **Make** | generate Swagger spec | any POSIX make |

---

## 2. Configuration

Copy the sample file and tweak as needed:

```bash
cp env.example .env
````

All keys already default to localhost values, so you usually don’t need to change anything.

---

## 3. Start Redis & Postgres

```bash
docker compose -f docker/docker-compose.yaml up -d
```

* Redis 7 on **6379**
* Postgres on **5432** (schema + seed scripts auto‑run)
* Adminer UI on **[http://localhost:9191](http://localhost:9191)**

---

## 4. Generate / update the OpenAPI spec

```bash
make swag          # → api_specs/all_apis_swagger.yaml
```

You can now explore every endpoint via Swagger UI (next section).

---

## 5. Run the Go service

```bash
go run ./...       # listens on :8085
```

---

## 6. Try it out

| What                      | URL                                                                                                                                                    |
| ------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **Swagger UI – all APIs** | [http://localhost:8085/swagger-apis/?url=/api-specs/all\_apis\_swagger.yaml](http://localhost:8085/swagger-apis/?url=/api-specs/all_apis_swagger.yaml) |
| **Demo Web UI**           | [http://localhost:8085/](http://localhost:8085/)                                                                                                       |

### Typical flow

1. **Start an auction**

   ```bash
   curl -X POST \
     "http://localhost:8085/auctions/auc123/start" \
     -H 'Content-Type: application/json' \
     -d '{"seller_id":"seller123","ends_at":"2025-12-31T23:59:00Z"}'
   ```

2. **WebSocket stream**

   Open the browser UI, enter `auc123`, click **Connect** – events/bids arrive live.

3. **REST actions**

   * `POST /auctions/{id}/bid` – place a bid
   * `POST /auctions/{id}/stop` – stop early
   * `GET  /auctions` – list finished / running auctions

All requests are documented in Swagger.

---

## 7. Tear down

```bash
docker compose -f docker/docker-compose.yaml down -v
```
