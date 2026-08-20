// Package discord provides Discord interaction and message handlers that wrap
// a kingshot.GiftCodeService, translating Discord events into service calls
// and formatting structured results into Discord messages.
package discord

import (
	"context"
	"errors"
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

// codeProcessingTimeout bounds sequential gift-code validation and redemption.
// Discord interaction tokens remain usable for up to 15 minutes, and the
// service deliberately rate-limits each external API request.
const codeProcessingTimeout = 15 * time.Minute

// GiftCodeHandler handles interaction requests for /player and /code commands.
// It's Handle method returns a handler that dispatches commands and button clicks to the appropriate sub-handlers,
// similar to an http.ServeHTTP() handler. Register this once at startup via session.AddHandler.
type GiftCodeHandler struct {
	service *kingshot.GiftCodeService
	store   kingshot.AllianceStore
}

// NewGiftCodeHandler creates a new GiftCodeHandler with the provided service and store.
func NewGiftCodeHandler(svc *kingshot.GiftCodeService, store kingshot.AllianceStore) *GiftCodeHandler {
	return &GiftCodeHandler{
		service: svc,
		store:   store,
	}
}

// Commands returns the application commands that this handler contributes to the registry.
func (h *GiftCodeHandler) Commands() []*discordgo.ApplicationCommand {
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
			Name:        "code",
			Description: "Gift code redemption commands.",
			Contexts:    &[]discordgo.InteractionContextType{discordgo.InteractionContextGuild},
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

// Handle returns a handler that dispatches /player and /code
// commands, and the unlink confirmation button clicks they can trigger.
// Register this once at startup via session.AddHandler.
func (h *GiftCodeHandler) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i == nil || i.Interaction == nil {
		slog.Error("received nil Discord interaction")
		return
	}
	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		command := i.ApplicationCommandData()
		if len(command.Options) == 0 {
			slog.Error("received application command without options", "command", command.Name)
			return
		}
		switch command.Name {
		case "player":
			subcommand := command.Options[0]
			switch subcommand.Name {
			case "register":
				h.handleRegisterPlayer(s, i)
			case "status":
				h.handlePlayerStatus(s, i)
			case "transfer":
				h.handleTransferPlayer(s, i)
			case "unlink":
				h.handleUnlinkPlayer(s, i)
			}
		case "code":
			subcommand := command.Options[0]
			switch subcommand.Name {
			case "redeem":
				PermissionMw(h.store)(h.handleAddCode())(s, i)
			case "channel":
				PermissionMw(h.store)(h.handleSetRedemptionChannel())(s, i)
			}
		default:
			return
		}
	case discordgo.InteractionMessageComponent:
		h.handleUnlinkConfirmation(s, i)

	}
}

func (h *GiftCodeHandler) handleRegisterPlayer(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := i.ApplicationCommandData().Options
	if len(options) == 0 || len(options[0].Options) < 2 {
		slog.Error("received malformed player register interaction")
		return
	}
	if !deferInteraction(s, i, discordgo.InteractionResponseDeferredChannelMessageWithSource, "register") {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), serviceCallTimeout)
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

	result, err := h.service.RegisterPlayer(ctx, req)
	if err != nil {
		switch {
		case errors.Is(err, kingshot.ErrMaxPlayersForKingdom):
			reply(s, i, "You have already registered the maximum number of players for this kingdom.")
			return
		case errors.Is(err, kingshot.ErrAlreadySelf):
			reply(s, i, "This player ID is already registered to your Discord account.")
			return
		case errors.Is(err, kingshot.ErrAlreadyOther):
			reply(s, i, "This player ID is already registered to another Discord account.")
			return
		}
		reply(s, i, "Error registering player.")
		return
	}

	replyWithEmbed(s, i, registrationEmbed(result))

}

func (h *GiftCodeHandler) handleAddCode() func(s *discordgo.Session, i *discordgo.InteractionCreate) {
	return func(s *discordgo.Session, i *discordgo.InteractionCreate) {
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

		ctx, cancel := context.WithTimeout(context.Background(), codeProcessingTimeout)
		defer cancel()

		result := h.service.ProcessNewCode(ctx, newCode)
		if result.Added && len(result.PlayerResults) > 0 {
			posted := postGuildRedemptionResults(ctx, s, h.store, result.Code, result.PlayerResults)
			reply(s, i, formatCodeDispatchResult(result.Code, len(posted)))
			return
		}

		formatted := formatCodeResult(result)
		for _, chunk := range chunkMessage(formatted, discordMaxMessageLen) {
			s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{Content: chunk})
		}
	}
}

func (h *GiftCodeHandler) handlePlayerStatus(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !deferInteraction(s, i, discordgo.InteractionResponseDeferredChannelMessageWithSource, "player status") {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), serviceCallTimeout)
	defer cancel()

	players, err := h.service.GetPlayersByUser(ctx, i.Member.User.ID)
	if err != nil {
		slog.Info("failed to get players by user", "error", err, "user_id", i.Member.User.ID)
		reply(s, i, "Error fetching your players.")
		return
	}

	if len(players) == 0 {
		reply(s, i, "You have no registered players.")
		return
	}

	embed := &discordgo.MessageEmbed{
		Title:       "Player Status",
		Description: fmt.Sprintf("You have %d registered player(s).", len(players)),
		Color:       embedColor,
		Thumbnail:   &discordgo.MessageEmbedThumbnail{URL: thumbnailURL},
		Author: &discordgo.MessageEmbedAuthor{
			Name:    "Goaf's Herald",
			IconURL: thumbnailURL,
		},
	}
	for _, player := range players {
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   fmt.Sprintf("Player %s", player.PlayerID),
			Value:  fmt.Sprintf("Kingdom ID: %s", player.KingdomID),
			Inline: true,
		})
	}

	replyWithEmbed(s, i, embed)
}

