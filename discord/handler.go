// Package discord provides Discord interaction and message handlers that wrap
// a kingshot.GiftCodeService, translating Discord events into service calls
// and formatting structured results into Discord messages.
package discord

import (
	"fmt"
	"log/slog"

	"github.com/bwmarrin/discordgo"
	"github.com/mchipperfield/kingshot"
)

// Register adds the KingShot interaction handler to s once at startup.
func Register(s *discordgo.Session, svc *kingshot.GiftCodeService) {
	s.AddHandler(InteractionHandler(svc))
}

// GiftCodeCommands returns the slash command definitions for the KingShot gift
// code system. Register these once in the Ready handler.
func GiftCodeCommands() []*discordgo.ApplicationCommand {
	return []*discordgo.ApplicationCommand{
		{
			Name:        "player",
			Description: "Player-related commands",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Name:        "register",
					Description: "Register your KingShot player ID",
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Options: []*discordgo.ApplicationCommandOption{
						{
							Type:        discordgo.ApplicationCommandOptionString,
							Name:        "player-id",
							Description: "Your KingShot Player ID",
							Required:    true,
						},
						{
							Type:        discordgo.ApplicationCommandOptionString,
							Name:        "kingdom-id",
							Description: "Your Kingdom ID",
							Required:    true,
						},
					},
				},
			},
		},
		{
			Name:        "code",
			Description: "Adds a new gift code for redemption.",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "code",
					Description: "The gift code to add.",
					Required:    true,
				},
			},
		},
	}
}

// InteractionHandler returns a handler that dispatches /register and /code
// interactions. Register this once at startup via session.AddHandler.
func InteractionHandler(svc *kingshot.GiftCodeService) func(s *discordgo.Session, i *discordgo.InteractionCreate) {
	return func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if i.Type != discordgo.InteractionApplicationCommand {
			return
		}

		switch i.ApplicationCommandData().Name {
		case "player":
			subcommand := i.ApplicationCommandData().Options[0].Name
			switch subcommand {
			case "register":
				handleRegisterPlayer(s, i, svc)
			}
		case "code":
			handleAddCode(s, i, svc)
		}
	}
}

func handleRegisterPlayer(s *discordgo.Session, i *discordgo.InteractionCreate, svc *kingshot.GiftCodeService) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})
	if err != nil {
		slog.Error("failed to defer interaction response for register", "error", err)
		return
	}

	options := i.ApplicationCommandData().Options[0].Options
	var playerID, kingdomID string
	for _, opt := range options {
		switch opt.Name {
		case "player-id":
			playerID = opt.StringValue()
		case "kingdom-id":
			kingdomID = opt.StringValue()
		}
	}

	req := kingshot.NewPlayerRequest{
		PlayerID:  playerID,
		KingdomID: kingdomID,
		UserID:    i.Member.User.ID,
		GuildID:   i.Interaction.GuildID,
	}

	result := svc.RegisterPlayer(req)
	reply(s, i, formatRegisterResult(result))
}

func handleAddCode(s *discordgo.Session, i *discordgo.InteractionCreate, svc *kingshot.GiftCodeService) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})
	if err != nil {
		slog.Error("failed to defer interaction response for code", "error", err)
		return
	}

	if !hasCodePermission(i.Member) {
		reply(s, i, "You do not have permission to use this command.")
		return
	}

	newCode := i.ApplicationCommandData().Options[0].StringValue()
	reply(s, i, fmt.Sprintf("Code %s received: processing...", newCode))

	result := svc.ProcessNewCode(newCode)
	formatted := formatCodeResult(result)
	for _, chunk := range chunkMessage(formatted, discordMaxMessageLen) {
		s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{Content: chunk})
	}
}
