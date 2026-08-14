package discord

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/mchipperfield/kingshot"
)

type BearHandler struct {
	svc *kingshot.BearService
}

func NewBearHandler(svc *kingshot.BearService) *BearHandler {
	return &BearHandler{svc: svc}
}

func (h *BearHandler) Commands() []*discordgo.ApplicationCommand {
	return []*discordgo.ApplicationCommand{
		{
			Name:        "bear",
			Description: "Bear command",
			Type:        discordgo.ChatApplicationCommand,
			Contexts:    &[]discordgo.InteractionContextType{discordgo.InteractionContextGuild},
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "status",
					Description: "Status of the bear trap",
					Options: []*discordgo.ApplicationCommandOption{
						{
							Type:         discordgo.ApplicationCommandOptionString,
							Name:         "trap",
							Description:  "Which bear trap?",
							Required:     true,
							Autocomplete: false,
							Choices: []*discordgo.ApplicationCommandOptionChoice{
								{
									Name:  "1",
									Value: "1",
								},
								{
									Name:  "2",
									Value: "2",
								},
							},
						},
					},
				},
			},
		},
	}
}
func (h *BearHandler) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	command := i.ApplicationCommandData()
	subcommand := command.Options[0]
	switch subcommand.Name {
	case "status":
		h.bearStatus(s, i)

	}

}

func (h *BearHandler) bearStatus(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !deferInteraction(s, i, discordgo.InteractionResponseDeferredChannelMessageWithSource, "bear status") {
		return
	}

	data := i.ApplicationCommandData()
	subcommand := data.Options[0]
	trapID := subcommand.Options[0].StringValue()

	status, err := h.svc.GetBearStatus(context.Background(), i.GuildID, trapID)
	if err != nil {
		slog.Info("failed to get bear status", "error", err, "guild_id", i.GuildID, "trap_id", trapID)
		reply(s, i, "Failed to get bear status")
		return
	}

	embed := discordgo.MessageEmbed{
		Title:       fmt.Sprintf("Bear Trap %s", trapID),
		Description: "Status of the bear trap",
		Timestamp:   time.Now().Format(time.RFC3339),
		Color:       11261619,
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Set by %s on %s", status.SetBy, status.SetAt.Format(time.RFC3339)),
		},
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: thumbnailURL,
		},
		Author: &discordgo.MessageEmbedAuthor{
			Name:    "Goaf's Herald",
			IconURL: thumbnailURL,
		},
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:  "Next Bear At:",
				Value: status.Next.Format(time.RFC3339),
			},
			{
				Name:  "That's in:",
				Value: time.Until(status.Next).String(),
			},
		},
	}
	replyWithEmbed(s, i, &embed)
}
