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
// Names are not stored in Firestore because they can be retrieved from the Discord API and are subject to change.
type alliance struct {
	GuildId         string    `firestore:"guild_id"`
	CreatedAt       time.Time `firestore:"created_at"`
	UpdatedAt       time.Time `firestore:"updated_at"`
	GiftCodeChannel channel   `firestore:"code_channel,omitempty"`
	BearChannel     channel   `firestore:"bear_channel,omitempty"`
	AccessRole      role      `firestore:"access_role,omitempty"`
}

type channel struct {
	ChannelId string    `firestore:"channel_id"`
	GuildId   string    `firestore:"guild_id"`
	UserId    string    `firestore:"user_id"` // The user who set the code channel
	UpdatedAt time.Time `firestore:"updated_at"`
}

type role struct {
	RoleId    string    `firestore:"role_id"`
	GuildId   string    `firestore:"guild_id"`
	UserId    string    `firestore:"user_id"` // The user who set the access role
	UpdatedAt time.Time `firestore:"updated_at"`
}

func (s *AllianceStore) SetChannel(ctx context.Context, kind kingshot.ChannelKind, req *kingshot.SetChannelRequest) error {
	field, err := channelField(kind)
	if err != nil {
		return err
	}
	docRef := s.client.Collection("alliances").Doc(req.GuildId)

	return s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		now := time.Now()
		_, err := tx.Get(docRef)
		if err != nil {
			switch status.Code(err) {
			case codes.NotFound:
				return tx.Set(docRef, map[string]any{
					"guild_id":   req.GuildId,
					"created_at": now,
					"updated_at": now,
					field: channel{
						ChannelId: req.ChannelId,
						GuildId:   req.GuildId,
						UserId:    req.UserId,
						UpdatedAt: now,
					},
				})
			default:
				return fmt.Errorf("firestore: failed to get alliance: %w", err)
			}
		}

		return tx.Update(docRef, []firestore.Update{
			{Path: field, Value: channel{
				ChannelId: req.ChannelId,
				GuildId:   req.GuildId,
				UserId:    req.UserId,
				UpdatedAt: now,
			}},
			{Path: "updated_at", Value: now},
		})
	})
}

func (s *AllianceStore) GetChannel(ctx context.Context, kind kingshot.ChannelKind, guildId string) (string, error) {
	_, err := channelField(kind)
	if err != nil {
		return "", err
	}
	docRef := s.client.Collection("alliances").Doc(guildId)
	docSnap, err := docRef.Get(ctx)
	if err != nil {
		switch status.Code(err) {
		case codes.NotFound:
			return "", kingshot.ErrNotFound
		default:
			return "", fmt.Errorf("firestore: failed to get alliance: %w", err)
		}
	}
	var alliance alliance
	if err := docSnap.DataTo(&alliance); err != nil {
		return "", fmt.Errorf("firestore: failed to parse alliance: %w", err)
	}

	var configured channel
	switch kind {
	case kingshot.RedemptionChannel:
		configured = alliance.GiftCodeChannel
	case kingshot.BearChannel:
		configured = alliance.BearChannel
	}
	if configured.ChannelId == "" {
		return "", kingshot.ErrNotFound
	}
	return configured.ChannelId, nil
}

func (s *AllianceStore) GetAccessRole(ctx context.Context, guildId string) (string, error) {
	docRef := s.client.Collection("alliances").Doc(guildId)
	docSnap, err := docRef.Get(ctx)
	if err != nil {
		switch status.Code(err) {
		case codes.NotFound:
			return "", kingshot.ErrNotFound
		default:
			return "", fmt.Errorf("firestore: failed to get alliance: %w", err)
		}
	}

	var a alliance
	if err := docSnap.DataTo(&a); err != nil {
		return "", fmt.Errorf("firestore: failed to parse alliance: %w", err)
	}
	if a.AccessRole.RoleId == "" {
		return "", kingshot.ErrNotFound
	}
	return a.AccessRole.RoleId, nil
}
func (s *AllianceStore) SetAccessRole(ctx context.Context, guildId, roleId, userId string) error {

	docRef := s.client.Collection("alliances").Doc(guildId)
	return s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		now := time.Now()
		_, err := tx.Get(docRef)
		if err != nil {
			switch status.Code(err) {
			case codes.NotFound:
				return tx.Set(docRef, map[string]any{
					"guild_id":   guildId,
					"created_at": now,
					"updated_at": now,
					"access_role": role{
						RoleId:    roleId,
						GuildId:   guildId,
						UserId:    userId,
						UpdatedAt: now,
					},
				})
			default:
				return fmt.Errorf("firestore: failed to get alliance: %w", err)
			}
		}

		return tx.Update(docRef, []firestore.Update{
			{Path: "access_role", Value: role{
				RoleId:    roleId,
				GuildId:   guildId,
				UserId:    userId,
				UpdatedAt: now,
			}},
			{Path: "updated_at", Value: now},
		})
	})
}

func (s *AllianceStore) ResetAccessRole(ctx context.Context, guildId string, userId string) error {
	return s.SetAccessRole(ctx, guildId, "", userId)
}

func channelField(kind kingshot.ChannelKind) (string, error) {
	switch kind {
	case kingshot.RedemptionChannel:
		return "code_channel", nil
	case kingshot.BearChannel:
		return "bear_channel", nil
	default:
		return "", fmt.Errorf("unknown channel kind %q", kind)
	}
}
