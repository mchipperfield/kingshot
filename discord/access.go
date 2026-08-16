package discord

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/mchipperfield/kingshot"
)

type AccessHandler struct {
	store kingshot.AllianceStore
}

func NewAccessHandler(store kingshot.AllianceStore) *AccessHandler {
	return &AccessHandler{
		store: store,
	}
}

func (h *AccessHandler) Commands() []*discordgo.ApplicationCommand {
	return []*discordgo.ApplicationCommand{
		{
			Name:        "access",
			Description: "Access control commands",
			Type:        discordgo.ChatApplicationCommand,
			Contexts:    &[]discordgo.InteractionContextType{discordgo.InteractionContextGuild},
			Options: []*discordgo.ApplicationCommandOption{
				{
					Name:        "view",
					Description: "View who has permission to use the bot management commands.",
					Type:        discordgo.ApplicationCommandOptionSubCommand,
				},
				{
					Name:        "set",
					Description: "Set the minimum role required to use the bot management commands.",
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Options: []*discordgo.ApplicationCommandOption{
						{
							Name:        "role",
							Description: "Role to set for access control.",
							Type:        discordgo.ApplicationCommandOptionRole,
							Required:    true,
						},
					},
				},
				{
					Name:        "reset",
					Description: "Reset access to Manage Server or Administrator permissions.",
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Options: []*discordgo.ApplicationCommandOption{
						{
							Name:        "confirm",
							Description: "Confirm resetting access control",
							Type:        discordgo.ApplicationCommandOptionBoolean,
							Required:    true,
						},
					},
				},
			},
		},
	}
}

func (h *AccessHandler) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i == nil || i.Interaction == nil {
		return
	}
	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		command := i.ApplicationCommandData()
		if len(command.Options) == 0 {
			slog.Error("received application command without options", "command", command.Name)
			return
		}
		if command.Name != "access" {
			return
		}
		subcommand := command.Options[0]
		switch subcommand.Name {
		case "view":
			h.view(s, i)
		case "set":
			PermissionMw(h.store)(h.set)(s, i)

		case "reset":
			PermissionMw(h.store)(h.reset)(s, i)
		default:
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "Unknown subcommand.",
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
		}
	}
}

func (h *AccessHandler) view(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !deferInteraction(s, i, discordgo.InteractionResponseDeferredChannelMessageWithSource, "access view") {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), serviceCallTimeout)
	defer cancel()

	roleId, err := h.store.GetAccessRole(ctx, i.GuildID)
	if err != nil {
		if errors.Is(err, kingshot.ErrNotFound) {
			reply(s, i, "No roles configured - default settings in use. This can be configured with `/access set`.")
			return
		}
		slog.Info("Failed to get access role", "guildID", i.GuildID, "error", err)
		reply(s, i, "Failed to retrieve access control settings. Try again later.")
		return
	}

	guildID := i.Interaction.GuildID
	guild, err := s.Guild(guildID)
	if err != nil {
		slog.Info("Failed to lookup guild information", "guildID", guildID, "error", err)
		reply(s, i, "Failed to lookup guild information. Try again later.")
		return
	}

	if !slices.ContainsFunc(guild.Roles, func(r *discordgo.Role) bool {
		return r.ID == roleId
	}) {
		reply(s, i, "The configured role no longer exists. This can be re-configured with `/access set`.")
		return
	}

	reply(s, i, "The current access role is set to: <@&"+roleId+">.")
}

func (h *AccessHandler) set(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !deferInteraction(s, i, discordgo.InteractionResponseDeferredChannelMessageWithSource, "access set") {
		return
	}
	guildID := i.Interaction.GuildID
	role := i.Interaction.ApplicationCommandData().Options[0].Options[0].RoleValue(s, guildID)
	if role == nil {
		slog.Info("failed to get role from command options")
		reply(s, i, "Invalid role specified.")
		return
	}
	guild, err := s.Guild(guildID)
	if err != nil {
		slog.Info("Failed to lookup guild information", "guildID", guildID, "error", err)
		reply(s, i, "Failed to lookup guild information. Try again later.")
		return
	}
	if !slices.ContainsFunc(guild.Roles, func(r *discordgo.Role) bool {
		return r.ID == role.ID
	}) {
		reply(s, i, "The specified role does not exist in this guild.")
		return
	}

	var highest *discordgo.Role
	for _, memberRoleID := range i.Interaction.Member.Roles {
		for _, guildRole := range guild.Roles {
			if guildRole.ID != memberRoleID {
				continue
			}
			if highest == nil || guildRole.Position > highest.Position {
				highest = guildRole
			}
			break
		}
	}
	if i.Member.User.ID != guild.OwnerID && highest == nil {
		reply(s, i, "Unable to determine your highest role.")
		return
	}
	if i.Member.User.ID != guild.OwnerID && role.ID != highest.ID && role.Position >= highest.Position {
		reply(s, i, "You cannot configure access using a higher role or another role at the same position as your highest role.")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), serviceCallTimeout)
	defer cancel()

	err = h.store.SetAccessRole(ctx, guildID, role.ID, i.Interaction.Member.User.ID)
	if err != nil {
		slog.Info("Failed to set access role", "guildID", guildID, "roleID", role.ID, "error", err)
		reply(s, i, "Failed to set access role. Try again later.")
		return
	}

	reply(s, i, "Access role set to: <@&"+role.ID+">.")
	slog.Info("Access role set", "guildID", guildID, "roleID", role.ID, "userID", i.Interaction.Member.User.ID)
}

