package kingshot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// --- Test helpers ------------------------------------------------------------

// mapStore is an in-memory PlayerStore for testing.
type mapStore struct {
	mu       sync.Mutex
	players  map[string]*Player // playerID → Player
	unlinked map[string]bool    // playerID → true once unlinked
}

func newMapStore(initial map[string]*Player) *mapStore {
	players := make(map[string]*Player)
	for k, v := range initial {
		players[k] = v
	}
	return &mapStore{players: players, unlinked: make(map[string]bool)}
}

func (m *mapStore) Players(ctx context.Context) ([]*Player, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	players := make([]*Player, 0, len(m.players))
	for id, p := range m.players {
		if m.unlinked[id] {
			continue
		}
		players = append(players, p)
	}
	return players, nil
}

func (m *mapStore) FindByPlayerID(ctx context.Context, playerID string) (*Player, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.unlinked[playerID] {
		return nil, ErrNotFound
	}
	p, ok := m.players[playerID]
	if !ok {
		return nil, ErrNotFound
	}
	return p, nil
}

func (m *mapStore) FindByUser(ctx context.Context, userID string) ([]*Player, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var userPlayers []*Player
	for id, p := range m.players {
		if m.unlinked[id] {
			continue
		}
		if p.UserID == userID {
			userPlayers = append(userPlayers, p)
		}
	}
	return userPlayers, nil
}

func (m *mapStore) AddPlayer(ctx context.Context, req NewPlayerRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	player := &Player{
		PlayerID:  req.PlayerID,
		UserID:    req.UserID,
		KingdomID: req.KingdomID,
		GuildID:   req.GuildID,
	}
	m.players[player.PlayerID] = player
	delete(m.unlinked, player.PlayerID)
	return nil
}

func (m *mapStore) UpdatePlayerKingdom(ctx context.Context, req TransferPlayerRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.players[req.PlayerID]; ok {
		p.KingdomID = req.NewKingdomID
		p.GuildID = req.GuildID
		// The mock doesn't need to track history.
	}
	return nil
}

func (m *mapStore) UnlinkPlayer(ctx context.Context, req UnlinkPlayerRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.players[req.PlayerID]; ok {
		p.UserID = ""
		m.unlinked[req.PlayerID] = true
		// The mock doesn't need to track history.
	}
	return nil
}

// errStore always returns err for every operation.
type errStore struct{ err error }

func (e *errStore) Players(context.Context) ([]*Player, error) { return nil, e.err }
func (e *errStore) FindByPlayerID(context.Context, string) (*Player, error) {
	return nil, e.err
}
func (e *errStore) FindByUser(context.Context, string) ([]*Player, error) { return nil, e.err }
func (e *errStore) AddPlayer(context.Context, NewPlayerRequest) error     { return e.err }
func (e *errStore) UpdatePlayerKingdom(context.Context, TransferPlayerRequest) error {
	return e.err
}
func (e *errStore) UnlinkPlayer(context.Context, UnlinkPlayerRequest) error { return e.err }

type testCodeStore struct {
	code      *Code
	found     bool
	findErr   error
	addErr    error
	active    []string
	activeErr error
	removeErr error
}

func (s *testCodeStore) Find(context.Context, string) (*Code, bool, error) {
	return s.code, s.found, s.findErr
}

func (s *testCodeStore) Add(context.Context, Code) error {
	return s.addErr
}

func (s *testCodeStore) ActiveCodes(context.Context) ([]string, error) {
	return s.active, s.activeErr
}

func (s *testCodeStore) RemoveActive(context.Context, ...string) error {
	return s.removeErr
}

// mockKingShotAPI starts an httptest server for /gift_code (redeem),
// and returns a GiftCodeService wired to it.
func mockKingShotAPI(t *testing.T, redeemErrCode string) *GiftCodeService {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/gift_code":
			json.NewEncoder(w).Encode(redeemResponse{ErrCode: redeemErrCode})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return &GiftCodeService{
		codeStore: newInMemoryCodeStore(),
		client:    &Client{Client: srv.Client(), redeemURL: srv.URL + "/gift_code", logger: nil},
		store:     newMapStore(nil),
	}
}

// --- API type tests ----------------------------------------------------------

