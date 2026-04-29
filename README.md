# whisked-platform

Backend for [Whisked](https://visitwhisked.ca), a ceremonial matcha bar in Edmonton building toward global expansion. This repository contains the Go API server that powers the iOS loyalty app, the Next.js public website, and the internal analytics dashboard.

Built and maintained by [Rajzyngier Research](https://rajzyngierresearch.com) under a retained engagement.

---

## Architecture

The system is organized around three concerns:

- **Operational data** — what the business needs to function (customers, sessions, loyalty events, locations). Lives in PostgreSQL, accessed exclusively through sqlc-generated type-safe queries.
- **Analytical data** — what the business needs to learn from (behavioral events, funnel metrics, cohort data). Append-only event log in PostgreSQL, read by the dashboard.
- **Ephemeral state** — rate limit counters, nonce deduplication, session revocation. Lives in Redis with explicit TTLs.

See [ARCHITECTURE.md](docs/ARCHITECTURE.md) for the full picture and [docs/adr/](docs/adr/) for the reasoning behind significant decisions.

---

## Getting started

**Prerequisites**

- Go 1.22+
- PostgreSQL 15+
- Redis 7+
- [sqlc](https://sqlc.dev) — `go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest`
- [golang-migrate](https://github.com/golang-migrate/migrate) — for running migrations locally

**Local setup**

```bash
cp .env.example .env
# edit .env with your local credentials

make migrate        # run database migrations
make generate       # regenerate sqlc types from SQL queries
make run            # start the server on :8080
```

**Running tests**

```bash
make test           # unit tests with race detector
make test-int       # integration tests (requires local Postgres + Redis)
make test-security  # security property tests
```

---

## Project structure

```
cmd/server/             entry point — wires dependencies, starts server
internal/
  audit/                append-only audit log — every security event persisted
  config/               environment loading — secrets wrapped in Secret type
  domain/               business logic organized by concept
    customers/          customer profiles and identity
    loyalty/            steeps, rewards, Shopify sync
    analytics/          behavioral event ingestion and querying
    dashboard/          internal dashboard API (separate auth boundary)
  middleware/           HTTP middleware — HMAC, rate limiting, auth, logging
  platform/             shared primitives — typed IDs, error types
  server/               router and middleware chain
  telemetry/            OpenTelemetry tracing + Prometheus metrics
db/
  migrations/           SQL migration files (source of truth for schema)
  queries/              SQL query files (sqlc reads these)
internal/gen/dbgen/     sqlc-generated Go — do not edit by hand
docs/
  ARCHITECTURE.md       how the system fits together
  THREAT_MODEL.md       what we defend against and how
  adr/                  architecture decision records
```

---

## Security

The security posture is documented in [THREAT_MODEL.md](docs/THREAT_MODEL.md). Key properties:

- All secrets wrapped in `config.Secret` — cannot be logged or serialized
- Typed IDs (`CustomerID`, `OrderID`) prevent cross-domain parameter confusion
- iOS requests signed with HMAC-SHA256 + UUID nonce replay prevention via Redis
- All database access through sqlc-generated queries — SQL injection is architecturally impossible
- Every security-relevant action written to the append-only `audit_log` table
- Rate limiting per authenticated user, per IP as fallback

---

## Deployment

The server is hosted on Railway. Deployments are triggered by pushes to `main` after CI passes. The Dockerfile produces a minimal Alpine image.

**Migrations run automatically at server startup** — the migration files are embedded in the binary. No separate migration step is needed on Railway.

### Railway setup (first deploy)

1. Create a Railway project and add a PostgreSQL database and Redis instance.

2. Set the following environment variables on the server service:

   | Variable | Notes |
   |---|---|
   | `DATABASE_URL` | Provided by Railway Postgres plugin |
   | `REDIS_URL` | Provided by Railway Redis plugin |
   | `JWT_SECRET` | ≥32 random characters |
   | `HMAC_SHARED_KEY` | Shared with the iOS app |
   | `ADMIN_PIN` | ≥8 chars, not all same character |
   | `SHOPIFY_WEBHOOK_SECRET` | From Shopify Partners dashboard |
   | `ANTHROPIC_API_KEY` | Optional — enables brand chat |
   | `OTEL_EXPORTER_OTLP_ENDPOINT` | Optional — enables distributed tracing |

3. Deploy. Migrations apply automatically on first start.

4. Create the first staff account using the seed command:

   ```bash
   # Run locally with production DATABASE_URL
   DATABASE_URL="<railway-postgres-url>" make seed \
     EMAIL=admin@whisked.ca \
     NAME="Belle" \
     PASSWORD=yoursecurepassword
   ```

### Subsequent deploys

Push to `main`. Railway redeploys automatically after CI passes. Migrations apply on startup — `ErrNoChange` is treated as success.

See the [CI workflow](.github/workflows/ci.yml) for the full gate: vet, staticcheck, gosec, govulncheck, race tests.
