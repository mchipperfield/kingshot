package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/mchipperfield/kingshot"
	"github.com/mchipperfield/kingshot/discord"
	"github.com/mchipperfield/kingshot/firestore"
	inmem "github.com/mchipperfield/kingshot/store"
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

	svc := kingshot.NewService(playerStore, codeStore, logger.Logger)

	allianceStore := firestore.NewAllianceStore(client)
	giftCodeHandler := discord.NewGiftCodeHandler(svc, allianceStore)
	bearService := kingshot.NewBearService(inmem.NewBearStore(firestore.NewBearStore(client)), logger.Logger)

	bearHandler := discord.NewBearHandler(bearService, allianceStore)
	accessHandler := discord.NewAccessHandler(allianceStore)
	commandRegistry := discord.NewCommandRegistry(giftCodeHandler, bearHandler, accessHandler)

	session.AddHandler(giftCodeHandler.Handle)
	session.AddHandler(bearHandler.Handle)
	session.AddHandler(accessHandler.Handle)
	session.AddHandler(commandRegistry.HandleReady)
	gatewayStatus := make(chan bool)
	session.AddHandler(func(s *discordgo.Session, r *discordgo.Connect) {
		logger.Log("discord session connected")
		gatewayStatus <- true
	})
	session.AddHandler(func(s *discordgo.Session, r *discordgo.Disconnect) {
		logger.Log("discord session disconnected")
		gatewayStatus <- false
	})
	gatewayUnhealthy := watchGateway(gatewayStatus, 5*time.Minute)
	// startReminders is called once when the bot is ready,
	// and starts a goroutine to listen for reminders from the BearService and send them to the appropriate guild channels.
	// Must be called only once, as discord sessions can disconnect and reconnect,
	// and we don't want to start multiple goroutines for the same reminders.
	var startReminders sync.Once
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {

		startReminders.Do(func() {
			go func() {
				if err := bearService.Start(ctx); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
					logger.Log("failed to start bear reminders", "error", err)
				}
			}()
			go bearHandler.ProcessBearReminders(ctx, s)
		})
	})

	if err := session.Open(); err != nil {
		logger.Log("error opening websocket", "error", err)
		os.Exit(1)
	}
	defer session.Close()

	logger.Log("websocket established")

	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, syscall.SIGINT, syscall.SIGTERM)
	select {
	case sig := <-stopChan:
		logger.Log("signal received, shutting down", "signal", sig)
	case <-gatewayUnhealthy:
		logger.Log("discord gateway remained disconnected, shutting down")
		cancel()
		session.Close()
		client.Close()
		os.Exit(1)
	}
}

func watchGateway(status <-chan bool, timeout time.Duration) <-chan struct{} {
	unhealthy := make(chan struct{})
	go func() {
		var timer *time.Timer
		var timeoutC <-chan time.Time

		for {
			select {
			case connected := <-status:
				if connected {
					if timer != nil {
						timer.Stop()
					}
					timer = nil
					timeoutC = nil
					continue
				}
				if timer == nil {
					timer = time.NewTimer(timeout)
					timeoutC = timer.C
				}
			case <-timeoutC:
				close(unhealthy)
				return
			}
		}
	}()
	return unhealthy
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
