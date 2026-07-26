package kingshot

// PlayerStore manages persistent storage of registered players.
type PlayerStore interface {
	// PlayerIDs returns all registered player IDs in storage order.
	PlayerIDs() ([]string, error)
	// FindByPlayerID looks up the external ID (e.g. Discord user ID) associated
	// with playerID. found is false when the player is not registered.
	FindByPlayerID(playerID string) (externalID string, found bool, err error)
	// AddPlayer stores a new playerID → externalID association.
	AddPlayer(playerID, externalID string) error
}