func (h *GiftCodeHandler) handleTransferPlayer(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := i.ApplicationCommandData().Options
	if len(options) == 0 || len(options[0].Options) < 2 {
		slog.Error("received malformed player transfer interaction")
		return
	}
	if !deferInteraction(s, i, discordgo.InteractionResponseDeferredChannelMessageWithSource, "player transfer") {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), serviceCallTimeout)
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

	player, err := h.service.TransferPlayer(ctx, req)
	if err != nil {
		switch {
		case errors.Is(err, kingshot.NotYourPlayer):
			reply(s, i, "This player is not registered to your Discord account.")
		case errors.Is(err, kingshot.ErrAlreadyInKingdom):
			reply(s, i, "This player is already in that kingdom.")
		case errors.Is(err, kingshot.ErrMaxPlayersForKingdom):
			reply(s, i, "You have already registered the maximum number of players for the new kingdom.")
		case errors.Is(err, kingshot.ErrNotFound):
			reply(s, i, "Player not found. We tried to register it for you instead:\n\n"+formatRegisterResult(*player.RegistrationResult))
		default:
			slog.Info("Failed to transfer player", "player_id", playerID, "error", err)
			reply(s, i, "Error transferring player. Please try again later.")
		}
		return
	}

	reply(s, i, fmt.Sprintf("Player `%s` has been successfully transferred to kingdom `%s`.", player.PlayerID, player.KingdomID))
}

const (
	// unlinkConfirmCustomID prefixes the confirm button's custom ID; the
	// playerID to unlink is appended after it.
	unlinkConfirmCustomID = "player-unlink-confirm:"
	// unlinkCancelCustomID is the custom ID of the unlink flow's cancel button.
	unlinkCancelCustomID = "player-unlink-cancel"
)

func (h *GiftCodeHandler) handleUnlinkPlayer(s *discordgo.Session, i *discordgo.InteractionCreate) {
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
func (h *GiftCodeHandler) handleUnlinkConfirmation(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i == nil || i.Interaction == nil || i.Type != discordgo.InteractionMessageComponent {
		slog.Error("received nil Discord interaction or message component data")
		return
	}
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

	ctx, cancel := context.WithTimeout(context.Background(), serviceCallTimeout)
	defer cancel()

	req := kingshot.UnlinkPlayerRequest{
		PlayerID: playerID,
		UserID:   i.Member.User.ID,
		GuildID:  i.GuildID,
	}

	err := h.service.UnlinkPlayer(ctx, req)
	if err != nil {
		switch {
		case errors.Is(err, kingshot.ErrNotFound):
			respondFinal(s, i, "Player not found.")
		case errors.Is(err, kingshot.NotYourPlayer):
			respondFinal(s, i, "This player is not registered to your Discord account.")
		default:
			slog.Info("Failed to unlink player", "player_id", playerID, "error", err)
			respondFinal(s, i, "Error unlinking player. Please try again later.")
		}
	}
	respondFinal(s, i, "Player has been unlinked from your Discord account.")
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
		channelID := guildChannel(ctx, s, store, kingshot.RedemptionChannel, guildID)

		if channelID == "" {
			slog.Error("no available channel for guild redemption results", "guild_id", guildID, "code", code)
			continue
		}
		posted := true
		for _, embed := range redemptionEmbeds(code, guildResults) {
			if _, err := s.ChannelMessageSendEmbed(channelID, embed); err != nil {
				slog.Error("failed to post guild redemption results", "error", err, "guild_id", guildID, "channel_id", channelID, "code", code)
				posted = false
				break
			}
		}
		if !posted {
			continue
		}
		postedGuilds = append(postedGuilds, guildID)
	}

	return postedGuilds
}

func (h *GiftCodeHandler) handleSetRedemptionChannel() func(s *discordgo.Session, i *discordgo.InteractionCreate) {
	return func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		options := i.ApplicationCommandData().Options
		if len(options) == 0 || len(options[0].Options) < 1 {
			slog.Error("received malformed code channel interaction")
			return
		}

		if !deferInteraction(s, i, discordgo.InteractionResponseDeferredChannelMessageWithSource, "code channel") {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), serviceCallTimeout)
		defer cancel()

		if h.store == nil {
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

		if err := h.store.SetChannel(ctx, kingshot.RedemptionChannel, &req); err != nil {
			slog.Error("failed to set redemption channel", "error", err, "guild_id", i.GuildID, "channel_id", channel.ID, "user_id", i.Member.User.ID)
			reply(s, i, "Failed to set redemption channel.")
			return
		}

		slog.Info("redemption channel set", "user_id", i.Member.User.ID, "channel_id", channel.ID, "guild_id", i.GuildID)
		reply(s, i, fmt.Sprintf("Redemption channel set to <#%s>.", channel.ID))
	}
}
