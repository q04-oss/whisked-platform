package customers

import (
	"context"
	"fmt"
	"strings"

	"github.com/q04-oss/whisked-platform/internal/audit"
	"github.com/q04-oss/whisked-platform/internal/platform"
)

// repository is the interface the Service requires from its data layer.
// Defined here at the consumer boundary — the pgx implementation satisfies
// it implicitly without importing this package.
type repository interface {
	create(ctx context.Context, params CreateParams) (*Customer, error)
	getByID(ctx context.Context, id platform.CustomerID) (*Customer, error)
	getByEmail(ctx context.Context, email string) (*Customer, error)
	update(ctx context.Context, id platform.CustomerID, params UpdateParams) (*Customer, error)
	delete(ctx context.Context, id platform.CustomerID) error
	linkAnonymousVisitor(ctx context.Context, visitorID platform.AnonymousVisitorID, customerID platform.CustomerID) error
}

// Service contains the business logic for customer operations.
type Service struct {
	repo  repository
	audit *audit.Writer
}

// NewService returns a Service with the given repository and audit writer.
func NewService(repo repository, audit *audit.Writer) *Service {
	return &Service{repo: repo, audit: audit}
}

// Create registers a new customer. Returns ErrConflict if the email is already
// registered.
func (s *Service) Create(ctx context.Context, params CreateParams) (*Customer, error) {
	if err := validateCreateParams(params); err != nil {
		return nil, err
	}

	params.Email = normalizeEmail(params.Email)

	customer, err := s.repo.create(ctx, params)
	if err != nil {
		if isDuplicateEmail(err) {
			return nil, platform.ErrConflict
		}
		return nil, fmt.Errorf("customers.Create: %w", err)
	}

	s.audit.Write(ctx, audit.Entry{
		EventType:  audit.EventCustomerRead,
		ActorType:  audit.ActorCustomer,
		ActorID:    ptr(customer.ID.Int64()),
		TargetType: ptr("customer"),
		TargetID:   ptr(customer.ID.String()),
	})

	return customer, nil
}

// GetByID returns the customer with the given ID.
// Returns a wrapped ErrNotFound if no customer exists with that ID.
func (s *Service) GetByID(ctx context.Context, id platform.CustomerID) (*Customer, error) {
	customer, err := s.repo.getByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("customers.GetByID: %w", err)
	}
	return customer, nil
}

// GetByEmail returns the customer with the given email address.
// Returns a wrapped ErrNotFound if no customer exists with that email.
func (s *Service) GetByEmail(ctx context.Context, email string) (*Customer, error) {
	customer, err := s.repo.getByEmail(ctx, normalizeEmail(email))
	if err != nil {
		return nil, fmt.Errorf("customers.GetByEmail: %w", err)
	}
	return customer, nil
}

// Update modifies the mutable fields of a customer profile.
// Only the caller's own profile should be passed here — authorization is
// enforced at the handler layer.
func (s *Service) Update(ctx context.Context, id platform.CustomerID, params UpdateParams) (*Customer, error) {
	if params.DisplayName != nil {
		trimmed := strings.TrimSpace(*params.DisplayName)
		if trimmed == "" {
			return nil, platform.Wrap(platform.ErrBadRequest, fmt.Errorf("display_name cannot be blank"))
		}
		params.DisplayName = &trimmed
	}

	customer, err := s.repo.update(ctx, id, params)
	if err != nil {
		return nil, fmt.Errorf("customers.Update: %w", err)
	}

	s.audit.Write(ctx, audit.Entry{
		EventType:  audit.EventCustomerUpdated,
		ActorType:  audit.ActorCustomer,
		ActorID:    ptr(id.Int64()),
		TargetType: ptr("customer"),
		TargetID:   ptr(id.String()),
	})

	return customer, nil
}

// Delete permanently removes a customer and their data. This is a real
// deletion, not a soft delete — GDPR right to erasure.
func (s *Service) Delete(ctx context.Context, id platform.CustomerID) error {
	if err := s.repo.delete(ctx, id); err != nil {
		return fmt.Errorf("customers.Delete: %w", err)
	}

	s.audit.Write(ctx, audit.Entry{
		EventType:  audit.EventCustomerDeleted,
		ActorType:  audit.ActorCustomer,
		ActorID:    ptr(id.Int64()),
		TargetType: ptr("customer"),
		TargetID:   ptr(id.String()),
	})

	return nil
}

// LinkAnonymousVisitor associates a visitor's behavioral history with the
// customer. A no-op if the visitor is already linked.
func (s *Service) LinkAnonymousVisitor(ctx context.Context, visitorID platform.AnonymousVisitorID, customerID platform.CustomerID) error {
	if err := s.repo.linkAnonymousVisitor(ctx, visitorID, customerID); err != nil {
		return fmt.Errorf("customers.LinkAnonymousVisitor: %w", err)
	}
	return nil
}

// ── Validation ────────────────────────────────────────────────────────────────

func validateCreateParams(p CreateParams) error {
	if p.Email == "" {
		return platform.Wrap(platform.ErrBadRequest, fmt.Errorf("email is required"))
	}
	if !strings.Contains(p.Email, "@") {
		return platform.Wrap(platform.ErrBadRequest, fmt.Errorf("email is invalid"))
	}
	if len(p.DisplayName) > 100 {
		return platform.Wrap(platform.ErrBadRequest, fmt.Errorf("display_name exceeds 100 characters"))
	}
	return nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// isDuplicateEmail detects a PostgreSQL unique constraint violation on the
// customers.email column.
func isDuplicateEmail(err error) bool {
	return strings.Contains(err.Error(), "unique") &&
		strings.Contains(err.Error(), "customers_email_key")
}

func ptr[T any](v T) *T { return &v }
