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
func Register(s *discordgo.Session, svc *kingshot.GiftCodeService, store kingshot.AllianceStore) {
	s.AddHandler(InteractionHandler(svc, store))
}

// GiftCodeCommands returns the slash command definitions for the KingShot gift
// code system. Register these once in the Ready handler.
func GiftCodeCommands() []*discordgo.ApplicationCommand {
	return []*discordgo.ApplicationCommand{
		{
			Name:        "player",
			Description: "Player-related commands",
			Contexts:    &[]discordgo.InteractionContextType{discordgo.InteractionContextGuild},
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
			Name:                     "code",
			Description:              "Gift code redemption commands.",
			Contexts:                 &[]discordgo.InteractionContextType{discordgo.InteractionContextGuild},
			DefaultMemberPermissions: permPointer(discordgo.PermissionAdministrator),
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "redeem",
					Description: "The gift code to add.",
					Options: []*discordgo.ApplicationCommandOption{
						{
							Type:        discordgo.ApplicationCommandOptionString,
							Name:        "code",
							Description: "The gift code to add.",
							Required:    true,
						},
					},
				},
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "channel",
					Description: "Set the channel where gift code redemption results are posted.",
					Options: []*discordgo.ApplicationCommandOption{
						{
							Type:        discordgo.ApplicationCommandOptionChannel,
							Name:        "channel",
							Description: "The channel where gift code redemption results will be posted.",
							Required:    true,
							ChannelTypes: []discordgo.ChannelType{
								discordgo.ChannelTypeGuildText,
								discordgo.ChannelTypeGuildNews,
							},
						},
					},
				},
			},
		},
	}
}

func permPointer(p int64) *int64 {
	return &p
}

// InteractionHandler returns a handler that dispatches /player and /code
// commands, and the unlink confirmation button clicks they can trigger.
// Register this once at startup via session.AddHandler.
func InteractionHandler(svc *kingshot.GiftCodeService, allianceStore kingshot.AllianceStore) func(s *discordgo.Session, i *discordgo.InteractionCreate) {
	return func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if i == nil || i.Interaction == nil {
			slog.Error("received nil Discord interaction")
			return
		}
		switch i.Type {
		case discordgo.InteractionApplicationCommand:
			data := i.ApplicationCommandData()
			if len(data.Options) == 0 {
				slog.Error("received application command without options", "command", data.Name)
				return
			}
			switch data.Name {
			case "player":
				subcommand := data.Options[0].Name
				switch subcommand {
				case "register":
					handleRegisterPlayer(s, i, svc)
				case "status":
					handlePlayerStatus(s, i, svc)
				case "transfer":
					handleTransferPlayer(s, i, svc)
				case "unlink":
					handleUnlinkPlayer(s, i)
				}
			case "code":
				subcommand := data.Options[0].Name
				switch subcommand {
				case "redeem":
					handleAddCode(s, i, svc, allianceStore)
				case "channel":
					handleSetRedemptionChannel(s, i, allianceStore)
				}
			}
		case discordgo.InteractionMessageComponent:
			handleUnlinkConfirmation(s, i, svc)
		}
	}
}

func deferInteraction(s *discordgo.Session, i *discordgo.InteractionCreate, responseType discordgo.InteractionResponseType, operation string) bool {
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: responseType}); err != nil {
		slog.Error("failed to defer interaction response", "operation", operation, "error", err)
		return false
	}
	return true
}

func serviceContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), serviceCallTimeout)
}