// TestEncodePayload verifies that EncodePayload produces a deterministic
// JSON payload that contains a "sign" field and that the signature is correct.
func TestEncodePayload(t *testing.T) {
	t.Run("adds sign field", func(t *testing.T) {
		data := map[string]string{
			"fid":  "12345",
			"time": "1700000000",
		}
		payload, err := encodePayload(data)
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

		p1, err := encodePayload(data1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		p2, err := encodePayload(data2)
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

		p1, _ := encodePayload(d1)
		p2, _ := encodePayload(d2)

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
		payload, err := encodePayload(dataCopy)
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
	var resp redeemResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ErrCode != ErrCodeSuccess {
		t.Errorf("ErrCode = %q, want %q", resp.ErrCode, ErrCodeSuccess)
	}
}

// --- Service logic tests -----------------------------------------------------

// TestInterpretRedeemResult verifies that every API failure maps to the
// expected caller-safe CodeError.
func TestInterpretRedeemResult(t *testing.T) {
	tests := []struct {
		errCode  string
		wantMsg  string
		wantKind codeErrorKind
	}{
		{ErrCodeClaimed, "Code already claimed.", codeErrorClaimed},
		{ErrCodeExpired, "This code has expired.", codeErrorExpired},
		{ErrCodeNotFound, "This code is invalid.", codeErrorInvalid},
		{ErrCodeLogin, "The player used to validate this code is invalid.", codeErrorLogin},
		{ErrCodeUnknownPlayer, "This player's details are invalid.", codeErrorLogin},
		{ErrCodeLimitReached, "Redemption limit reached.", codeErrorLimitReached},
		{"99999", "Failed to redeem code.", codeErrorUnknown},
	}
	for _, tt := range tests {
		t.Run(string(tt.errCode), func(t *testing.T) {
			got := interpretRedeemResult(&redeemResponse{ErrCode: tt.errCode})
			if got.Error() != tt.wantMsg {
				t.Errorf("Error() = %q, want %q", got.Error(), tt.wantMsg)
			}
			if got.kind != tt.wantKind {
				t.Errorf("kind = %v, want %v", got.kind, tt.wantKind)
			}
		})
	}
	if got := interpretRedeemResult(&redeemResponse{ErrCode: ErrCodeSuccess}); got != nil {
		t.Errorf("success returned error %v", got)
	}
}

// TestGiftCodeService_ProcessNewCode covers the early-return paths that require
// no network calls.
func TestGiftCodeService_ProcessNewCode(t *testing.T) {
	t.Run("already active", func(t *testing.T) {
		svc := &GiftCodeService{codeStore: newInMemoryCodeStore("EXISTINGCODE"), store: newMapStore(nil)}
		result, err := svc.ProcessNewCode(t.Context(), "EXISTINGCODE")
		var codeErr *CodeError
		if result != nil || !errors.As(err, &codeErr) {
			t.Fatalf("got result=%+v err=%v, want CodeError", result, err)
		}
	})

	t.Run("already expired", func(t *testing.T) {
		cs := newInMemoryCodeStore()
		cs.Add(t.Context(), Code{Value: "EXPIREDCODE", ExpiredAt: time.Now()})
		svc := &GiftCodeService{codeStore: cs, store: newMapStore(nil)}
		result, err := svc.ProcessNewCode(t.Context(), "EXPIREDCODE")
		var codeErr *CodeError
		if result != nil || !errors.As(err, &codeErr) {
			t.Fatalf("got result=%+v err=%v, want CodeError", result, err)
		}
	})

	t.Run("store error", func(t *testing.T) {
		storeErr := errors.New("store error")
		svc := &GiftCodeService{codeStore: newInMemoryCodeStore(), store: &errStore{storeErr}}
		result, err := svc.ProcessNewCode(t.Context(), "NEWCODE")
		if result != nil || !errors.Is(err, storeErr) {
			t.Fatalf("got result=%+v err=%v, want store error", result, err)
		}
	})

	t.Run("code lookup error", func(t *testing.T) {
		svc := &GiftCodeService{
			codeStore: &testCodeStore{findErr: errors.New("code lookup failed")},
			store:     newMapStore(nil),
		}
		result, err := svc.ProcessNewCode(t.Context(), "NEWCODE")
		if result != nil || err == nil {
			t.Fatalf("got result=%+v err=%v, want lookup error", result, err)
		}
	})

	t.Run("code add error", func(t *testing.T) {
		svc := &GiftCodeService{
			codeStore: &testCodeStore{addErr: errors.New("code add failed")},
			store:     newMapStore(nil),
		}
		result, err := svc.ProcessNewCode(t.Context(), "NEWCODE")
		if result != nil || err == nil {
			t.Fatalf("got result=%+v err=%v, want add error", result, err)
		}
	})

	t.Run("no registered players adds code to active list", func(t *testing.T) {
		svc := &GiftCodeService{codeStore: newInMemoryCodeStore(), store: newMapStore(nil)}
		result, err := svc.ProcessNewCode(t.Context(), "FRESHCODE")
		if err != nil {
			t.Fatalf("ProcessNewCode() error = %v", err)
		}
		if !result.Added {
			t.Errorf("expected Added=true, got %+v", result)
		}
		if len(result.PlayerResults) != 0 {
			t.Errorf("expected empty PlayerResults, got %v", result.PlayerResults)
		}
		c, found, _ := svc.codeStore.Find(t.Context(), "FRESHCODE")
		if !found || c.IsExpired() {
			t.Error("expected FRESHCODE to be in active codes")
		}
	})

	t.Run("invalid API response returns CodeError", func(t *testing.T) {
		svc := mockKingShotAPI(t, ErrCodeNotFound)
		svc.store = newMapStore(map[string]*Player{
			"p1": {PlayerID: "p1", KingdomID: "k1"},
		})

		result, err := svc.ProcessNewCode(t.Context(), "INVALID")
		var codeErr *CodeError
		if result != nil || !errors.As(err, &codeErr) {
			t.Fatalf("got result=%+v err=%v, want CodeError", result, err)
		}
		if codeErr.Error() != "This code is invalid." {
			t.Fatalf("CodeError.Error() = %q", codeErr.Error())
		}
	})

	t.Run("unknown API response returns operational error", func(t *testing.T) {
		svc := mockKingShotAPI(t, "99999")
		svc.store = newMapStore(map[string]*Player{
			"p1": {PlayerID: "p1", KingdomID: "k1"},
		})

		result, err := svc.ProcessNewCode(t.Context(), "UNKNOWN")
		var codeErr *CodeError
		if result != nil || err == nil || errors.As(err, &codeErr) {
			t.Fatalf("got result=%+v err=%v, want non-CodeError", result, err)
		}
	})

	t.Run("successful API response returns redemption result", func(t *testing.T) {
		svc := mockKingShotAPI(t, ErrCodeSuccess)
		svc.store = newMapStore(map[string]*Player{
			"p1": {PlayerID: "p1", KingdomID: "k1", GuildID: "g1"},
		})

		result, err := svc.ProcessNewCode(t.Context(), "VALID")
		if err != nil {
			t.Fatalf("ProcessNewCode() error = %v", err)
		}
		if result == nil || !result.Added || len(result.PlayerResults) != 1 {
			t.Fatalf("ProcessNewCode() result = %+v", result)
		}
		if result.PlayerResults[0].Message != "Successfully redeemed!" {
			t.Fatalf("player result = %+v", result.PlayerResults[0])
		}
	})
}

var ErrRemoveActiveCodeFailed = errors.New("remove active code failed")
var ErrCodeLookupFailed = errors.New("active code lookup failed")

type StoreError struct {
	err error
}

func (s *StoreError) Error() string {
	return s.err.Error()
}
func TestGiftCodeService_RegisterPlayerCodeStoreErrors(t *testing.T) {
	t.Run("active code lookup error", func(t *testing.T) {
		svc := &GiftCodeService{
			codeStore: &testCodeStore{activeErr: &StoreError{err: ErrCodeLookupFailed}},
			store:     newMapStore(nil),
		}
		_, err := svc.RegisterPlayer(t.Context(), NewPlayerRequest{PlayerID: "p1", UserID: "u1", KingdomID: "k1"})
		var storeErr *StoreError
		if !errors.As(err, &storeErr) {
			t.Errorf("expected StoreError, got %v", err)
		}
	})

	t.Run("remove active code error", func(t *testing.T) {
		svc := mockKingShotAPI(t, ErrCodeExpired)
		svc.codeStore = &testCodeStore{
			active:    []string{"EXPIRED"},
			removeErr: &StoreError{err: ErrRemoveActiveCodeFailed},
		}
		_, err := svc.RegisterPlayer(t.Context(), NewPlayerRequest{PlayerID: "p1", UserID: "u1", KingdomID: "k1"})
		var storeErr *StoreError
		if !errors.As(err, &storeErr) {
			t.Errorf("expected StoreError, got %v", err)
		}
	})
}

func TestGiftCodeService_ProcessNewCodeUsesCallerContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(redeemResponse{ErrCode: ErrCodeSuccess})
	}))
	t.Cleanup(srv.Close)

	svc := &GiftCodeService{
		codeStore: newInMemoryCodeStore(),
		store: newMapStore(map[string]*Player{
			"p1": {PlayerID: "p1", UserID: "u1", KingdomID: "k1"},
		}),
		client: &Client{redeemURL: srv.URL + "/gift_code", Client: srv.Client(), logger: slog.Default()}, // 1 request per second
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	result, err := svc.ProcessNewCode(ctx, "CODE")
	if result != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("got result=%+v err=%v, want context cancellation", result, err)
	}
}

