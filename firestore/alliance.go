package firestore

import (
	"context"
	"time"

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

// Alliance represents an alliance in the Firestore database. Each alliance is keyed by its GuildId.
// Names are not stored in Firestore because they can be retrieved from the Discord API and are subject to change. The CodeChannel field is optional and may be nil if no code channel has been set for the alliance.
type Alliance struct {
	GuildId   string    `firestore:"guild_id"`
	CreatedAt time.Time `firestore:"created_at"`
	UpdatedAt time.Time `firestore:"updated_at"`
	Channel   Channel   `firestore:"code_channel,omitempty"`
}

// The channel where gift codes are redeemed. Optional; may be nil if no code channel has been set for the alliance.
type Channel struct {
	ChannelId string    `firestore:"channel_id"`
	UserId    string    `firestore:"user_id"` // The user who set the code channel
	CreatedAt time.Time `firestore:"created_at"`
	UpdatedAt time.Time `firestore:"updated_at"`
}

func (s *AllianceStore) SetRedemptionChannel(ctx context.Context, req *kingshot.SetChannelRequest) error {
	now := time.Now()
	_, err := s.client.Collection("alliances").Doc(req.GuildId).Set(ctx, Alliance{
		GuildId:   req.GuildId,
		CreatedAt: now,
		UpdatedAt: now,
		Channel: Channel{
			ChannelId: req.ChannelId,
			UserId:    req.UserId,
			CreatedAt: now,
			UpdatedAt: now,
		},
	})
	return err
}