func handleRegisterPlayer(s *discordgo.Session, i *discordgo.InteractionCreate, svc *kingshot.GiftCodeService) {
	options := i.ApplicationCommandData().Options
	if len(options) == 0 || len(options[0].Options) < 2 {
		slog.Error("received malformed player register interaction")
		return
	}
	if !deferInteraction(s, i, discordgo.InteractionResponseDeferredChannelMessageWithSource, "register") {
		return
	}

	ctx, cancel := serviceContext()
	defer cancel()

	options = options[0].Options
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

func handleAddCode(s *discordgo.Session, i *discordgo.InteractionCreate, svc *kingshot.GiftCodeService, allianceStore kingshot.AllianceStore) {
	options := i.ApplicationCommandData().Options
	if len(options) == 0 || len(options[0].Options) < 1 {
		slog.Error("received malformed code redeem interaction")
		return
	}
	if !deferInteraction(s, i, discordgo.InteractionResponseDeferredChannelMessageWithSource, "code redeem") {
		return
	}

	newCode := options[0].Options[0].StringValue()
	reply(s, i, fmt.Sprintf("Code %s received: processing per guild...", newCode))

	ctx, cancel := serviceContext()
	defer cancel()

	result := svc.ProcessNewCode(ctx, newCode)
	if result.Added && len(result.PlayerResults) > 0 {
		posted := postGuildRedemptionResults(ctx, s, allianceStore, result.Code, result.PlayerResults)
		reply(s, i, formatCodeDispatchResult(result.Code, len(posted)))
		return
	}

	formatted := formatCodeResult(result)
	for _, chunk := range chunkMessage(formatted, discordMaxMessageLen) {
		s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{Content: chunk})
	}
}

func handlePlayerStatus(s *discordgo.Session, i *discordgo.InteractionCreate, svc *kingshot.GiftCodeService) {
	if !deferInteraction(s, i, discordgo.InteractionResponseDeferredChannelMessageWithSource, "player status") {
		return
	}

	ctx, cancel := serviceContext()
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
	options := i.ApplicationCommandData().Options
	if len(options) == 0 || len(options[0].Options) < 2 {
		slog.Error("received malformed player transfer interaction")
		return
	}
	if !deferInteraction(s, i, discordgo.InteractionResponseDeferredChannelMessageWithSource, "player transfer") {
		return
	}

	ctx, cancel := serviceContext()
	defer cancel()

	options = options[0].Options
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

func handleUnlinkPlayer(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := i.ApplicationCommandData().Options
	if len(options) == 0 || len(options[0].Options) < 1 {
		slog.Error("received malformed player unlink interaction")
		return
	}
	playerID := options[0].Options[0].StringValue()

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

	if !deferInteraction(s, i, discordgo.InteractionResponseDeferredMessageUpdate, "unlink confirmation") {
		return
	}

	ctx, cancel := serviceContext()
	defer cancel()

	req := kingshot.UnlinkPlayerRequest{
		PlayerID: playerID,
		UserID:   i.Member.User.ID,
		GuildID:  i.GuildID,
	}

	result := svc.UnlinkPlayer(ctx, req)
	respondFinal(s, i, formatUnlinkResult(result))
}

func postGuildRedemptionResults(ctx context.Context, s *discordgo.Session, store kingshot.AllianceStore, code string, results []kingshot.PlayerRedeemResult) []string {
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
		channelIDs := redemptionChannelCandidates(ctx, s, store, guildID)

		lines := make([]string, 0, len(guildResults))
		for _, result := range guildResults {
			lines = append(lines, fmt.Sprintf("Player `%s`: %s", result.PlayerID, result.Message))
		}
		message := formatRedemptionReport(code, len(guildResults), lines)
		posted := false
		for _, channelID := range channelIDs {
			if _, err := s.ChannelMessageSend(channelID, message); err != nil {
				slog.Error("failed to post guild redemption results", "error", err, "guild_id", guildID, "channel_id", channelID, "code", code)
				continue
			}
			posted = true
			break
		}
		if !posted {
			slog.Error("failed to post guild redemption results to any channel", "guild_id", guildID, "code", code)
			continue
		}
		postedGuilds = append(postedGuilds, guildID)
	}

	return postedGuilds
}