func (h *AccessHandler) reset(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !deferInteraction(s, i, discordgo.InteractionResponseDeferredChannelMessageWithSource, "access reset") {
		return
	}
	confirm := i.Interaction.ApplicationCommandData().Options[0].Options[0].BoolValue()
	if !confirm {
		reply(s, i, "Reset not confirmed. Use `/access reset confirm:true` to confirm.")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), serviceCallTimeout)
	defer cancel()

	err := h.store.ResetAccessRole(ctx, i.GuildID, i.Interaction.Member.User.ID)
	if err != nil {
		slog.Info("Failed to reset access role", "guildID", i.GuildID, "error", err)
		reply(s, i, "Failed to reset access role. Try again later.")
		return
	}
	slog.Info("Access role reset", "guildID", i.GuildID, "userID", i.Interaction.Member.User.ID)
	reply(s, i, "Access role has been reset.")
}

// PermissionMw is a middleware function that checks if the user has the required permissions to execute a command.
// Here we can centralize the permission checking logic and apply it to any command that requires it.

func PermissionMw(store kingshot.AllianceStore) func(func(*discordgo.Session, *discordgo.InteractionCreate)) func(*discordgo.Session, *discordgo.InteractionCreate) {
	return func(next func(*discordgo.Session, *discordgo.InteractionCreate)) func(*discordgo.Session, *discordgo.InteractionCreate) {
		return func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			if i == nil || i.Interaction == nil {
				slog.Error("interaction is nil")
				return
			}
			if s == nil {
				slog.Error("Discord session is nil")
				return
			}
			if i.Interaction.Member == nil {
				slog.Error("interaction or member is nil")
				respond(s, i, "Unable to verify your permissions.")
				return
			}

			// If user is administrator or manage guild permissions, no need for guild specific role checks,
			// allow them to proceed.
			if isAdmin(i.Member) {
				next(s, i)
				return
			}
			if store == nil {
				slog.Error("alliance store is nil")
				respond(s, i, "Failed to check access permissions. Try again later.")
				return
			}

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			roleID, err := store.GetAccessRole(ctx, i.GuildID)
			if err != nil {
				if errors.Is(err, kingshot.ErrNotFound) {
					// No access role set, allow default permissions (admin or manage guild)
					respond(s, i, "No access role set for your guild, you must have Manage Guild permissions.")
					return
				}
				slog.Error("Failed to get access role", "guildID", i.GuildID, "error", err)
				respond(s, i, "Failed to check access permissions. Try again later.")
				return
			}

			// check that the role still exists in the guild.
			guildRoles, err := s.GuildRoles(i.GuildID)
			if err != nil {
				slog.Error("Failed to lookup guild information", "guildID", i.GuildID, "error", err)
				respond(s, i, "Failed to lookup guild information. Try again later.")
				return
			}
			var requiredRole *discordgo.Role

			if !slices.ContainsFunc(guildRoles, func(r *discordgo.Role) bool {
				if r.ID == roleID {
					requiredRole = r
					return true
				}
				return false
			}) {
				respond(s, i, "The specified role no longer exists in this guild.")
				return
			}
			if slices.ContainsFunc(i.Member.Roles, func(r string) bool {
				return slices.ContainsFunc(guildRoles, func(gr *discordgo.Role) bool {
					return (gr.ID == r) && (gr.Position >= requiredRole.Position)
				})
			}) {
				// If they do, call the next handler in the chain.
				next(s, i)
				return
			}
			respond(s, i, "You do not have the required role to execute this command.")
		}
	}
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
