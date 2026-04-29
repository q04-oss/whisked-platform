// Package platform contains shared primitives used across the application:
// typed identifiers, error types, and other cross-cutting building blocks.
//
// Typed IDs are the primary tool for type-driven security in this codebase.
// Defining CustomerID and LocationID as distinct types means the compiler
// rejects any attempt to pass one where the other is expected — a class of
// bug that would otherwise require careful code review to catch.
package platform

import "strconv"

// The integer ID types all share the same underlying representation but are
// intentionally incompatible with each other at the type level.

// CustomerID identifies a Whisked customer in the operational database.
// Distinct from ShopifyCustomerID — the two are linked but not interchangeable.
type CustomerID int64

func (id CustomerID) String() string { return strconv.FormatInt(int64(id), 10) }
func (id CustomerID) Int64() int64   { return int64(id) }

// LocationID identifies a Whisked location (permanent bar, pop-up, or wholesale account).
type LocationID int64

func (id LocationID) String() string { return strconv.FormatInt(int64(id), 10) }
func (id LocationID) Int64() int64   { return int64(id) }

// LoyaltyEventID identifies a row in the loyalty_events append-only table.
type LoyaltyEventID int64

func (id LoyaltyEventID) String() string { return strconv.FormatInt(int64(id), 10) }
func (id LoyaltyEventID) Int64() int64   { return int64(id) }

// AnalyticEventID identifies a row in the analytic_events append-only table.
type AnalyticEventID int64

func (id AnalyticEventID) String() string { return strconv.FormatInt(int64(id), 10) }
func (id AnalyticEventID) Int64() int64   { return int64(id) }

// StaffID identifies a Whisked staff member who can access the dashboard.
// Staff are not customers — they have a separate identity table.
type StaffID int64

func (id StaffID) String() string { return strconv.FormatInt(int64(id), 10) }
func (id StaffID) Int64() int64   { return int64(id) }

// String-based IDs for external system references.

// ShopifyOrderID is Shopify's order identifier. Shopify uses large integers
// that exceed JavaScript's safe integer range, so they are treated as strings.
type ShopifyOrderID string

func (id ShopifyOrderID) String() string { return string(id) }

// ShopifyCustomerID is Shopify's customer identifier.
type ShopifyCustomerID string

func (id ShopifyCustomerID) String() string { return string(id) }

// AnonymousVisitorID is a stable first-party identifier for unidentified website
// visitors. Stored in a first-party cookie. When a visitor identifies themselves,
// their behavioral history is linked to their CustomerID via a visitor.identified event.
type AnonymousVisitorID string

func (id AnonymousVisitorID) String() string { return string(id) }

// SessionID is a JWT ID claim (jti) used for token revocation tracking in Redis.
type SessionID string

func (id SessionID) String() string { return string(id) }
