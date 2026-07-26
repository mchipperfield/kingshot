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

const defaultGuildID = "1423406563850190850"

func main() {
	logger := logger{slog.Default()}

	fs := flag.NewFlagSet("", flag.ContinueOnError)
	var (
		token             = fs.String("bot_token", "", "bot authentication token")
		guildID           = fs.String("guild_id", defaultGuildID, "guild ID to register commands in")
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
		logger.Log("bot_token must be provided")
		os.Exit(1)
	}
	if *giftCodeChannelID == "" {
		logger.Log("gift_code_channel_id must be provided")
		os.Exit(1)
	}

	session, err := discordgo.New("Bot " + *token)
	if err != nil {
		logger.Log("failed to create discord session", "error", err)
		os.Exit(1)
	}

	var initialCodes []string
	for _, code := range strings.Split(*activeCodes, ",") {
		code = strings.TrimSpace(code)
		if code == "" {
			continue
		}
		initialCodes = append(initialCodes, code)
	}

	ks := kingshot.NewKingShot(*playerIDFile, initialCodes...)
	ks.Register(session, *giftCodeChannelID)

	commandNames := map[string]struct{}{}
	for _, cmd := range ks.GiftCodeCommands() {
		commandNames[cmd.Name] = struct{}{}
	}

	session.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		slog.Info("Bot is up!", "user", r.User.String(), "session_id", r.SessionID, "version", r.Version)

		existing, err := s.ApplicationCommands(s.State.User.ID, *guildID)
		if err != nil {
			logger.Log("could not fetch existing commands", "guild", *guildID, "error", err)
			return
		}

		for _, cmd := range existing {
			if _, ok := commandNames[cmd.Name]; !ok {
				continue
			}
			if err := s.ApplicationCommandDelete(s.State.User.ID, *guildID, cmd.ID); err != nil {
				logger.Log("cannot delete command", "command", cmd.Name, "error", err)
			}
		}

		for _, cmd := range ks.GiftCodeCommands() {
			if _, err := s.ApplicationCommandCreate(s.State.User.ID, *guildID, cmd); err != nil {
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
