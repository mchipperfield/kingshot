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
	players map[string]*Player // playerID → Player
}

func newMapStore(initial map[string]*Player) *mapStore {
	players := make(map[string]*Player)
	if initial != nil {
		for k, v := range initial {
			players[k] = v
		}
	}
	return &mapStore{players: players}
}

func (m *mapStore) Players() ([]*Player, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	players := make([]*Player, 0, len(m.players))
	for _, p := range m.players {
		players = append(players, p)
	}
	return players, nil
}

func (m *mapStore) FindByPlayerID(playerID string) (*Player, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, found := m.players[playerID]
	return p, found, nil
}

func (m *mapStore) FindByUser(userID string) ([]*Player, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var userPlayers []*Player
	for _, p := range m.players {
		if p.UserID == userID {
			userPlayers = append(userPlayers, p)
		}
	}
	return userPlayers, nil
}

func (m *mapStore) AddPlayer(req NewPlayerRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	player := &Player{
		PlayerID:  req.PlayerID,
		UserID:    req.UserID,
		KingdomID: req.KingdomID,
	}
	m.players[player.PlayerID] = player
	return nil
}

func (m *mapStore) UpdatePlayerKingdom(playerID, newKingdomID, guildID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.players[playerID]; ok {
		p.KingdomID = newKingdomID
		// The mock doesn't need to track history.
	}
	return nil
}

// errStore always returns err for every operation.
type errStore struct{ err error }

func (e *errStore) Players() ([]*Player, error)                  { return nil, e.err }
func (e *errStore) FindByPlayerID(string) (*Player, bool, error) { return nil, false, e.err }
func (e *errStore) FindByUser(string) ([]*Player, error)         { return nil, e.err }
func (e *errStore) AddPlayer(NewPlayerRequest) error                      { return e.err }
func (e *errStore) UpdatePlayerKingdom(string, string, string) error      { return e.err }

