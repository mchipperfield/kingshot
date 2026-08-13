package discord

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestCommandRegistryCollectsHandlerCommands(t *testing.T) {
	registry := NewCommandRegistry(nil)
	giftCommands := GiftCodeCommands()
	bearCommands := NewBearHandler().Commands()

	registry.Add(giftCommands...)
	registry.Add(bearCommands...)

	if len(registry.commands) != len(giftCommands)+len(bearCommands) {
		t.Fatalf("collected %d commands, want %d", len(registry.commands), len(giftCommands)+len(bearCommands))
	}
	if registry.commands[len(registry.commands)-1].Name != "bear" {
		t.Fatalf("last command = %q, want bear", registry.commands[len(registry.commands)-1].Name)
	}
}

func TestBearCommands(t *testing.T) {
	commands := NewBearHandler().Commands()
	if len(commands) != 1 {
		t.Fatalf("got %d bear commands, want 1", len(commands))
	}
	if commands[0].Type != discordgo.ChatApplicationCommand || commands[0].Name != "bear" {
		t.Fatalf("got command %#v, want bear chat command", commands[0])
	}
}
