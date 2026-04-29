# Threat Model

This document describes what the whisked-platform defends against, what it does not defend against, the assumptions the security posture relies on, and how those assumptions might be violated.

It is a living document. When architecture evolves, this document is updated in the same commit.

---

## What we're protecting

- **Customer PII** — names, emails, behavioral history, purchase history
- **Payment-adjacent data** — Shopify order references, loyalty redemption records
- **Business intelligence** — conversion funnels, cohort data, revenue metrics that represent competitive advantage
- **System integrity** — the loyalty ledger must be accurate; fraudulent steeps or redemptions have direct revenue impact

---

## Threat inventory

### T1 — Customer data exfiltration

**What it looks like:** An attacker reads customer records, behavioral histories, or the analytics database in bulk.

**Defenses:**
- All database access through sqlc-generated parameterized queries — SQL injection is architecturally eliminated
- JWT authentication required on all customer data endpoints
- Per-user rate limiting constrains bulk enumeration even with valid credentials
- Dashboard access to customer data is audit-logged with actor, timestamp, and query parameters
- Database credentials are Railway-managed secrets, never in source control

**Residual risk:** A compromised Railway account or a dependency with a remote code execution vulnerability could bypass application-layer controls. Mitigated by dependency hygiene (govulncheck in CI) and Railway's access controls.

---

### T2 — Payment fraud / loyalty abuse

**What it looks like:** An attacker earns steeps without making purchases, redeems rewards multiple times, or replays a valid redemption request.

**Defenses:**
- In-bar stamp recording requires a valid HMAC-signed request from the iOS app
- All loyalty mutations are idempotent — duplicate requests are detected via Redis `SET NX EX` and rejected with 409
- Shopify webhook integrity is verified with Shopify's HMAC signature before any loyalty credit is applied
- Every steep earn and reward redemption is written to `audit_log` — anomaly patterns are detectable
- Loyalty operations are database transactions — partial state is impossible

**Residual risk:** A compromised `HMAC_SHARED_KEY` would allow forged iOS requests. Key rotation procedure is documented in [ARCHITECTURE.md](ARCHITECTURE.md). Key compromise requires a coordinated deploy + key rotation.

---

### T3 — Account takeover

**What it looks like:** An attacker gains access to a customer's account — by credential stuffing, session theft, or token forgery.

**Defenses:**
- Passwords hashed with Argon2id at conservative cost parameters — credential stuffing against the database yields no usable passwords
- JWT access tokens are short-lived (15 minutes); refresh tokens are longer-lived but Redis-revocable
- All `jti` claims checked against Redis revocation set on every authenticated request
- Authentication events (login, logout, token refresh, failed attempts) written to `audit_log`
- Rate limiting on authentication endpoints is stricter than general endpoints

**Residual risk:** JWT secret compromise allows arbitrary token forgery. The `JWT_SECRET` is a Railway-managed secret with no standing read access. Rotation requires a deploy + forced re-authentication of all users.

---

### T4 — Denial of service

**What it looks like:** An attacker floods the API to make it unavailable to legitimate users.

**Defenses:**
- Per-IP rate limiting at the middleware layer — 60 requests/minute baseline
- Per-user rate limiting when authenticated — stricter limits on expensive endpoints
- Request body size capped at 1 MB — large payload attacks rejected before handler runs
- Read/write timeouts enforced at the HTTP server level
- Redis rate limit failure mode is fail-open — Redis unavailability degrades rate limiting but does not take down the API

**Residual risk:** A volumetric attack at Railway's infrastructure layer is outside application control. Railway provides DDoS mitigation at the network level; application-layer rate limiting handles the long tail.

---

### T5 — Supply chain attacks

**What it looks like:** A malicious or compromised dependency introduces backdoor code, data exfiltration, or cryptographic weaknesses.

**Defenses:**
- `govulncheck` runs in CI on every commit and blocks merges with known vulnerabilities
- All dependencies pinned via `go.sum` — the exact dependency graph is reproducible from any commit
- Dependabot monitors the dependency graph for newly published advisories
- The `unsafe` package is forbidden in application code; any transitive dependency using it is explicitly documented and audited
- Minimal dependency surface — standard library used wherever it suffices

**Residual risk:** A zero-day in a dependency that has not yet been published as a vulnerability advisory is not detectable by govulncheck. The minimal dependency surface reduces exposure.

---

### T6 — Insider threats

**What it looks like:** A Whisked staff member or Rajzyngier Research engineer abuses privileged access.

**Defenses:**
- Production database access requires explicit elevation — no standing access
- All dashboard queries against customer data are written to `audit_log` with actor identity
- Admin API endpoints require an `ADMIN_PIN` in addition to authentication — knowledge factor separate from possession
- CI/CD pipeline access is restricted and logged
- Audit log table is append-only at the database level (trigger prevents UPDATE/DELETE)

**Residual risk:** An engineer with direct database access can bypass application-layer audit logging. This is a fundamental limitation of application-level auditing — it is not a substitute for database-level audit logging, which is a future improvement.

---

### T7 — Request forgery (iOS app)

**What it looks like:** An attacker intercepts or replays a valid signed iOS request to perform an action twice.

**Defenses:**
- Every iOS request carries a UUID nonce in `X-Nonce`
- Nonce stored in Redis with `SET NX EX` — duplicate nonces rejected with 409 before any business logic runs
- Timestamp validated within ±5 minutes — old captured requests cannot be replayed
- Signed message includes method, path, timestamp, nonce, and body hash — changing any component invalidates the signature

**Residual risk:** An attacker with the `HMAC_SHARED_KEY` and the ability to generate valid timestamps can forge requests. Key is Railway-managed and never transmitted to the iOS app in plaintext.

---

## Trust boundaries

| Boundary | What crosses it | How it's validated |
|---|---|---|
| iOS app → API | HMAC-signed requests | Signature + nonce + timestamp |
| Website → API | Session JWT or anonymous visitor ID | JWT signature + Redis revocation check |
| Dashboard → API | Dashboard session credential | Separate auth, MFA required |
| Shopify → API | Webhook payloads | Shopify HMAC signature |
| API → PostgreSQL | sqlc queries | Parameterized bindings, no string construction |
| API → Redis | Key-value operations | Named key prefixes, TTLs always set |

---

## Explicit non-goals

- **Physical security** — device theft, shoulder surfing, and social engineering of staff are outside scope
- **Shopify platform security** — Shopify's own security posture is their responsibility
- **Railway infrastructure security** — network-layer attacks, hypervisor escapes, and datacenter physical security are Railway's responsibility
- **Client-side security** — XSS in the Next.js frontend, jailbroken iOS devices, and client-side data extraction are frontend concerns, not backend concerns

---

## Assumptions that if violated would break the security model

1. Railway's secret management is not compromised — all key material lives there
2. The `go.sum` lockfile accurately represents the dependency graph — supply chain integrity depends on it
3. The audit log Postgres instance is not directly accessible to application-layer attackers — audit log integrity depends on database-level access controls
4. HMAC nonce TTL (10 minutes) in Redis exceeds the window in which a replayed request could be useful — if an attacker can replay 10+ minutes later, the nonce window must be extended