// mockKingShotAPI starts an httptest server for /gift_code (redeem),
// and returns a GiftCodeService wired to it.
func mockKingShotAPI(t *testing.T, redeemErrCode string) *GiftCodeService {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/gift_code":
			json.NewEncoder(w).Encode(RedeemResponse{ErrCode: ErrCode(redeemErrCode)})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return &GiftCodeService{
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

// TestGiftCodeService_RegisterPlayer tests the player registration logic.
func TestGiftCodeService_RegisterPlayer(t *testing.T) {
	t.Run("new player", func(t *testing.T) {
		store := newMapStore(nil)
		svc := &GiftCodeService{store: store, client: &http.Client{}}
		req := NewPlayerRequest{PlayerID: "p1", UserID: "u1", KingdomID: "k1"}
		result := svc.RegisterPlayer(req)
		if !result.Success {
			t.Fatalf("expected success, got %+v", result)
		}
		if p, found, _ := store.FindByPlayerID("p1"); !found || p.UserID != "u1" {
			t.Errorf("player not added to store correctly")
		}
	})

	t.Run("player already registered to self", func(t *testing.T) {
		store := newMapStore(map[string]*Player{"p1": {PlayerID: "p1", UserID: "u1"}})
		svc := &GiftCodeService{store: store}
		req := NewPlayerRequest{PlayerID: "p1", UserID: "u1"}
		result := svc.RegisterPlayer(req)
		if !result.AlreadySelf {
			t.Errorf("expected AlreadySelf=true, got %+v", result)
		}
	})

	t.Run("player already registered to other", func(t *testing.T) {
		store := newMapStore(map[string]*Player{"p1": {PlayerID: "p1", UserID: "u2"}})
		svc := &GiftCodeService{store: store}
		req := NewPlayerRequest{PlayerID: "p1", UserID: "u1"}
		result := svc.RegisterPlayer(req)
		if !result.AlreadyOther {
			t.Errorf("expected AlreadyOther=true, got %+v", result)
		}
	})

	t.Run("max players for kingdom reached", func(t *testing.T) {
		store := newMapStore(map[string]*Player{
			"p1": {PlayerID: "p1", UserID: "u1", KingdomID: "k1"},
			"p2": {PlayerID: "p2", UserID: "u1", KingdomID: "k1"},
		})
		svc := &GiftCodeService{store: store}
		req := NewPlayerRequest{PlayerID: "p3", UserID: "u1", KingdomID: "k1"}
		result := svc.RegisterPlayer(req)
		if !result.MaxPlayersForKingdomReached {
			t.Errorf("expected MaxPlayersForKingdomReached=true, got %+v", result)
		}
	})
}

func TestGiftCodeService_TransferPlayer(t *testing.T) {
	t.Run("successful transfer", func(t *testing.T) {
		store := newMapStore(map[string]*Player{
			"p1": {PlayerID: "p1", UserID: "u1", KingdomID: "k1"},
		})
		svc := &GiftCodeService{store: store}
		req := TransferPlayerRequest{PlayerID: "p1", UserID: "u1", NewKingdomID: "k2"}
		result := svc.TransferPlayer(req)
		if !result.Success {
			t.Fatalf("expected success, got %+v", result)
		}
		if p, _, _ := store.FindByPlayerID("p1"); p.KingdomID != "k2" {
			t.Errorf("player kingdom not updated, got %s", p.KingdomID)
		}
	})

	t.Run("player not found, registers new player", func(t *testing.T) {
		store := newMapStore(nil)
		svc := &GiftCodeService{store: store, client: &http.Client{}}
		req := TransferPlayerRequest{PlayerID: "p1", UserID: "u1", NewKingdomID: "k1"}
		result := svc.TransferPlayer(req)
		if !result.PlayerNotFound {
			t.Fatalf("expected player not found, got %+v", result)
		}
		if result.RegistrationResult == nil {
			t.Fatal("expected registration result, got nil")
		}
		if !result.RegistrationResult.Success {
			t.Errorf("expected registration to be successful, got %+v", result.RegistrationResult)
		}
		if p, found, _ := store.FindByPlayerID("p1"); !found || p.UserID != "u1" {
			t.Errorf("player not added to store correctly")
		}
	})

	t.Run("not your player", func(t *testing.T) {
		store := newMapStore(map[string]*Player{
			"p1": {PlayerID: "p1", UserID: "u2", KingdomID: "k1"},
		})
		svc := &GiftCodeService{store: store}
		req := TransferPlayerRequest{PlayerID: "p1", UserID: "u1", NewKingdomID: "k2"}
		result := svc.TransferPlayer(req)
		if !result.NotYourPlayer {
			t.Errorf("expected NotYourPlayer=true, got %+v", result)
		}
	})

	t.Run("max players for new kingdom reached", func(t *testing.T) {
		store := newMapStore(map[string]*Player{
			"p1": {PlayerID: "p1", UserID: "u1", KingdomID: "k1"},
			"p2": {PlayerID: "p2", UserID: "u1", KingdomID: "k2"},
			"p3": {PlayerID: "p3", UserID: "u1", KingdomID: "k2"},
		})
		svc := &GiftCodeService{store: store}
		req := TransferPlayerRequest{PlayerID: "p1", UserID: "u1", NewKingdomID: "k2"}
		result := svc.TransferPlayer(req)
		if !result.MaxPlayersForNewKingdomReached {
			t.Errorf("expected MaxPlayersForNewKingdomReached=true, got %+v", result)
		}
	})

	t.Run("transfer to same kingdom with 2 players should be allowed", func(t *testing.T) {
		store := newMapStore(map[string]*Player{
			"p1": {PlayerID: "p1", UserID: "u1", KingdomID: "k1"},
			"p2": {PlayerID: "p2", UserID: "u1", KingdomID: "k1"},
		})
		svc := &GiftCodeService{store: store}
		req := TransferPlayerRequest{PlayerID: "p1", UserID: "u1", NewKingdomID: "k1"}
		result := svc.TransferPlayer(req)
		if !result.Success {
			t.Fatalf("expected success, got %+v", result)
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

// TestGiftCodeService_redeemForPlayer tests the redeem → interpret
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
		{"unknown error code", "99999", "Failed to redeem code."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := mockKingShotAPI(t, tt.redeemErrCode)
			player := &Player{PlayerID: "player1", KingdomID: "k1", UserID: "discord1"}
			got := svc.redeemForPlayer(player, "TESTCODE")
			if got != tt.wantMsg {
				t.Errorf("got %q, want %q", got, tt.wantMsg)
			}
		})
	}

	t.Run("redeem HTTP failure", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"code": "not an int"}`))
		}))
		t.Cleanup(srv.Close)
		svc := &GiftCodeService{
			redeemURL: srv.URL + "/gift_code",
			client:    srv.Client(),
			store:     newMapStore(nil),
		}
		player := &Player{PlayerID: "player1", KingdomID: "k1", UserID: "discord1"}
		got := svc.redeemForPlayer(player, "TESTCODE")
		if got != "Error redeeming code." {
			t.Errorf("got %q, want %q", got, "Error redeeming code.")
		}
	})
}

// --- Helpers -----------------------------------------------------------------

// isHex returns true if s contains only hex characters.
func isHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}
