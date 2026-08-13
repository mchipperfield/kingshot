package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/mchipperfield/kingshot/api"
	"github.com/peterbourgon/ff"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/endpoints"
)

func main() {
	fs := flag.NewFlagSet("", flag.ContinueOnError)
	var (
		port                  = fs.Int("listen_address", 3000, "HTTP server listen address")
		discord_client_id     = fs.String("discord_client_id", "", "Discord OAuth2 client ID")
		discord_client_secret = fs.String("discord_client_secret", "", "Discord OAuth2 client secret")
		discord_redirect_uri  = fs.String("discord_redirect_uri", "http://localhost:3000/oauth/discord/callback", "Discord OAuth2 redirect URI")
		signing_key           = fs.String("signing_key", "", "Signing key for session cookies")
	)

	if err := ff.Parse(fs,
		os.Args[1:],
		ff.WithEnvVarNoPrefix(),
		ff.WithConfigFile(".env"),
		ff.WithConfigFileParser(dotEnvParser)); err != nil {
		slog.Error("failed to parse flags", "error", err)
		os.Exit(1)
	}

	cfg := oauth2.Config{
		ClientID:     *discord_client_id,
		ClientSecret: *discord_client_secret,
		Endpoint:     endpoints.Discord,
		RedirectURL:  *discord_redirect_uri,
		Scopes:       []string{"identify"},
	}
	handler := api.NewPrivacyHandler(cfg, []byte(*signing_key))

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", *port),
		Handler:           recoverPanicMw(handler),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      35 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	errChan := make(chan error, 1)
	go func() {
		slog.Info("privacy API listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errChan <- err
		}
	}()
	select {
	case err := <-errChan:
		slog.Error("listen and serve", "addr", srv.Addr, "error", err)
	case <-stop:
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("failed to shut down HTTP server", "addr", srv.Addr, "error", err)
	}

}

func recoverPanicMw(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				slog.Error("panic recovered", "error", err)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
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
