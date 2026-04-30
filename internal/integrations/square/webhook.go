package square

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
)

// ValidateWebhook verifies the authenticity of a Square webhook payload.
//
// Square computes: Base64(HMAC-SHA256(signing_key, notification_url + body))
// The notification URL must exactly match what is configured in the Square
// Developer dashboard — including scheme, host, path, and no trailing slash.
func ValidateWebhook(signingKey string, notificationURL string, body []byte, signature string) bool {
	if signature == "" {
		return false
	}

	mac := hmac.New(sha256.New, []byte(signingKey))
	mac.Write([]byte(notificationURL))
	mac.Write(body)
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	return subtle.ConstantTimeCompare([]byte(expected), []byte(signature)) == 1
}

// PaymentEvent is the subset of a Square payment.completed webhook payload
// that the loyalty domain needs.
type PaymentEvent struct {
	Type string `json:"type"`
	Data struct {
		Object struct {
			Payment struct {
				ID         string `json:"id"`
				CustomerID string `json:"customer_id"`
				Status     string `json:"status"`
				LocationID string `json:"location_id"`
			} `json:"payment"`
		} `json:"object"`
	} `json:"data"`
}
