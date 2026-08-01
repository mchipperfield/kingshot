package kingshot

import "context"

// Player holds all the information for a given player
type Player struct {
	PlayerID  string `firestore:"playerID"`
	UserID    string `firestore:"userID"`
	KingdomID string `firestore:"kingdomID"`
	GuildID   string `firestore:"guildID"`
}

// PlayerStore manages persistent storage of registered players.
// Implementations must respect ctx cancellation/deadlines for any I/O.
type PlayerStore interface {
	// Players returns all active registered players in storage order.
	Players(ctx context.Context) ([]*Player, error)
	// FindByPlayerID looks up the player by their playerID. found is false when
	// the player is not registered, including if it has been unlinked.
	FindByPlayerID(ctx context.Context, playerID string) (player *Player, found bool, err error)
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
