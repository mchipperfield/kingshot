package discord

import "github.com/bwmarrin/discordgo"

type BearHandler struct {
}

func NewBearHandler() *BearHandler {
	return &BearHandler{}
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
					Name:        "config",
					Description: "Config to modify",
				},
			},
		},
	}
}
func (h *BearHandler) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {

	//	i.ApplicationCommandData()

}
