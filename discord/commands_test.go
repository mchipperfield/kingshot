package discord

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestCommandRegistryStoresCommandSources(t *testing.T) {
	giftCodeHandler := NewGiftCodeHandler(nil, nil)
	bearHandler := NewBearHandler(nil, nil)
	registry := NewCommandRegistry(giftCodeHandler, bearHandler)

	if len(registry.sources) != 2 {
		t.Fatalf("stored %d sources, want 2", len(registry.sources))
	}
	if registry.sources[0] != giftCodeHandler {
		t.Fatalf("first source = %T, want GiftCodeHandler", registry.sources[0])
	}
	if registry.sources[1] != bearHandler {
		t.Fatalf("second source = %T, want BearHandler", registry.sources[1])
	}
}

func TestBearCommands(t *testing.T) {
	commands := NewBearHandler(nil, nil).Commands()
	if len(commands) != 1 {
		t.Fatalf("got %d bear commands, want 1", len(commands))
	}
	if commands[0].Type != discordgo.ChatApplicationCommand || commands[0].Name != "bear" {
		t.Fatalf("got command %#v, want bear chat command", commands[0])
	}
}

func TestCanConfigureBearChannel(t *testing.T) {
	for _, test := range []struct {
		name        string
		permissions int64
		want        bool
	}{
		{name: "administrator", permissions: discordgo.PermissionAdministrator, want: true},
		{name: "manage guild", permissions: discordgo.PermissionManageGuild, want: true},
		{name: "regular member", permissions: 0, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := canConfigureBearChannel(test.permissions); got != test.want {
				t.Errorf("canConfigureBearChannel() = %t, want %t", got, test.want)
			}
		})
	}
}
