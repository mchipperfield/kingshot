package kingshot

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sync"
	"testing"
)

// --- Test helpers ------------------------------------------------------------

// mapStore is an in-memory PlayerStore for testing.
type mapStore struct {
	mu      sync.Mutex
	players map[string]Player // playerID → Player
}

func newMapStore(initial map[string]Player) *mapStore {
	players := make(map[string]Player)
	for k, v := range initial {
		players[k] = v
	}
	return &mapStore{players: players}
}

func (m *mapStore) Players() ([]Player, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]Player, 0, len(m.players))
	for _, p := range m.players {
		result = append(result, p)
	}
	return result, nil
}

func (m *mapStore) FindByPlayerID(playerID string) (Player, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, found := m.players[playerID]
	return p, found, nil
}

func (m *mapStore) AddPlayer(player Player) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.players[player.ID] = player
	return nil
}

// errStore always returns err for every operation.
type errStore struct{ err error }

func (e *errStore) Players() ([]Player, error)                        { return nil, e.err }
func (e *errStore) FindByPlayerID(string) (Player, bool, error)       { return Player{}, false, e.err }
func (e *errStore) AddPlayer(Player) error                            { return e.err }

// mockKingShotAPI starts an httptest server for /player (login) and
// /gift_code (redeem), and returns a GiftCodeService wired to it.
func mockKingShotAPI(t *testing.T, loginCode int, redeemErrCode string) *GiftCodeService {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/player":
			json.NewEncoder(w).Encode(LoginResponse{Code: loginCode})
		case "/gift_code":
			json.NewEncoder(w).Encode(RedeemResponse{ErrCode: ErrCode(redeemErrCode)})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return &GiftCodeService{
		loginURL:  srv.URL + "/player",
		redeemURL: srv.URL + "/gift_code",
		client:    srv.Client(),
		store:     newMapStore(nil),
	}
}

// --- API type tests ----------------------------------------------------------

// TestErrCodeUnmarshalJSON verifies that ErrCode correctly deserialises both
// string and numeric JSON values.
func TestErrCodeUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  ErrCode
	}{
		{"string value", `"20000"`, ErrCode("20000")},
		{"numeric value", `20000`, ErrCode("20000")},
		{"error string", `"40008"`, ErrCode("40008")},
		{"error numeric", `40008`, ErrCode("40008")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got ErrCode
			if err := json.Unmarshal([]byte(tt.input), &got); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}

	t.Run("invalid value", func(t *testing.T) {
		var got ErrCode
		err := json.Unmarshal([]byte(`true`), &got)
		if err == nil {
			t.Fatal("expected error for boolean input, got nil")
		}
	})
}

