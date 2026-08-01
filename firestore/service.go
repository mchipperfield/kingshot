package firestore

import (
	"context"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/mchipperfield/kingshot"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type PlayerStore struct {
	*firestore.Client
}

type player struct {
	PlayerID     string    `firestore:"player_id"`
	UserID       string    `firestore:"user_id"`
	KingdomID    string    `firestore:"kingdom_id"`
	RegisteredAt time.Time `firestore:"registered_at"`
	GuildID      string    `firestore:"guild_id"`
	// IsActive is cleared when a player is unlinked. Requires every document to
	// have this field set (see AddPlayer/UnlinkPlayer) for the Players() query to see it.
	IsActive bool           `firestore:"is_active"`
	History  []historyEntry `firestore:"history,omitempty"`
}

type historyEntry struct {
	GuildID   string    `firestore:"guild_id"`
	UserID    string    `firestore:"user_id"`
	Timestamp time.Time `firestore:"timestamp"`
	Action    string    `firestore:"action"`
}

func NewPlayerStore(projectId string) (*PlayerStore, error) {
	client, err := firestore.NewClient(context.Background(), projectId)
	if err != nil {
		return nil, err
	}
	return &PlayerStore{
		Client: client,
	}, nil
}

func (ps *PlayerStore) Players(ctx context.Context) ([]*kingshot.Player, error) {
	var players []*kingshot.Player
	iter := ps.Client.Collection("players").Where("is_active", "==", true).Documents(ctx)
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		var p player
		if err := doc.DataTo(&p); err != nil {
			return nil, err
		}
		players = append(players, &kingshot.Player{
			PlayerID:  p.PlayerID,
			UserID:    p.UserID,
			KingdomID: p.KingdomID,
			GuildID:   p.GuildID,
		})
	}
	return players, nil
}

func (ps *PlayerStore) FindByPlayerID(ctx context.Context, playerID string) (*kingshot.Player, bool, error) {
	docRef := ps.Client.Collection("players").Doc(playerID)
	docSnap, err := docRef.Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, false, nil
		}
		return nil, false, err
	}

	var p player
	if err := docSnap.DataTo(&p); err != nil {
		return nil, false, err
	}
	// Get is by document ID, so it can't apply a Where filter; treat an
	// unlinked player the same as one that was never registered.
	if !p.IsActive {
		return nil, false, nil
	}

	return &kingshot.Player{
		PlayerID:  p.PlayerID,
		UserID:    p.UserID,
		KingdomID: p.KingdomID,
		GuildID:   p.GuildID,
	}, true, nil
}

func (ps *PlayerStore) FindByUser(ctx context.Context, userID string) ([]*kingshot.Player, error) {
	var players []*kingshot.Player
	iter := ps.Client.Collection("players").
		Where("user_id", "==", userID).
		Where("is_active", "==", true).
		Documents(ctx)
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		var p player
		if err := doc.DataTo(&p); err != nil {
			// It might be better to log the error and continue
			return nil, err
		}
		players = append(players, &kingshot.Player{
			PlayerID:  p.PlayerID,
			UserID:    p.UserID,
			KingdomID: p.KingdomID,
			GuildID:   p.GuildID,
		})
	}
	return players, nil
}

func (ps *PlayerStore) AddPlayer(ctx context.Context, req kingshot.NewPlayerRequest) error {
	entry := historyEntry{
		GuildID:   req.GuildID,
		UserID:    req.UserID,
		Timestamp: time.Now(),
		Action:    "register",
	}
	// MergeAll upserts: creates the document for a brand new player, or
	// reactivates and re-links a previously unlinked one while preserving its
	// existing history via ArrayUnion.
	_, err := ps.Client.Collection("players").Doc(req.PlayerID).Set(ctx, map[string]any{
		"player_id":     req.PlayerID,
		"user_id":       req.UserID,
		"kingdom_id":    req.KingdomID,
		"guild_id":      req.GuildID,
		"registered_at": time.Now(),
		"is_active":     true,
		"history":       firestore.ArrayUnion(entry),
	}, firestore.MergeAll)
	if err != nil {
		return err
	}
	_, err = ps.Client.Collection("players").Doc(req.PlayerID).Update(ctx, []firestore.Update{
		{Path: "history", Value: firestore.ArrayUnion(entry)},
	})
	return err
}

func (ps *PlayerStore) UpdatePlayerKingdom(ctx context.Context, req kingshot.TransferPlayerRequest) error {
	entry := historyEntry{
		GuildID:   req.GuildID,
		UserID:    req.UserID,
		Timestamp: time.Now(),
		Action:    "transfer",
	}
	_, err := ps.Client.Collection("players").Doc(req.PlayerID).Update(ctx, []firestore.Update{
		{Path: "kingdom_id", Value: req.NewKingdomID},
		{Path: "history", Value: firestore.ArrayUnion(entry)},
	})
	return err
}

// UnlinkPlayer clears the player's owner and marks it inactive so it is
// excluded from future code redemptions, while preserving its history.
func (ps *PlayerStore) UnlinkPlayer(ctx context.Context, req kingshot.UnlinkPlayerRequest) error {
	entry := historyEntry{
		GuildID:   req.GuildID,
		UserID:    req.UserID,
		Timestamp: time.Now(),
		Action:    "unlink",
	}
	_, err := ps.Client.Collection("players").Doc(req.PlayerID).Update(ctx, []firestore.Update{
		{Path: "user_id", Value: ""},
		{Path: "is_active", Value: false},
		{Path: "history", Value: firestore.ArrayUnion(entry)},
	})
	return err
}
