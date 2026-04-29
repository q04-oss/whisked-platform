# ADR-001: Go as the implementation language

**Status:** Accepted  
**Date:** 2026-04-29

## Context

The whisked-platform backend needs to be maintainable over a multi-year horizon by a small team. The candidate languages were Go and Rust.

Rust's ownership model and borrow checker provide compile-time guarantees that Go cannot match — memory safety without a GC, fearless concurrency, and type-level invariants that Go expresses only at runtime. For a security-sensitive backend, these properties are genuinely valuable.

However, Whisked is privately funded and intends to remain so. Maintenance cost scales with the complexity of onboarding contributors. Rust has a substantially steeper learning curve than Go, a longer ramp time before contributors can work independently, and a significantly smaller talent pool. For a business whose technical risk is primarily operational (data integrity, availability, security posture) rather than systems-level (memory corruption, undefined behavior), the Rust compiler's strongest guarantees do not correspond to the most likely failure modes.

## Decision

Go.

The security properties that matter for this system — type-driven ID safety, secret non-exposure, audit logging, authenticated requests, SQL injection prevention — are achievable in Go through discipline and tooling rather than compiler enforcement. The discipline is enforced through code review, static analysis (gosec, staticcheck), and architectural constraints (sqlc for SQL, `config.Secret` for credentials).

## Consequences

- Onboarding a Go contributor takes days, not weeks
- The dependency on developer discipline for some properties that Rust enforces at compile time is an accepted trade-off, mitigated by CI gates
- Memory corruption and certain classes of concurrency bugs are possible in Go in ways they are not in Rust — this is accepted given the threat model
- Go's standard library is excellent and covers most of what this system needs without reaching for external dependencies
