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
	redeemURL    string
}

// New returns a GiftCodeService ready for use. Any activeCodes provided are
// pre-loaded as already-active codes.
func New(store PlayerStore, activeCodes ...string) *GiftCodeService {
	return &GiftCodeService{
		store:       store,
		activeCodes: activeCodes,
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

	players, err := s.store.Players()
	if err != nil {
		return CodeResult{Code: code, StoreError: err}
	}

	if len(players) == 0 {
		s.activeCodes = append(s.activeCodes, code)
		slog.Info("code added with no registered players", "code", code)
		return CodeResult{Code: code, Added: true}
	}

	firstPlayer := players[0]
	redeemResp, err := s.redeemGiftCode(firstPlayer.PlayerID, firstPlayer.KingdomID, code)
	if err != nil {
		slog.Error("failed to validate new code", "error", err, "code", code)
		return CodeResult{Code: code, APIError: err}
	}

	slog.Info("redeem response", "code", code, "err_code", redeemResp.ErrCode, "player_id", firstPlayer.PlayerID)

	outcome := interpretRedeemResult(redeemResp.ErrCode)
	if outcome.codeExpired {
		s.expiredCodes = append(s.expiredCodes, code)
		return CodeResult{Code: code, AlreadyExpired: true}
	}
	if outcome.codeInvalid {
		return CodeResult{Code: code, Invalid: true}
	}
	if outcome.loginFailed {
		// This error code is now repurposed to mean the player is invalid
		return CodeResult{Code: code, InvalidPlayer: true}
	}

	s.activeCodes = append(s.activeCodes, code)
	slog.Info("code added", "code", code)

	results := make([]PlayerRedeemResult, 0, len(players))
	results = append(results, PlayerRedeemResult{PlayerID: firstPlayer.PlayerID, Message: outcome.msg})
	for _, player := range players[1:] {
		results = append(results, PlayerRedeemResult{
			PlayerID: player.PlayerID,
			Message:  s.redeemForPlayer(player, code),
		})
	}

	return CodeResult{Code: code, Added: true, PlayerResults: results}
}

// NewPlayerRequest is the set of parameters for registering a new player.
type NewPlayerRequest struct {
	PlayerID, UserID, KingdomID, GuildID string
}

// RegisterPlayer validates playerID via the KingShot API, registers it with
// UserID in the store, and redeems any currently active codes for the
// new player. It is safe to call concurrently.
func (s *GiftCodeService) RegisterPlayer(req NewPlayerRequest) RegisterResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.registerPlayer(req)
}

func (s *GiftCodeService) registerPlayer(req NewPlayerRequest) RegisterResult {
	existing, found, err := s.store.FindByPlayerID(req.PlayerID)
	if err != nil {
		slog.Error("failed to look up player for registration", "error", err)
		return RegisterResult{StoreError: err}
	}
	if found {
		if existing.UserID == req.UserID {
			return RegisterResult{AlreadySelf: true}
		}
		return RegisterResult{AlreadyOther: true}
	}

	return s.addNewPlayer(req)
}

// addNewPlayer stores req in the store and redeems active codes for the new player.
// Callers must have already verified that the player does not exist.
// Caller must hold s.mu.
func (s *GiftCodeService) addNewPlayer(req NewPlayerRequest) RegisterResult {
	userPlayers, err := s.store.FindByUser(req.UserID)
	if err != nil {
		slog.Error("failed to look up players by user for registration", "error", err)
		return RegisterResult{StoreError: err}
	}

	kingdomPlayerCount := 0
	for _, p := range userPlayers {
		if p.KingdomID == req.KingdomID {
			kingdomPlayerCount++
		}
	}

	if kingdomPlayerCount >= 2 {
		return RegisterResult{MaxPlayersForKingdomReached: true}
	}

	if err := s.store.AddPlayer(req); err != nil {
		slog.Error("failed to add player", "error", err)
		return RegisterResult{StoreError: err}
	}

	player := &Player{
		PlayerID:  req.PlayerID,
		UserID:    req.UserID,
		KingdomID: req.KingdomID,
	}

	slog.Info("user subscribed to bot", "player_id", req.PlayerID, "user_id", req.UserID)

	codeResults := s.redeemActiveCodes(player)
	return RegisterResult{
		PlayerID:    req.PlayerID,
		UserID:      req.UserID,
		Success:     true,
		CodeResults: codeResults,
	}
}

