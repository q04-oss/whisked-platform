# Architecture

## System overview

The whisked-platform backend serves three distinct clients over a single HTTP API:

- **iOS app** — loyalty features, customer profile, in-bar stamp recording
- **Next.js public website** — brand content, Claude-powered search, Shopify integration
- **Next.js internal dashboard** — real-time analytics, funnel data, cohort analysis

These clients share the same backend but are authenticated differently and access different route groups. The iOS app signs every request with HMAC-SHA256. The website uses session tokens for identified users and anonymous visitor IDs for unidentified ones. The dashboard uses a separate credential store with its own MFA requirements.

---

## Package organization

Packages are organized by business concept, not by layer. Each domain package owns its types, repository, service, and HTTP handlers together. Cross-cutting concerns get their own top-level packages.

```
internal/domain/customers/   types.go  repository.go  service.go  handler.go  routes.go
internal/domain/loyalty/     types.go  repository.go  service.go  handler.go  routes.go
internal/domain/analytics/   types.go  repository.go  service.go  handler.go  routes.go
internal/domain/dashboard/   types.go  repository.go  service.go  handler.go  routes.go
```

This makes it possible to understand the full lifecycle of any concept — from database row to HTTP response — by reading a single directory. A contributor working on loyalty never needs to navigate to a separate `handlers/` or `repositories/` package.

Cross-cutting packages:

```
internal/platform/     typed IDs, error types, shared primitives
internal/config/       environment loading, Secret wrapper
internal/telemetry/    OpenTelemetry tracing, Prometheus metrics
internal/audit/        append-only audit log writer
internal/middleware/   HTTP middleware stack
internal/server/       router construction
```

---

## Data architecture

### Operational data

Relational, normalized, transactional. This is what the API reads and writes in the hot path. Accessed exclusively through sqlc-generated queries — the query files in `db/queries/` are the source of truth, and the generated Go in `internal/gen/dbgen/` is never edited by hand.

### Analytical data

Append-only behavioral event log. Every meaningful user interaction is recorded as an immutable event with a type, actor, and JSON payload. Current state is derived from events where practical. This separation means:

- The operational database isn't burdened with analytical read patterns
- Historical data is never overwritten — only appended
- New metrics can be computed retroactively from the event log
- The dashboard can query events without touching operational tables

### Ephemeral state (Redis)

Three uses, each with explicit semantics:

| Key pattern | Purpose | TTL |
|---|---|---|
| `whisked:nonce:<uuid>` | HMAC replay prevention | 10 min |
| `whisked:rl:<ip>:<bucket>` | Per-IP rate limit counter | 2 min |
| `whisked:rl:user:<id>:<bucket>` | Per-user rate limit counter | 2 min |
| `whisked:revoked:<jti>` | JWT revocation | Matches token expiry |

Redis is required — there is no in-process fallback for security-critical operations. The HMAC nonce check fails closed if Redis is unavailable.

---

## Request lifecycle

```
client
  → SecurityHeaders (HSTS, CSP, etc.)
  → RequestID (UUID attached to context + response header)
  → Logger (structured slog, includes request ID)
  → Recoverer (panic → 500, never crashes server)
  → RateLimit (Redis per-IP, or per-user when authenticated)
  → [RequireHMAC] (iOS routes only — HMAC + nonce validation)
  → [RequireAuth] (authenticated routes — JWT validation)
  → handler
      → service (business logic)
          → repository (sqlc-generated queries)
              → PostgreSQL
  → audit.Write (security events persisted after handler returns)
```

---

## Shopify integration

Whisked uses Shopify for retail (matcha tins, merchandise). The backend does not process payments or manage inventory. Instead:

1. Shopify sends `orders/paid` webhooks to `/v1/webhooks/shopify`
2. The webhook handler validates the Shopify HMAC signature
3. A loyalty event is written for the purchasing customer (if identifiable by email)
4. The Shopify order is stored as an immutable reference in the analytical event log

Shopify remains the source of truth for all commerce. The backend's Shopify integration is strictly additive — it reads signals from Shopify, never writes back.

---

## Anonymous-to-identified linking

Website visitors are assigned a stable `AnonymousVisitorID` stored in a first-party cookie. When a visitor creates an account or signs in, their behavioral history is linked to their `CustomerID`. The linking is one-way and permanent. If a customer deletes their account, linked anonymous history is deleted with it.

---

## Dashboard authentication boundary

The internal dashboard is a separate Next.js application with its own authentication flow. It talks to the same backend but through a dedicated `/dashboard/v1/` route group protected by dashboard-specific credentials. Dashboard users are not customers — they have a separate identity table, separate session management, and all their actions are written to `audit_log` with `actor_type = 'staff'`.

---

## Key constraints

- **No manual SQL** — all database access through sqlc-generated queries. See [ADR-002](adr/002_sqlc_typesafe_queries.md).
- **No `unsafe` package** — forbidden in application code. Dependencies that use it internally are documented.
- **Secrets never logged** — `config.Secret` implements `slog.LogValuer` returning `[REDACTED]`.
- **All mutations audited** — any handler that modifies customer data or performs an administrative action writes an audit event.
