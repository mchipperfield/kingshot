package main

import (
	"errors"
	"io"
	"log/slog"
	"reflect"
	"testing"

	"github.com/bwmarrin/discordgo"
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

	commands := []*discordgo.ApplicationCommand{
		{Name: "register"},
		{Name: "code"},
	}

	names := commandNameSet(commands)

	if _, ok := names["register"]; !ok {
		t.Fatalf("expected register command in set")
	}
	if _, ok := names["code"]; !ok {
		t.Fatalf("expected code command in set")
	}
	if len(names) != 2 {
		t.Fatalf("expected 2 command names, got %d", len(names))
	}
}

func TestReconcileGlobalCommandsDeletesKnownAndCreatesAll(t *testing.T) {
	t.Parallel()

	var deleted []string
	var created []string

	commands := []*discordgo.ApplicationCommand{
		{ID: "new-register", Name: "register"},
		{ID: "new-code", Name: "code"},
	}
	commandNames := commandNameSet(commands)

	reconcileGlobalCommands(
		logger{slog.New(slog.NewTextHandler(io.Discard, nil))},
		commands,
		commandNames,
		func() ([]*discordgo.ApplicationCommand, error) {
			return []*discordgo.ApplicationCommand{
				{ID: "old-register", Name: "register"},
				{ID: "old-unrelated", Name: "other"},
			}, nil
		},
		func(id, _ string) error {
			deleted = append(deleted, id)
			return nil
		},
		func(cmd *discordgo.ApplicationCommand) error {
			created = append(created, cmd.Name)
			return nil
		},
	)

	if !reflect.DeepEqual(deleted, []string{"old-register"}) {
		t.Fatalf("deleted IDs = %v, want [old-register]", deleted)
	}
	if !reflect.DeepEqual(created, []string{"register", "code"}) {
		t.Fatalf("created commands = %v, want [register code]", created)
	}
}

func TestReconcileGlobalCommandsCreatesWhenFetchFails(t *testing.T) {
	t.Parallel()

	var created []string

	commands := []*discordgo.ApplicationCommand{
		{Name: "register"},
		{Name: "code"},
	}

	reconcileGlobalCommands(
		logger{slog.New(slog.NewTextHandler(io.Discard, nil))},
		commands,
		commandNameSet(commands),
		func() ([]*discordgo.ApplicationCommand, error) {
			return nil, errors.New("fetch failed")
		},
		func(_, _ string) error {
			t.Fatal("delete should not be called when fetch fails")
			return nil
		},
		func(cmd *discordgo.ApplicationCommand) error {
			created = append(created, cmd.Name)
			return nil
		},
	)

	if !reflect.DeepEqual(created, []string{"register", "code"}) {
		t.Fatalf("created commands = %v, want [register code]", created)
	}
}
