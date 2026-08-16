package kingshot

import (
	"context"
	"errors"
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

func TestInMemoryAllianceStoreKeepsAccessRolesPerGuild(t *testing.T) {
	store := NewAllianceStore()
	ctx := context.Background()

	if err := store.SetAccessRole(ctx, "guild-1", "role-1", "user-1"); err != nil {
		t.Fatalf("SetAccessRole() error = %v", err)
	}
	if err := store.SetAccessRole(ctx, "guild-2", "role-2", "user-2"); err != nil {
		t.Fatalf("SetAccessRole() error = %v", err)
	}

	for guildID, want := range map[string]string{"guild-1": "role-1", "guild-2": "role-2"} {
		roleID, err := store.GetAccessRole(ctx, guildID)
		if err != nil {
			t.Fatalf("GetAccessRole(%q) error = %v", guildID, err)
		}
		if roleID != want {
			t.Errorf("GetAccessRole(%q) = %q, want %q", guildID, roleID, want)
		}
	}

	if err := store.ResetAccessRole(ctx, "guild-1", "user-3"); err != nil {
		t.Fatalf("ResetAccessRole() error = %v", err)
	}
	if _, err := store.GetAccessRole(ctx, "guild-1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetAccessRole() after reset error = %v, want ErrNotFound", err)
	}
}
