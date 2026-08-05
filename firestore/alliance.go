package firestore

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cloud.google.com/go/firestore"
	"github.com/mchipperfield/kingshot"
)

type AllianceStore struct {
	client *firestore.Client
}

func NewAllianceStore(client *firestore.Client) *AllianceStore {
	return &AllianceStore{
		client: client,
	}
}

// alliance represents an alliance in the Firestore database. Each alliance is keyed by its GuildId.
// Names are not stored in Firestore because they can be retrieved from the Discord API and are subject to change. The CodeChannel field is optional and may be nil if no code channel has been set for the alliance.
type alliance struct {
	GuildId   string    `firestore:"guild_id"`
	CreatedAt time.Time `firestore:"created_at"`
	UpdatedAt time.Time `firestore:"updated_at"`
	Channel   channel   `firestore:"code_channel,omitempty"`
}

// The channel where gift codes are redeemed. Optional; may be nil if no code channel has been set for the alliance.
type channel struct {
	ChannelId string    `firestore:"channel_id"`
	GuildId   string    `firestore:"guild_id"`
	UserId    string    `firestore:"user_id"` // The user who set the code channel
	UpdatedAt time.Time `firestore:"updated_at"`
}

func (s *AllianceStore) SetRedemptionChannel(ctx context.Context, req *kingshot.SetChannelRequest) error {

	docRef := s.client.Collection("alliances").Doc(req.GuildId)

	return s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		now := time.Now()
		// Get the current alliance document
		docSnap, err := tx.Get(docRef)
		if err != nil {
			switch status.Code(err) {
			case codes.NotFound:
				// If the document doesn't exist, create a new one
				alliance := alliance{
					GuildId:   req.GuildId,
					CreatedAt: now,
					UpdatedAt: now,
					Channel: channel{
						ChannelId: req.ChannelId,
						GuildId:   req.GuildId,
						UserId:    req.UserId,
						UpdatedAt: now,
					},
				}
				return tx.Set(docRef, alliance)
			default:
				return fmt.Errorf("firestore: set redemption channel: failed to get alliance: %w", err)
			}
		}

		// If the document exists, update the code channel
		var alliance alliance
		if err := docSnap.DataTo(&alliance); err != nil {
			return fmt.Errorf("firestore: set redemption channel: failed to parse alliance: %w", err)
		}
		c := channel{
			ChannelId: req.ChannelId,
			GuildId:   req.GuildId,
			UserId:    req.UserId,
			UpdatedAt: now,
		}
		return tx.Update(docRef, []firestore.Update{
			{Path: "code_channel", Value: c},
			{Path: "updated_at", Value: now},
		})

	})

}
