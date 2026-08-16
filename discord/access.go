package discord

import (
	"github.com/bwmarrin/discordgo"
)

func PermissionMw(next func(s *discordgo.Session, i *discordgo.InteractionCreate)) func(s *discordgo.Session, i *discordgo.InteractionCreate) {
	return func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		// 1) Check if the user has the required permissions to execute the command
		// For now its a simple check for Administrator or Manage Guild permissions, but will be extended to be a configurable setting.
		if !canConfigureGuild(i.Member) {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "You do not have permission to execute this command.",
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}
		// 3) If they do, call the next handler in the chain.
		next(s, i)
	}
}

func canConfigureGuild(member *discordgo.Member) bool {
	return member != nil && member.Permissions&(discordgo.PermissionAdministrator|discordgo.PermissionManageGuild) != 0
}
