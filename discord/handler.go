// Package discord provides Discord interaction and message handlers that wrap
// a kingshot.GiftCodeService, translating Discord events into service calls
// and formatting structured results into Discord messages.
package discord

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/mchipperfield/kingshot"
)

// serviceCallTimeout bounds how long a handler will wait on svc, so a stalled
// store or API call fails fast instead of leaving the interaction hanging.
const serviceCallTimeout = 10 * time.Second

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
				{
					Name:        "status",
					Description: "Show your registered players",
					Type:        discordgo.ApplicationCommandOptionSubCommand,
				},
				{
					Name:        "transfer",
					Description: "Transfer a player to a new kingdom",
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Options: []*discordgo.ApplicationCommandOption{
						{
							Type:        discordgo.ApplicationCommandOptionString,
							Name:        "player-id",
							Description: "Your KingShot Player ID to transfer",
							Required:    true,
						},
						{
							Type:        discordgo.ApplicationCommandOptionString,
							Name:        "new-kingdom-id",
							Description: "The new Kingdom ID",
							Required:    true,
						},
					},
				},
				{
					Name:        "unlink",
					Description: "Unlink a player ID from your Discord account",
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Options: []*discordgo.ApplicationCommandOption{
						{
							Type:        discordgo.ApplicationCommandOptionString,
							Name:        "player-id",
							Description: "The KingShot Player ID to unlink",
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

// InteractionHandler returns a handler that dispatches /player and /code
// commands, and the unlink confirmation button clicks they can trigger.
// Register this once at startup via session.AddHandler.
func InteractionHandler(svc *kingshot.GiftCodeService) func(s *discordgo.Session, i *discordgo.InteractionCreate) {
	return func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		switch i.Type {
		case discordgo.InteractionApplicationCommand:
			switch i.ApplicationCommandData().Name {
			case "player":
				subcommand := i.ApplicationCommandData().Options[0].Name
				switch subcommand {
				case "register":
					handleRegisterPlayer(s, i, svc)
				case "status":
					handlePlayerStatus(s, i, svc)
				case "transfer":
					handleTransferPlayer(s, i, svc)
				case "unlink":
					handleUnlinkPlayer(s, i, svc)
				}
			case "code":
				handleAddCode(s, i, svc)
			}
		case discordgo.InteractionMessageComponent:
			handleUnlinkConfirmation(s, i, svc)
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

	ctx, cancel := context.WithTimeout(context.Background(), serviceCallTimeout)
	defer cancel()

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

	result := svc.RegisterPlayer(ctx, req)
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
	reply(s, i, fmt.Sprintf("Code %s received: processing per guild...", newCode))

	ctx := context.Background()

	result := svc.ProcessNewCode(ctx, newCode)
	if result.Added && len(result.PlayerResults) > 0 {
		posted := postGuildRedemptionResults(s, result.Code, result.PlayerResults)
		reply(s, i, formatCodeDispatchResult(result.Code, len(posted)))
		return
	}

	formatted := formatCodeResult(result)
	for _, chunk := range chunkMessage(formatted, discordMaxMessageLen) {
		s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{Content: chunk})
	}
}

func handlePlayerStatus(s *discordgo.Session, i *discordgo.InteractionCreate, svc *kingshot.GiftCodeService) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})
	if err != nil {
		slog.Error("failed to defer interaction response for status", "error", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), serviceCallTimeout)
	defer cancel()

	players, err := svc.GetPlayersByUser(ctx, i.Member.User.ID)
	if err != nil {
		slog.Error("failed to get players for user", "error", err, "user_id", i.Member.User.ID)
		reply(s, i, "Error fetching your players.")
		return
	}

	if len(players) == 0 {
		reply(s, i, "You have no registered players.")
		return
	}

	var builder strings.Builder
	builder.WriteString("Your registered players:\n")
	for _, p := range players {
		builder.WriteString(fmt.Sprintf("- Player ID: `%s`, Kingdom ID: `%s`\n", p.PlayerID, p.KingdomID))
	}

	reply(s, i, builder.String())
}

func handleTransferPlayer(s *discordgo.Session, i *discordgo.InteractionCreate, svc *kingshot.GiftCodeService) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})
	if err != nil {
		slog.Error("failed to defer interaction response for transfer", "error", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), serviceCallTimeout)
	defer cancel()

	options := i.ApplicationCommandData().Options[0].Options
	var playerID, newKingdomID string
	for _, opt := range options {
		switch opt.Name {
		case "player-id":
			playerID = opt.StringValue()
		case "new-kingdom-id":
			newKingdomID = opt.StringValue()
		}
	}

	req := kingshot.TransferPlayerRequest{
		PlayerID:     playerID,
		NewKingdomID: newKingdomID,
		UserID:       i.Member.User.ID,
		GuildID:      i.GuildID,
	}

	result := svc.TransferPlayer(ctx, req)
	reply(s, i, formatTransferResult(result))
}

