package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"golang.org/x/oauth2"
)

const (
	deletePath = "/delete"
)

func NewPrivacyHandler(cfg oauth2.Config, signingKey []byte) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+deletePath, startOAuthFlow(cfg))
	mux.HandleFunc("GET /oauth/discord/callback", handleOAuthCallback(cfg, signingKey))
	mux.HandleFunc("POST "+deletePath, deleteMyData(signingKey))
	csrf := http.NewCrossOriginProtection()
	return csrf.Handler(mux)
}

const (
	oauthStateCookie     = "privacy_oauth_state"
	privacySessionCookie = "privacy_session"
)

func startOAuthFlow(cfg oauth2.Config) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		state, err := generateStateToken()
		if err != nil {
			http.Error(w, "failed to generate state token", http.StatusInternalServerError)
			return
		}
		setStateCookie(w, state)

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "delete my data <a href=\"%s\">here</a>", cfg.AuthCodeURL(state))
	}
}

func handleOAuthCallback(cfg oauth2.Config, signingKey []byte) func(w http.ResponseWriter, r *http.Request) {
	// Discord user info response structure, only used in this handler so no need for package level visibility.
	type discordUser struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		stateCookie, err := r.Cookie(oauthStateCookie)
		clearCookie(w, oauthStateCookie)
		if err != nil || r.URL.Query().Get("state") == "" || r.URL.Query().Get("state") != stateCookie.Value {
			slog.Info("invalid state", "error", err)
			http.Error(w, "invalid state", http.StatusBadRequest)
			return
		}
		token, err := cfg.Exchange(r.Context(), r.URL.Query().Get("code"))
		if err != nil {
			slog.Error("failed to exchange code", "error", err)
			http.Error(w, "failed to exchange code", http.StatusInternalServerError)
			return
		}
		client := cfg.Client(r.Context(), token)
		resp, err := client.Get("https://discord.com/api/users/@me")
		if err != nil {
			slog.Error("failed to get user", "error", err)
			http.Error(w, "failed to get user", http.StatusInternalServerError)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			slog.Error("failed to get user", "status", resp.StatusCode)
			http.Error(w, "failed to get user", http.StatusInternalServerError)
			return
		}

		var user discordUser
		err = json.NewDecoder(resp.Body).Decode(&user)
		if err != nil {
			slog.Error("failed to decode user", "error", err)
			http.Error(w, "failed to decode user", http.StatusInternalServerError)
			return
		}
		err = setSessionCookie(w, user.ID, signingKey)
		if err != nil {
			slog.Error("failed to set session cookie", "error", err)
			http.Error(w, "failed to set session cookie", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `<form method="POST" action="/delete"><p>Are you sure?</p><input type="submit" /></form>`)
		fmt.Fprintf(os.Stdout, "user: %+v", user)
	}
}

func deleteMyData(signingKey []byte) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {

		sessionCookie, err := r.Cookie(privacySessionCookie)
		clearCookie(w, privacySessionCookie)
		if err != nil {
			slog.Error("failed to get session cookie", "error", err)
			http.Error(w, "failed to get session cookie", http.StatusUnauthorized)
			return
		}
		_, err = parseAndVerifySessionCookie(sessionCookie.Value, signingKey)
		if err != nil {
			slog.Error("failed to verify session cookie", "error", err)
			http.Error(w, "failed to verify session cookie", http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "your data has been deleted")
	}
}
