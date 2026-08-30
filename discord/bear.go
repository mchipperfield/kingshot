package discord

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/mchipperfield/kingshot"
)

type BearHandler struct {
	svc   *kingshot.BearService
	store kingshot.AllianceStore
}

func NewBearHandler(svc *kingshot.BearService, store kingshot.AllianceStore) *BearHandler {
	return &BearHandler{svc: svc, store: store}
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
							Name:         "date",
							Description:  "Date of next bear trap (YYYY-MM-DD)",
							Required:     true,
							Autocomplete: false,
						},
						{
							Type:         discordgo.ApplicationCommandOptionString,
							Name:         "time",
							Description:  "Time (UTC) of next bear trap (HH:MM, 24-hour format)",
							Required:     true,
							Autocomplete: false,
						},
					},
				},
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "disable",
					Description: "Disable bear reminders",
					Options: []*discordgo.ApplicationCommandOption{
						{
							Type:         discordgo.ApplicationCommandOptionString,
							Name:         "trap",
							Description:  "Which bear trap?",
							Required:     true,
							Autocomplete: false,
							Choices: []*discordgo.ApplicationCommandOptionChoice{
								{Name: "1", Value: "1"},
								{Name: "2", Value: "2"},
							},
						},
					},
				},
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "channel",
					Description: "Set the channel for bear reminders",
					Options: []*discordgo.ApplicationCommandOption{
						{
							Type:         discordgo.ApplicationCommandOptionChannel,
							Name:         "channel",
							Description:  "Channel for bear reminders",
							Required:     true,
							ChannelTypes: []discordgo.ChannelType{discordgo.ChannelTypeGuildText, discordgo.ChannelTypeGuildNews},
						},
					},
				},
			},
		},
	}
}
func (h *BearHandler) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	command := i.ApplicationCommandData()
	if command.Name != "bear" {
		return
	}
	subcommand := command.Options[0]
	switch subcommand.Name {
	case "status":
		h.bearStatus(s, i)
	case "set":
		PermissionMw(h.store)(h.bearSet)(s, i)
	case "disable":
		PermissionMw(h.store)(h.bearDisable)(s, i)
	case "channel":
		PermissionMw(h.store)(h.bearChannel)(s, i)
	default:
		slog.Warn("unrecognised bear subcommand", "subcommand", subcommand.Name)
		reply(s, i, fmt.Sprintf("Unrecognised bear subcommand %q", subcommand.Name))
	}

}

func (h *BearHandler) ProcessBearReminders(ctx context.Context, s *discordgo.Session) {
	slog.Info("bear reminder listener started")
	for {
		select {
		case <-ctx.Done():
			return
		case r, ok := <-h.svc.ReminderChannel():
			if !ok {
				slog.Warn("bear reminder channel closed")
				return
			}

			embed := &discordgo.MessageEmbed{
				Title:       fmt.Sprintf("🐻 Bear Trap %s", r.BearID),
				Description: fmt.Sprintf("Bear starts at <t:%d:F> — rally up!", r.Next.Unix()),
				Color:       11261619,
				Thumbnail:   &discordgo.MessageEmbedThumbnail{URL: thumbnailURL},
				Footer:      &discordgo.MessageEmbedFooter{Text: "https://buymeacoffee.com/goaferlx", IconURL: thumbnailURL},
				Author: &discordgo.MessageEmbedAuthor{
					Name:    "Goaf's Herald",
					IconURL: thumbnailURL,
				},
				Fields: []*discordgo.MessageEmbedField{
					//{Name: "Starts at", Value: fmt.Sprintf("<t:%d:F>", r.Next.Unix())},
					{Name: "That's", Value: fmt.Sprintf("<t:%d:R>", r.Next.Unix())},
				},
			}
			channelCtx, cancel := context.WithTimeout(ctx, serviceCallTimeout)
			channelID := guildChannel(channelCtx, s, h.store, kingshot.BearChannel, r.GuildID)
			cancel()
			if channelID == "" {
				slog.Error("no available channel for bear reminder", "guild_id", r.GuildID, "bear_id", r.BearID)
				continue
			}
			msg, err := s.ChannelMessageSendEmbed(channelID, embed)
			if err != nil {
				slog.Error("failed to send bear reminder", "error", err, "guild_id", r.GuildID, "bear_id", r.BearID, "channel_id", channelID)
				continue
			}

			slog.Info("bear reminder sent", "guild_id", r.GuildID, "bear_id", r.BearID, "message_id", msg.ID)

		}
	}
}

func (h *BearHandler) bearStatus(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !deferInteraction(s, i, discordgo.InteractionResponseDeferredChannelMessageWithSource, "bear status") {
		return
	}
	data := i.ApplicationCommandData()
	subcommand := data.Options[0]
	trapID := subcommand.Options[0].StringValue()

	ctx, cancel := context.WithTimeout(context.Background(), serviceCallTimeout)
	defer cancel()
	status, err := h.svc.GetBearStatus(ctx, i.GuildID, trapID)
	if err != nil {
		slog.Info("failed to get bear status", "error", err, "guild_id", i.GuildID, "trap_id", trapID)
		if errors.Is(err, kingshot.ErrNotFound) {
			reply(s, i, fmt.Sprintf("Bear trap %s has not been configured yet. You can do this with \"bear set\".", trapID))
			return
		}
		reply(s, i, "Failed to get bear status")
		return
	}

	replyWithEmbed(s, i, bearStatusEmbed(status, "Status of the bear trap", userName(s, status.SetBy)))
}

