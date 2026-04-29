// Package dashboard provides the internal analytics API for Whisked's
// leadership team. It has a separate authentication boundary from the
// customer-facing API — staff accounts are distinct from customer accounts,
// authenticated with a separate JWT claim, and every read is audit-logged
// with the staff member's identity.
//
// Access levels:
//   - admin: full read access + staff management
//   - viewer: read-only access to all analytics
//
// Statistical note: comparative metrics include sample sizes and a
// significance flag. Small sample sizes (n < 30) are flagged explicitly
// so leadership doesn't act on statistically meaningless differences.
package dashboard

import "time"

// Staff is a Whisked team member with dashboard access.
type Staff struct {
	ID          int64
	Email       string
	DisplayName string
	Role        string // "admin" | "viewer"
	Active      bool
}

// StaffTokenPair is the auth response for a successful staff login.
type StaffTokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Role         string `json:"role"`
}

// Overview is the top-level metrics snapshot shown on the dashboard home.
type Overview struct {
	TotalCustomers       int64   `json:"total_customers"`
	ActiveCustomers30d   int64   `json:"active_customers_30d"`
	SteepsThisWeek       int64   `json:"steeps_this_week"`
	SteepsThisMonth      int64   `json:"steeps_this_month"`
	TotalSteepsEarned    int64   `json:"total_steeps_earned"`
	TotalRewardsRedeemed int64   `json:"total_rewards_redeemed"`
	// RedemptionRate is rewards_redeemed / total_rewards_eligible (floor(steeps/9)).
	RedemptionRate float64 `json:"redemption_rate"`
}

// DailyCount is a single data point in a time-series chart.
type DailyCount struct {
	Date   string `json:"date"`   // YYYY-MM-DD
	Count  int64  `json:"count"`
	Series string `json:"series"` // "in_bar" | "shopify" — for multi-series charts
}

// LoyaltySummary is the loyalty program analytics view.
type LoyaltySummary struct {
	TotalSteepsEarned    int64        `json:"total_steeps_earned"`
	TotalRewardsRedeemed int64        `json:"total_rewards_redeemed"`
	InBarSteeps          int64        `json:"in_bar_steeps"`
	ShopifySteeps        int64        `json:"shopify_steeps"`
	SteepsByDay          []DailyCount `json:"steeps_by_day"`
}

// RecentLoyaltyEvent is a single entry in the live loyalty event stream.
type RecentLoyaltyEvent struct {
	ID            int64     `json:"id"`
	EventType     string    `json:"event_type"`
	Source        string    `json:"source"`
	CustomerEmail string    `json:"customer_email"`
	CustomerName  string    `json:"customer_name"`
	CreatedAt     time.Time `json:"created_at"`
}

// FunnelStats describes the conversion funnel from anonymous visit to repeat customer.
type FunnelStats struct {
	TotalVisitors      int64   `json:"total_visitors"`
	IdentifiedVisitors int64   `json:"identified_visitors"`
	// IdentificationRate is identified_visitors / total_visitors.
	IdentificationRate float64 `json:"identification_rate"`
	TotalCustomers     int64   `json:"total_customers"`
	CustomersWithSteeps int64  `json:"customers_with_steeps"`
	// FirstSteepRate is customers_with_steeps / total_customers.
	FirstSteepRate float64 `json:"first_steep_rate"`
	RepeatCustomers int64  `json:"repeat_customers"`
	// RepeatRate is repeat_customers / customers_with_steeps.
	RepeatRate float64 `json:"repeat_rate"`
	// SmallSample is true when any stage has n < 30 — results may not be meaningful.
	SmallSample bool `json:"small_sample"`
}

// CustomerSummary is a single row in the customer list.
type CustomerSummary struct {
	ID              int64      `json:"id"`
	Email           string     `json:"email"`
	DisplayName     string     `json:"display_name"`
	JoinedAt        time.Time  `json:"joined_at"`
	SteepsEarned    int64      `json:"steeps_earned"`
	RewardsRedeemed int64      `json:"rewards_redeemed"`
	LastActivity    *time.Time `json:"last_activity,omitempty"`
}
