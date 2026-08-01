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
	PlayerID     string         `firestore:"player_id"`
	UserID       string         `firestore:"user_id"`
	KingdomID    string         `firestore:"kingdom_id"`
	RegisteredAt time.Time      `firestore:"registered_at"`
	GuildID      string         `firestore:"guild_id"`
	History      []historyEntry `firestore:"history,omitempty"`
}

type historyEntry struct {
	GuildID   string    `firestore:"guild_id"`
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

func (ps *PlayerStore) Players() ([]*kingshot.Player, error) {
	var players []*kingshot.Player
	iter := ps.Client.Collection("players").Documents(context.Background())
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
		})
	}
	return players, nil
}

func (ps *PlayerStore) FindByPlayerID(playerID string) (*kingshot.Player, bool, error) {
	docRef := ps.Client.Collection("players").Doc(playerID)
	docSnap, err := docRef.Get(context.Background())
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

	return &kingshot.Player{
		PlayerID:  p.PlayerID,
		UserID:    p.UserID,
		KingdomID: p.KingdomID,
	}, true, nil
}

func (ps *PlayerStore) FindByUser(userID string) ([]*kingshot.Player, error) {
	var players []*kingshot.Player
	iter := ps.Client.Collection("players").Where("user_id", "==", userID).Documents(context.Background())
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
		})
	}
	return players, nil
}

func (ps *PlayerStore) AddPlayer(req kingshot.NewPlayerRequest) error {
	internalPlayer := player{
		PlayerID:     req.PlayerID,
		UserID:       req.UserID,
		KingdomID:    req.KingdomID,
		RegisteredAt: time.Now(),
		GuildID:      req.GuildID,
		History: []historyEntry{
			{
				GuildID:   req.GuildID,
				Timestamp: time.Now(),
				Action:    "register",
			},
		},
	}
	_, err := ps.Client.Collection("players").Doc(req.PlayerID).Set(context.Background(), internalPlayer)
	return err
}

func (ps *PlayerStore) UpdatePlayerKingdom(req kingshot.UpdatePlayerKingdomRequest) error {
	entry := historyEntry{
		GuildID:   req.GuildID,
		Timestamp: time.Now(),
		Action:    "transfer",
	}
	_, err := ps.Client.Collection("players").Doc(req.PlayerID).Update(context.Background(), []firestore.Update{
		{Path: "kingdom_id", Value: req.NewKingdomID},
		{Path: "history", Value: firestore.ArrayUnion(entry)},
	})
	return err
}
