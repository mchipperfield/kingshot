package discord

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/mchipperfield/kingshot"
)

// r4RoleId is the Discord role that is allowed to add gift codes.
const r4RoleId = "1432032487021875373"

// discordMaxMessageLen is the safe character limit for a single Discord message.
const discordMaxMessageLen = 1900

// hasCodePermission returns true if the member is an administrator or has the r4 role.
func hasCodePermission(member *discordgo.Member) bool {
	if member.Permissions&discordgo.PermissionAdministrator == discordgo.PermissionAdministrator {
		return true
	}
	for _, roleID := range member.Roles {
		if roleID == r4RoleId {
			return true
		}
	}
	return false
}

// formatCodeResult formats a CodeResult as a Discord-ready message string.
func formatCodeResult(r kingshot.CodeResult) string {
	switch {
	case r.StoreError != nil:
		return fmt.Sprintf("Code `%s` has not been added, as we failed to open player file.", r.Code)
	case r.APIError != nil:
		return fmt.Sprintf("Failed to validate code `%s` due to an error. The code has not been added.", r.Code)
	case r.AlreadyActive:
		return fmt.Sprintf("Code `%s` is already active.", r.Code)
	case r.AlreadyExpired:
		return fmt.Sprintf("Code `%s` has expired and cannot be re-added.", r.Code)
	case r.Invalid:
		return fmt.Sprintf("Code `%s` is not valid and was not added.", r.Code)
	case r.LoginFailed:
		return fmt.Sprintf("Code `%s` could not be validated - unable to login.", r.Code)
	case r.Added && len(r.PlayerResults) == 0:
		return fmt.Sprintf("There are no registered players, but code `%s` has been added to the active list.", r.Code)
	}

	results := make([]string, 0, len(r.PlayerResults))
	for _, pr := range r.PlayerResults {
		results = append(results, fmt.Sprintf("Player `%s`: %s", pr.PlayerID, pr.Message))
	}
	return formatRedemptionReport(r.Code, len(r.PlayerResults), results)
}

// formatRedemptionReport builds the final summary message after a code has been
// added and redeemed for all players.
func formatRedemptionReport(code string, playerCount int, results []string) string {
	return fmt.Sprintf(
		"Code `%s` has been added to the active list.\n\n**Redemption Results for %d players:**\n%s",
		code, playerCount, strings.Join(results, "\n"),
	)
}

// formatRegisterResult formats a RegisterResult as a Discord-ready message string.
func formatRegisterResult(r kingshot.RegisterResult) string {
	switch {
	case r.APIError != nil:
		return "Error validating player ID. Please try again later."
	case r.InvalidPlayer:
		return "Invalid player ID provided."
	case r.StoreError != nil:
		return "Error registering player ID."
	case r.AlreadySelf:
		return "This player ID is already registered to your Discord account."
	case r.AlreadyOther:
		return "This player ID is already registered to another Discord account."
	}

	response := fmt.Sprintf(
		"**Registration Successful!**\n**Your player ID *%s* has been registered successfully!**",
		r.PlayerID,
	)
	if len(r.CodeResults) > 0 {
		lines := make([]string, 0, len(r.CodeResults))
		for _, cr := range r.CodeResults {
			lines = append(lines, fmt.Sprintf("`%s`: %s", cr.Code, cr.Message))
		}
		response += "\n\n**Gift Code Redemption Results:**\n" + strings.Join(lines, "\n")
	}
	return response
}

// chunkMessage splits s into slices of at most maxLen characters, breaking on
// newline boundaries where possible.
func chunkMessage(s string, maxLen int) []string {
	if len(s) <= maxLen {
		return []string{s}
	}
	var chunks []string
	for len(s) > 0 {
		end := maxLen
		if len(s) < end {
			end = len(s)
		}
		if idx := strings.LastIndex(s[:end], "\n"); idx != -1 {
			end = idx
		}
		chunks = append(chunks, s[:end])
		s = strings.TrimPrefix(s[end:], "\n")
	}
	return chunks
}

// reply edits the deferred interaction response with the given message.
func reply(s *discordgo.Session, i *discordgo.InteractionCreate, msg string) {
	_, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &msg})
	if err != nil {
		slog.Error("failed to edit interaction response", "error", err)
	}
}
