package kingshot

import (
	"context"
	"testing"
)

func TestInMemoryAllianceStoreKeepsChannelsPerGuild(t *testing.T) {
	store := NewAllianceStore()
	ctx := context.Background()

	for _, request := range []*SetChannelRequest{
		{GuildId: "guild-1", ChannelId: "channel-1"},
		{GuildId: "guild-2", ChannelId: "channel-2"},
	} {
		if err := store.SetChannel(ctx, BearChannel, request); err != nil {
			t.Fatalf("SetChannel() error = %v", err)
		}
	}

	for guildID, want := range map[string]string{"guild-1": "channel-1", "guild-2": "channel-2"} {
		channelID, err := store.GetChannel(ctx, BearChannel, guildID)
		if err != nil {
			t.Fatalf("GetChannel(%q) error = %v", guildID, err)
		}
		if channelID != want {
			t.Errorf("GetChannel(%q) = %q, want %q", guildID, channelID, want)
		}
	}
}