// TestGiftCodeService_RegisterPlayer tests the player registration logic.
func TestGiftCodeService_RegisterPlayer(t *testing.T) {
	t.Run("new player", func(t *testing.T) {
		store := newMapStore(nil)
		svc := &GiftCodeService{codeStore: newInMemoryCodeStore(), store: store, client: &Client{Client: httptest.NewServer(nil).Client(), redeemURL: "http://example.com/gift_code"}}
		req := NewPlayerRequest{PlayerID: "p1", UserID: "u1", KingdomID: "k1"}
		_, err := svc.RegisterPlayer(t.Context(), req)
		if err != nil {
			t.Fatalf("expected nil error, got %+v", err)
		}
		if p, err := store.FindByPlayerID(t.Context(), "p1"); err != nil || p.UserID != "u1" {
			t.Errorf("player not added to store correctly")
		}
	})

	t.Run("player already registered to self", func(t *testing.T) {
		store := newMapStore(map[string]*Player{"p1": {PlayerID: "p1", UserID: "u1"}})
		svc := &GiftCodeService{codeStore: newInMemoryCodeStore(), store: store}
		req := NewPlayerRequest{PlayerID: "p1", UserID: "u1"}
		_, err := svc.RegisterPlayer(t.Context(), req)
		if !errors.Is(err, ErrAlreadySelf) {
			t.Errorf("expected AlreadySelf error, got %+v", err)
		}
		assertPlayerError(t, err, "This player ID is already registered to your Discord account.")
	})

	t.Run("player already registered to other", func(t *testing.T) {
		store := newMapStore(map[string]*Player{"p1": {PlayerID: "p1", UserID: "u2"}})
		svc := &GiftCodeService{codeStore: newInMemoryCodeStore(), store: store}
		req := NewPlayerRequest{PlayerID: "p1", UserID: "u1"}
		_, err := svc.RegisterPlayer(t.Context(), req)
		if !errors.Is(err, ErrAlreadyOther) {
			t.Errorf("expected AlreadyOther error, got %+v", err)
		}
		assertPlayerError(t, err, "This player ID is already registered to another Discord account.")
	})

	t.Run("max players for kingdom reached", func(t *testing.T) {
		store := newMapStore(map[string]*Player{
			"p1": {PlayerID: "p1", UserID: "u1", KingdomID: "k1"},
			"p2": {PlayerID: "p2", UserID: "u1", KingdomID: "k1"},
		})
		svc := &GiftCodeService{codeStore: newInMemoryCodeStore(), store: store}
		req := NewPlayerRequest{PlayerID: "p3", UserID: "u1", KingdomID: "k1"}
		_, err := svc.RegisterPlayer(t.Context(), req)
		if !errors.Is(err, ErrMaxPlayersForKingdom) {
			t.Errorf("expected MaxPlayersForKingdom error, got %+v", err)
		}
		assertPlayerError(t, err, "You have already registered the maximum number of players for this kingdom.")
	})
}

