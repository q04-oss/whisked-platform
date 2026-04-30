// Package square provides a minimal client for the Square API.
// Only the endpoints needed by whisked-platform are implemented:
// customer creation and lookup for loyalty auto-stamping.
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

// Client calls the Square API on behalf of the Whisked merchant.
type Client struct {
	accessToken string
	http        *http.Client
}

// New returns a Client using the given access token.
func New(accessToken string, http *http.Client) *Client {
	return &Client{accessToken: accessToken, http: http}
}

// Customer is the subset of Square's customer object used by this integration.
type Customer struct {
	ID          string `json:"id"`
	EmailAddress string `json:"email_address"`
	GivenName   string `json:"given_name"`
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

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/customers", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("square: building request: %w", err)
	}
	c.setHeaders(req)

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

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/customers/search", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("square: building request: %w", err)
	}
	c.setHeaders(req)

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

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Square-Version", "2024-01-18")
}

// IsEnabled returns false when no access token is configured.
// All callers should check this before making API calls.
func (c *Client) IsEnabled() bool { return c.accessToken != "" }

// NewFromConfig returns a Client, or nil if Square is not configured.
func NewFromConfig(accessToken string, httpClient *http.Client) *Client {
	if accessToken == "" {
		return nil
	}
	return &Client{
		accessToken: accessToken,
		http: &http.Client{Timeout: 15 * time.Second},
	}
}
