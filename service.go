package kingshot

import (
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// GiftCodeService manages gift code state and interacts with the KingShot API.
// All mutable state is owned here; no package-level globals.
type GiftCodeService struct {
	mu           sync.Mutex
	activeCodes  []string
	expiredCodes []string
	store        PlayerStore
	client       *http.Client
	loginURL     string
	redeemURL    string
}

// New returns a GiftCodeService ready for use. Any activeCodes provided are
// pre-loaded as already-active codes.
func New(store PlayerStore, activeCodes ...string) *GiftCodeService {
	return &GiftCodeService{
		store:       store,
		activeCodes: activeCodes,
		loginURL:    defaultLoginURL,
		redeemURL:   defaultRedeemURL,
		client: &http.Client{
			Transport: &transport{
				limiter: rate.NewLimiter(rate.Every(2*time.Second), 1),
			},
		},
	}
}

// ProcessNewCode validates code against the KingShot API and redeems it for
// all registered players. It is safe to call concurrently.
func (s *GiftCodeService) ProcessNewCode(code string) CodeResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	if active, expired := s.isCodeKnown(code); active {
		return CodeResult{Code: code, AlreadyActive: true}
	} else if expired {
		return CodeResult{Code: code, AlreadyExpired: true}
	}

	playerIDs, err := s.store.PlayerIDs()
	if err != nil {
		return CodeResult{Code: code, StoreError: err}
	}

	if len(playerIDs) == 0 {
		s.activeCodes = append(s.activeCodes, code)
		slog.Info("code added with no registered players", "code", code)
		return CodeResult{Code: code, Added: true}
	}

	firstPlayerID := playerIDs[0]
	if _, err := s.login(firstPlayerID); err != nil {
		slog.Error("failed to login before validating new code", "error", err)
		return CodeResult{Code: code, APIError: err}
	}

	redeemResp, err := s.redeemGiftCode(firstPlayerID, code)
	if err != nil {
		slog.Error("failed to validate new code", "error", err, "code", code)
		return CodeResult{Code: code, APIError: err}
	}

	slog.Info("redeem response", "code", code, "err_code", redeemResp.ErrCode, "player_id", firstPlayerID)

	outcome := interpretRedeemResult(redeemResp.ErrCode)
	if outcome.codeExpired {
		s.expiredCodes = append(s.expiredCodes, code)
		return CodeResult{Code: code, AlreadyExpired: true}
	}
	if outcome.codeInvalid {
		return CodeResult{Code: code, Invalid: true}
	}
	if outcome.loginFailed {
		return CodeResult{Code: code, LoginFailed: true}
	}

	s.activeCodes = append(s.activeCodes, code)
	slog.Info("code added", "code", code)

	results := make([]PlayerRedeemResult, 0, len(playerIDs))
	results = append(results, PlayerRedeemResult{PlayerID: firstPlayerID, Message: outcome.msg})
	for _, playerID := range playerIDs[1:] {
		results = append(results, PlayerRedeemResult{
			PlayerID: playerID,
			Message:  s.redeemForPlayer(playerID, code),
		})
	}

	return CodeResult{Code: code, Added: true, PlayerResults: results}
}

// RegisterPlayer validates playerID via the KingShot API, registers it with
// externalID in the store, and redeems any currently active codes for the
// new player. It is safe to call concurrently.
func (s *GiftCodeService) RegisterPlayer(playerID, externalID string) RegisterResult {
	loginResp, err := s.login(playerID)
	if err != nil {
		slog.Error("failed to call login endpoint", "error", err)
		return RegisterResult{APIError: err}
	}
	if loginResp.Code != 0 {
		slog.Info("invalid player id", "player_id", playerID, "response_code", loginResp.Code)
		return RegisterResult{InvalidPlayer: true}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	existing, found, err := s.store.FindByPlayerID(playerID)
	if err != nil {
		slog.Error("failed to look up player for registration", "error", err)
		return RegisterResult{StoreError: err}
	}
	if found {
		if existing == externalID {
			return RegisterResult{AlreadySelf: true}
		}
		return RegisterResult{AlreadyOther: true}
	}

	if err := s.store.AddPlayer(playerID, externalID); err != nil {
		slog.Error("failed to add player", "error", err)
		return RegisterResult{StoreError: err}
	}

	slog.Info("user subscribed to bot", "player_id", playerID, "external_id", externalID)

	codeResults := s.redeemActiveCodes(playerID)
	return RegisterResult{
		PlayerID:    playerID,
		ExternalID:  externalID,
		Success:     true,
		CodeResults: codeResults,
	}
}

// isCodeKnown reports whether code is already tracked. Caller must hold s.mu.
func (s *GiftCodeService) isCodeKnown(code string) (active, expired bool) {
	return slices.Contains(s.activeCodes, code), slices.Contains(s.expiredCodes, code)
}

// redeemForPlayer logs playerID in, redeems code, and returns a human-readable result.
func (s *GiftCodeService) redeemForPlayer(playerID, code string) string {
	if _, err := s.login(playerID); err != nil {
		slog.Error("failed to login", "error", err, "player_id", playerID)
		return "Failed to login."
	}
	resp, err := s.redeemGiftCode(playerID, code)
	if err != nil {
		slog.Error("failed to redeem", "error", err, "player_id", playerID, "code", code)
		return "Error redeeming code."
	}
	slog.Info("redeem response", "player_id", playerID, "code", code, "err_code", resp.ErrCode)
	return interpretRedeemResult(resp.ErrCode).msg
}

// redeemActiveCodes redeems all currently active codes for playerID and returns
// a slice of per-code results. Caller must hold s.mu.
func (s *GiftCodeService) redeemActiveCodes(playerID string) []ActiveCodeResult {
	if len(s.activeCodes) == 0 {
		return nil
	}

	var results []ActiveCodeResult
	var codesToRemove []string

	for _, code := range s.activeCodes {
		redeemResp, err := s.redeemGiftCode(playerID, code)
		if err != nil {
			slog.Error("failed to redeem gift code after registration", "error", err, "code", code, "player_id", playerID)
			results = append(results, ActiveCodeResult{Code: code, Message: "Error redeeming code."})
			continue
		}
		slog.Info("redeem response", "code", code, "err_code", redeemResp.ErrCode, "player_id", playerID)

		outcome := interpretRedeemResult(redeemResp.ErrCode)
		if outcome.codeExpired || outcome.codeInvalid {
			codesToRemove = append(codesToRemove, code)
		}
		results = append(results, ActiveCodeResult{Code: code, Message: outcome.msg})
	}

	if len(codesToRemove) > 0 {
		s.activeCodes = slices.DeleteFunc(s.activeCodes, func(c string) bool {
			return slices.Contains(codesToRemove, c)
		})
	}

	return results
}