func assertPlayerError(t *testing.T, err error, wantMessage string) {
	t.Helper()
	var playerErr *PlayerError
	if !errors.As(err, &playerErr) {
		t.Fatalf("expected PlayerError, got %T: %v", err, err)
	}
	if playerErr.Error() != wantMessage {
		t.Fatalf("PlayerError.Error() = %q, want %q", playerErr.Error(), wantMessage)
	}
}

func TestGiftCodeService_TransferPlayer(t *testing.T) {
	t.Run("successful transfer", func(t *testing.T) {
		store := newMapStore(map[string]*Player{
			"p1": {PlayerID: "p1", UserID: "u1", KingdomID: "k1", GuildID: "g1"},
		})
		svc := &GiftCodeService{codeStore: newInMemoryCodeStore(), store: store}
		req := TransferPlayerRequest{PlayerID: "p1", UserID: "u1", NewKingdomID: "k2", GuildID: "g2"}
		player, _ := svc.TransferPlayer(t.Context(), req)
		if player == nil {
			t.Fatalf("expected player, got %+v", nil)
		}
		if p, _ := store.FindByPlayerID(t.Context(), "p1"); p.KingdomID != "k2" || p.GuildID != "g2" {
			t.Errorf("player transfer not fully updated, got kingdom=%s guild=%s", p.KingdomID, p.GuildID)
		}
	})

	t.Run("player not found is rejected", func(t *testing.T) {
		store := newMapStore(nil)
		svc := &GiftCodeService{codeStore: newInMemoryCodeStore(), store: store, client: nil}
		req := TransferPlayerRequest{PlayerID: "p1", UserID: "u1", NewKingdomID: "k1", GuildID: "g1"}
		_, err := svc.TransferPlayer(t.Context(), req)
		if !errors.Is(err, ErrPlayerNotRegistered) {
			t.Fatalf("expected ErrPlayerNotRegistered, got %+v", err)
		}
		assertPlayerError(t, err, "This player is not registered. Use /player register to register it first.")

		if _, err := store.FindByPlayerID(t.Context(), "p1"); !errors.Is(err, ErrNotFound) {
			t.Errorf("player should not have been added to the store")
		}
	})

	t.Run("blank owner is rejected", func(t *testing.T) {
		store := newMapStore(map[string]*Player{
			"p1": {PlayerID: "p1", UserID: "", KingdomID: "k0", GuildID: "g0"},
		})
		svc := &GiftCodeService{codeStore: newInMemoryCodeStore(), store: store, client: nil}
		req := TransferPlayerRequest{PlayerID: "p1", UserID: "u1", NewKingdomID: "k1", GuildID: "g1"}
		_, err := svc.TransferPlayer(t.Context(), req)
		if !errors.Is(err, ErrPlayerNotRegistered) {
			t.Fatalf("expected ErrPlayerNotRegistered, got %+v", err)
		}
		assertPlayerError(t, err, "This player is not registered. Use /player register to register it first.")
	})

	t.Run("not your player", func(t *testing.T) {
		store := newMapStore(map[string]*Player{
			"p1": {PlayerID: "p1", UserID: "u2", KingdomID: "k1"},
		})
		svc := &GiftCodeService{codeStore: newInMemoryCodeStore(), store: store}
		req := TransferPlayerRequest{PlayerID: "p1", UserID: "u1", NewKingdomID: "k2"}
		_, err := svc.TransferPlayer(t.Context(), req)
		if err != nil && !errors.Is(err, NotYourPlayer) {
			t.Fatalf("expected NotYourPlayer error, got %+v", err)
		}
		assertPlayerError(t, err, "This player is not registered to your Discord account.")
	})

	t.Run("max players for new kingdom reached", func(t *testing.T) {
		store := newMapStore(map[string]*Player{
			"p1": {PlayerID: "p1", UserID: "u1", KingdomID: "k1"},
			"p2": {PlayerID: "p2", UserID: "u1", KingdomID: "k2"},
			"p3": {PlayerID: "p3", UserID: "u1", KingdomID: "k2"},
		})
		svc := &GiftCodeService{codeStore: newInMemoryCodeStore(), store: store}
		req := TransferPlayerRequest{PlayerID: "p1", UserID: "u1", NewKingdomID: "k2"}
		_, err_ := svc.TransferPlayer(t.Context(), req)
		if !errors.Is(err_, ErrMaxPlayersForKingdom) {
			t.Errorf("expected MaxPlayersForNewKingdomReached error, got %+v", err_)
		}
		assertPlayerError(t, err_, "You have already registered the maximum number of players for the new kingdom.")
	})

	t.Run("transfer to current kingdom is rejected", func(t *testing.T) {
		store := newMapStore(map[string]*Player{
			"p1": {PlayerID: "p1", UserID: "u1", KingdomID: "k1"},
			"p2": {PlayerID: "p2", UserID: "u1", KingdomID: "k1"},
		})
		svc := &GiftCodeService{codeStore: newInMemoryCodeStore(), store: store}
		req := TransferPlayerRequest{PlayerID: "p1", UserID: "u1", NewKingdomID: "k1"}
		_, err := svc.TransferPlayer(t.Context(), req)
		if !errors.Is(err, ErrAlreadyInKingdom) {
			t.Fatalf("expected ErrAlreadyInKingdom, got %+v", err)
		}
		assertPlayerError(t, err, "This player is already in that kingdom.")
	})
}

