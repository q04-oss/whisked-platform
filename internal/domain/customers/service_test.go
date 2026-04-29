package customers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/q04-oss/whisked-platform/internal/audit"
	"github.com/q04-oss/whisked-platform/internal/platform"
)

// mockRepository is a hand-written mock that satisfies the repository interface.
// Defined in the test file — only the service needs to know about it.
type mockRepository struct {
	customers map[int64]*Customer
	nextID    int64
	// Override specific methods for error injection.
	createErr error
	getErr    error
}

func newMockRepo() *mockRepository {
	return &mockRepository{
		customers: make(map[int64]*Customer),
		nextID:    1,
	}
}

func (m *mockRepository) create(_ context.Context, params CreateParams) (*Customer, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	// Check for duplicate email.
	for _, c := range m.customers {
		if c.Email == params.Email {
			return nil, errors.New("unique constraint customers_email_key")
		}
	}
	c := &Customer{
		ID:          platform.CustomerID(m.nextID),
		Email:       params.Email,
		DisplayName: params.DisplayName,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	m.customers[m.nextID] = c
	m.nextID++
	return c, nil
}

func (m *mockRepository) getByID(_ context.Context, id platform.CustomerID) (*Customer, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	c, ok := m.customers[id.Int64()]
	if !ok {
		return nil, platform.ErrNotFound
	}
	return c, nil
}

func (m *mockRepository) getByEmail(_ context.Context, email string) (*Customer, error) {
	for _, c := range m.customers {
		if c.Email == email {
			return c, nil
		}
	}
	return nil, platform.ErrNotFound
}

func (m *mockRepository) update(_ context.Context, id platform.CustomerID, params UpdateParams) (*Customer, error) {
	c, ok := m.customers[id.Int64()]
	if !ok {
		return nil, platform.ErrNotFound
	}
	if params.DisplayName != nil {
		c.DisplayName = *params.DisplayName
	}
	return c, nil
}

func (m *mockRepository) delete(_ context.Context, id platform.CustomerID) error {
	if _, ok := m.customers[id.Int64()]; !ok {
		return platform.ErrNotFound
	}
	delete(m.customers, id.Int64())
	return nil
}

func (m *mockRepository) linkAnonymousVisitor(_ context.Context, _ platform.AnonymousVisitorID, _ platform.CustomerID) error {
	return nil
}

// noopAudit satisfies audit.Writer for tests that don't care about audit output.
// We pass a nil *audit.Writer — Write is a no-op when db is nil in test context.
func noopAudit() *audit.Writer { return audit.New(nil) }

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestService_Create(t *testing.T) {
	tests := []struct {
		name      string
		params    CreateParams
		setupRepo func(*mockRepository)
		wantErr   bool
		errIs     *platform.ClientError
	}{
		{
			name:    "valid registration",
			params:  CreateParams{Email: "belle@whisked.ca", DisplayName: "Belle"},
			wantErr: false,
		},
		{
			name:    "email normalized to lowercase",
			params:  CreateParams{Email: "Belle@Whisked.CA"},
			wantErr: false,
		},
		{
			name:    "missing email",
			params:  CreateParams{DisplayName: "Belle"},
			wantErr: true,
			errIs:   platform.ErrBadRequest,
		},
		{
			name:    "invalid email format",
			params:  CreateParams{Email: "not-an-email"},
			wantErr: true,
			errIs:   platform.ErrBadRequest,
		},
		{
			name:    "display name too long",
			params:  CreateParams{Email: "a@b.com", DisplayName: string(make([]byte, 101))},
			wantErr: true,
			errIs:   platform.ErrBadRequest,
		},
		{
			name:   "duplicate email",
			params: CreateParams{Email: "belle@whisked.ca"},
			setupRepo: func(m *mockRepository) {
				m.customers[99] = &Customer{ID: 99, Email: "belle@whisked.ca"}
			},
			wantErr: true,
			errIs:   platform.ErrConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockRepo()
			if tt.setupRepo != nil {
				tt.setupRepo(repo)
			}
			svc := NewService(repo, noopAudit())

			customer, err := svc.Create(context.Background(), tt.params)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errIs != nil {
					ce, ok := platform.AsClientError(err)
					if !ok {
						t.Fatalf("expected ClientError, got %T: %v", err, err)
					}
					if ce.HTTPStatus != tt.errIs.HTTPStatus {
						t.Errorf("status = %d, want %d", ce.HTTPStatus, tt.errIs.HTTPStatus)
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if customer == nil {
				t.Fatal("expected customer, got nil")
			}
		})
	}
}

func TestService_Create_EmailNormalized(t *testing.T) {
	repo := newMockRepo()
	svc := NewService(repo, noopAudit())

	customer, err := svc.Create(context.Background(), CreateParams{Email: "  Belle@Whisked.CA  "})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if customer.Email != "belle@whisked.ca" {
		t.Errorf("email = %q, want %q", customer.Email, "belle@whisked.ca")
	}
}

func TestService_Update(t *testing.T) {
	tests := []struct {
		name    string
		params  UpdateParams
		wantErr bool
		errIs   *platform.ClientError
	}{
		{
			name:    "valid update",
			params:  UpdateParams{DisplayName: ptr("New Name")},
			wantErr: false,
		},
		{
			name:    "blank display name rejected",
			params:  UpdateParams{DisplayName: ptr("   ")},
			wantErr: true,
			errIs:   platform.ErrBadRequest,
		},
		{
			name:    "nil display name is no-op",
			params:  UpdateParams{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockRepo()
			repo.customers[1] = &Customer{ID: 1, Email: "belle@whisked.ca", DisplayName: "Belle"}
			svc := NewService(repo, noopAudit())

			_, err := svc.Update(context.Background(), platform.CustomerID(1), tt.params)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errIs != nil {
					ce, ok := platform.AsClientError(err)
					if !ok {
						t.Fatalf("expected ClientError, got %T", err)
					}
					if ce.HTTPStatus != tt.errIs.HTTPStatus {
						t.Errorf("status = %d, want %d", ce.HTTPStatus, tt.errIs.HTTPStatus)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestService_Delete(t *testing.T) {
	repo := newMockRepo()
	repo.customers[1] = &Customer{ID: 1, Email: "belle@whisked.ca"}
	svc := NewService(repo, noopAudit())

	if err := svc.Delete(context.Background(), platform.CustomerID(1)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := repo.customers[1]; ok {
		t.Error("customer still exists after deletion")
	}
}
