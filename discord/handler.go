// Package discord provides Discord interaction and message handlers that wrap
// a kingshot.GiftCodeService, translating Discord events into service calls
// and formatting structured results into Discord messages.
package discord

import (
	"fmt"
	"log/slog"
	"regexp"

	"github.com/bwmarrin/discordgo"
	"github.com/mchipperfield/kingshot"
)

// Register adds the KingShot interaction and message handlers to s once at startup.
func Register(s *discordgo.Session, svc *kingshot.GiftCodeService, giftCodeChannelID string) {
	s.AddHandler(InteractionHandler(svc))
	s.AddHandler(MessageHandler(svc, giftCodeChannelID))
}

// GiftCodeCommands returns the slash command definitions for the KingShot gift
// code system. Register these once in the Ready handler.
func GiftCodeCommands() []*discordgo.ApplicationCommand {
	return []*discordgo.ApplicationCommand{
		{
			Name:        "register",
			Description: "Register your KingShot player ID",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "player-id",
					Description: "Your KingShot player ID",
					Required:    true,
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
		case "register":
			handleRegisterPlayer(s, i, svc)
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

	playerID := i.ApplicationCommandData().Options[0].StringValue()
	discordID := i.Member.User.ID

	result := svc.RegisterPlayer(playerID, discordID)
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

// MessageHandler returns a handler that watches channelID for bot-posted gift
// codes and triggers automatic redemption. Register once at startup.
func MessageHandler(svc *kingshot.GiftCodeService, channelID string) func(s *discordgo.Session, m *discordgo.MessageCreate) {
	codeRegex := regexp.MustCompile(`Gift Code: ([A-Z0-9]+)`)

	return func(s *discordgo.Session, m *discordgo.MessageCreate) {
		if !m.Author.Bot || m.ChannelID != channelID || m.Type != discordgo.MessageTypeDefault {
			return
		}

		slog.Info("followed channel message received", "author", m.Author.Username)

		matches := codeRegex.FindStringSubmatch(m.Content)
		if len(matches) < 2 {
			slog.Info("no gift code found in message content")
			return
		}

		newCode := matches[1]
		slog.Info("extracted gift code", "code", newCode)

		thinkingMsg, err := s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Processing new gift code: `%s`...", newCode))
		if err != nil {
			slog.Error("failed to send thinking message", "error", err)
		}

		result := svc.ProcessNewCode(newCode)
		formatted := formatCodeResult(result)

		if thinkingMsg != nil {
			_, err := s.ChannelMessageEdit(thinkingMsg.ChannelID, thinkingMsg.ID, formatted)
			if err != nil {
				slog.Error("failed to edit message with result", "error", err)
				s.ChannelMessageSend(m.ChannelID, formatted)
			}
		} else {
			s.ChannelMessageSend(m.ChannelID, formatted)
		}
	}
}