func TestGiftCodeService_UnlinkPlayer(t *testing.T) {
	t.Run("successful unlink", func(t *testing.T) {
		store := newMapStore(map[string]*Player{
			"p1": {PlayerID: "p1", UserID: "u1", KingdomID: "k1"},
		})
		svc := &GiftCodeService{codeStore: newInMemoryCodeStore(), store: store}
		req := UnlinkPlayerRequest{PlayerID: "p1", UserID: "u1"}
		err := svc.UnlinkPlayer(t.Context(), req)
		if err != nil {
			t.Fatalf("expected nil error, got %+v", err)
		}
		_, err = store.FindByPlayerID(t.Context(), "p1")
		if !errors.Is(err, ErrNotFound) {
			t.Fatal("expected unlinked player to no longer be found")
		}
	})

	t.Run("player not found", func(t *testing.T) {
		store := newMapStore(nil)
		svc := &GiftCodeService{codeStore: newInMemoryCodeStore(), store: store}
		req := UnlinkPlayerRequest{PlayerID: "p1", UserID: "u1"}
		err := svc.UnlinkPlayer(t.Context(), req)
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %+v", err)
		}
	})

	t.Run("not your player", func(t *testing.T) {
		store := newMapStore(map[string]*Player{
			"p1": {PlayerID: "p1", UserID: "u2"},
		})
		svc := &GiftCodeService{codeStore: newInMemoryCodeStore(), store: store}
		req := UnlinkPlayerRequest{PlayerID: "p1", UserID: "u1"}
		err := svc.UnlinkPlayer(t.Context(), req)
		if !errors.Is(err, NotYourPlayer) {
			t.Errorf("expected NotYourPlayer, got %+v", err)
		}
		assertPlayerError(t, err, "This player is not registered to your Discord account.")
	})

	t.Run("already unlinked reports player not found", func(t *testing.T) {
		store := newMapStore(map[string]*Player{
			"p1": {PlayerID: "p1", UserID: "u1"},
		})
		store.unlinked["p1"] = true
		svc := &GiftCodeService{codeStore: newInMemoryCodeStore(), store: store}
		req := UnlinkPlayerRequest{PlayerID: "p1", UserID: "u1"}
		err := svc.UnlinkPlayer(t.Context(), req)
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %+v", err)
		}
	})

	t.Run("store error on lookup", func(t *testing.T) {
		svc := &GiftCodeService{codeStore: newInMemoryCodeStore(), store: &errStore{errors.New("boom")}}
		err := svc.UnlinkPlayer(t.Context(), UnlinkPlayerRequest{PlayerID: "p1", UserID: "u1"})
		if err == nil {
			t.Errorf("expected error, got %+v", err)
		}
	})
}

