package discord

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/mchipperfield/kingshot"
)

// TestGiftCodeCommands verifies that the command list contains exactly the
// /player and /code commands.
func TestGiftCodeCommands(t *testing.T) {
	cmds := NewGiftCodeHandler(nil, nil).Commands()

	if len(cmds) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(cmds))
	}

	commands := map[string]*discordgo.ApplicationCommand{}
	for _, c := range cmds {
		commands[c.Name] = c
		if len(c.Options) == 0 {
			t.Errorf("command %q has no options", c.Name)
		}
	}

	for _, want := range []string{"player", "code"} {
		if commands[want] == nil {
			t.Errorf("expected command %q, not found", want)
		}
	}

	codeCommand := commands["code"]
	if codeCommand.DefaultMemberPermissions != nil {
		t.Errorf("expected /code access to be controlled by runtime permission checks")
	}
	if len(codeCommand.Options) != 2 || codeCommand.Options[0].Name != "redeem" || codeCommand.Options[1].Name != "channel" {
		t.Errorf("expected /code redeem and /code channel subcommands, got %v", codeCommand.Options)
	}
}

// TestInteractionHandler_IgnoresNonAppCommand verifies that the interaction
// handler silently ignores message component interactions with an
// unrecognised custom ID.
func TestInteractionHandler_IgnoresNonAppCommand(t *testing.T) {
	// svc is never dereferenced for an unrecognised custom ID.
	h := NewGiftCodeHandler(nil, nil)
	h.Handle(nil, &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type: discordgo.InteractionMessageComponent,
			Data: discordgo.MessageComponentInteractionData{
				CustomID: "some-unrelated-component",
			},
		},
	})
}

func TestBearHandlerIgnoresMessageComponents(t *testing.T) {
	h := NewBearHandler(nil, nil)
	h.Handle(nil, &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type: discordgo.InteractionMessageComponent,
			Data: discordgo.MessageComponentInteractionData{
				CustomID: "player-unlink-confirm:player-1",
			},
		},
	})
}

// TestInteractionHandler_IgnoresUnknownCommand verifies that the interaction
// handler silently ignores unrecognised slash command names.
func TestInteractionHandler_IgnoresUnknownCommand(t *testing.T) {
	// svc is never dereferenced for unknown command names.
	h := NewGiftCodeHandler(nil, nil)
	h.Handle(nil, &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type: discordgo.InteractionApplicationCommand,
			Data: discordgo.ApplicationCommandInteractionData{
				Name: "unknowncommand",
			},
		},
	})
}

func TestInteractionHandler_IgnoresMalformedCommands(t *testing.T) {
	t.Parallel()

	tests := []discordgo.ApplicationCommandInteractionData{
		{Name: "player"},
		{Name: "player", Options: []*discordgo.ApplicationCommandInteractionDataOption{{Name: "register"}}},
		{Name: "code", Options: []*discordgo.ApplicationCommandInteractionDataOption{{Name: "redeem"}}},
		{Name: "player", Options: []*discordgo.ApplicationCommandInteractionDataOption{{Name: "transfer"}}},
		{Name: "player", Options: []*discordgo.ApplicationCommandInteractionDataOption{{Name: "unlink"}}},
		{Name: "code", Options: []*discordgo.ApplicationCommandInteractionDataOption{{Name: "channel"}}},
	}

	h := NewGiftCodeHandler(nil, nil)
	for _, data := range tests {
		h.Handle(nil, &discordgo.InteractionCreate{
			Interaction: &discordgo.Interaction{
				Type: discordgo.InteractionApplicationCommand,
				Data: data,
			},
		})
	}
}

// TestChunkMessage verifies that long messages are split correctly.
func TestChunkMessage(t *testing.T) {
	t.Run("short message returned as-is", func(t *testing.T) {
		chunks := chunkMessage("hello world", 100)
		if len(chunks) != 1 || chunks[0] != "hello world" {
			t.Errorf("got %v", chunks)
		}
	})

	t.Run("exact length not split", func(t *testing.T) {
		s := strings.Repeat("x", 100)
		chunks := chunkMessage(s, 100)
		if len(chunks) != 1 {
			t.Errorf("expected 1 chunk, got %d", len(chunks))
		}
	})

	t.Run("splits on newline boundary and round-trips", func(t *testing.T) {
		s := "line1\nline2\nline3\nline4"
		chunks := chunkMessage(s, 12)
		for _, c := range chunks {
			if len(c) > 12 {
				t.Errorf("chunk %q (%d chars) exceeds maxLen 12", c, len(c))
			}
		}
		if joined := strings.Join(chunks, "\n"); joined != s {
			t.Errorf("round-trip failed:\n got: %q\nwant: %q", joined, s)
		}
	})

	t.Run("no newlines falls back to hard cut", func(t *testing.T) {
		s := strings.Repeat("x", 50)
		chunks := chunkMessage(s, 20)
		for _, c := range chunks {
			if len(c) > 20 {
				t.Errorf("chunk len %d exceeds 20", len(c))
			}
		}
	})
}

