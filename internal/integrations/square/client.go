// Package square provides a minimal client for the Square API.
// Only the endpoints needed by whisked-platform are implemented:
// customer creation and lookup for loyalty auto-stamping, and order
// creation for in-app drink orders placed through the platform.
package square

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const baseURL = "https://connect.squareup.com/v2"

// tokenSource resolves the current access token. Both static credentials
// (legacy env var) and dynamic OAuth tokens implement this interface.
type tokenSource interface {
	AccessToken(ctx context.Context) (string, error)
}

// staticToken wraps a fixed access token for the legacy env-var path.
type staticToken struct{ token string }

func (s staticToken) AccessToken(_ context.Context) (string, error) { return s.token, nil }

// Client calls the Square API on behalf of the Whisked merchant.
type Client struct {
	tokens tokenSource
	http   *http.Client
}

// New returns a Client backed by the given tokenSource.
func New(tokens tokenSource, httpClient *http.Client) *Client {
	return &Client{tokens: tokens, http: httpClient}
}

// NewFromConfig returns a Client using a static access token, or nil if the
// token is empty. Used as a fallback before OAuth is completed.
func NewFromConfig(accessToken string, httpClient *http.Client) *Client {
	if accessToken == "" {
		return nil
	}
	return &Client{
		tokens: staticToken{token: accessToken},
		http:   &http.Client{Timeout: 15 * time.Second},
	}
}

// NewFromTokenStore returns a Client backed by dynamic OAuth tokens.
// This is the preferred constructor once Belle has completed the OAuth flow.
func NewFromTokenStore(ts *TokenStore, httpClient *http.Client) *Client {
	return &Client{
		tokens: ts,
		http:   httpClient,
	}
}

// Customer is the subset of Square's customer object used by this integration.
type Customer struct {
	ID           string `json:"id"`
	EmailAddress string `json:"email_address"`
	GivenName    string `json:"given_name"`
}

// CreateCustomer creates a Square customer record linked to the Whisked account.
// Called on customer registration so future Square transactions can be
// automatically matched to the Whisked loyalty account by email.
func (c *Client) CreateCustomer(ctx context.Context, email, displayName string) (*Customer, error) {
	body, _ := json.Marshal(map[string]string{
		"email_address": email,
		"given_name":    displayName,
		"reference_id":  "whisked-" + email, // idempotency across retries
	})

	req, err := c.newRequest(ctx, http.MethodPost, baseURL+"/customers", body)
	if err != nil {
		return nil, fmt.Errorf("square: building request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("square: create customer: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Customer *Customer `json:"customer"`
		Errors   []struct {
			Detail string `json:"detail"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("square: decoding response: %w", err)
	}
	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("square: %s", result.Errors[0].Detail)
	}
	return result.Customer, nil
}

// FindCustomerByEmail searches the Square customer directory by email.
// Returns nil, nil if no matching customer is found.
func (c *Client) FindCustomerByEmail(ctx context.Context, email string) (*Customer, error) {
	body, _ := json.Marshal(map[string]any{
		"query": map[string]any{
			"filter": map[string]any{
				"email_address": map[string]any{
					"exact": email,
				},
			},
		},
	})

	req, err := c.newRequest(ctx, http.MethodPost, baseURL+"/customers/search", body)
	if err != nil {
		return nil, fmt.Errorf("square: building request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("square: search customers: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Customers []Customer `json:"customers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("square: decoding response: %w", err)
	}
	if len(result.Customers) == 0 {
		return nil, nil
	}
	return &result.Customers[0], nil
}

// OrderLineItem is one drink line in a Square order.
type OrderLineItem struct {
	Name     string `json:"name"`
	Quantity string `json:"quantity"`
	// BasePriceMoney is in the smallest currency unit (cents for CAD).
	BasePriceMoney struct {
		Amount   int64  `json:"amount"`
		Currency string `json:"currency"`
	} `json:"base_price_money"`
}

// CreateOrder creates a paid order on Belle's Square POS so it appears on
// her kitchen display. Called after a successful in-app payment.
func (c *Client) CreateOrder(ctx context.Context, locationID string, items []OrderLineItem, referenceID string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"idempotency_key": referenceID,
		"order": map[string]any{
			"location_id":  locationID,
			"reference_id": referenceID, // whisked order ID — visible on POS
			"line_items":   items,
			"state":        "OPEN",
		},
	})

	req, err := c.newRequest(ctx, http.MethodPost, baseURL+"/orders", body)
	if err != nil {
		return "", fmt.Errorf("square: building request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("square: create order: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Order struct {
			ID string `json:"id"`
		} `json:"order"`
		Errors []struct {
			Detail string `json:"detail"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("square: decoding response: %w", err)
	}
	if len(result.Errors) > 0 {
		return "", fmt.Errorf("square: %s", result.Errors[0].Detail)
	}
	return result.Order.ID, nil
}

// newRequest builds an authenticated request. Token resolution is lazy —
// it calls the tokenSource on every request so OAuth refreshes are transparent.
func (c *Client) newRequest(ctx context.Context, method, url string, body []byte) (*http.Request, error) {
	token, err := c.tokens.AccessToken(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Square-Version", "2024-01-18")
	return req, nil
}
