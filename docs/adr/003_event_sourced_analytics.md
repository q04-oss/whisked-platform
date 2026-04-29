# ADR-003: Event-sourced behavioral analytics

**Status:** Accepted  
**Date:** 2026-04-29

## Context

Whisked's leadership needs to make data-driven business decisions — expansion timing, menu changes, loyalty mechanic adjustments, marketing attribution. This requires behavioral data that is rich enough to answer questions that haven't been asked yet.

A mutable state model (rows that get updated as users progress) answers the questions it was designed for and no others. If you later want to know what the conversion path looked like before a product change, or how a cohort that first visited in January behaved compared to one that first visited in March, the data is gone.

## Decision

All behavioral signals are stored as immutable events in an append-only log. An event captures: what happened, who it happened to, when, where (location, page), and any relevant metadata.

Examples:
- `page.viewed` — anonymous or identified visitor viewed a page
- `steep.earned` — customer earned a steep from an in-bar purchase or Shopify order
- `reward.redeemed` — customer redeemed a reward
- `visitor.identified` — anonymous visitor linked to a customer account

Current state (loyalty balance, redemption history) is derived from events and cached in operational tables for hot-path reads. The event log is the source of truth.

## Consequences

- Any metric can be computed retroactively from the event log — new business questions don't require schema migrations
- Anonymous-to-identified linking is an event, not a data transformation — the full journey from first visit to conversion is preserved
- The event log grows without bound — retention policy and archival are operational concerns that will need to be addressed as volume grows
- Read performance for aggregate queries depends on indexing strategy and, at scale, may require a dedicated analytical store; PostgreSQL is sufficient for the expected volume at launch
- The event log is append-only at the application layer and enforced at the database layer (trigger prevents UPDATE/DELETE)