func TestAppendUnique(t *testing.T) {
	values := []string{"system", "updates"}
	values = appendUnique(values, "")
	values = appendUnique(values, "updates")
	values = appendUnique(values, "text")

	want := []string{"system", "updates", "text"}
	if !reflect.DeepEqual(values, want) {
		t.Fatalf("appendUnique() = %v, want %v", values, want)
	}
}

// TestFormatCodeResult verifies that every CodeResult variant produces a
// message containing the expected substring.
func TestFormatCodeResult(t *testing.T) {
	result := &kingshot.RedeemResult{Code: "X", Added: true}
	got := formatCodeResult(result)
	if !strings.Contains(strings.ToLower(got), "no registered players") {
		t.Errorf("formatCodeResult() = %q, want no-player success message", got)
	}
}

// TestFormatCodeResult_WithPlayers verifies the full redemption report path.
func TestFormatCodeResult_WithPlayers(t *testing.T) {
	r := kingshot.RedeemResult{
		Code:  "TESTCODE",
		Added: true,
		PlayerResults: []kingshot.PlayerRedeemResult{
			{PlayerID: "p1", Message: "Successfully redeemed!"},
			{PlayerID: "p2", Message: "Already claimed."},
		},
	}
	got := formatCodeResult(&r)
	for _, want := range []string{"TESTCODE", "2 players", "p1", "p2", "Successfully redeemed!", "Already claimed."} {
		if !strings.Contains(got, want) {
			t.Errorf("report missing %q:\n%s", want, got)
		}
	}
}

// TestFormatRedemptionReport verifies the summary message format.
func TestFormatRedemptionReport(t *testing.T) {
	results := []string{
		"Player `p1`: Successfully redeemed!",
		"Player `p2`: Already claimed.",
	}
	got := formatRedemptionReport("TESTCODE", 2, results)
	for _, want := range []string{"TESTCODE", "2 players", "Successfully redeemed!", "Already claimed."} {
		if !strings.Contains(got, want) {
			t.Errorf("report missing %q:\n%s", want, got)
		}
	}
}

func TestRegistrationEmbed(t *testing.T) {
	embed := registrationEmbed(&kingshot.RegisterResult{
		Player: kingshot.Player{PlayerID: "player-1"},
		CodeResults: []kingshot.RegistrationResult{
			{Code: "CODE1", Message: "Redeemed successfully."},
		},
	})

	if embed.Title != "Player Registered" {
		t.Errorf("Title = %q, want Player Registered", embed.Title)
	}
	if embed.Author == nil {
		t.Fatalf("Author is nil, want support URL %q", supportURL)
	}
	if embed.Author.URL != supportURL {
		t.Errorf("Author.URL = %q, want %q", embed.Author.URL, supportURL)
	}
	if len(embed.Fields) != 2 {
		t.Fatalf("field count = %d, want 2", len(embed.Fields))
	}
	if embed.Fields[0].Value != "`player-1`" || embed.Fields[1].Name != "Code CODE1" {
		t.Errorf("unexpected fields: %#v", embed.Fields)
	}
}

func TestRedemptionEmbedsBatchesPlayers(t *testing.T) {
	results := make([]kingshot.PlayerRedeemResult, maxEmbedFields+1)
	for index := range results {
		results[index] = kingshot.PlayerRedeemResult{
			PlayerID: fmt.Sprintf("player-%d", index),
			Message:  "Redeemed successfully.",
		}
	}

	embeds := redemptionEmbeds("CODE1", results)
	if len(embeds) != 2 {
		t.Fatalf("embed count = %d, want 2", len(embeds))
	}
	if len(embeds[0].Fields) != maxEmbedFields || len(embeds[1].Fields) != 1 {
		t.Errorf("field counts = %d, %d; want %d, 1", len(embeds[0].Fields), len(embeds[1].Fields), maxEmbedFields)
	}
	for index, embed := range embeds {
		if embed.Author == nil {
			t.Errorf("embed %d Author is nil, want support URL %q", index, supportURL)
			continue
		}
		if embed.Author.URL != supportURL {
			t.Errorf("embed %d Author.URL = %q, want %q", index, embed.Author.URL, supportURL)
		}
	}
}

func TestBearStatusEmbedAuthorURL(t *testing.T) {
	embed := bearStatusEmbed(&kingshot.BearStatus{
		Bear: "1",
		Next: time.Now().Add(time.Hour),
	}, "Bear trap configured", "user")

	if embed.Author == nil {
		t.Fatalf("Author is nil, want support URL %q", supportURL)
	}
	if embed.Author.URL != supportURL {
		t.Errorf("Author.URL = %q, want %q", embed.Author.URL, supportURL)
	}
}