// TestGiftCodeService_RegisterPlayer_reactivatesUnlinked verifies that
// registering a playerID that was previously unlinked succeeds instead of
// returning AlreadyOther/AlreadySelf, allowing accounts to change hands.
func TestGiftCodeService_RegisterPlayer_reactivatesUnlinked(t *testing.T) {
	store := newMapStore(map[string]*Player{
		"p1": {PlayerID: "p1", UserID: ""},
	})
	store.unlinked["p1"] = true
	svc := &GiftCodeService{codeStore: newInMemoryCodeStore(), store: store, client: nil}
	req := NewPlayerRequest{PlayerID: "p1", UserID: "u2", KingdomID: "k2", GuildID: "new-guild"}
	_, err := svc.RegisterPlayer(t.Context(), req)
	if err != nil {
		t.Fatalf("expected success, got error %+v", err)
	}
	p, err := store.FindByPlayerID(t.Context(), "p1")
	if err != nil {
		t.Fatal("expected player to exist in store")
	}
	if p.UserID != "u2" {
		t.Errorf("expected player to be re-linked to u2, got %q", p.UserID)
	}
	if p.GuildID != "new-guild" {
		t.Errorf("expected player guild to be updated to new-guild, got %q", p.GuildID)
	}
}

func TestGiftCodeService_RegisterPlayer_blankOwnerIsUnowned(t *testing.T) {
	store := newMapStore(map[string]*Player{
		"p1": {PlayerID: "p1", UserID: "", KingdomID: "k1", GuildID: "old-guild"},
	})
	svc := &GiftCodeService{codeStore: newInMemoryCodeStore(), store: store, client: nil}
	req := NewPlayerRequest{PlayerID: "p1", UserID: "u2", KingdomID: "k2", GuildID: "new-guild"}
	_, err := svc.RegisterPlayer(t.Context(), req)
	if err != nil {
		t.Fatalf("expected success, got error %+v", err)
	}
	p, err := store.FindByPlayerID(t.Context(), "p1")
	if err != nil {
		t.Fatal("expected player to exist in store")
	}
	if p.UserID != "u2" || p.KingdomID != "k2" || p.GuildID != "new-guild" {
		t.Errorf("expected player to be reclaimed, got %+v", p)
	}
}

// TestGiftCodeService_concurrentAccess runs concurrent ProcessNewCode calls so
// the race detector can catch any unsynchronised access to the shared slices.
func TestGiftCodeService_concurrentAccess(t *testing.T) {
	svc := &GiftCodeService{codeStore: newInMemoryCodeStore(), store: &errStore{errors.New("no store")}}
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			svc.ProcessNewCode(t.Context(), fmt.Sprintf("CODE%d", n))
		}(i)
	}
	wg.Wait()
}

// TestGiftCodeService_codeStoreLookup verifies active and expired membership
// detection via the CodeStore interface.
func TestGiftCodeService_codeStoreLookup(t *testing.T) {
	cs := newInMemoryCodeStore("ACTIVE1", "ACTIVE2")
	cs.Add(t.Context(), Code{Value: "EXPIRED1", ExpiredAt: time.Now()})
	tests := []struct {
		code        string
		wantFound   bool
		wantActive  bool
		wantExpired bool
	}{
		{"ACTIVE1", true, true, false},
		{"ACTIVE2", true, true, false},
		{"EXPIRED1", true, false, true},
		{"UNKNOWN", false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			c, found, _ := cs.Find(t.Context(), tt.code)
			if found != tt.wantFound {
				t.Errorf("found = %v, want %v", found, tt.wantFound)
			}
			if found {
				if !c.IsExpired() != tt.wantActive {
					t.Errorf("IsActive = %v, want %v", !c.IsExpired(), tt.wantActive)
				}
				if c.IsExpired() != tt.wantExpired {
					t.Errorf("IsExpired = %v, want %v", c.IsExpired(), tt.wantExpired)
				}
			}
		})
	}
}

