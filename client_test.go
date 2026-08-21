package kingshot

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientRedeemGiftCodeRetriesAfterError(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			http.Error(w, "invalid response", http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(redeemResponse{ErrCode: ErrCodeSuccess})
	}))
	t.Cleanup(server.Close)

	client := &Client{
		Client:    server.Client(),
		redeemURL: server.URL,
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	response, err := client.redeemGiftCode(context.Background(), "player", "kingdom", "code")
	if err != nil {
		t.Fatalf("redeemGiftCode() error = %v", err)
	}
	if response == nil || response.ErrCode != ErrCodeSuccess {
		t.Fatalf("redeemGiftCode() response = %#v, want success", response)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
}

func TestClientRedeemGiftCodeReturnsExhaustedAfterThreeErrors(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, "invalid response", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	client := &Client{
		Client:    server.Client(),
		redeemURL: server.URL,
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	response, err := client.redeemGiftCode(context.Background(), "player", "kingdom", "code")
	if response != nil {
		t.Fatalf("redeemGiftCode() response = %#v, want nil", response)
	}
	if err == nil || !strings.Contains(err.Error(), "attempts exhausted") {
		t.Fatalf("redeemGiftCode() error = %v, want exhausted error", err)
	}
	if requests != maxRedeemAttempts {
		t.Fatalf("requests = %d, want %d", requests, maxRedeemAttempts)
	}
}
