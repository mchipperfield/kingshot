package main

import (
	"bufio"
	"context"
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
		token             = fs.String("bot_token", "", "bot authentication token")
		firestore_project = fs.String("firestore_project_id", "", "project ID for Firestore")
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
	session.Identify.Intents = discordgo.IntentsGuilds

	client, err := firestore.NewClient(context.Background(), *firestore_project)
	if err != nil {
		logger.Log("failed to create firestore client", "error", err)
		os.Exit(1)
	}
	defer client.Close()

	playerStore := firestore.NewPlayerStore(client)
	codeStore := firestore.NewCodeStore(client)
	svc := kingshot.NewWithCodeStore(playerStore, codeStore)

	discord.Register(session, svc, firestore.NewAllianceStore(client))

	commands := discord.GiftCodeCommands()

	session.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		logger.Log("Bot is up!", "user", r.User.String(), "session_id", r.SessionID, "version", r.Version)

		reconcileGlobalCommands(
			logger,
			commands,
			func(commands []*discordgo.ApplicationCommand) error {
				_, err := s.ApplicationCommandBulkOverwrite(s.State.User.ID, "", commands)
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

func reconcileGlobalCommands(
	logger logger,
	commands []*discordgo.ApplicationCommand,
	overwrite func([]*discordgo.ApplicationCommand) error,
) {
	logger.Log("reconciling global commands", "count", len(commands))
	if err := overwrite(commands); err != nil {
		logger.Log("could not reconcile global commands", "error", err)
		return
	}
	logger.Log("reconciled global commands", "count", len(commands))
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
