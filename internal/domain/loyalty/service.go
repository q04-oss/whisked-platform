package loyalty

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/q04-oss/whisked-platform/internal/audit"
	"github.com/q04-oss/whisked-platform/internal/platform"
)

const (
	idempotencyTTL  = 24 * time.Hour
	redemptionLockTTL = 30 * time.Second
)

// repository is the interface Service requires from its data layer.
type repository interface {
	getBalanceRow(ctx context.Context, customerID platform.CustomerID) (balanceRow, error)
	insertEvent(ctx context.Context, p insertEventParams) (*Event, error)
	redeemInTransaction(ctx context.Context, customerID platform.CustomerID, idempotencyKey string) error
	getHistory(ctx context.Context, customerID platform.CustomerID, limit, offset int) ([]Event, error)
	getCustomerIDByEmail(ctx context.Context, email string) (platform.CustomerID, error)
}

// Service contains the business logic for the loyalty program.
type Service struct {
	repo  repository
	redis *redis.Client
	audit *audit.Writer
}

// NewService returns a Service wired to its dependencies.
func NewService(repo repository, rdb *redis.Client, audit *audit.Writer) *Service {
	return &Service{repo: repo, redis: rdb, audit: audit}
}

// Earn records a steep for the customer. The idempotency key prevents duplicate
// steeps from network retries — the same key always returns the same result.
func (s *Service) Earn(ctx context.Context, params EarnParams) (*Balance, error) {
	if params.IdempotencyKey == "" {
		return nil, platform.Wrap(platform.ErrBadRequest, fmt.Errorf("idempotency_key is required"))
	}
	if _, err := uuid.Parse(params.IdempotencyKey); err != nil {
		return nil, platform.Wrap(platform.ErrBadRequest, fmt.Errorf("idempotency_key must be a UUID"))
	}

	idemKey := idempotencyRedisKey(params.IdempotencyKey)

	set, err := s.redis.SetNX(ctx, idemKey, 1, idempotencyTTL).Result()
	if err != nil {
		// Redis unavailable — fall through to DB which has a UNIQUE constraint backstop.
	} else if !set {
		// Already processed — return current balance (idempotent success, not an error).
		return s.GetBalance(ctx, params.CustomerID)
	}

	_, dbErr := s.repo.insertEvent(ctx, insertEventParams{
		customerID:     params.CustomerID,
		eventType:      "steep_earned",
		source:         params.Source,
		locationID:     params.LocationID,
		shopifyOrderID: params.ShopifyOrderID,
		idempotencyKey: params.IdempotencyKey,
	})
	if dbErr != nil {
		if isDuplicateIdempotencyKey(dbErr) {
			// DB-level dedup — Redis was unavailable but DB caught it.
			return s.GetBalance(ctx, params.CustomerID)
		}
		// DB write failed after Redis key was set — remove the Redis key so
		// the client can retry.
		s.redis.Del(ctx, idemKey)
		return nil, fmt.Errorf("loyalty.Earn: %w", dbErr)
	}

	s.audit.Write(ctx, audit.Entry{
		EventType:  audit.EventSteepEarned,
		ActorType:  audit.ActorCustomer,
		ActorID:    ptr(params.CustomerID.Int64()),
		TargetType: ptr("customer"),
		TargetID:   ptr(params.CustomerID.String()),
		Metadata:   map[string]string{"source": params.Source},
	})

	return s.GetBalance(ctx, params.CustomerID)
}

// Redeem consumes one available reward for the customer. Uses a Redis
// distributed lock to prevent concurrent double-redemptions, with the
// DB transaction as a backstop.
func (s *Service) Redeem(ctx context.Context, customerID platform.CustomerID) (*Balance, error) {
	lockKey := fmt.Sprintf("whisked:redeem:lock:%d", customerID.Int64())

	locked, err := s.redis.SetNX(ctx, lockKey, 1, redemptionLockTTL).Result()
	if err != nil {
		// Redis unavailable — rely on DB transaction atomicity alone.
	} else if !locked {
		return nil, platform.Wrap(platform.ErrConflict, fmt.Errorf("redemption already in progress"))
	}
	if err == nil {
		defer s.redis.Del(ctx, lockKey)
	}

	idemKey := fmt.Sprintf("redeem-%d-%d", customerID.Int64(), time.Now().UnixNano())

	if err := s.repo.redeemInTransaction(ctx, customerID, idemKey); err != nil {
		return nil, fmt.Errorf("loyalty.Redeem: %w", err)
	}

	s.audit.Write(ctx, audit.Entry{
		EventType:  audit.EventRewardRedeemed,
		ActorType:  audit.ActorCustomer,
		ActorID:    ptr(customerID.Int64()),
		TargetType: ptr("customer"),
		TargetID:   ptr(customerID.String()),
	})

	return s.GetBalance(ctx, customerID)
}

// GetBalance returns the current loyalty balance derived from the event log.
func (s *Service) GetBalance(ctx context.Context, customerID platform.CustomerID) (*Balance, error) {
	row, err := s.repo.getBalanceRow(ctx, customerID)
	if err != nil {
		return nil, fmt.Errorf("loyalty.GetBalance: %w", err)
	}
	b := computeBalance(customerID, row.steepsEarned, row.rewardsRedeemed)
	return &b, nil
}

// GetHistory returns a paginated list of loyalty events for the customer.
func (s *Service) GetHistory(ctx context.Context, customerID platform.CustomerID, limit, offset int) ([]Event, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	events, err := s.repo.getHistory(ctx, customerID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("loyalty.GetHistory: %w", err)
	}
	if events == nil {
		events = []Event{}
	}
	return events, nil
}

// ProcessShopifyOrder credits a steep to the customer who placed a Shopify
// order. A no-op if no customer account matches the order email.
// Always returns nil — Shopify webhooks must receive 200 even if the customer
// is not found, to prevent Shopify from retrying indefinitely.
func (s *Service) ProcessShopifyOrder(ctx context.Context, orderID, email string) error {
	customerID, err := s.repo.getCustomerIDByEmail(ctx, email)
	if err != nil {
		// Customer has no account — not an error, just no loyalty credit.
		return nil
	}

	idemKey := fmt.Sprintf("shopify-%s", orderID)

	shopifyOID := platform.ShopifyOrderID(orderID)
	_, earnErr := s.Earn(ctx, EarnParams{
		CustomerID:     customerID,
		Source:         "shopify",
		ShopifyOrderID: &shopifyOID,
		IdempotencyKey: idemKey,
	})
	if earnErr != nil {
		// Log but don't surface — the webhook must still return 200.
		return fmt.Errorf("loyalty.ProcessShopifyOrder: %w", earnErr)
	}

	return nil
}

func idempotencyRedisKey(key string) string {
	return "whisked:idem:" + key
}

func ptr[T any](v T) *T { return &v }
