package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now(),
		MaxAge:   -1,
	})
}

func parseAndVerifySessionCookie(cookieValue string, signingKey []byte) (string, error) {
	parts := strings.Split(cookieValue, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid cookie format")
	}
	userid := parts[0]
	exp := parts[1]
	signature := parts[2]

	expectedSignature, err := hashUserID(strings.Join(parts[:2], "."), signingKey)
	if err != nil {
		return "", err
	}
	if !hmac.Equal([]byte(signature), []byte(expectedSignature)) {
		return "", fmt.Errorf("invalid cookie signature")
	}
	expInt, err := strconv.ParseInt(exp, 10, 64)
	if err != nil {
		return "", fmt.Errorf("invalid cookie expiration")
	}
	if time.Now().Unix() > expInt {
		return "", fmt.Errorf("cookie has expired")
	}
	return userid, nil
}
func setSessionCookie(w http.ResponseWriter, userID string, signingKey []byte) error {
	exp := time.Now().Add(5 * time.Minute).Unix()
	payload := fmt.Sprintf("%s.%d", userID, exp)
	signature, err := hashUserID(payload, signingKey)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     privacySessionCookie,
		Value:    payload + "." + signature,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Path:     deletePath,
		Expires:  time.Now().Add(5 * time.Minute),
		MaxAge:   int((5 * time.Minute).Seconds()),
	})
	return nil
}

func setStateCookie(w http.ResponseWriter, state string) {
	http.SetCookie(w, &http.Cookie{
		Name:     oauthStateCookie,
		Value:    state,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Path:     "/oauth/discord/callback",
		Expires:  time.Now().Add(5 * time.Minute),
		MaxAge:   int((5 * time.Minute).Seconds()),
	})
}

func hashUserID(payload string, signingKey []byte) (string, error) {
	h := hmac.New(sha256.New, signingKey)
	_, err := h.Write([]byte(payload))
	if err != nil {
		slog.Error("failed to hash user ID", "error", err)
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