// unlinkConfirmCustomID prefixes the confirm button's custom ID; the
// playerID to unlink is appended after it.
const unlinkConfirmCustomID = "player-unlink-confirm:"

// unlinkCancelCustomID is the custom ID of the unlink flow's cancel button.
const unlinkCancelCustomID = "player-unlink-cancel"

func handleUnlinkPlayer(s *discordgo.Session, i *discordgo.InteractionCreate, svc *kingshot.GiftCodeService) {
	playerID := i.ApplicationCommandData().Options[0].Options[0].StringValue()

	// Ephemeral: only the invoking user can see or click these buttons.
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf("Are you sure you want to unlink player `%s`? It will stop receiving gift codes until it is registered again.", playerID),
			Flags:   discordgo.MessageFlagsEphemeral,
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.Button{
							Label:    "Unlink",
							Style:    discordgo.DangerButton,
							CustomID: unlinkConfirmCustomID + playerID,
						},
						discordgo.Button{
							Label:    "Cancel",
							Style:    discordgo.SecondaryButton,
							CustomID: unlinkCancelCustomID,
						},
					},
				},
			},
		},
	})
	if err != nil {
		slog.Error("failed to respond with unlink confirmation", "error", err)
	}
}

// handleUnlinkConfirmation handles clicks on the confirm/cancel buttons
// produced by handleUnlinkPlayer.
func handleUnlinkConfirmation(s *discordgo.Session, i *discordgo.InteractionCreate, svc *kingshot.GiftCodeService) {
	customID := i.MessageComponentData().CustomID

	if customID == unlinkCancelCustomID {
		err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:    "Unlink cancelled.",
				Components: []discordgo.MessageComponent{},
			},
		})
		if err != nil {
			slog.Error("failed to acknowledge unlink cancellation", "error", err)
		}
		return
	}

	playerID, ok := strings.CutPrefix(customID, unlinkConfirmCustomID)
	if !ok {
		return
	}

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate,
	})
	if err != nil {
		slog.Error("failed to defer interaction response for unlink confirmation", "error", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), serviceCallTimeout)
	defer cancel()

	req := kingshot.UnlinkPlayerRequest{
		PlayerID: playerID,
		UserID:   i.Member.User.ID,
		GuildID:  i.GuildID,
	}

	result := svc.UnlinkPlayer(ctx, req)
	respondFinal(s, i, formatUnlinkResult(result))
}

func postGuildRedemptionResults(s *discordgo.Session, code string, results []kingshot.PlayerRedeemResult) []string {
	grouped := make(map[string][]kingshot.PlayerRedeemResult)
	for _, result := range results {
		grouped[result.GuildID] = append(grouped[result.GuildID], result)
	}

	guildIDs := make([]string, 0, len(grouped))
	for guildID := range grouped {
		guildIDs = append(guildIDs, guildID)
	}
	slices.Sort(guildIDs)

	postedGuilds := make([]string, 0, len(guildIDs))
	for _, guildID := range guildIDs {
		guildResults := grouped[guildID]
		channelID, err := guildRedemptionChannel(s, guildID)
		if err != nil {
			slog.Error("failed to resolve guild channel for redemption results", "error", err, "guild_id", guildID, "code", code)
			continue
		}

		lines := make([]string, 0, len(guildResults))
		for _, result := range guildResults {
			lines = append(lines, fmt.Sprintf("Player `%s`: %s", result.PlayerID, result.Message))
		}
		message := formatRedemptionReport(code, len(guildResults), lines)
		if _, err := s.ChannelMessageSend(channelID, message); err != nil {
			slog.Error("failed to post guild redemption results", "error", err, "guild_id", guildID, "channel_id", channelID, "code", code)
			continue
		}
		postedGuilds = append(postedGuilds, guildID)
	}

	return postedGuilds
}

func guildRedemptionChannel(s *discordgo.Session, guildID string) (string, error) {
	guild, err := s.Guild(guildID)
	if err != nil {
		return "", err
	}
	if guild.SystemChannelID != "" {
		return guild.SystemChannelID, nil
	}
	if guild.PublicUpdatesChannelID != "" {
		return guild.PublicUpdatesChannelID, nil
	}
	channels, err := s.GuildChannels(guildID)
	if err != nil {
		return "", err
	}
	for _, channel := range channels {
		if channel.Type == discordgo.ChannelTypeGuildText {
			return channel.ID, nil
		}
	}
	return "", fmt.Errorf("no suitable channel found for guild %s", guildID)
}