// TestEncodePayload verifies that EncodePayload produces a deterministic
// JSON payload that contains a "sign" field and that the signature is correct.
func TestEncodePayload(t *testing.T) {
	t.Run("adds sign field", func(t *testing.T) {
		data := map[string]string{
			"fid":  "12345",
			"time": "1700000000",
		}
		payload, err := EncodePayload(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var result map[string]string
		if err := json.Unmarshal([]byte(payload), &result); err != nil {
			t.Fatalf("payload is not valid JSON: %v", err)
		}

		if _, ok := result["sign"]; !ok {
			t.Error("payload missing 'sign' field")
		}
		if result["fid"] != "12345" {
			t.Errorf("fid = %q, want %q", result["fid"], "12345")
		}
	})

	t.Run("sign is deterministic for same input", func(t *testing.T) {
		data1 := map[string]string{"fid": "abc", "time": "999"}
		data2 := map[string]string{"fid": "abc", "time": "999"}

		p1, err := EncodePayload(data1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		p2, err := EncodePayload(data2)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var r1, r2 map[string]string
		json.Unmarshal([]byte(p1), &r1)
		json.Unmarshal([]byte(p2), &r2)

		if r1["sign"] != r2["sign"] {
			t.Errorf("expected deterministic sign, got %q and %q", r1["sign"], r2["sign"])
		}
	})

	t.Run("different inputs produce different signs", func(t *testing.T) {
		d1 := map[string]string{"fid": "player1", "time": "1000"}
		d2 := map[string]string{"fid": "player2", "time": "1000"}

		p1, _ := EncodePayload(d1)
		p2, _ := EncodePayload(d2)

		var r1, r2 map[string]string
		json.Unmarshal([]byte(p1), &r1)
		json.Unmarshal([]byte(p2), &r2)

		if r1["sign"] == r2["sign"] {
			t.Error("expected different signs for different inputs")
		}
	})

	t.Run("sign is computed correctly", func(t *testing.T) {
		data := map[string]string{
			"fid":  "testplayer",
			"time": "1700000000",
		}

		values := url.Values{}
		for k, v := range data {
			values.Set(k, v)
		}
		dataToHash := values.Encode() + Key

		dataCopy := map[string]string{"fid": "testplayer", "time": "1700000000"}
		payload, err := EncodePayload(dataCopy)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var result map[string]string
		json.Unmarshal([]byte(payload), &result)

		sign := result["sign"]
		if len(sign) != 32 {
			t.Errorf("sign length = %d, want 32; dataToHash = %q", len(sign), dataToHash)
		}
		if !isHex(sign) {
			t.Errorf("sign %q is not a valid hex string", sign)
		}
	})
}

// TestLoginResponseDecoding verifies that LoginResponse JSON with both string
// and numeric err_code values deserialises correctly.
func TestLoginResponseDecoding(t *testing.T) {
	t.Run("numeric err_code", func(t *testing.T) {
		raw := `{"code": 0, "msg": "ok", "data": null, "err_code": 20000}`
		var resp LoginResponse
		if err := json.Unmarshal([]byte(raw), &resp); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.ErrCode != ErrCode("20000") {
			t.Errorf("ErrCode = %q, want %q", resp.ErrCode, "20000")
		}
	})

	t.Run("string err_code", func(t *testing.T) {
		raw := `{"code": 0, "msg": "ok", "data": null, "err_code": "20000"}`
		var resp LoginResponse
		if err := json.Unmarshal([]byte(raw), &resp); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.ErrCode != ErrCode("20000") {
			t.Errorf("ErrCode = %q, want %q", resp.ErrCode, "20000")
		}
	})
}

// TestRedeemResponseDecoding mirrors TestLoginResponseDecoding for RedeemResponse.
func TestRedeemResponseDecoding(t *testing.T) {
	raw := fmt.Sprintf(`{"code": 0, "msg": "success", "err_code": "%s"}`, ErrCodeSuccess)
	var resp RedeemResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ErrCode != ErrCode(ErrCodeSuccess) {
		t.Errorf("ErrCode = %q, want %q", resp.ErrCode, ErrCodeSuccess)
	}
}

// --- Service logic tests -----------------------------------------------------

// TestInterpretRedeemResult verifies that every known ErrCode maps to the
// correct outcome flags and message.
func TestInterpretRedeemResult(t *testing.T) {
	tests := []struct {
		errCode     ErrCode
		wantMsg     string
		wantExpired bool
		wantInvalid bool
		wantLogin   bool
	}{
		{ErrCodeSuccess, "Successfully redeemed!", false, false, false},
		{ErrCodeClaimed, "Already claimed.", false, false, false},
		{ErrCodeExpired, "Code expired or not found.", true, false, false},
		{ErrCodeNotFound, "Code is not valid.", false, true, false},
		{ErrCodeLogin, "Unable to login.", false, false, true},
		{ErrCodeLimitReached, "Redemption Limit Reached", false, false, false},
		{ErrCodeUserDetails, "User details incorrect.", false, false, true},
		{"99999", "Failed to redeem code.", false, false, false},
	}
	for _, tt := range tests {
		t.Run(string(tt.errCode), func(t *testing.T) {
			got := interpretRedeemResult(tt.errCode)
			if got.msg != tt.wantMsg {
				t.Errorf("msg = %q, want %q", got.msg, tt.wantMsg)
			}
			if got.codeExpired != tt.wantExpired {
				t.Errorf("codeExpired = %v, want %v", got.codeExpired, tt.wantExpired)
			}
			if got.codeInvalid != tt.wantInvalid {
				t.Errorf("codeInvalid = %v, want %v", got.codeInvalid, tt.wantInvalid)
			}
			if got.loginFailed != tt.wantLogin {
				t.Errorf("loginFailed = %v, want %v", got.loginFailed, tt.wantLogin)
			}
		})
	}
}

// TestGiftCodeService_ProcessNewCode covers the early-return paths that require
// no network calls.
func TestGiftCodeService_ProcessNewCode(t *testing.T) {
	t.Run("already active", func(t *testing.T) {
		svc := &GiftCodeService{activeCodes: []string{"EXISTINGCODE"}, store: newMapStore(nil)}
		result := svc.ProcessNewCode("EXISTINGCODE")
		if !result.AlreadyActive {
			t.Errorf("expected AlreadyActive=true, got %+v", result)
		}
	})

	t.Run("already expired", func(t *testing.T) {
		svc := &GiftCodeService{expiredCodes: []string{"EXPIREDCODE"}, store: newMapStore(nil)}
		result := svc.ProcessNewCode("EXPIREDCODE")
		if !result.AlreadyExpired {
			t.Errorf("expected AlreadyExpired=true, got %+v", result)
		}
	})

	t.Run("store error", func(t *testing.T) {
		svc := &GiftCodeService{store: &errStore{errors.New("store error")}}
		result := svc.ProcessNewCode("NEWCODE")
		if result.StoreError == nil {
			t.Error("expected StoreError to be set")
		}
	})

	t.Run("no registered players adds code to active list", func(t *testing.T) {
		svc := &GiftCodeService{store: newMapStore(nil)}
		result := svc.ProcessNewCode("FRESHCODE")
		if !result.Added {
			t.Errorf("expected Added=true, got %+v", result)
		}
		if len(result.PlayerResults) != 0 {
			t.Errorf("expected empty PlayerResults, got %v", result.PlayerResults)
		}
		if !slices.Contains(svc.activeCodes, "FRESHCODE") {
			t.Error("expected FRESHCODE to be in activeCodes")
		}
	})
}

// TestGiftCodeService_concurrentAccess runs concurrent ProcessNewCode calls so
// the race detector can catch any unsynchronised access to the shared slices.
func TestGiftCodeService_concurrentAccess(t *testing.T) {
	svc := &GiftCodeService{store: &errStore{errors.New("no store")}}
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			svc.ProcessNewCode(fmt.Sprintf("CODE%d", n))
		}(i)
	}
	wg.Wait()
}

