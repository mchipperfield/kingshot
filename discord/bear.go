package discord

import (
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