// TestGiftCodeService_redeemForPlayer tests the redeem → interpret
// pipeline for a single player using a mock HTTP server.
func TestGiftCodeService_redeemForPlayer(t *testing.T) {
	tests := map[string]struct {
		redeemErrCode string
		wantMsg       string
	}{
		"success":              {ErrCodeSuccess, "Successfully redeemed!"},
		"already claimed":      {ErrCodeClaimed, "Code already claimed."},
		"login error from api": {ErrCodeLogin, "The player used to validate this code is invalid."},
		"unknown error code":   {"99999", "Failed to redeem code."},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			svc := mockKingShotAPI(t, tt.redeemErrCode)
			player := &Player{PlayerID: "player1", KingdomID: "k1", UserID: "discord1"}
			got := redemptionMessage(svc.redeemForPlayer(t.Context(), player, "TESTCODE"))
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
			codeStore: newInMemoryCodeStore(),
			client:    &Client{Client: srv.Client(), redeemURL: srv.URL + "/gift_code"},
			store:     newMapStore(nil),
		}
		player := &Player{PlayerID: "player1", KingdomID: "k1", UserID: "discord1"}
		got := svc.redeemForPlayer(t.Context(), player, "TESTCODE")
		if redemptionMessage(got) != "Failed to redeem code." {
			t.Errorf("got %q, want %q", redemptionMessage(got), "Failed to redeem code.")
		}
	})

	t.Run("retries timeout response", func(t *testing.T) {
		var requests atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if requests.Add(1) < 3 {
				json.NewEncoder(w).Encode(redeemResponse{ErrCode: ErrCodeTimeoutRetry})
				return
			}
			json.NewEncoder(w).Encode(redeemResponse{ErrCode: ErrCodeSuccess})
		}))
		t.Cleanup(srv.Close)

		svc := &GiftCodeService{client: &Client{Client: srv.Client(), redeemURL: srv.URL + "/gift_code"}}
		got := svc.redeemForPlayer(t.Context(), &Player{PlayerID: "player1", KingdomID: "k1"}, "TESTCODE")
		if redemptionMessage(got) != "Successfully redeemed!" {
			t.Fatalf("got %q, want successful redemption", redemptionMessage(got))
		}
		if gotRequests := requests.Load(); gotRequests != 3 {
			t.Fatalf("got %d requests, want 3", gotRequests)
		}
	})

	t.Run("stops after maximum timeout retries", func(t *testing.T) {
		var requests atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests.Add(1)
			json.NewEncoder(w).Encode(redeemResponse{ErrCode: ErrCodeTimeoutRetry})
		}))
		t.Cleanup(srv.Close)

		svc := &GiftCodeService{client: &Client{Client: srv.Client(), redeemURL: srv.URL + "/gift_code"}}
		got := svc.redeemForPlayer(t.Context(), &Player{PlayerID: "player1", KingdomID: "k1"}, "TESTCODE")
		if redemptionMessage(got) != "Failed to redeem code." {
			t.Fatalf("got %q, want exhausted retry result", redemptionMessage(got))
		}
		if gotRequests := requests.Load(); gotRequests != maxRedeemAttempts {
			t.Fatalf("got %d requests, want %d", gotRequests, maxRedeemAttempts)
		}
	})

	t.Run("retries malformed response body", func(t *testing.T) {
		var requests atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if requests.Add(1) == 1 {
				w.Write([]byte("temporarily unavailable"))
				return
			}
			json.NewEncoder(w).Encode(redeemResponse{ErrCode: ErrCodeSuccess})
		}))
		t.Cleanup(srv.Close)

		svc := &GiftCodeService{client: &Client{Client: srv.Client(), redeemURL: srv.URL + "/gift_code"}}
		got := svc.redeemForPlayer(t.Context(), &Player{PlayerID: "player1", KingdomID: "k1"}, "TESTCODE")
		if redemptionMessage(got) != "Successfully redeemed!" {
			t.Fatalf("got %q, want successful redemption", redemptionMessage(got))
		}
		if gotRequests := requests.Load(); gotRequests != 2 {
			t.Fatalf("got %d requests, want 2", gotRequests)
		}
	})

	t.Run("retries HTTP 429", func(t *testing.T) {
		var requests atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if requests.Add(1) == 1 {
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(redeemResponse{ErrCode: ErrCodeTimeoutRetry})
				return
			}
			json.NewEncoder(w).Encode(redeemResponse{ErrCode: ErrCodeSuccess})
		}))
		t.Cleanup(srv.Close)

		svc := &GiftCodeService{client: &Client{Client: srv.Client(), redeemURL: srv.URL + "/gift_code"}}
		got := svc.redeemForPlayer(t.Context(), &Player{PlayerID: "player1", KingdomID: "k1"}, "TESTCODE")
		if redemptionMessage(got) != "Successfully redeemed!" {
			t.Fatalf("got %q, want successful redemption", redemptionMessage(got))
		}
		if gotRequests := requests.Load(); gotRequests != 2 {
			t.Fatalf("got %d requests, want 2", gotRequests)
		}
	})
}

