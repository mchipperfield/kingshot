package main

import (
	"errors"
	"io"
	"log/slog"
	"reflect"
	"sort"
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/mchipperfield/kingshot/discord"
)

func TestParseActiveCodes(t *testing.T) {
	t.Parallel()

	got := parseActiveCodes(" CODE1, ,CODE2 ,  CODE3  ,")
	want := []string{"CODE1", "CODE2", "CODE3"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseActiveCodes() = %v, want %v", got, want)
	}
}

func TestCommandNameSet(t *testing.T) {
	t.Parallel()

	commands := discord.GiftCodeCommands()
	names := commandNameSet(commands)

	if _, ok := names["player"]; !ok {
		t.Fatalf("expected player command in set")
	}
	if _, ok := names["code"]; !ok {
		t.Fatalf("expected code command in set")
	}
	if len(names) != 2 {
		t.Fatalf("expected 2 command names, got %d", len(names))
	}
}

func TestReconcileGlobalCommands(t *testing.T) {
	t.Parallel()

	var created []string
	var deleted []string

	commands := discord.GiftCodeCommands() // "player", "code"
	commandNames := commandNameSet(commands)

	reconcileGlobalCommands(
		logger{slog.New(slog.NewTextHandler(io.Discard, nil))},
		commands,
		commandNames,
		func() ([]*discordgo.ApplicationCommand, error) {
			// Existing commands on discord
			return []*discordgo.ApplicationCommand{
				{ID: "1", Name: "player"},        // Will be updated by create
				{ID: "2", Name: "stale-command"}, // Should be deleted
			}, nil
		},
		func(id, name string) error {
			deleted = append(deleted, name)
			return nil
		},
		func(cmd *discordgo.ApplicationCommand) error {
			created = append(created, cmd.Name)
			return nil
		},
	)

	// Check created
	sort.Strings(created)
	if !reflect.DeepEqual(created, []string{"code", "player"}) {
		t.Errorf("created commands = %v, want [code player]", created)
	}

	// Check deleted
	if !reflect.DeepEqual(deleted, []string{"stale-command"}) {
		t.Errorf("deleted commands = %v, want [stale-command]", deleted)
	}
}

func TestReconcileGlobalCommands_FetchFails(t *testing.T) {
	t.Parallel()

	var created []string
	var deleted []string

	commands := discord.GiftCodeCommands()
	commandNames := commandNameSet(commands)

	reconcileGlobalCommands(
		logger{slog.New(slog.NewTextHandler(io.Discard, nil))},
		commands,
		commandNames,
		func() ([]*discordgo.ApplicationCommand, error) {
			return nil, errors.New("fetch failed")
		},
		func(id, name string) error {
			deleted = append(deleted, name)
			return nil
		},
		func(cmd *discordgo.ApplicationCommand) error {
			created = append(created, cmd.Name)
			return nil
		},
	)

	// Check created
	sort.Strings(created)
	if !reflect.DeepEqual(created, []string{"code", "player"}) {
		t.Errorf("created commands = %v, want [code player]", created)
	}

	// Check deleted
	if len(deleted) != 0 {
		t.Errorf("deleted commands = %v, want []", deleted)
	}
}
