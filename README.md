# Web3 Event Platform

An event-driven microservices platform in Go. A public API gateway fronts
several independently deployable services that communicate asynchronously over a
NATS message bus. `docker compose up` runs the whole stack; `make smoke`
exercises it end to end.

## Architecture

```
                    ┌──────────────┐
   client ──REST──▶ │ API Gateway  │  JWT issue/verify · RBAC · rate-limit
                    └──────┬───────┘  (only public service)
          ┌────────────────┼────────────────┐
          ▼                ▼                 │  X-Auth-Subject / X-Auth-Role
   ┌─────────────┐  ┌─────────────┐          │  (verified identity headers)
   │ User Service│  │Wallet Service│         │
   └──────┬──────┘  └──────┬───────┘         │
          │ publish        │ publish         │
          └────────┬───────┴─────────────────┘
                   ▼
            ┌─────────────┐   user.created
            │    NATS      │   wallet.created  ──▶ ┌──────────────────┐
            │  (event bus) │   transaction.*       │ Notification Svc │
            └─────────────┘   payment.*            └──────────────────┘
```

| Service | Role | Port |
|---|---|---|
| `gateway` | OAuth2 token issuance, JWT verification, RBAC, rate limiting, reverse proxy | 8080 (public) |
| `user` | User identities; publishes `user.created` | 8081 |
| `wallet` | Ethereum key custody (encrypted keystore); publishes `wallet.created` | 8082 |
| `notification` | Event consumer | 8083 |
| `chainmonitor` | Follows an EVM chain: indexes blocks + ERC-20 transfers into Postgres, handles reorgs, publishes `chain.*` | 8084 |
| `margin` | Portfolio margin engine: values an account's on-chain holdings and computes a health factor / margin status | 8085 |

### Chain Monitor

`chainmonitor` follows the chain tip over JSON-RPC and indexes blocks and ERC-20
`Transfer` logs into Postgres. It is built for the realities of an L1:

- **Event/log decoding.** `Transfer(address,address,uint256)` logs are filtered
  by topic and decoded via the parsed ABI (indexed args from topics, value from
  data).
- **Reorg handling.** Each block is checked for parent-hash linkage and the tip
  is re-validated every poll; on divergence the affected blocks are rolled back
  (transfers cascade-delete) and re-indexed on the canonical chain.
- **Confirmations / finality.** A block is marked `confirmed` only after a
  configurable depth (`CONFIRMATIONS`), so reorgs only ever touch unconfirmed
  data.

The reorg, confirmation-gating, and decode logic are unit-tested against a fake
chain backend, so the suite runs with no network or node.

> **RPC endpoint:** sustained indexing needs a node that serves historical
> blocks and `eth_getLogs` (e.g. a free Alchemy/Infura key) — set `ETH_RPC_URL`.
> The bundled public endpoint follows the tip but rejects archive/log queries,
> so the monitor will index near-tip blocks and then log-and-retry. It degrades
> gracefully rather than crashing.

### Margin engine

`margin` is a simplified DeFi prime-broker risk layer. Given an account, it:

- **Reads on-chain positions** — ERC-20 `balanceOf` via `eth_call` (ABI-bound).
- **Values them** — each holding is marked to USD through a `PriceOracle`
  (Chainlink `latestRoundData` adapter; a static oracle backs tests).
- **Applies collateral factors** — per-asset haircuts give the weighted
  collateral (borrowing power).
- **Computes a health factor** — `weighted collateral / debt`, and a status:
  `healthy` → `margin_call` → `liquidatable`.

```
GET /api/v1/margin/{address}?debt_usd=1500
{ "gross_collateral_usd": "2000.00", "weighted_collateral_usd": "1600.00",
  "debt_usd": "1500.00", "health_factor": "1.0667", "status": "margin_call", ... }
```

The valuation and health-factor math is exact (`big.Rat`) and unit-tested at the
threshold boundaries (HF = 1.15 and 1.00), where a risk engine's correctness
actually matters. On-chain reads sit behind `BalanceReader` / `PriceOracle`
interfaces, so the risk logic tests with no node. Configure the collateral
registry (token, decimals, Chainlink feed, haircut) via `ASSETS_JSON`.

