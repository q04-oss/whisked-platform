// Package customers manages Whisked customer profiles and identity.
//
// A customer is an identified user — someone who has created an account via
// the iOS app or the website. Anonymous website visitors are tracked separately
// in the analytics layer and linked to a customer when they identify themselves.
//
// Customers are distinct from staff. Staff accounts are in the dashboard domain
// and have no overlap with the customer identity system.
package customers

import (
	"time"

	"github.com/q04-oss/whisked-platform/internal/platform"
)

// Customer is the domain representation of a Whisked customer.
// It is the type returned by the service layer — not a database row, not an
// HTTP response. Mapping to and from those forms happens at the boundaries.
type Customer struct {
	ID                platform.CustomerID
	Email             string
	DisplayName       string
	ShopifyCustomerID *platform.ShopifyCustomerID
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// CreateParams carries the inputs needed to register a new customer.
type CreateParams struct {
	Email       string
	DisplayName string
}

// UpdateParams carries the fields that can be changed on an existing customer.
// Nil fields are left unchanged.
type UpdateParams struct {
	DisplayName *string
}

// Profile is the JSON-serializable view of a customer returned by the API.
// Omits internal fields (ShopifyCustomerID) that are not relevant to clients.
type Profile struct {
	ID          int64     `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	CreatedAt   time.Time `json:"created_at"`
}

func toProfile(c *Customer) Profile {
	return Profile{
		ID:          c.ID.Int64(),
		Email:       c.Email,
		DisplayName: c.DisplayName,
		CreatedAt:   c.CreatedAt,
	}
}
