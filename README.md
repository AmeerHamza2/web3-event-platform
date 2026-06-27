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
cmd/        service entrypoints (gateway, user, wallet, notification)
internal/   service-private logic
pkg/        shared: auth, events (NATS bus), httpx, logging, server
scripts/    smoke.sh
.github/    CI: fmt, vet, race tests, build, image build
```

## Roadmap

1. Chain Monitor + Postgres — subscribe to Geth block/log events, index on-chain
   activity.
2. Kubernetes manifests behind an ingress controller.
3. Prometheus metrics + OpenTelemetry tracing across services.
4. Raft leader election so a single Chain Monitor instance ingests at a time.
