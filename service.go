package kingshot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// GiftCodeService manages gift code state and interacts with the KingShot API.
// All mutable state is owned here; no package-level globals.
type GiftCodeService struct {
	mu        sync.Mutex
	codeStore CodeStore
	store     PlayerStore
	client    *Client
	logger    *slog.Logger
}

// NewService returns a GiftCodeService using the supplied PlayerStore
// and CodeStore implementations, such as the Firestore-backed stores.
// A nil logger falls back to slog.Default(). The logger is tagged with a
// "component" attribute so its log lines can be attributed to this service.
func NewService(store PlayerStore, cs CodeStore, logger *slog.Logger) *GiftCodeService {
	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With("component", "code_service")
	return &GiftCodeService{
		store:     store,
		codeStore: cs,
		client:    NewClient(logger),
		logger:    logger,
	}
}

// ProcessNewCode validates code against the KingShot API and redeems it for
// all registered players. It is safe to call concurrently. ctx bounds all
// store and HTTP calls made while processing code.
func (s *GiftCodeService) ProcessNewCode(ctx context.Context, code string) (*RedeemResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if c, found, err := s.codeStore.Find(ctx, code); err != nil {
		return nil, fmt.Errorf("code service: find code %s: %w", code, err)
	} else if found {
		if !c.IsExpired() {
			return nil, newCodeError("This code is already active.", codeErrorActive, nil)
		}
		return nil, newCodeError("This code has expired and cannot be re-added.", codeErrorExpired, nil)
	}

	players, err := s.store.Players(ctx)
	if err != nil {
		return nil, fmt.Errorf("code service: fetch players: %w", err)
	}

	if len(players) == 0 {
		if err := s.codeStore.Add(ctx, Code{Value: code}); err != nil {
			return nil, fmt.Errorf("code service: add code %s: %w", code, err)
		}
		s.logger.Info("code added with no registered players", "code", code)
		return &RedeemResult{Code: code, Added: true}, nil
	}

	firstPlayer := players[0]
	redeemResp, err := s.client.redeemGiftCode(ctx, firstPlayer.PlayerID, firstPlayer.KingdomID, code)
	if err != nil {
		return nil, fmt.Errorf("code service: validate code %s: %w", code, err)
	}

	s.logger.Info("redeem response", "code", code, "err_code", redeemResp.ErrCode, "player_id", firstPlayer.PlayerID)

	outcome := interpretRedeemResult(redeemResp)
	if outcome != nil && outcome.kind == codeErrorExpired {
		if err := s.codeStore.Add(ctx, Code{Value: code, ExpiredAt: time.Now()}); err != nil {
			return nil, fmt.Errorf("code service: record expired code %s: %w", code, err)
		}
		return nil, outcome
	}
	if outcome != nil && (outcome.kind == codeErrorInvalid || outcome.kind == codeErrorLogin) {
		// This error code is now repurposed to mean the player is invalid
		return nil, outcome
	}
	if outcome != nil && outcome.kind == codeErrorUnknown {
		return nil, fmt.Errorf("code service: unexpected redemption response: %w", redeemResp)
	}

	if err := s.codeStore.Add(ctx, Code{Value: code}); err != nil {
		return nil, fmt.Errorf("code service: add code %s: %w", code, err)
	}
	s.logger.Info("code added", "code", code)

	results := make([]PlayerRedeemResult, 0, len(players))
	firstPlayerMessage := "Successfully redeemed!"
	if outcome != nil {
		firstPlayerMessage = outcome.Error()
	}
	results = append(results, PlayerRedeemResult{GuildID: firstPlayer.GuildID, PlayerID: firstPlayer.PlayerID, Message: firstPlayerMessage})
	for _, player := range players[1:] {
		result := s.redeemForPlayer(ctx, player, code)
		results = append(results, PlayerRedeemResult{
			GuildID:  player.GuildID,
			PlayerID: player.PlayerID,
			Message:  redemptionMessage(result),
		})
	}

	return &RedeemResult{Code: code, Added: true, PlayerResults: results}, nil
}

// NewPlayerRequest is the set of parameters for registering a new player.
type NewPlayerRequest struct {
	PlayerID, UserID, KingdomID, GuildID string
}

var (
	ErrAlreadySelf  = errors.New("player already registered to this user")
	ErrAlreadyOther = errors.New("player already registered to a different user")
)

// PlayerError is a caller-safe player registration or transfer failure.
// Error may be shown directly to a user.
type PlayerError struct {
	message string
	cause   error
}

func newPlayerError(message string, cause error) *PlayerError {
	return &PlayerError{message: message, cause: cause}
}

func (e *PlayerError) Error() string {
	return e.message
}

func (e *PlayerError) Unwrap() error {
	return e.cause
}

