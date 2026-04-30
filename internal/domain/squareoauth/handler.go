package squareoauth

import (
	"net/http"
)

type handler struct {
	service *Service
	cfg     handlerConfig
}

type handlerConfig struct {
	AppID       string
	RedirectURL string
}

// connect redirects the authenticated staff member to Square's OAuth consent page.
// A CSRF state token is generated and stored in Redis before the redirect.
func (h *handler) connect(w http.ResponseWriter, r *http.Request) {
	if h.cfg.AppID == "" {
		http.Error(w, "Square OAuth is not configured (SQUARE_APP_ID missing)", http.StatusServiceUnavailable)
		return
	}

	url, err := h.service.AuthorizationURL(r.Context(), h.cfg.AppID, h.cfg.RedirectURL)
	if err != nil {
		http.Error(w, "failed to build authorization URL", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, url, http.StatusFound)
}

// callback receives the authorization code from Square after the merchant
// approves the permission request. It validates the CSRF state, exchanges
// the code for tokens, and shows a branded confirmation page.
func (h *handler) callback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	errParam := r.URL.Query().Get("error")

	if errParam != "" {
		// Square sends error=access_denied when the merchant clicks "Deny".
		oauthResultPage(w, false, errParam)
		return
	}
	if code == "" || state == "" {
		http.Error(w, "missing code or state", http.StatusBadRequest)
		return
	}

	if err := h.service.HandleCallback(r.Context(), code, state); err != nil {
		oauthResultPage(w, false, err.Error())
		return
	}

	oauthResultPage(w, true, "")
}

// oauthResultPage renders a minimal branded HTML page shown after the OAuth flow.
// On success the page tells Belle she's all set.
// On failure it shows the error so she can report it or retry.
func oauthResultPage(w http.ResponseWriter, ok bool, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if ok {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Whisked · Square connected</title>
<style>
  body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;
       background:#f7f3ed;display:flex;align-items:center;justify-content:center;
       min-height:100vh;margin:0}
  .card{background:#fff;border-radius:16px;padding:40px 32px;max-width:380px;
        text-align:center;box-shadow:0 2px 16px rgba(0,0,0,.06)}
  h1{font-size:1.25rem;font-weight:600;color:#1e190d;margin:16px 0 8px}
  p{font-size:.875rem;color:#7a7060;line-height:1.5;margin:0}
  .bell{font-size:2.5rem}
</style>
</head>
<body>
<div class="card">
  <div class="bell">🔔</div>
  <h1>Square connected</h1>
  <p>Online orders will now appear on your Square POS automatically.<br>
     You can close this tab.</p>
</div>
</body>
</html>`))
		return
	}

	w.WriteHeader(http.StatusBadRequest)
	w.Write([]byte(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Whisked · Connection failed</title>
<style>
  body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;
       background:#f7f3ed;display:flex;align-items:center;justify-content:center;
       min-height:100vh;margin:0}
  .card{background:#fff;border-radius:16px;padding:40px 32px;max-width:380px;
        text-align:center;box-shadow:0 2px 16px rgba(0,0,0,.06)}
  h1{font-size:1.25rem;font-weight:600;color:#1e190d;margin:16px 0 8px}
  p{font-size:.875rem;color:#7a7060;line-height:1.5;margin:0}
  code{font-size:.75rem;color:#c0392b;background:#fdf0ee;padding:2px 6px;border-radius:4px}
</style>
</head>
<body>
<div class="card">
  <div style="font-size:2.5rem">⚠️</div>
  <h1>Connection failed</h1>
  <p>Something went wrong connecting your Square account.<br>
     <code>` + errMsg + `</code><br><br>
     Please try again or contact support.</p>
</div>
</body>
</html>`))
}
