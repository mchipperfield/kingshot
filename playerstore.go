package kingshot

// Player holds the stored attributes for a registered player.
type Player struct {
	ID         string // KingShot player ID (fid)
	KID        string // kingdom ID
	ExternalID string // external identifier, e.g. Discord user ID
}

// PlayerStore manages persistent storage of registered players.
type PlayerStore interface {
	// Players returns all registered players in storage order.
	Players() ([]Player, error)
	// FindByPlayerID looks up the player record associated with playerID.
	// found is false when the player is not registered.
	FindByPlayerID(playerID string) (player Player, found bool, err error)
	// AddPlayer stores a new player record.
	AddPlayer(player Player) error
}