Producers publish domain events and don't know who consumes them; adding a
consumer is a deployment, not a change to producers. The `events.Bus`,
`user.Store`, and keystore boundaries make NATS→Kafka, in-memory→Postgres, and
keystore→KMS swaps implementation changes rather than redesigns.

## Run

```bash
docker compose up --build      # NATS + 4 services; gateway on :8080
make smoke                     # token → user → wallet → RBAC check
make logs                      # watch the notification service react to events
```

Manual flow:

```bash
TOKEN=$(curl -s -X POST localhost:8080/api/v1/auth/token \
  -H 'Content-Type: application/json' \
  -d '{"client_id":"admin-client","client_secret":"admin-secret"}' \
  | grep -o '"access_token":"[^"]*"' | cut -d'"' -f4)

curl -X POST localhost:8080/api/v1/users \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"email":"alice@example.com"}'

curl -X POST localhost:8080/api/v1/wallets -H "Authorization: Bearer $TOKEN"
curl localhost:8080/api/v1/wallets -H "Authorization: Bearer $TOKEN"   # admin-only
```

Without Docker: `make build && make test && make vet`.

## Security

- HS256 JWTs; the verifier pins the algorithm and rejects `alg=none` (tested).
- Gateway refuses to start in `ENV=production` with the default JWT secret.
- Wallet keys stored via go-ethereum's keystore (scrypt + AES-128-CTR + Keccak
  MAC); keys never leave the service.
- Services ship as static binaries on a distroless non-root base.
- Per-IP rate limiting, request-ID propagation, panic recovery, slowloris
  `ReadHeaderTimeout`.

## Layout

```
cmd/        service entrypoints (gateway, user, wallet, notification, chainmonitor, margin)
internal/   service-private logic (chainmonitor: indexer/reorg/decode; margin: valuation/risk)
pkg/        shared: auth, events (NATS bus), httpx, logging, server
migrations/ Postgres schema for the chain indexer
deploy/k8s/ Kubernetes manifests (Deployments, Services, HPA, Ingress)
loadtest/   dependency-free concurrent HTTP load generator
scripts/    smoke.sh
.github/    CI: fmt, vet, race tests, build, image build
```

## Scalability

The platform is built to scale horizontally, and the design was validated with a
load test (`loadtest/`) rather than asserted.

**What makes it scale:**

- **Stateless services.** The user service keeps state in Postgres, not in
  process, so it runs as N replicas behind the gateway (`replicas: 2` in Compose;
  an HPA to 20 in k8s). The gateway and margin engine are stateless too.
- **Shared rate limiting.** A Redis-backed fixed-window limiter enforces one
  limit across all gateway replicas (the in-memory limiter only bounds a single
  instance).
- **Kubernetes.** `deploy/k8s/` has Deployments, Services, readiness/liveness
  probes, resource requests/limits, HorizontalPodAutoscalers (gateway → 30,
  user → 20), and an Ingress. Stateful singletons (wallet custody, chain
  monitor) are intentionally *not* autoscaled — and the README says why.

**Measured** (full stack in Docker on a laptop, 100 concurrent clients, 15s):

| Path | Throughput | p50 | p99 | Errors |
|---|---|---|---|---|
| Gateway edge (`/healthz`) | ~1,980 req/s | 35 ms | 150 ms | 0 |
| Authed margin (JWT verify → proxy → engine) | ~1,600 req/s | 53 ms | 195 ms | 0 |

**What the load test caught:** the authed path initially failed ~60% of requests
with 502s under concurrency. The cause was Go's default reverse-proxy transport
(`MaxIdleConnsPerHost = 2`) churning connections; raising the idle pool took the
path from 167 → ~1,600 req/s with zero errors. That fix is in
[`internal/gateway/gateway.go`](internal/gateway/gateway.go).

**Honest limits:** these are single-node-infra numbers on a laptop (one Postgres,
one Redis), not a proof of "millions of users." The point is that the
architecture scales *linearly with replicas* and has no per-instance state in the
hot path — getting to very high scale is a matter of more replicas plus Postgres
read replicas / connection pooling and load testing at that size, not a redesign.

## Roadmap

1. Kubernetes manifests behind an ingress controller.
2. Prometheus metrics + OpenTelemetry tracing across services.
3. Raft leader election so a single Chain Monitor instance ingests at a time.
