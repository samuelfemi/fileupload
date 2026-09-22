package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"golang.org/x/oauth2"

	"github.com/Falasefemi2/fileupload/internal/auth"
)

func oauthOfflinePrompt() []oauth2.AuthCodeOption {
	return []oauth2.AuthCodeOption{
		oauth2.AccessTypeOffline,
		oauth2.ApprovalForce,
	}
}

func (h *Handlers) GoogleLogin(w http.ResponseWriter, r *http.Request) {
	state, err := auth.GenerateState()
	if err != nil {
		http.Error(w, "failed to start login", http.StatusInternalServerError)
		return
	}

	sameSite := http.SameSiteLaxMode
	if h.cfg.Env == "production" {
		sameSite = http.SameSiteNoneMode
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cfg.Env == "production",
		SameSite: sameSite,
		MaxAge:   600, // 10 min
	})

	url := h.googleCfg.AuthCodeURL(state, oauthOfflinePrompt()...)
	http.Redirect(w, r, url, http.StatusFound)
}

func (h *Handlers) GoogleCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	stateCookie, err := r.Cookie("oauth_state")
	if err != nil || r.URL.Query().Get("state") != stateCookie.Value {
		http.Error(w, "invalid oauth state", http.StatusBadRequest)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}

	googleUser, err := auth.FetchGoogleUser(ctx, h.googleCfg, code)
	if err != nil {
		http.Error(w, "google auth failed: "+err.Error(), http.StatusUnauthorized)
		return
	}

	var userID string
	err = h.db.QueryRowContext(ctx, `
		INSERT INTO users (google_id, email, name, avatar_url)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (google_id) DO UPDATE
			SET email = EXCLUDED.email, name = EXCLUDED.name, avatar_url = EXCLUDED.avatar_url
		RETURNING id
	`, googleUser.ID, googleUser.Email, googleUser.Name, googleUser.Picture).Scan(&userID)
	if err != nil {
		http.Error(w, "failed to save user: "+err.Error(), http.StatusInternalServerError)
		return
	}

	sessionToken, err := auth.IssueSessionToken(h.cfg.JWTSecret, userID)
	if err != nil {
		http.Error(w, "failed to issue session", http.StatusInternalServerError)
		return
	}

	sameSite := http.SameSiteLaxMode
	if h.cfg.Env == "production" {
		sameSite = http.SameSiteNoneMode
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    sessionToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cfg.Env == "production",
		SameSite: sameSite,
		MaxAge:   7 * 24 * 3600,
	})

	http.Redirect(w, r, h.cfg.FrontendURL, http.StatusFound)
}

func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	sameSite := http.SameSiteLaxMode
	if h.cfg.Env == "production" {
		sameSite = http.SameSiteNoneMode
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cfg.Env == "production",
		SameSite: sameSite,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) Me(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var email, name, avatarURL string
	err := h.db.QueryRowContext(r.Context(),
		`SELECT email, name, COALESCE(avatar_url, '') FROM users WHERE id = $1`, userID,
	).Scan(&email, &name, &avatarURL)
	if err != nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{
		"id":        userID,
		"email":     email,
		"name":      name,
		"avatarUrl": avatarURL,
	})
}
