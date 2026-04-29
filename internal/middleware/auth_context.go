package middleware

import (
	"context"

	"github.com/q04-oss/whisked-platform/internal/platform"
)

type customerIDContextKey struct{}

// SetAuthenticatedCustomerID stores the authenticated customer's typed ID in
// the context. Called by the auth middleware after successful JWT validation.
func SetAuthenticatedCustomerID(ctx context.Context, id platform.CustomerID) context.Context {
	return context.WithValue(ctx, customerIDContextKey{}, id)
}

// AuthenticatedCustomerID retrieves the authenticated customer ID from the
// context. Returns the ID and true if present; zero value and false otherwise.
func AuthenticatedCustomerID(ctx context.Context) (platform.CustomerID, bool) {
	id, ok := ctx.Value(customerIDContextKey{}).(platform.CustomerID)
	return id, ok
}