func (h *BearHandler) bearSet(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !deferInteraction(s, i, discordgo.InteractionResponseDeferredChannelMessageWithSource, "bear set") {
		return
	}
	data := i.ApplicationCommandData()
	subcommand := data.Options[0]
	trapID := subcommand.Options[0].StringValue()
	dateStr := subcommand.Options[1].StringValue()
	timeStr := subcommand.Options[2].StringValue()
	dateTime := strings.Join([]string{dateStr, timeStr}, " ")

	setTime, err := time.Parse("2006-01-02 15:04", dateTime)
	if err != nil {
		slog.Info("failed to parse time", "error", err, "guild_id", i.GuildID, "trap_id", trapID, "time", timeStr)
		reply(s, i, "Invalid time format. Please use YYYY-MM-DD for date and HH:MM (24-hour) for time.")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), serviceCallTimeout)
	defer cancel()
	err = h.svc.SetBear(ctx, i.GuildID, trapID, setTime, i.Member.User.ID)
	if err != nil {
		slog.Info("failed to set bear trap", "error", err, "guild_id", i.GuildID, "trap_id", trapID)
		if errors.Is(err, kingshot.ErrSetTimeInPast) {
			reply(s, i, "The bear trap time must be in the future.")
			return
		}
		if errors.Is(err, kingshot.ErrInvalidBear) {
			reply(s, i, "Please select a valid bear trap. Valid options are 1 or 2.")
			return
		}
		reply(s, i, "Failed to set bear trap - please try again later.")
		return
	}

	status := &kingshot.BearStatus{
		Bear:             trapID,
		GuildID:          i.GuildID,
		SetBy:            i.Member.User.ID,
		SetAt:            time.Now(),
		Next:             setTime,
		RemindersEnabled: true,
	}
	replyWithEmbed(s, i, bearStatusEmbed(status, "Bear trap configured", userName(s, status.SetBy)))
}

func (h *BearHandler) bearDisable(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !deferInteraction(s, i, discordgo.InteractionResponseDeferredChannelMessageWithSource, "bear disable") {
		return
	}
	trapID := i.ApplicationCommandData().Options[0].Options[0].StringValue()
	ctx, cancel := context.WithTimeout(context.Background(), serviceCallTimeout)
	defer cancel()
	if err := h.svc.DisableBearReminders(ctx, i.GuildID, trapID); err != nil {
		slog.Info("failed to disable bear reminders", "error", err, "guild_id", i.GuildID, "trap_id", trapID)
		if errors.Is(err, kingshot.ErrNotFound) {
			reply(s, i, fmt.Sprintf("Bear trap %s has not been configured yet.", trapID))
			return
		}
		reply(s, i, "Failed to disable bear reminders - please try again later.")
		return
	}

	reply(s, i, fmt.Sprintf("Bear reminders disabled for trap %s.", trapID))
}

func (h *BearHandler) bearChannel(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !deferInteraction(s, i, discordgo.InteractionResponseDeferredChannelMessageWithSource, "bear channel") {
		return
	}
	if h.store == nil {
		reply(s, i, "Unable to set bear reminder channel due to a database error.")
		return
	}

	options := i.ApplicationCommandData().Options
	if len(options) == 0 || len(options[0].Options) != 1 {
		slog.Error("received malformed bear channel interaction")
		reply(s, i, "Invalid channel specified.")
		return
	}

	channel := options[0].Options[0].ChannelValue(s)
	if channel == nil || channel.GuildID != i.GuildID {
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

	ctx, cancel := context.WithTimeout(context.Background(), serviceCallTimeout)
	defer cancel()
	if err := h.store.SetChannel(ctx, kingshot.BearChannel, &kingshot.SetChannelRequest{
		GuildId:   i.GuildID,
		ChannelId: channel.ID,
		UserId:    i.Member.User.ID,
	}); err != nil {
		slog.Error("failed to set bear reminder channel", "error", err, "guild_id", i.GuildID, "channel_id", channel.ID)
		reply(s, i, "Failed to set bear reminder channel.")
		return
	}

	slog.Info("bear reminder channel set", "guild_id", i.GuildID, "channel_id", channel.ID, "user_id", i.Member.User.ID)
	reply(s, i, fmt.Sprintf("Bear reminder channel set to <#%s>.", channel.ID))
}

func userName(s *discordgo.Session, userID string) string {
	user, err := s.User(userID)
	if err != nil {
		slog.Info("failed to look up Discord user", "error", err, "user_id", userID)
		return "Unknown user"
	}
	return user.Username
}

func bearStatusEmbed(status *kingshot.BearStatus, description, setBy string) *discordgo.MessageEmbed {
	next := fmt.Sprintf("<t:%d:F>", status.Next.UTC().Unix())

	return &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("Bear Trap %s", status.Bear),
		Description: description,
		//Timestamp:   time.Now().Format(time.RFC3339),
		Color: 11261619,
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Set by %s on %s", setBy, status.SetAt.UTC().Format("02 Jan 2006 at 15:04 UTC")),
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
				Name:  "Reminders",
				Value: status.Reminders(),
			},
			{
				Name:  "Next Bear At:",
				Value: next,
			},
			{
				Name:  "That's:",
				Value: fmt.Sprintf("<t:%d:R>", status.Next.Unix()),
			},
		},
	}
}
