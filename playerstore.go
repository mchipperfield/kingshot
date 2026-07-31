package kingshot

// Player holds all the information for a given player
type Player struct {
	PlayerID   string `firestore:"playerID"`
	ExternalID string `firestore:"discordID"`
	KingdomID  string `firestore:"kingdomID"`
}

// PlayerStore manages persistent storage of registered players.
type PlayerStore interface {
	// Players returns all registered players in storage order.
	Players() ([]*Player, error)
	// FindByPlayerID looks up the player by their playerID. found is false when
	// the player is not registered.
	FindByPlayerID(playerID string) (player *Player, found bool, err error)
	// AddPlayer stores a new player.
	AddPlayer(player *Player) error
}
