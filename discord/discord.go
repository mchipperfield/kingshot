package discord

import (
	"context"
	"errors"
	"log/slog"

	"github.com/bwmarrin/discordgo"
	"github.com/mchipperfield/kingshot"
)

func deferInteraction(s *discordgo.Session, i *discordgo.InteractionCreate, responseType discordgo.InteractionResponseType, operation string) bool {
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: responseType}); err != nil {
		slog.Error("failed to defer interaction response", "operation", operation, "error", err)
		return false
	}
	return true
}

func userName(s *discordgo.Session, userID string) string {
	user, err := s.User(userID)
	if err != nil {
		slog.Info("failed to look up Discord user", "error", err, "user_id", userID)
		return "Unknown user"
	}
	return user.Username
}

func botHasPermission(s *discordgo.Session, channelID string) bool {
	permissions, err := s.State.UserChannelPermissions(s.State.User.ID, channelID)
	if err != nil {
		slog.Info("failed to get bot permissions for channel", "error", err, "channel_id", channelID, "user_id", s.State.User.ID)
		return false
	}

	return permissions&(discordgo.PermissionSendMessages|discordgo.PermissionViewChannel) == (discordgo.PermissionSendMessages | discordgo.PermissionViewChannel)
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

func guildChannel(ctx context.Context, s *discordgo.Session, store kingshot.AllianceStore, kind kingshot.ChannelKind, guildID string) string {
	var candidates []string
	if store != nil {
		channelID, err := store.GetChannel(ctx, kind, guildID)
		if err == nil {
			candidates = appendUnique(candidates, channelID)
		} else if !errors.Is(err, kingshot.ErrNotFound) {
			slog.Error("failed to get configured channel, falling back to default channels", "error", err, "guild_id", guildID, "channel_kind", kind)
		}
	}

	defaultChannels, err := guildFindDefaultChannels(s, guildID)
	if err != nil {
		slog.Error("failed to resolve default guild channels", "error", err, "guild_id", guildID)
	}
	for _, channelID := range defaultChannels {
		candidates = appendUnique(candidates, channelID)
	}

	for _, channelID := range candidates {
		if botHasPermission(s, channelID) {
			return channelID
		}
	}
	return ""
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
		if channel.Type == discordgo.ChannelTypeGuildText || channel.Type == discordgo.ChannelTypeGuildNews {
			channels = appendUnique(channels, channel.ID)
		}
	}
	return channels, nil
}

func isAdmin(member *discordgo.Member) bool {
	return member != nil && member.Permissions&(discordgo.PermissionAdministrator|discordgo.PermissionManageGuild) != 0
}

func respond(s *discordgo.Session, i *discordgo.InteractionCreate, msg string) {
	if s == nil || i == nil || i.Interaction == nil {
		slog.Error("failed to respond: session or interaction is nil")
		return
	}
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: msg,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		slog.Error("failed to respond to permission check", "error", err)
	}
}
