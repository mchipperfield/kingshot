package main

import (
	"bufio"
	"flag"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/bwmarrin/discordgo"
	"github.com/mchipperfield/kingshot"
	"github.com/mchipperfield/kingshot/discord"
	"github.com/mchipperfield/kingshot/firestore"
	"github.com/peterbourgon/ff"
)

func main() {
	logger := logger{slog.Default()}

	fs := flag.NewFlagSet("", flag.ContinueOnError)
	var (
		token = fs.String("bot_token", "", "bot authentication token")
	)

	if err := ff.Parse(fs,
		os.Args[1:],
		ff.WithEnvVarNoPrefix(),
		ff.WithConfigFile(".env"),
		ff.WithConfigFileParser(dotEnvParser)); err != nil {
		logger.Log("failed to parse flags", "error", err)
		os.Exit(1)
	}

	if *token == "" {
		logger.Log("failed to validate configuration", "error", "bot_token is required")
		os.Exit(1)
	}

	session, err := discordgo.New("Bot " + *token)
	if err != nil {
		logger.Log("failed to create discord session", "error", err)
		os.Exit(1)
	}

	store, err := firestore.NewPlayerStore(os.Getenv("GCP_PROJECT_ID"))
	if err != nil {
		logger.Log("failed to create firestore client", "error", err)
		os.Exit(1)
	}

	svc := kingshot.New(store, []string{"Kingshot888", "VIP777"}...)
	discord.Register(session, svc)

	commands := discord.GiftCodeCommands()
	commandNames := commandNameSet(commands)

	session.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		slog.Info("Bot is up!", "user", r.User.String(), "session_id", r.SessionID, "version", r.Version)

		reconcileGlobalCommands(
			logger,
			commands,
			commandNames,
			func() ([]*discordgo.ApplicationCommand, error) {
				return s.ApplicationCommands(s.State.User.ID, "")
			},
			func(id, _ string) error {
				return s.ApplicationCommandDelete(s.State.User.ID, "", id)
			},
			func(cmd *discordgo.ApplicationCommand) error {
				_, err := s.ApplicationCommandCreate(s.State.User.ID, "", cmd)
				return err
			},
		)
	})

	if err := session.Open(); err != nil {
		logger.Log("error opening websocket", "error", err)
		os.Exit(1)
	}
	defer session.Close()

	logger.Log("websocket established")

	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, syscall.SIGINT, syscall.SIGTERM)
	sig := <-stopChan
	logger.Log("signal received", "signal", sig)
}

type logger struct {
	*slog.Logger
}

func (l logger) Log(msg string, keyvals ...any) error {
	l.Logger.Info(msg, keyvals...)
	return nil
}

func parseActiveCodes(codes string) []string {
	var active []string
	for _, code := range strings.Split(codes, ",") {
		code = strings.TrimSpace(code)
		if code == "" {
			continue
		}
		active = append(active, code)
	}
	return active
}

func commandNameSet(commands []*discordgo.ApplicationCommand) map[string]struct{} {
	names := make(map[string]struct{}, len(commands))
	for _, cmd := range commands {
		names[cmd.Name] = struct{}{}
	}
	return names
}

func reconcileGlobalCommands(
	logger logger,
	commands []*discordgo.ApplicationCommand,
	commandNames map[string]struct{},
	fetch func() ([]*discordgo.ApplicationCommand, error),
	deleteCommand func(id, name string) error,
	createCommand func(*discordgo.ApplicationCommand) error,
) {
	for _, cmd := range commands {
		logger.Log("creating command", "command", cmd.Name)
		if err := createCommand(cmd); err != nil {
			logger.Log("cannot create command", "command", cmd.Name, "error", err)
		} else {
			logger.Log("created command", "command", cmd.Name)
		}
	}

	existing, err := fetch()
	if err != nil {
		logger.Log("could not fetch existing global commands for cleanup", "error", err)
		return
	}

	for _, cmd := range existing {
		if _, ok := commandNames[cmd.Name]; !ok {
			logger.Log("deleting stale command", "command", cmd.Name, "id", cmd.ID)
			if err := deleteCommand(cmd.ID, cmd.Name); err != nil {
				logger.Log("cannot delete stale command", "command", cmd.Name, "error", err)
			} else {
				logger.Log("deleted stale command", "command", cmd.Name, "id", cmd.ID)
			}
		}
	}
}

func dotEnvParser(r io.Reader, set func(name, value string) error) error {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(parts[0]))
		value := strings.TrimSpace(parts[1])
		if err := set(name, value); err != nil {
			return err
		}
	}
	return scanner.Err()
}