// RegisterPlayer validates playerID via the KingShot API, registers it with
// UserID in the store, and redeems any currently active codes for the
// new player. It is safe to call concurrently. ctx bounds all store and HTTP
// calls made while registering req.
func (s *GiftCodeService) RegisterPlayer(ctx context.Context, req NewPlayerRequest) (*RegisterResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.registerPlayer(ctx, req)
}

func (s *GiftCodeService) registerPlayer(ctx context.Context, req NewPlayerRequest) (*RegisterResult, error) {
	player, err := s.store.FindByPlayerID(ctx, req.PlayerID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	// Treat a blank owner as unowned so a previously unlinked player can be
	// reclaimed even if the lookup surfaces the document.
	if player != nil && player.UserID != "" {
		if player.UserID == req.UserID {
			return nil, newPlayerError("This player ID is already registered to your Discord account.", ErrAlreadySelf)
		}
		return nil, newPlayerError("This player ID is already registered to another Discord account.", ErrAlreadyOther)
	}

	return s.addNewPlayer(ctx, req)
}

// addNewPlayer stores req in the store and redeems active codes for the new player.
// Callers must have already verified that the player does not exist.
// Caller must hold s.mu.
func (s *GiftCodeService) addNewPlayer(ctx context.Context, req NewPlayerRequest) (*RegisterResult, error) {
	userPlayers, err := s.store.FindByUser(ctx, req.UserID)
	if err != nil {
		return nil, err
	}

	kingdomPlayerCount := 0
	for _, p := range userPlayers {
		if p.KingdomID == req.KingdomID {
			kingdomPlayerCount++
		}
	}

	if kingdomPlayerCount >= 2 {
		return nil, newPlayerError("You have already registered the maximum number of players for this kingdom.", ErrMaxPlayersForKingdom)
	}

	if err := s.store.AddPlayer(ctx, req); err != nil {
		s.logger.Error("failed to add player", "error", err)
		return nil, err
	}

	player := &Player{
		PlayerID:  req.PlayerID,
		UserID:    req.UserID,
		KingdomID: req.KingdomID,
		GuildID:   req.GuildID,
	}

	s.logger.Info("user subscribed to bot", "player_id", req.PlayerID, "user_id", req.UserID)

	redeemResults, err := s.redeemActiveCodes(ctx, player)
	if err != nil {
		return nil, err
	}
	return &RegisterResult{
		Player:      *player,
		CodeResults: redeemResults,
	}, nil
}

var (
	ErrAlreadyInKingdom     = errors.New("player already in kingdom")
	ErrMaxPlayersForKingdom = errors.New("max players for kingdom reached")
	NotYourPlayer           = errors.New("not your player")
	ErrPlayerNotRegistered  = errors.New("player not registered")
)

// TransferPlayerRequest is the input to the TransferPlayer service method.
type TransferPlayerRequest struct {
	PlayerID     string
	NewKingdomID string
	UserID       string
	GuildID      string
}

// TransferPlayer transfers req.PlayerID to req.NewKingdomID. ctx bounds all
// store calls made while processing req.
func (s *GiftCodeService) TransferPlayer(ctx context.Context, req TransferPlayerRequest) (*Player, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	player, err := s.store.FindByPlayerID(ctx, req.PlayerID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	// Treat a blank owner as unowned so an unregistered or previously
	// unlinked player is rejected rather than silently reclaimed.
	if errors.Is(err, ErrNotFound) || player.UserID == "" {
		return nil, newPlayerError("This player is not registered. Use /player register to register it first.", ErrPlayerNotRegistered)
	}

	if player.UserID != req.UserID {
		return nil, newPlayerError("This player is not registered to your Discord account.", NotYourPlayer)
	}

	if player.KingdomID == req.NewKingdomID {
		return nil, newPlayerError("This player is already in that kingdom.", ErrAlreadyInKingdom)
	}

	// Check if the new kingdom has space
	userPlayers, err := s.store.FindByUser(ctx, req.UserID)
	if err != nil {
		return nil, fmt.Errorf("find players by user %s: %w", req.UserID, err)
	}

	kingdomPlayerCount := 0
	for _, p := range userPlayers {
		// an existing registration for this player should not count towards the limit
		if p.KingdomID == req.NewKingdomID && p.PlayerID != req.PlayerID {
			kingdomPlayerCount++
		}
	}

	if kingdomPlayerCount >= 2 {
		return nil, newPlayerError("You have already registered the maximum number of players for the new kingdom.", ErrMaxPlayersForKingdom)
	}

	if err := s.store.UpdatePlayerKingdom(ctx, req); err != nil {
		return nil, fmt.Errorf("update player kingdom: %w", err)
	}

	return &Player{
		PlayerID:  req.PlayerID,
		KingdomID: req.NewKingdomID,
		UserID:    req.UserID,
		GuildID:   req.GuildID,
	}, nil
}

// UnlinkPlayerRequest is the input to the UnlinkPlayer service method.
type UnlinkPlayerRequest struct {
	PlayerID string
	UserID   string
	GuildID  string
}

// UnlinkPlayer removes req.UserID's ownership of req.PlayerID and marks it
// inactive so it is no longer redeemed for new codes. ctx bounds all store
// calls made while processing req.
func (s *GiftCodeService) UnlinkPlayer(ctx context.Context, req UnlinkPlayerRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, err := s.store.FindByPlayerID(ctx, req.PlayerID)
	if err != nil {
		return fmt.Errorf("find player by id %s: %w", req.PlayerID, err)
	}

	if existing.UserID != req.UserID {
		return newPlayerError("This player is not registered to your Discord account.", NotYourPlayer)
	}

	if err := s.store.UnlinkPlayer(ctx, req); err != nil {
		return fmt.Errorf("unlink player %s: %w", req.PlayerID, err)
	}

	return nil
}

func (s *GiftCodeService) GetPlayersByUser(ctx context.Context, userID string) ([]*Player, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.store.FindByUser(ctx, userID)
}

type codeErrorKind uint8

const (
	codeErrorUnknown codeErrorKind = iota
	codeErrorActive
	codeErrorClaimed
	codeErrorExpired
	codeErrorInvalid
	codeErrorLogin
	codeErrorLimitReached
)

// CodeError is a caller-safe gift-code failure. Error may be shown directly
// to a user.
type CodeError struct {
	message string
	kind    codeErrorKind
	cause   error
}

func newCodeError(message string, kind codeErrorKind, cause error) *CodeError {
	return &CodeError{message: message, kind: kind, cause: cause}
}

func (e *CodeError) Error() string {
	return e.message
}

func (e *CodeError) Unwrap() error {
	return e.cause
}

// interpretRedeemResult maps a KingShot API response to a caller-safe error.
func interpretRedeemResult(resp *redeemResponse) *CodeError {
	if resp.ErrCode == ErrCodeSuccess {
		return nil
	}
	switch resp.ErrCode {
	case ErrCodeClaimed:
		return newCodeError("Code already claimed.", codeErrorClaimed, resp)
	case ErrCodeExpired:
		return newCodeError("This code has expired.", codeErrorExpired, resp)
	case ErrCodeNotFound:
		return newCodeError("This code is invalid.", codeErrorInvalid, resp)
	case ErrCodeLogin:
		return newCodeError("The player used to validate this code is invalid.", codeErrorLogin, resp)
	case ErrCodeUnknownPlayer:
		return newCodeError("This player's details are invalid.", codeErrorLogin, resp)
	case ErrCodeLimitReached:
		return newCodeError("Redemption limit reached.", codeErrorLimitReached, resp)
	default:
		return newCodeError("Failed to redeem code.", codeErrorUnknown, resp)
	}
}

// redeemForPlayer logs playerID in, redeems code, and returns a human-readable result.
func (s *GiftCodeService) redeemForPlayer(ctx context.Context, player *Player, code string) error {
	resp, err := s.client.redeemGiftCode(ctx, player.PlayerID, player.KingdomID, code)
	if err != nil {
		return fmt.Errorf("code service: redeem code %s for player %s: %w", code, player.PlayerID, err)
	}
	codeErr := interpretRedeemResult(resp)
	if codeErr == nil {
		return nil
	}
	return codeErr
}

func redemptionMessage(err error) string {
	if err == nil {
		return "Successfully redeemed!"
	}
	var codeErr *CodeError
	if errors.As(err, &codeErr) {
		return codeErr.Error()
	}
	return "Failed to redeem code."
}

// redeemActiveCodes redeems all currently active codes for playerID and returns
// a slice of per-code results. Caller must hold s.mu.
func (s *GiftCodeService) redeemActiveCodes(ctx context.Context, player *Player) ([]RegistrationResult, error) {
	active, err := s.codeStore.ActiveCodes(ctx)
	if err != nil {
		return nil, fmt.Errorf("code service: fetch active codes: %w", err)
	}
	if len(active) == 0 {
		return nil, nil
	}

	var results []RegistrationResult
	var codesToRemove []string

	for _, code := range active {
		redeemResp, err := s.client.redeemGiftCode(ctx, player.PlayerID, player.KingdomID, code)
		if err != nil {
			results = append(results, RegistrationResult{Code: code, Message: "Error redeeming code."})
			continue
		}

		result := interpretRedeemResult(redeemResp)
		if result != nil {
			if result.kind == codeErrorExpired || result.kind == codeErrorInvalid {
				codesToRemove = append(codesToRemove, code)
			}
			results = append(results, RegistrationResult{Code: code, Message: result.Error()})
		} else {
			results = append(results, RegistrationResult{Code: code, Message: "Successfully redeemed."})
		}
	}

	if len(codesToRemove) > 0 {
		if err := s.codeStore.RemoveActive(ctx, codesToRemove...); err != nil {
			return results, fmt.Errorf("code service: remove active codes: %w", err)
		}
	}

	return results, nil
}
