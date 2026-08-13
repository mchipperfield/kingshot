package main

import (
	"errors"
	"io"
	"log/slog"
	"reflect"
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

func TestValidateConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		token            string
		firestoreProject string
		wantErr          string
	}{
		{name: "missing bot token", firestoreProject: "project", wantErr: "bot_token is required"},
		{name: "missing Firestore project", token: "token", wantErr: "firestore_project_id is required"},
		{name: "valid", token: "token", firestoreProject: "project"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(tt.token, tt.firestoreProject)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateConfig() error = %v", err)
				}
				return
			}
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("validateConfig() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestReconcileGlobalCommands(t *testing.T) {
	t.Parallel()

	var overwritten []*discordgo.ApplicationCommand

	commands := discord.GiftCodeCommands() // "player", "code"

	reconcileGlobalCommands(
		logger{slog.New(slog.NewTextHandler(io.Discard, nil))},
		commands,
		func(commands []*discordgo.ApplicationCommand) error {
			overwritten = commands
			return nil
		},
	)

	if !reflect.DeepEqual(overwritten, commands) {
		t.Errorf("overwritten commands = %v, want %v", overwritten, commands)
	}
}

func TestReconcileGlobalCommands_OverwriteFails(t *testing.T) {
	t.Parallel()

	commands := discord.GiftCodeCommands()

	reconcileGlobalCommands(
		logger{slog.New(slog.NewTextHandler(io.Discard, nil))},
		commands,
		func(commands []*discordgo.ApplicationCommand) error {
			return errors.New("overwrite failed")
		},
	)
}
