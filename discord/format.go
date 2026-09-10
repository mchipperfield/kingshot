package discord

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/mchipperfield/kingshot"
)

// discordMaxMessageLen is the safe character limit for a single Discord message.
const discordMaxMessageLen = 1900

const (
	thumbnailURL   = "https://matthewchipperfield.dev/public/images/gopherize.png"
	supportURL     = "https://buymeacoffee.com/goaferlx"
	embedColor     = 11261619
	maxEmbedFields = 25
)

func registrationEmbed(result *kingshot.RegisterResult) *discordgo.MessageEmbed {
	embed := &discordgo.MessageEmbed{
		Title:       "Player Registered",
		Description: "Your player is ready for gift-code redemptions.",
		Color:       embedColor,
		Footer:      &discordgo.MessageEmbedFooter{Text: "https://buymeacoffee.com/goaferlx", IconURL: thumbnailURL},
		Thumbnail:   &discordgo.MessageEmbedThumbnail{URL: thumbnailURL},
		Author: &discordgo.MessageEmbedAuthor{
			Name:    "Goaf's Herald",
			IconURL: thumbnailURL,
			URL:     supportURL,
		},
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Player ID", Value: fmt.Sprintf("`%s`", result.PlayerID), Inline: true},
		},
	}
	for _, codeResult := range result.CodeResults {
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:  fmt.Sprintf("Code %s", codeResult.Code),
			Value: codeResult.Message,
		})
	}
	return embed
}

func redemptionEmbeds(code string, results []kingshot.PlayerRedeemResult) []*discordgo.MessageEmbed {
	embeds := make([]*discordgo.MessageEmbed, 0, (len(results)+maxEmbedFields-1)/maxEmbedFields)
	for start := 0; start < len(results); start += maxEmbedFields {
		end := min(start+maxEmbedFields, len(results))
		embed := &discordgo.MessageEmbed{
			Title:       fmt.Sprintf("Gift Code %s", code),
			Description: fmt.Sprintf("Redemption results for %d player(s).", len(results)),
			Color:       embedColor,
			Footer:      &discordgo.MessageEmbedFooter{Text: "https://buymeacoffee.com/goaferlx", IconURL: thumbnailURL},
			Thumbnail:   &discordgo.MessageEmbedThumbnail{URL: thumbnailURL},
			Author: &discordgo.MessageEmbedAuthor{
				Name:    "Goaf's Herald",
				IconURL: thumbnailURL,
				URL:     supportURL,
			},
		}
		for _, result := range results[start:end] {
			embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
				Name:   fmt.Sprintf("Player %s", result.PlayerID),
				Value:  result.Message,
				Inline: true,
			})
		}
		embeds = append(embeds, embed)
	}
	return embeds
}

// formatCodeResult formats a successful redemption response for Discord.
func formatCodeResult(r *kingshot.RedeemResult) string {
	if len(r.PlayerResults) == 0 {
		return fmt.Sprintf("There are no registered players, but code `%s` has been added to the active list.", r.Code)
	}

	results := make([]string, 0, len(r.PlayerResults))
	for _, pr := range r.PlayerResults {
		results = append(results, fmt.Sprintf("Player `%s`: %s", pr.PlayerID, pr.Message))
	}
	return formatRedemptionReport(r.Code, len(r.PlayerResults), results)
}

func formatCodeDispatchResult(code string, guildCount int) string {
	if guildCount == 0 {
		return fmt.Sprintf("Code `%s` has been added, but there were no guilds to post results to.", code)
	}
	return fmt.Sprintf("Code `%s` has been added and posted to %d guild(s).", code, guildCount)
}

// formatRedemptionReport builds the final summary message after a code has been
// added and redeemed for all players.
func formatRedemptionReport(code string, playerCount int, results []string) string {
	return fmt.Sprintf(
		"Code `%s` has been added to the active list.\n\n**Redemption Results for %d players:**\n%s",
		code, playerCount, strings.Join(results, "\n"),
	)
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

func replyWithEmbed(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) {
	_, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Embeds: &[]*discordgo.MessageEmbed{embed}})
	if err != nil {
		slog.Error("failed to edit interaction response", "error", err)
	}
}

// deferInteraction sends a deferred response to the interaction and returns true
// respondFinal edits the deferred interaction response with msg and strips
// any components, so a confirmation prompt's buttons can't be reused.
func respondFinal(s *discordgo.Session, i *discordgo.InteractionCreate, msg string) {
	components := []discordgo.MessageComponent{}
	_, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &msg, Components: &components})
	if err != nil {
		slog.Error("failed to edit interaction response", "error", err)
	}
}

func bearStatusEmbed(status *kingshot.BearStatus, description, setBy string) *discordgo.MessageEmbed {
	next := fmt.Sprintf("<t:%d:F>", status.Next.UTC().Unix())

	return &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("Bear Trap %s", status.Bear),
		Description: description,
		//Timestamp:   time.Now().Format(time.RFC3339),
		Color: 11261619,
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Set by %s on %s", setBy, status.SetAt.UTC().Format("02 Jan 2006 at 15:04 UTC")),
		},
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: thumbnailURL,
		},
		Author: &discordgo.MessageEmbedAuthor{
			Name:    "Goaf's Herald",
			IconURL: thumbnailURL,
			URL:     supportURL,
		},
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:  "Reminders",
				Value: status.Reminders(),
			},
			{
				Name:  "Next Bear At:",
				Value: next,
			},
			{
				Name:  "That's:",
				Value: fmt.Sprintf("<t:%d:R>", status.Next.Unix()),
			},
		},
	}
}