func redemptionChannelCandidates(ctx context.Context, s *discordgo.Session, store kingshot.AllianceStore, guildID string) []string {
	var candidates []string
	if store != nil {
		channelID, err := store.GetRedemptionChannel(ctx, guildID)
		if err == nil {
			candidates = appendUnique(candidates, channelID)
		} else {
			slog.Error("failed to get redemption channel, falling back to default channels", "error", err, "guild_id", guildID)
		}
	}

	defaultChannels, err := guildFindDefaultChannels(s, guildID)
	if err != nil {
		slog.Error("failed to resolve default guild channels", "error", err, "guild_id", guildID)
		return candidates
	}
	for _, channelID := range defaultChannels {
		candidates = appendUnique(candidates, channelID)
	}
	return candidates
}

func appendUnique(values []string, value string) []string {
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func guildFindDefaultChannels(s *discordgo.Session, guildID string) ([]string, error) {
	guild, err := s.Guild(guildID)
	if err != nil {
		return nil, err
	}
	channels := make([]string, 0, 2)
	if guild.SystemChannelID != "" {
		channels = appendUnique(channels, guild.SystemChannelID)
	}
	if guild.PublicUpdatesChannelID != "" {
		channels = appendUnique(channels, guild.PublicUpdatesChannelID)
	}
	guildChannels, err := s.GuildChannels(guildID)
	if err != nil {
		return channels, err
	}
	for _, channel := range guildChannels {
		if channel.Type == discordgo.ChannelTypeGuildText {
			channels = appendUnique(channels, channel.ID)
		}
	}
	return channels, nil
}

func handleSetRedemptionChannel(s *discordgo.Session, i *discordgo.InteractionCreate, store kingshot.AllianceStore) {
	options := i.ApplicationCommandData().Options
	if len(options) == 0 || len(options[0].Options) < 1 {
		slog.Error("received malformed code channel interaction")
		return
	}
	if !deferInteraction(s, i, discordgo.InteractionResponseDeferredChannelMessageWithSource, "code channel") {
		return
	}
	ctx, cancel := serviceContext()
	defer cancel()

	if store == nil {
		reply(s, i, "Unable to set redemption channel due to a database error.")
		return
	}

	channel := options[0].Options[0].ChannelValue(s)
	if channel == nil {
		reply(s, i, "Invalid channel specified.")
		return
	}

	if channel.GuildID != i.GuildID {
		reply(s, i, "The specified channel is not in this guild.")
		return
	}

	if channel.Type != discordgo.ChannelTypeGuildText && channel.Type != discordgo.ChannelTypeGuildNews {
		reply(s, i, "The specified channel is not a supported text channel.")
		return
	}

	if !botHasPermission(s, channel.ID) {
		reply(s, i, "The bot does not have permission to send messages to this channel.")
		return
	}

	req := kingshot.SetChannelRequest{
		GuildId:   i.GuildID,
		UserId:    i.Member.User.ID,
		ChannelId: channel.ID,
	}

	if err := store.SetRedemptionChannel(ctx, &req); err != nil {
		slog.Error("failed to set redemption channel", "error", err, "guild_id", i.GuildID, "channel_id", channel.ID, "user_id", i.Member.User.ID)
		reply(s, i, "Failed to set redemption channel.")
		return
	}

	slog.Info("redemption channel set", "user_id", i.Member.User.ID, "channel_id", channel.ID, "guild_id", i.GuildID)
	reply(s, i, fmt.Sprintf("Redemption channel set to <#%s>.", channel.ID))
}

func botHasPermission(s *discordgo.Session, channelID string) bool {
	permissions, err := s.State.UserChannelPermissions(s.State.User.ID, channelID)
	if err != nil {
		slog.Info("failed to get bot permissions for channel", "error", err, "channel_id", channelID, "user_id", s.State.User.ID)
		return false
	}

	return permissions&(discordgo.PermissionSendMessages|discordgo.PermissionViewChannel) == (discordgo.PermissionSendMessages | discordgo.PermissionViewChannel)
}