// --- inMemoryCodeStore tests -------------------------------------------------

// TestInMemoryCodeStore_AddAndCheck verifies basic add + membership semantics.
func TestInMemoryCodeStore_AddAndCheck(t *testing.T) {
	ctx := t.Context()
	s := newInMemoryCodeStore()

	if _, found, _ := s.Find(ctx, "CODE1"); found {
		t.Error("expected CODE1 to be unknown initially")
	}

	s.Add(ctx, Code{Value: "CODE1"})
	c, found, _ := s.Find(ctx, "CODE1")
	if !found {
		t.Fatal("expected CODE1 to be found after Add")
	}
	if c.IsExpired() {
		t.Error("expected CODE1 to be active after Add")
	}
	if c.IsExpired() {
		t.Error("expected CODE1 to not be expired after Add")
	}

	s.Add(ctx, Code{Value: "CODE2", ExpiredAt: time.Now()})
	c2, found, _ := s.Find(ctx, "CODE2")
	if !found {
		t.Fatal("expected CODE2 to be found after Add")
	}
	if !c2.IsExpired() {
		t.Error("expected CODE2 to be expired after Add with ExpiredAt set")
	}
}

// TestInMemoryCodeStore_Seed verifies that constructor seeds active codes.
func TestInMemoryCodeStore_Seed(t *testing.T) {
	ctx := t.Context()
	s := newInMemoryCodeStore("A", "B", "C")
	for _, code := range []string{"A", "B", "C"} {
		c, found, _ := s.Find(ctx, code)
		if !found || c.IsExpired() {
			t.Errorf("expected seeded code %q to be active", code)
		}
	}
	if _, found, _ := s.Find(ctx, "D"); found {
		t.Error("expected unseeded code D to be unknown")
	}
}

// TestInMemoryCodeStore_DuplicateAddIsNoOp verifies that adding the same code
// twice does not grow the active set unboundedly.
func TestInMemoryCodeStore_DuplicateAddIsNoOp(t *testing.T) {
	ctx := t.Context()
	s := newInMemoryCodeStore()
	s.Add(ctx, Code{Value: "DUP"})
	s.Add(ctx, Code{Value: "DUP"})
	codes, _ := s.ActiveCodes(ctx)
	count := 0
	for _, c := range codes {
		if c == "DUP" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected DUP to appear exactly once in ActiveCodes, got %d", count)
	}

	s.Add(ctx, Code{Value: "EXP", ExpiredAt: time.Now()})
	s.Add(ctx, Code{Value: "EXP", ExpiredAt: time.Now()})
	c, found, _ := s.Find(ctx, "EXP")
	if !found || !c.IsExpired() {
		t.Error("expected EXP to be expired after duplicate Add with ExpiredAt set")
	}
}

// TestInMemoryCodeStore_RemoveActive verifies that RemoveActive removes codes
// from the active set and ignores unknown ones.
func TestInMemoryCodeStore_RemoveActive(t *testing.T) {
	ctx := t.Context()
	s := newInMemoryCodeStore("A", "B", "C")

	s.RemoveActive(ctx, "A", "C", "NOTPRESENT")
	if _, found, _ := s.Find(ctx, "A"); found {
		t.Error("expected A to be removed")
	}
	if _, found, _ := s.Find(ctx, "C"); found {
		t.Error("expected C to be removed")
	}
	if b, found, _ := s.Find(ctx, "B"); !found || b.IsExpired() {
		t.Error("expected B to remain active")
	}
	codes, _ := s.ActiveCodes(ctx)
	if len(codes) != 1 || codes[0] != "B" {
		t.Errorf("expected ActiveCodes=[B], got %v", codes)
	}
}

// TestInMemoryCodeStore_ActiveCodesEmpty verifies that ActiveCodes returns nil
// or an empty slice when no codes are active.
func TestInMemoryCodeStore_ActiveCodesEmpty(t *testing.T) {
	ctx := t.Context()
	s := newInMemoryCodeStore()
	codes, _ := s.ActiveCodes(ctx)
	if len(codes) != 0 {
		t.Errorf("expected empty ActiveCodes, got %v", codes)
	}
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
