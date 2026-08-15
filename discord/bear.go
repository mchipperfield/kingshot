package discord

import (
	"context"
	"fmt"
	"log/slog"
	"time"

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
					Name:        "status",
					Description: "Status of the bear trap",
					Options: []*discordgo.ApplicationCommandOption{
						{
							Type:         discordgo.ApplicationCommandOptionString,
							Name:         "trap",
							Description:  "Which bear trap?",
							Required:     true,
							Autocomplete: false,
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
						},
					},
				},
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "set",
					Description: "Set the bear trap",
					Options: []*discordgo.ApplicationCommandOption{
						{
							Type:         discordgo.ApplicationCommandOptionString,
							Name:         "trap",
							Description:  "Which bear trap?",
							Required:     true,
							Autocomplete: false,
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
						},
						{
							Type:         discordgo.ApplicationCommandOptionString,
							Name:         "time",
							Description:  "When to set the bear trap",
							Required:     true,
							Autocomplete: false,
						},
					},
				},
			},
		},
	}
}
func (h *BearHandler) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !deferInteraction(s, i, discordgo.InteractionResponseDeferredChannelMessageWithSource, "bear status") {
		return
	}
	command := i.ApplicationCommandData()
	subcommand := command.Options[0]
	switch subcommand.Name {
	case "status":
		h.bearStatus(s, i)
	case "set":
		h.bearSet(s, i)
	default:
		slog.Warn("unrecognised bear subcommand", "subcommand", subcommand.Name)
		reply(s, i, fmt.Sprintf("Unrecognised bear subcommand %q", subcommand.Name))
	}

}

func (h *BearHandler) ProcessBearReminders(ctx context.Context, s *discordgo.Session) {
	for {
		select {
		case <-ctx.Done():
			return
		case r, ok := <-h.svc.ReminderChannel():
			if !ok {
				slog.Warn("bear reminder channel closed")
				return
			}
			slog.Info("bear reminder", "guild_id", r.GuildID, "bear_id", r.BearID, "next", r.Next)
			channels, err := guildFindDefaultChannels(s, r.GuildID)
			if err != nil {
				slog.Error("failed to find default channels", "error", err, "guild_id", r.GuildID)
				continue
			}
			if len(channels) == 0 {
				slog.Info("no channel to send reminder to", "guild_id", r.GuildID)
			}
			channel, err := s.Channel(channels[0])
			if err != nil {
				slog.Error("failed to get channel", "error", err, "guild_id", r.GuildID)
				continue
			}

			embed := &discordgo.MessageEmbed{
				Title:       fmt.Sprintf("🐻 Bear Trap %s", r.BearID),
				Description: fmt.Sprintf("Starting <t:%d:R> — rally up!", r.Next.Unix()),
				Color:       11261619,
				Thumbnail:   &discordgo.MessageEmbedThumbnail{URL: thumbnailURL},
				Author: &discordgo.MessageEmbedAuthor{
					Name:    "Goaf's Herald",
					IconURL: thumbnailURL,
				},
				Fields: []*discordgo.MessageEmbedField{
					{Name: "Starts at", Value: fmt.Sprintf("<t:%d:F>", r.Next.Unix())},
				},
			}
			msg, err := s.ChannelMessageSendEmbed(channel.ID, embed)
			if err != nil {
				slog.Error("failed to send bear reminder", "error", err, "guild_id", r.GuildID, "bear_id", r.BearID)
				continue
			}
			slog.Info("bear reminder sent", "guild_id", r.GuildID, "bear_id", r.BearID, "message_id", msg.ID)

		}
	}
}

func (h *BearHandler) bearStatus(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()
	subcommand := data.Options[0]
	trapID := subcommand.Options[0].StringValue()

	status, err := h.svc.GetBearStatus(context.Background(), i.GuildID, trapID)
	if err != nil {
		slog.Info("failed to get bear status", "error", err, "guild_id", i.GuildID, "trap_id", trapID)
		reply(s, i, "Failed to get bear status")
		return
	}

	embed := discordgo.MessageEmbed{
		Title:       fmt.Sprintf("Bear Trap %s", trapID),
		Description: "Status of the bear trap",
		Timestamp:   time.Now().Format(time.RFC3339),
		Color:       11261619,
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Set by %s on %s", status.SetBy, status.SetAt.Format(time.RFC3339)),
		},
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: thumbnailURL,
		},
		Author: &discordgo.MessageEmbedAuthor{
			Name:    "Goaf's Herald",
			IconURL: thumbnailURL,
		},
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:  "Next Bear At:",
				Value: status.Next.Format(time.RFC3339),
			},
			{
				Name:  "That's in:",
				Value: time.Until(status.Next).String(),
			},
		},
	}
	replyWithEmbed(s, i, &embed)
}

func (h *BearHandler) bearSet(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()
	subcommand := data.Options[0]
	trapID := subcommand.Options[0].StringValue()
	timeStr := subcommand.Options[1].StringValue()

	setTime, err := time.Parse(time.RFC3339, timeStr)
	if err != nil {
		slog.Info("failed to parse time", "error", err, "guild_id", i.GuildID, "trap_id", trapID, "time", timeStr)
		reply(s, i, "Failed to parse time")
		return
	}

	err = h.svc.SetBear(context.Background(), i.GuildID, trapID, setTime, i.User.ID)
	if err != nil {
		slog.Info("failed to set bear trap", "error", err, "guild_id", i.GuildID, "trap_id", trapID)
		reply(s, i, "Failed to set bear trap")
		return
	}

	reply(s, i, fmt.Sprintf("Bear trap %s set for %s", trapID, setTime.Format(time.RFC3339)))
}