func (s *GiftCodeService) TransferPlayer(req TransferPlayerRequest) TransferPlayerResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	player, found, err := s.store.FindByPlayerID(req.PlayerID)
	if err != nil {
		slog.Error("failed to look up player for transfer", "error", err)
		return TransferPlayerResult{StoreError: err}
	}

	if !found {
		// Player doesn't exist, so let's register them instead.
		registerReq := NewPlayerRequest{
			PlayerID:  req.PlayerID,
			KingdomID: req.NewKingdomID,
			UserID:    req.UserID,
			GuildID:   req.GuildID,
		}
		regResult := s.addNewPlayer(registerReq)
		return TransferPlayerResult{
			PlayerNotFound:     true,
			RegistrationResult: &regResult,
		}
	}

	if player.UserID != req.UserID {
		return TransferPlayerResult{NotYourPlayer: true}
	}

	// Check if the new kingdom has space
	userPlayers, err := s.store.FindByUser(req.UserID)
	if err != nil {
		slog.Error("failed to look up players by user for transfer", "error", err)
		return TransferPlayerResult{StoreError: err}
	}

	kingdomPlayerCount := 0
	for _, p := range userPlayers {
		// an existing registration for this player should not count towards the limit
		if p.KingdomID == req.NewKingdomID && p.PlayerID != req.PlayerID {
			kingdomPlayerCount++
		}
	}

	if kingdomPlayerCount >= 2 {
		return TransferPlayerResult{MaxPlayersForNewKingdomReached: true}
	}

	if err := s.store.UpdatePlayerKingdom(UpdatePlayerKingdomRequest{
		PlayerID:     req.PlayerID,
		NewKingdomID: req.NewKingdomID,
		GuildID:      req.GuildID,
	}); err != nil {
		slog.Error("failed to update player kingdom", "error", err)
		return TransferPlayerResult{StoreError: err}
	}

	return TransferPlayerResult{
		PlayerID:     req.PlayerID,
		NewKingdomID: req.NewKingdomID,
		UserID:       req.UserID,
		Success:      true,
	}
}

func (s *GiftCodeService) GetPlayersByUser(userID string) ([]*Player, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.store.FindByUser(userID)
}

// isCodeKnown reports whether code is already tracked. Caller must hold s.mu.
func (s *GiftCodeService) isCodeKnown(code string) (active, expired bool) {
	return slices.Contains(s.activeCodes, code), slices.Contains(s.expiredCodes, code)
}

// redeemForPlayer logs playerID in, redeems code, and returns a human-readable result.
func (s *GiftCodeService) redeemForPlayer(player *Player, code string) string {
	resp, err := s.redeemGiftCode(player.PlayerID, player.KingdomID, code)
	if err != nil {
		slog.Error("failed to redeem", "error", err, "player_id", player.PlayerID, "code", code)
		return "Error redeeming code."
	}
	slog.Info("redeem response", "player_id", player.PlayerID, "code", code, "err_code", resp.ErrCode, "message", resp.Message)
	return interpretRedeemResult(resp.ErrCode).msg
}

// redeemActiveCodes redeems all currently active codes for playerID and returns
// a slice of per-code results. Caller must hold s.mu.
func (s *GiftCodeService) redeemActiveCodes(player *Player) []ActiveCodeResult {
	if len(s.activeCodes) == 0 {
		return nil
	}

	var results []ActiveCodeResult
	var codesToRemove []string

	for _, code := range s.activeCodes {
		redeemResp, err := s.redeemGiftCode(player.PlayerID, player.KingdomID, code)
		if err != nil {
			slog.Error("failed to redeem gift code after registration", "error", err, "code", code, "player_id", player.PlayerID)
			results = append(results, ActiveCodeResult{Code: code, Message: "Error redeeming code."})
			continue
		}
		slog.Info("redeem response", "code", code, "err_code", redeemResp.ErrCode, "player_id", player.PlayerID)

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
