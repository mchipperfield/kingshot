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
					Required:    true,
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
					Options: []*discordgo.ApplicationCommandOption{
						&discordgo.ApplicationCommandOption{
							Type:        discordgo.ApplicationCommandOptionString,
							Name:        "time",
							Description: "Time to set",
							Required:    true,
						},
					},
				},
			},
		},
	}
}
func (h *BearHandler) Register(s *discordgo.Session, registry *CommandRegistry) {

	s.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		//	i.ApplicationCommandData()
	})
	registry.Add(h.Commands()...)
}
