package discord

import (
	"log/slog"

	"github.com/bwmarrin/discordgo"
)

// CommandRegistry collects commands from independently registered handlers and
// reconciles the complete command set when Discord establishes a session.
type CommandRegistry struct {
	session  *discordgo.Session
	commands []*discordgo.ApplicationCommand
}

func NewCommandRegistry(session *discordgo.Session) *CommandRegistry {
	return &CommandRegistry{session: session}
}

func (r *CommandRegistry) Add(commands ...*discordgo.ApplicationCommand) {
	r.commands = append(r.commands, commands...)
}

func (r *CommandRegistry) RegisterOnReady() {
	r.session.AddHandler(func(s *discordgo.Session, ready *discordgo.Ready) {
		slog.Info("Discord session ready", "user", ready.User.String(), "session_id", ready.SessionID, "version", ready.Version)
		if _, err := s.ApplicationCommandBulkOverwrite(s.State.User.ID, "", r.commands); err != nil {
			slog.Error("failed to reconcile global commands", "error", err)
			return
		}
		slog.Info("reconciled global commands", "count", len(r.commands))
	})
}
