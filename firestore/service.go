package firestore

import (
	"context"

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
	PlayerID  string `firestore:"playerID"`
	UserID    string `firestore:"userID"`
	KingdomID string `firestore:"kingdomID"`
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
	iter := ps.Client.Collection("players").Where("userID", "==", userID).Documents(context.Background())
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

func (ps *PlayerStore) AddPlayer(p *kingshot.Player) error {
	internalPlayer := player{
		PlayerID:  p.PlayerID,
		UserID:    p.UserID,
		KingdomID: p.KingdomID,
	}
	_, err := ps.Client.Collection("players").Doc(p.PlayerID).Set(context.Background(), internalPlayer)
	return err
}
