package kingshot

import (
	"context"
	"time"
)

// Player holds all the information for a given player
type Player struct {
	PlayerID  string
	UserID    string
	KingdomID string
	GuildID   string
}

// PlayerStore manages persistent storage of registered players.
// Implementations must respect ctx cancellation/deadlines for any I/O.
type PlayerStore interface {
	// Players returns all active registered players in storage order.
	Players(ctx context.Context) ([]*Player, error)
	// FindByPlayerID looks up the player by their playerID. It returns
	// ErrNotFound when the player is not registered, including if it has been
	// unlinked.
	FindByPlayerID(ctx context.Context, playerID string) (player *Player, err error)
	// FindByUser returns all players registered to a given user.
	FindByUser(ctx context.Context, userID string) ([]*Player, error)
	// AddPlayer stores a new player, or reactivates and re-links a previously
	// unlinked one.
	AddPlayer(ctx context.Context, req NewPlayerRequest) error
	// UpdatePlayerKingdom updates the kingdom for a given player.
	UpdatePlayerKingdom(ctx context.Context, req TransferPlayerRequest) error
	// UnlinkPlayer clears the player's owner and marks it unlinked so it is no
	// longer found or redeemed for new codes.
	UnlinkPlayer(ctx context.Context, req UnlinkPlayerRequest) error
}

// Code represents a tracked gift code and its current state.
type Code struct {
	Value     string
	ExpiredAt time.Time // zero value means the code is still active
}

// IsExpired reports whether the code is known to be expired.
func (c Code) IsExpired() bool { return !c.ExpiredAt.IsZero() }

// CodeStore manages the lifecycle of gift codes tracked by GiftCodeService.
// Implementations must be safe to call from a single goroutine at a time;
// GiftCodeService serialises all access through its own mutex.
type CodeStore interface {
	// Find looks up a code by value. found is false when the code is not
	// tracked at all.
	Find(ctx context.Context, code string) (*Code, bool, error)
	// Add stores a code. If a code with the same Value already exists its
	// state is updated.
	Add(ctx context.Context, code Code) error
	// ActiveCodes returns a snapshot of all currently active codes.
	ActiveCodes(ctx context.Context) ([]string, error)
	// RemoveActive removes the named codes from the active set. Codes that are
	// not present are silently ignored.
	RemoveActive(ctx context.Context, codes ...string) error
}

type AllianceStore interface {
	SetChannel(ctx context.Context, kind ChannelKind, req *SetChannelRequest) error
	GetChannel(ctx context.Context, kind ChannelKind, guildId string) (string, error)
	GetAccessRole(ctx context.Context, guildId string) (string, error)
	SetAccessRole(ctx context.Context, guildId, roleId, userId string) error
	ResetAccessRole(ctx context.Context, guildId string, userId string) error
}

type ChannelKind string

const (
	RedemptionChannel ChannelKind = "redemption"
	BearChannel       ChannelKind = "bear"
)

type SetChannelRequest struct {
	GuildId   string
	ChannelId string
	UserId    string
}

type BearStore interface {
	GetBearStatus(ctx context.Context, guildId, bearID string) (*BearStatus, error)
	SetBear(ctx context.Context, guildId, bearID string, setTime time.Time, setBy string, reminderLeadTime time.Duration) error
	SetBearRemindersEnabled(ctx context.Context, guildId, bearID string, enabled bool) error
	UpdateBearNext(ctx context.Context, guildId, bearID string, next time.Time) error
	GetAllBearStatuses(ctx context.Context) ([]BearStatus, error)
}
