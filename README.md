# Web3 Event Platform

An **event-driven microservices platform in Go**. A public API Gateway fronts
several independently deployable services that communicate asynchronously over a
**NATS** message bus. It demonstrates the backend architecture a Web3 product
runs on: edge auth, service decomposition, a message broker as the spine, and
encrypted Ethereum key custody.

> Built MVP-first as a coherent, runnable system — `docker compose up` brings up
> the whole stack and `make smoke` exercises it end-to-end. The design leaves
> clean seams for the next slices (chain monitor + Postgres, Kubernetes,
> observability, Raft leader election).

---

## Architecture

```
                    ┌──────────────┐
   client ──REST──▶ │ API Gateway  │  JWT issue/verify · RBAC · rate-limit
                    └──────┬───────┘  (the only public service)
          ┌────────────────┼────────────────┐
          ▼                ▼                 │  trusted identity headers
   ┌─────────────┐  ┌─────────────┐          │  (X-Auth-Subject / X-Auth-Role)
   │ User Service│  │Wallet Service│         │
   │ (in-mem*)   │  │ (keystore)   │         │
   └──────┬──────┘  └──────┬───────┘         │
          │ publish        │ publish         │
          └────────┬───────┴─────────────────┘
                   ▼
            ┌─────────────┐   user.created
            │    NATS      │   wallet.created   ──▶ ┌──────────────────┐
            │  (message    │   transaction.*        │ Notification Svc │
            │   bus)       │   payment.*            │ (consumer)       │
            └─────────────┘                         └──────────────────┘

  * in-memory store implements a Store interface; Postgres is the prod swap.
```

**Services** (each is its own `cmd/` binary and Docker image):

| Service | Role | Port |
|---|---|---|
| `gateway` | Public edge: OAuth2 token issuance, JWT verification, RBAC, rate limiting, reverse proxy | 8080 (public) |
| `user` | User identities; publishes `user.created` | 8081 (internal) |
| `wallet` | Ethereum key custody (encrypted keystore); publishes `wallet.created` | 8082 (internal) |
| `notification` | Pure event consumer; reacts to bus events | 8083 (internal) |

## Why this shape

- **Event-driven, not request-driven.** Producers publish domain events and walk
  away; they don't know consumers exist. Adding analytics/audit/indexer
  consumers is a deployment, not a change to producers. The bus is the
  decoupling boundary (`pkg/events`).
- **Edge owns auth.** The gateway is the single place that issues and verifies
  tokens, enforces RBAC, and rate-limits. Internal services stay small and trust
  the gateway-injected identity headers over the private network.
- **Swappable transports & stores.** `events.Bus`, `user.Store`, and
  `wallet.Signer`-style seams mean NATS→Kafka, in-memory→Postgres, and
  keystore→KMS/HSM are config/implementation swaps, not redesigns.

## Run it

```bash
docker compose up --build      # NATS + all 4 services; gateway on :8080
make smoke                     # end-to-end: token → user → wallet → RBAC check
make logs                      # watch the notification service react to events
```

Manual flow:

```bash
# 1. Get an admin token (OAuth2 client-credentials)
TOKEN=$(curl -s -X POST localhost:8080/api/v1/auth/token \
  -H 'Content-Type: application/json' \
  -d '{"client_id":"admin-client","client_secret":"admin-secret"}' \
  | grep -o '"access_token":"[^"]*"' | cut -d'"' -f4)

# 2. Register a user  → fires user.created on the bus
curl -X POST localhost:8080/api/v1/users \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"email":"alice@example.com"}'

# 3. Create a wallet  → fires wallet.created
curl -X POST localhost:8080/api/v1/wallets -H "Authorization: Bearer $TOKEN"

# 4. Admin-only listing (RBAC)
curl localhost:8080/api/v1/wallets -H "Authorization: Bearer $TOKEN"
```

Local development without Docker:

```bash
make build && make test && make vet
```

## Security design

- **JWT with pinned algorithm.** HS256 tokens; the verifier rejects `alg=none`
  and algorithm-confusion attempts (regression-tested in `pkg/auth`).
- **No secrets baked in.** The gateway refuses to start in `ENV=production` with
  the default JWT secret.
- **Encrypted key custody.** The wallet service stores keys via go-ethereum's
  keystore (Web3 Secret Storage: scrypt + AES-128-CTR + Keccak-256 MAC); private
  keys never leave the service.
- **Hardened images.** Every service ships as a static binary on a
  distroless non-root base — no shell, no package manager.
- **Edge hardening.** Per-IP token-bucket rate limiting, request-ID correlation
  propagated across services, panic recovery that never leaks internals,
  `ReadHeaderTimeout` against slowloris.

## How this maps to the role

| Requirement | Where |
|---|---|
| Go microservices | 4 independently deployable services (`cmd/*`) |
| Message broker / data streaming | NATS event bus is the backbone (`pkg/events`) |
| RESTful APIs | gateway + service REST endpoints |
| API Gateway / edge | `internal/gateway` (auth, RBAC, rate-limit, proxy) |
| AuthN/AuthZ (OAuth2, JWT, RBAC) | `pkg/auth` + gateway |
| Cryptography / key custody | `internal/wallet` (encrypted keystore) |
| Docker / containerization | per-service distroless images, Compose stack |
| CI/CD | GitHub Actions: fmt, vet, race tests, build, image build |
| Distributed systems / decoupling | event-driven pub/sub, service isolation |

## Roadmap (next slices)

Built MVP-first; each slice is a clean, explainable increment:

1. **Chain Monitor + Postgres** — a service subscribing to Geth
   `SubscribeNewHead`/log events, publishing on-chain activity to the bus and
   indexing it into Postgres (data-intensive piece).
2. **Kubernetes** — Deployment/Service/Ingress manifests; gateway behind an
   ingress controller.
3. **Observability** — Prometheus metrics + OpenTelemetry tracing across
   services (distributed-systems debugging).
4. **Raft leader election** — ensure a single Chain Monitor instance ingests
   blocks at a time (`hashicorp/raft`), demonstrating consensus/replication.

## Layout

```
cmd/            service entrypoints (gateway, user, wallet, notification)
internal/       service-private logic (gateway, user, wallet, notification)
pkg/            shared libraries
  auth          JWT issue/verify, RBAC, alg-confusion defense
  events        domain-event contract + NATS bus
  httpx         JSON helpers + middleware (request-id, recovery, logging, RBAC)
  logging       structured slog setup
  server        graceful HTTP lifecycle + env helpers
scripts/        smoke.sh end-to-end check
.github/        CI workflow
```