// TestGiftCodeService_isCodeKnown verifies active and expired membership
// detection without touching any I/O.
func TestGiftCodeService_isCodeKnown(t *testing.T) {
	svc := &GiftCodeService{
		activeCodes:  []string{"ACTIVE1", "ACTIVE2"},
		expiredCodes: []string{"EXPIRED1"},
		store:        newMapStore(nil),
	}
	tests := []struct {
		code        string
		wantActive  bool
		wantExpired bool
	}{
		{"ACTIVE1", true, false},
		{"ACTIVE2", true, false},
		{"EXPIRED1", false, true},
		{"UNKNOWN", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			active, expired := svc.isCodeKnown(tt.code)
			if active != tt.wantActive {
				t.Errorf("active = %v, want %v", active, tt.wantActive)
			}
			if expired != tt.wantExpired {
				t.Errorf("expired = %v, want %v", expired, tt.wantExpired)
			}
		})
	}
}

// TestGiftCodeService_redeemForPlayer tests the login → redeem → interpret
// pipeline for a single player using a mock HTTP server.
func TestGiftCodeService_redeemForPlayer(t *testing.T) {
	tests := []struct {
		name          string
		redeemErrCode string
		wantMsg       string
	}{
		{"success", ErrCodeSuccess, "Successfully redeemed!"},
		{"already claimed", ErrCodeClaimed, "Already claimed."},
		{"login error from api", ErrCodeLogin, "Unable to login."},
		{"user details incorrect", ErrCodeUserDetails, "User details incorrect."},
		{"unknown error code", "99999", "Failed to redeem code."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := mockKingShotAPI(t, 0, tt.redeemErrCode)
			p := Player{ID: "player1", KID: "kingdom1"}
			got := svc.redeemForPlayer(p, "TESTCODE")
			if got != tt.wantMsg {
				t.Errorf("got %q, want %q", got, tt.wantMsg)
			}
		})
	}

	t.Run("login HTTP failure", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "internal error", http.StatusInternalServerError)
		}))
		t.Cleanup(srv.Close)
		svc := &GiftCodeService{
			loginURL:  srv.URL + "/player",
			redeemURL: srv.URL + "/gift_code",
			client:    srv.Client(),
			store:     newMapStore(nil),
		}
		p := Player{ID: "player1", KID: "kingdom1"}
		got := svc.redeemForPlayer(p, "TESTCODE")
		if got != "Failed to login." {
			t.Errorf("got %q, want %q", got, "Failed to login.")
		}
	})
}

