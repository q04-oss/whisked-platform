package loyalty

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/q04-oss/whisked-platform/internal/audit"
	"github.com/q04-oss/whisked-platform/internal/httputil"
	"github.com/q04-oss/whisked-platform/internal/platform"
)

// shopifyOrder is the minimal subset of a Shopify orders/paid webhook payload
// that the loyalty domain needs. Shopify sends much more — we ignore the rest.
type shopifyOrder struct {
	ID    int64  `json:"id"`
	Email string `json:"email"`
}

// shopifyHandler handles Shopify webhook requests.
type shopifyHandler struct {
	service       *Service
	webhookSecret string
	audit         *audit.Writer
}

// handleOrderPaid processes a Shopify orders/paid webhook.
//
// Shopify requires a 200 response to consider the webhook delivered. Any
// non-200 response triggers Shopify's retry logic (19 retries over 48 hours).
// This handler always returns 200 after signature validation — internal errors
// are logged but not surfaced to Shopify.
func (h *shopifyHandler) handleOrderPaid(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "could not read body", http.StatusBadRequest)
		return
	}

	sig := r.Header.Get("X-Shopify-Hmac-Sha256")
	if !validateShopifyHMAC(h.webhookSecret, body, sig) {
		h.audit.Write(r.Context(), audit.Entry{
			EventType: audit.EventWebhookRejected,
			ActorType: audit.ActorSystem,
			Metadata:  map[string]string{"source": "shopify", "reason": "invalid_hmac"},
		})
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	h.audit.Write(r.Context(), audit.Entry{
		EventType: audit.EventWebhookReceived,
		ActorType: audit.ActorSystem,
		Metadata:  map[string]string{"source": "shopify", "topic": "orders/paid"},
	})

	var order shopifyOrder
	if err := json.Unmarshal(body, &order); err != nil {
		// Malformed payload — return 200 so Shopify doesn't retry.
		httputil.Respond(w, http.StatusOK, nil)
		return
	}

	email := strings.ToLower(strings.TrimSpace(order.Email))
	if email == "" || order.ID == 0 {
		httputil.Respond(w, http.StatusOK, nil)
		return
	}

	orderID := fmt.Sprintf("%d", order.ID)

	// ProcessShopifyOrder never returns a client-facing error — see its doc.
	if err := h.service.ProcessShopifyOrder(r.Context(), orderID, email); err != nil {
		// Log but still return 200 — the webhook was received and validated.
		// The error is an internal processing issue, not a delivery failure.
		_ = platform.Internal(err)
	}

	httputil.Respond(w, http.StatusOK, nil)
}

// validateShopifyHMAC verifies the X-Shopify-Hmac-Sha256 header.
// Shopify computes Base64(HMAC-SHA256(webhook_secret, raw_body)).
func validateShopifyHMAC(secret string, body []byte, signature string) bool {
	if signature == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return subtle.ConstantTimeCompare([]byte(expected), []byte(signature)) == 1
}
