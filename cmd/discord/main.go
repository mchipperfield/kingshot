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
	"github.com/mchipperfield/kingshot/kingshot"
	"github.com/peterbourgon/ff"
)

func main() {
	logger := logger{slog.Default()}

	fs := flag.NewFlagSet("", flag.ContinueOnError)
	var (
		token             = fs.String("bot_token", "", "bot authentication token")
		playerIDFile      = fs.String("player_id_file", "player_ids.csv", "file to store player IDs")
		activeCodes       = fs.String("active_codes", "PICNIC2026,AJISAI26JP,Kingshot888,VIP777", "comma-separated active gift codes")
		giftCodeChannelID = fs.String("gift_code_channel_id", "", "channel ID to listen for gift codes in")
	)

	if err := ff.Parse(fs,
		os.Args[1:],
		ff.WithEnvVarNoPrefix(),
		ff.WithConfigFile(".env"),
		ff.WithConfigFileParser(dotEnvParser),
	); err != nil {
		logger.Log("failed to parse flags", "error", err)
		os.Exit(1)
	}

	if *token == "" {
		logger.Log("failed to validate configuration", "error", "bot_token is required")
		os.Exit(1)
	}
	if *giftCodeChannelID == "" {
		logger.Log("failed to validate configuration", "error", "gift_code_channel_id flag is required")
		os.Exit(1)
	}

	session, err := discordgo.New("Bot " + *token)
	if err != nil {
		logger.Log("failed to create discord session", "error", err)
		os.Exit(1)
	}

	activeCodeList := parseActiveCodes(*activeCodes)

	ks := kingshot.NewKingShot(*playerIDFile, activeCodeList...)
	ks.Register(session, *giftCodeChannelID)

	commands := ks.GiftCodeCommands()
	commandNames := commandNameSet(commands)

	session.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		slog.Info("Bot is up!", "user", r.User.String(), "session_id", r.SessionID, "version", r.Version)

		existing, err := s.ApplicationCommands(s.State.User.ID, "")
		if err != nil {
			logger.Log("could not fetch existing global commands; continuing without cleanup", "error", err)
		} else {
			// Reconcile only the commands owned by this bot flow.
			for _, cmd := range existing {
				if _, ok := commandNames[cmd.Name]; !ok {
					continue
				}
				if err := s.ApplicationCommandDelete(s.State.User.ID, "", cmd.ID); err != nil {
					logger.Log("cannot delete command", "command", cmd.Name, "error", err)
				}
			}
		}

		for _, cmd := range commands {
			if _, err := s.ApplicationCommandCreate(s.State.User.ID, "", cmd); err != nil {
				logger.Log("cannot create command", "command", cmd.Name, "error", err)
			}
		}
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