// --- End-to-end tests --------------------------------------------------------

// TestEndToEnd_RedeemWithKID verifies the full login → redeem sequence,
// confirming that:
//  1. The /player (login) endpoint is called before /gift_code (redeem).
//  2. The redeem request includes the kid field for the player.
//  3. A 40020 (user details incorrect) response from the API is handled as a
//     login failure — covering the case where playerID or kid is invalid.
func TestEndToEnd_RedeemWithKID(t *testing.T) {
	const playerID = "player1"
	const kingdomID = "kingdom99"
	const giftCode = "GIFTCODE2025"

	t.Run("login called before redeem, kid included in request", func(t *testing.T) {
		var mu sync.Mutex
		var callOrder []string
		var capturedRedeemBody map[string]string

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/player":
				mu.Lock()
				callOrder = append(callOrder, "login")
				mu.Unlock()
				json.NewEncoder(w).Encode(LoginResponse{Code: 0})
			case "/gift_code":
				mu.Lock()
				callOrder = append(callOrder, "redeem")
				if err := json.NewDecoder(r.Body).Decode(&capturedRedeemBody); err != nil {
					t.Errorf("failed to decode redeem body: %v", err)
				}
				mu.Unlock()
				json.NewEncoder(w).Encode(RedeemResponse{ErrCode: ErrCode(ErrCodeSuccess)})
			default:
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(srv.Close)

		svc := &GiftCodeService{
			loginURL:  srv.URL + "/player",
			redeemURL: srv.URL + "/gift_code",
			client:    srv.Client(),
			store:     newMapStore(nil),
		}

		p := Player{ID: playerID, KID: kingdomID}
		got := svc.redeemForPlayer(p, giftCode)

		if got != "Successfully redeemed!" {
			t.Errorf("message = %q, want %q", got, "Successfully redeemed!")
		}

		mu.Lock()
		defer mu.Unlock()

		if len(callOrder) != 2 || callOrder[0] != "login" || callOrder[1] != "redeem" {
			t.Errorf("call order = %v, want [login redeem]", callOrder)
		}
		if capturedRedeemBody["kid"] != kingdomID {
			t.Errorf("redeem kid = %q, want %q", capturedRedeemBody["kid"], kingdomID)
		}
		if capturedRedeemBody["fid"] != playerID {
			t.Errorf("redeem fid = %q, want %q", capturedRedeemBody["fid"], playerID)
		}
		if capturedRedeemBody["cdk"] != giftCode {
			t.Errorf("redeem cdk = %q, want %q", capturedRedeemBody["cdk"], giftCode)
		}
	})

	t.Run("invalid player or kingdom id returns user details error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/player":
				json.NewEncoder(w).Encode(LoginResponse{Code: 0})
			case "/gift_code":
				json.NewEncoder(w).Encode(RedeemResponse{ErrCode: ErrCode(ErrCodeUserDetails)})
			default:
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(srv.Close)

		svc := &GiftCodeService{
			loginURL:  srv.URL + "/player",
			redeemURL: srv.URL + "/gift_code",
			client:    srv.Client(),
			store:     newMapStore(nil),
		}

		p := Player{ID: "badplayer", KID: "badkingdom"}
		got := svc.redeemForPlayer(p, giftCode)
		if got != "User details incorrect." {
			t.Errorf("message = %q, want %q", got, "User details incorrect.")
		}
	})
}

// isHex returns true if s contains only hex characters.
func isHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}
