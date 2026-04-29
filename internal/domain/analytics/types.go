// Package analytics manages behavioral event ingestion and anonymous visitor
// tracking for the Whisked platform.
//
// Events flow from two sources:
//   - The Next.js website (anonymous visitors and identified customers)
//   - The iOS app (identified customers only)
//
// The write path is intentionally simple: validate, check consent, persist.
// At Whisked's current scale, synchronous writes to PostgreSQL are sufficient.
// If ingestion volume grows significantly, the service layer is the right
// boundary to introduce async queuing — the handler and repository don't change.
//
// Privacy:
//   - Do Not Track (DNT: 1) is honored. Events from DNT clients are silently
//     discarded and the client receives a response indicating tracking was skipped.
//   - Anonymous visitors are assigned a stable UUID stored in a first-party cookie.
//     When a visitor identifies themselves, their history links to their CustomerID.
//   - Identified users can request deletion of their data (handled in customers domain).
package analytics

import "github.com/q04-oss/whisked-platform/internal/platform"

// Standard event types. Kept as typed constants so callers can't invent
// arbitrary strings — unknown event types are rejected at the service layer.
const (
	// Website events
	EventPageViewed     = "page.viewed"
	EventContentEngaged = "content.engaged"
	EventMenuViewed     = "menu.viewed"
	EventShopVisited    = "shop.visited"

	// Identity events
	EventVisitorIdentified = "visitor.identified"

	// iOS app events
	EventAppOpened    = "app.opened"
	EventLoyaltyViewed = "loyalty.viewed"
)

// knownEventTypes is the allowlist of accepted event types.
var knownEventTypes = map[string]bool{
	EventPageViewed:        true,
	EventContentEngaged:    true,
	EventMenuViewed:        true,
	EventShopVisited:       true,
	EventVisitorIdentified: true,
	EventAppOpened:         true,
	EventLoyaltyViewed:     true,
}

// IngestParams carries all inputs for recording a behavioral event.
type IngestParams struct {
	EventType  string
	VisitorID  *platform.AnonymousVisitorID // website clients
	CustomerID *platform.CustomerID         // identified clients (website + iOS)
	SessionID  string
	Page       string
	Metadata   map[string]any
	IPAddress  string
	UserAgent  string
}

// IngestResult is returned to the client after an ingest call.
type IngestResult struct {
	Tracked bool `json:"tracked"`
}
