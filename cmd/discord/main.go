package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
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

	if err := validateConfig(*token, *firestore_project); err != nil {
		logger.Log("failed to validate configuration", "error", err)
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

	svc := kingshot.NewService(playerStore, codeStore)

	giftCodeHandler := discord.NewGiftCodeHandler(svc, firestore.NewAllianceStore(client))
	bearHandler := discord.NewBearHandler(kingshot.NewBearService(firestore.NewBearStore(client)))
	commandRegistry := discord.NewCommandRegistry(giftCodeHandler, bearHandler)

	session.AddHandler(giftCodeHandler.Handle)
	session.AddHandler(bearHandler.Handle)
	session.AddHandler(commandRegistry.HandleReady)

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

func validateConfig(token, firestoreProject string) error {
	if token == "" {
		return fmt.Errorf("bot_token is required")
	}
	if firestoreProject == "" {
		return fmt.Errorf("firestore_project_id is required")
	}
	return nil
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
