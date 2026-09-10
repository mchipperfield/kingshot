package inmem

import (
	"context"

	"github.com/mchipperfield/kingshot"
)

type AllianceStore struct {
	channels    map[string]map[kingshot.ChannelKind]string
	accessRoles map[string]string
}

func (s *AllianceStore) SetChannel(_ context.Context, kind kingshot.ChannelKind, req *kingshot.SetChannelRequest) error {
	if s.channels[req.GuildId] == nil {
		s.channels[req.GuildId] = make(map[kingshot.ChannelKind]string)
	}
	s.channels[req.GuildId][kind] = req.ChannelId
	return nil
}

func (s *AllianceStore) GetChannel(_ context.Context, kind kingshot.ChannelKind, guildID string) (string, error) {
	if channelID := s.channels[guildID][kind]; channelID != "" {
		return channelID, nil
	}
	return "", kingshot.ErrNotFound
}

func (s *AllianceStore) GetAccessRole(_ context.Context, guildID string) (string, error) {
	if roleID := s.accessRoles[guildID]; roleID != "" {
		return roleID, nil
	}
	return "", kingshot.ErrNotFound
}

func (s *AllianceStore) SetAccessRole(_ context.Context, guildID, roleID, _ string) error {
	s.accessRoles[guildID] = roleID
	return nil
}

func (s *AllianceStore) ResetAccessRole(ctx context.Context, guildID, userID string) error {
	return s.SetAccessRole(ctx, guildID, "", userID)
}

func NewAllianceStore() *AllianceStore {
	return &AllianceStore{
		channels:    make(map[string]map[kingshot.ChannelKind]string),
		accessRoles: make(map[string]string),
	}
}
