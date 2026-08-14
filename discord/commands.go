package discord

import (
	"log/slog"

	"github.com/bwmarrin/discordgo"
)

type Commander interface {
	Commands() []*discordgo.ApplicationCommand
}

// CommandRegistry collects commands from independently registered handlers and
// reconciles the complete command set when Discord establishes a session.
type CommandRegistry struct {
	sources []Commander
}

func NewCommandRegistry(sources ...Commander) *CommandRegistry {
	return &CommandRegistry{sources: sources}
}

func (r *CommandRegistry) HandleReady(s *discordgo.Session, ready *discordgo.Ready) {
	var commands []*discordgo.ApplicationCommand
	for _, source := range r.sources {
		commands = append(commands, source.Commands()...)
	}
	slog.Info("Discord session ready", "user", ready.User.String(), "session_id", ready.SessionID, "version", ready.Version)
	if _, err := s.ApplicationCommandBulkOverwrite(s.State.User.ID, "518857120981057537", commands); err != nil {
		slog.Error("failed to reconcile global commands", "error", err)
		return
	}
	slog.Info("reconciled global commands", "count", len(commands))

}
