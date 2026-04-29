# ADR-004: Shopify as the commerce layer

**Status:** Accepted  
**Date:** 2026-04-29

## Context

Whisked operates a Shopify store for retail sales (ceremonial matcha tins, merchandise). Rebuilding payment processing, order management, and inventory in the custom backend would duplicate work Shopify already handles well and introduce PCI-DSS scope to the custom backend.

## Decision

Shopify is the source of truth for all commerce. The whisked-platform backend does not process payments, manage inventory, or store payment instrument details. Instead:

- Shopify sends `orders/paid` webhooks to the backend
- The backend validates the Shopify HMAC signature on every webhook
- On a valid webhook, a loyalty event is created if the customer is identifiable by email
- The order reference is stored in the analytical event log for attribution analysis

The backend never writes back to Shopify. The integration is strictly read-only from the backend's perspective.

## Consequences

- PCI-DSS scope is entirely Shopify's responsibility — the backend never sees card data
- Order management, refunds, and fulfillment remain in Shopify — staff do not need to learn a second system for commerce
- Loyalty credit for online purchases depends on webhook delivery — Shopify webhook retry behavior is the reliability boundary
- Future requirement: if Whisked moves off Shopify, the webhook handler and `ShopifyOrderID` type need to be replaced, but the loyalty domain is decoupled from commerce by design
