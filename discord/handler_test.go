package discord

import (
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/mchipperfield/kingshot"
)

// TestGiftCodeCommands verifies that the command list contains exactly the
// /register and /code commands, each with at least one required option.
func TestGiftCodeCommands(t *testing.T) {
	cmds := GiftCodeCommands()

	if len(cmds) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(cmds))
	}

	names := map[string]bool{}
	for _, c := range cmds {
		names[c.Name] = true
		if len(c.Options) == 0 {
			t.Errorf("command %q has no options", c.Name)
		}
	}

	for _, want := range []string{"register", "code"} {
		if !names[want] {
			t.Errorf("expected command %q, not found in %v", want, names)
		}
	}
}

// TestInteractionHandler_IgnoresNonAppCommand verifies that the interaction
// handler silently ignores non-ApplicationCommand interactions.
func TestInteractionHandler_IgnoresNonAppCommand(t *testing.T) {
	// svc is never dereferenced for non-ApplicationCommand interactions.
	h := InteractionHandler(nil)
	h(nil, &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type: discordgo.InteractionMessageComponent,
		},
	})
}

// TestInteractionHandler_IgnoresUnknownCommand verifies that the interaction
// handler silently ignores unrecognised slash command names.
func TestInteractionHandler_IgnoresUnknownCommand(t *testing.T) {
	// svc is never dereferenced for unknown command names.
	h := InteractionHandler(nil)
	h(nil, &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type: discordgo.InteractionApplicationCommand,
			Data: discordgo.ApplicationCommandInteractionData{
				Name: "unknowncommand",
			},
		},
	})
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

// TestFormatCodeResult verifies that every CodeResult variant produces a
// message containing the expected substring.
func TestFormatCodeResult(t *testing.T) {
	tests := []struct {
		name   string
		result kingshot.CodeResult
		want   string
	}{
		{
			"store error",
			kingshot.CodeResult{Code: "X", StoreError: errSentinel},
			"failed to open player file",
		},
		{
			"api error",
			kingshot.CodeResult{Code: "X", APIError: errSentinel},
			"Failed to validate",
		},
		{
			"already active",
			kingshot.CodeResult{Code: "X", AlreadyActive: true},
			"already active",
		},
		{
			"already expired",
			kingshot.CodeResult{Code: "X", AlreadyExpired: true},
			"expired",
		},
		{
			"invalid",
			kingshot.CodeResult{Code: "X", Invalid: true},
			"not valid",
		},
		{
			"login failed",
			kingshot.CodeResult{Code: "X", LoginFailed: true},
			"unable to login",
		},
		{
			"no players",
			kingshot.CodeResult{Code: "X", Added: true},
			"no registered players",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatCodeResult(tt.result)
			if !strings.Contains(strings.ToLower(got), strings.ToLower(tt.want)) {
				t.Errorf("formatCodeResult() = %q, want to contain %q", got, tt.want)
			}
		})
	}
}

// TestFormatCodeResult_WithPlayers verifies the full redemption report path.
func TestFormatCodeResult_WithPlayers(t *testing.T) {
	r := kingshot.CodeResult{
		Code:  "TESTCODE",
		Added: true,
		PlayerResults: []kingshot.PlayerRedeemResult{
			{PlayerID: "p1", Message: "Successfully redeemed!"},
			{PlayerID: "p2", Message: "Already claimed."},
		},
	}
	got := formatCodeResult(r)
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

// TestFormatRegisterResult verifies every RegisterResult variant.
func TestFormatRegisterResult(t *testing.T) {
	tests := []struct {
		name   string
		result kingshot.RegisterResult
		want   string
	}{
		{"api error", kingshot.RegisterResult{APIError: errSentinel}, "Error validating"},
		{"invalid player", kingshot.RegisterResult{InvalidPlayer: true}, "Invalid player"},
		{"store error", kingshot.RegisterResult{StoreError: errSentinel}, "Error registering"},
		{"already self", kingshot.RegisterResult{AlreadySelf: true}, "already registered to your"},
		{"already other", kingshot.RegisterResult{AlreadyOther: true}, "already registered to another"},
		{"success no codes", kingshot.RegisterResult{Success: true, PlayerID: "pid123"}, "pid123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatRegisterResult(tt.result)
			if !strings.Contains(got, tt.want) {
				t.Errorf("formatRegisterResult() = %q, want to contain %q", got, tt.want)
			}
		})
	}
}

// TestFormatRegisterResult_WithCodeResults verifies the code redemption section.
func TestFormatRegisterResult_WithCodeResults(t *testing.T) {
	r := kingshot.RegisterResult{
		Success:  true,
		PlayerID: "pid123",
		CodeResults: []kingshot.ActiveCodeResult{
			{Code: "CODE1", Message: "Successfully redeemed!"},
		},
	}
	got := formatRegisterResult(r)
	for _, want := range []string{"pid123", "CODE1", "Successfully redeemed!"} {
		if !strings.Contains(got, want) {
			t.Errorf("result missing %q:\n%s", want, got)
		}
	}
}

// errSentinel is a non-nil error used in table-driven format tests.
var errSentinel = &sentinelError{}

type sentinelError struct{}

func (e *sentinelError) Error() string { return "sentinel error" }
