package kingshot

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// GiftCodeService manages gift code state and interacts with the KingShot API.
// All mutable state is owned here; no package-level globals.
type GiftCodeService struct {
	mu        sync.Mutex
	codeStore CodeStore
	store     PlayerStore
	client    *http.Client
	redeemURL string
}

// New returns a GiftCodeService ready for use. Any activeCodes provided are
// pre-loaded as already-active codes.
func New(store PlayerStore, activeCodes ...string) *GiftCodeService {
	return &GiftCodeService{
		store:     store,
		codeStore: newInMemoryCodeStore(activeCodes...),
		redeemURL: defaultRedeemURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &transport{
				limiter: rate.NewLimiter(rate.Every(2*time.Second), 1),
			},
		},
	}
}

// ProcessNewCode validates code against the KingShot API and redeems it for
// all registered players. It is safe to call concurrently. ctx bounds all
// store and HTTP calls made while processing code.
func (s *GiftCodeService) ProcessNewCode(ctx context.Context, code string) CodeResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	if c, found := s.codeStore.Find(ctx, code); found {
		if c.IsActive() {
			return CodeResult{Code: code, AlreadyActive: true}
		}
		return CodeResult{Code: code, AlreadyExpired: true}
	}

	players, err := s.store.Players(ctx)
	if err != nil {
		return CodeResult{Code: code, StoreError: err}
	}

	if len(players) == 0 {
		s.codeStore.Add(ctx, Code{Value: code})
		slog.Info("code added with no registered players", "code", code)
		return CodeResult{Code: code, Added: true}
	}

	firstPlayer := players[0]
	redeemResp, err := s.redeemGiftCode(ctx, firstPlayer.PlayerID, firstPlayer.KingdomID, code)
	if err != nil {
		slog.Error("failed to validate new code", "error", err, "code", code)
		return CodeResult{Code: code, APIError: err}
	}

	slog.Info("redeem response", "code", code, "err_code", redeemResp.ErrCode, "player_id", firstPlayer.PlayerID)

	outcome := interpretRedeemResult(redeemResp.ErrCode)
	if outcome.codeExpired {
		s.codeStore.Add(ctx, Code{Value: code, Expired: true})
		return CodeResult{Code: code, AlreadyExpired: true}
	}
	if outcome.codeInvalid {
		return CodeResult{Code: code, Invalid: true}
	}
	if outcome.loginFailed {
		// This error code is now repurposed to mean the player is invalid
		return CodeResult{Code: code, InvalidPlayer: true}
	}

	s.codeStore.Add(ctx, Code{Value: code})
	slog.Info("code added", "code", code)

	results := make([]PlayerRedeemResult, 0, len(players))
	results = append(results, PlayerRedeemResult{GuildID: firstPlayer.GuildID, PlayerID: firstPlayer.PlayerID, Message: outcome.msg})
	for _, player := range players[1:] {
		results = append(results, PlayerRedeemResult{
			GuildID:  player.GuildID,
			PlayerID: player.PlayerID,
			Message:  s.redeemForPlayer(ctx, player, code),
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
// new player. It is safe to call concurrently. ctx bounds all store and HTTP
// calls made while registering req.
func (s *GiftCodeService) RegisterPlayer(ctx context.Context, req NewPlayerRequest) RegisterResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.registerPlayer(ctx, req)
}

func (s *GiftCodeService) registerPlayer(ctx context.Context, req NewPlayerRequest) RegisterResult {
	existing, found, err := s.store.FindByPlayerID(ctx, req.PlayerID)
	if err != nil {
		slog.Error("failed to look up player for registration", "error", err)
		return RegisterResult{StoreError: err}
	}
	// Treat a blank owner as unowned so a previously unlinked player can be
	// reclaimed even if the lookup surfaces the document.
	if found && existing.UserID != "" {
		if existing.UserID == req.UserID {
			return RegisterResult{AlreadySelf: true}
		}
		return RegisterResult{AlreadyOther: true}
	}

	return s.addNewPlayer(ctx, req)
}

// addNewPlayer stores req in the store and redeems active codes for the new player.
// Callers must have already verified that the player does not exist.
// Caller must hold s.mu.
func (s *GiftCodeService) addNewPlayer(ctx context.Context, req NewPlayerRequest) RegisterResult {
	userPlayers, err := s.store.FindByUser(ctx, req.UserID)
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

	if err := s.store.AddPlayer(ctx, req); err != nil {
		slog.Error("failed to add player", "error", err)
		return RegisterResult{StoreError: err}
	}

	player := &Player{
		PlayerID:  req.PlayerID,
		UserID:    req.UserID,
		KingdomID: req.KingdomID,
		GuildID:   req.GuildID,
	}

	slog.Info("user subscribed to bot", "player_id", req.PlayerID, "user_id", req.UserID)

	codeResults := s.redeemActiveCodes(ctx, player)
	return RegisterResult{
		PlayerID:    req.PlayerID,
		UserID:      req.UserID,
		Success:     true,
		CodeResults: codeResults,
	}
}

// TransferPlayer transfers req.PlayerID to req.NewKingdomID, or registers the
// player if not already known. ctx bounds all store and HTTP calls made while
// processing req.
func (s *GiftCodeService) TransferPlayer(ctx context.Context, req TransferPlayerRequest) TransferPlayerResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	player, found, err := s.store.FindByPlayerID(ctx, req.PlayerID)
	if err != nil {
		slog.Error("failed to look up player for transfer", "error", err)
		return TransferPlayerResult{StoreError: err}
	}

	// Treat a blank owner as unowned so a previously unlinked player can be
	// reclaimed even if the lookup surfaces the document.
	if !found || player.UserID == "" {
		// Player doesn't exist, so let's register them instead.
		registerReq := NewPlayerRequest{
			PlayerID:  req.PlayerID,
			KingdomID: req.NewKingdomID,
			UserID:    req.UserID,
			GuildID:   req.GuildID,
		}
		regResult := s.addNewPlayer(ctx, registerReq)
		return TransferPlayerResult{
			PlayerNotFound:     true,
			RegistrationResult: &regResult,
		}
	}

	if player.UserID != req.UserID {
		return TransferPlayerResult{NotYourPlayer: true}
	}

	if player.KingdomID == req.NewKingdomID {
		return TransferPlayerResult{AlreadyInKingdom: true}
	}

	// Check if the new kingdom has space
	userPlayers, err := s.store.FindByUser(ctx, req.UserID)
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

	if err := s.store.UpdatePlayerKingdom(ctx, req); err != nil {
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

// UnlinkPlayer removes req.UserID's ownership of req.PlayerID and marks it
// inactive so it is no longer redeemed for new codes. ctx bounds all store
// calls made while processing req.
func (s *GiftCodeService) UnlinkPlayer(ctx context.Context, req UnlinkPlayerRequest) UnlinkPlayerResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, found, err := s.store.FindByPlayerID(ctx, req.PlayerID)
	if err != nil {
		slog.Error("failed to look up player for unlink", "error", err)
		return UnlinkPlayerResult{StoreError: err}
	}
	// found is false both when the player was never registered and when it
	// has already been unlinked, so both cases report the same result.
	if !found {
		return UnlinkPlayerResult{PlayerNotFound: true}
	}
	if existing.UserID != req.UserID {
		return UnlinkPlayerResult{NotYourPlayer: true}
	}

	if err := s.store.UnlinkPlayer(ctx, req); err != nil {
		slog.Error("failed to unlink player", "error", err)
		return UnlinkPlayerResult{StoreError: err}
	}

	slog.Info("player unlinked", "player_id", req.PlayerID, "user_id", req.UserID)

	return UnlinkPlayerResult{PlayerID: req.PlayerID, Success: true}
}

func (s *GiftCodeService) GetPlayersByUser(ctx context.Context, userID string) ([]*Player, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.store.FindByUser(ctx, userID)
}

// redeemForPlayer logs playerID in, redeems code, and returns a human-readable result.
func (s *GiftCodeService) redeemForPlayer(ctx context.Context, player *Player, code string) string {
	resp, err := s.redeemGiftCode(ctx, player.PlayerID, player.KingdomID, code)
	if err != nil {
		slog.Error("failed to redeem", "error", err, "player_id", player.PlayerID, "code", code)
		return "Error redeeming code."
	}
	slog.Info("redeem response", "player_id", player.PlayerID, "code", code, "err_code", resp.ErrCode, "message", resp.Message)
	return interpretRedeemResult(resp.ErrCode).msg
}

// redeemActiveCodes redeems all currently active codes for playerID and returns
// a slice of per-code results. Caller must hold s.mu.
func (s *GiftCodeService) redeemActiveCodes(ctx context.Context, player *Player) []ActiveCodeResult {
	active := s.codeStore.ActiveCodes(ctx)
	if len(active) == 0 {
		return nil
	}

	var results []ActiveCodeResult
	var codesToRemove []string

	for _, code := range active {
		redeemResp, err := s.redeemGiftCode(ctx, player.PlayerID, player.KingdomID, code)
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
		s.codeStore.RemoveActive(ctx, codesToRemove...)
	}

	return results
}
