package kingshot

// Player holds all the information for a given player
type Player struct {
	PlayerID  string `firestore:"playerID"`
	UserID    string `firestore:"userID"`
	KingdomID string `firestore:"kingdomID"`
}

// PlayerStore manages persistent storage of registered players.
type PlayerStore interface {
	// Players returns all registered players in storage order.
	Players() ([]*Player, error)
	// FindByPlayerID looks up the player by their playerID. found is false when
	// the player is not registered.
	FindByPlayerID(playerID string) (player *Player, found bool, err error)
	// FindByUser returns all players registered to a given user.
	FindByUser(userID string) ([]*Player, error)
	// AddPlayer stores a new player.
	AddPlayer(req NewPlayerRequest) error
	// UpdatePlayerKingdom updates the kingdom for a given player.
	UpdatePlayerKingdom(playerID, newKingdomID, guildID string) error
}
