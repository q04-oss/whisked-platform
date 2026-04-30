package loyalty

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/q04-oss/whisked-platform/internal/audit"
	"github.com/q04-oss/whisked-platform/internal/httputil"
	"github.com/q04-oss/whisked-platform/internal/integrations/square"
)

// squareWebhookHandler handles Square payment webhooks.
// When a payment.completed event arrives with a known Square customer ID,
// a steep is automatically credited — no staff action required.
type squareWebhookHandler struct {
	service       *Service
	signingKey    string
	notificationURL string
	audit         *audit.Writer
}

func (h *squareWebhookHandler) handle(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "could not read body", http.StatusBadRequest)
		return
	}

	sig := r.Header.Get("x-square-hmacsha256-signature")
	if !square.ValidateWebhook(h.signingKey, h.notificationURL, body, sig) {
		h.audit.Write(r.Context(), audit.Entry{
			EventType: audit.EventWebhookRejected,
			ActorType: audit.ActorSystem,
			Metadata:  map[string]string{"source": "square", "reason": "invalid_signature"},
		})
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	h.audit.Write(r.Context(), audit.Entry{
		EventType: audit.EventWebhookReceived,
		ActorType: audit.ActorSystem,
		Metadata:  map[string]string{"source": "square"},
	})

	var event square.PaymentEvent
	if err := json.Unmarshal(body, &event); err != nil {
		// Malformed — ack to prevent Square retries.
		httputil.Respond(w, http.StatusOK, nil)
		return
	}

	if event.Type != "payment.completed" || event.Data.Object.Payment.Status != "COMPLETED" {
		httputil.Respond(w, http.StatusOK, nil)
		return
	}

	payment := event.Data.Object.Payment
	if err := h.service.ProcessSquarePayment(r.Context(), payment.ID, payment.CustomerID); err != nil {
		slog.ErrorContext(r.Context(), "square webhook: process payment failed",
			"payment_id", payment.ID,
			"error", err,
		)
	}

	// Always return 200 — Square retries on any non-2xx response.
	httputil.Respond(w, http.StatusOK, nil)
}
