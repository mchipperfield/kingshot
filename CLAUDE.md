# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Run all tests (always use -race)
go test -race ./...

# Run a single test
go test -race -run TestGiftCodeService_ProcessNewCode .

# Run tests in a specific package
go test -race ./discord/

# Build
go build ./...

# Vet
go vet ./...

# Run the bot (reads .env in working directory, from cmd/discord/)
go run ./cmd/discord/
```

## Architecture

This is a Go Discord bot for the NXG gaming server that manages KingShot gift code redemption.

### Package layout

```
(root) package kingshot  — GiftCodeService, PlayerStore interface, result types, KingShot API client
playerstore/csv/         — csvstore.Store: CSV-backed PlayerStore implementation
discord/                 — Discord interaction/message handlers wrapping GiftCodeService
cmd/discord/             — Binary entry point: flag parsing, dependency wiring
```

### Contract boundaries

`GiftCodeService` (root package) is the core domain service. It:
- Owns all mutable state (`activeCodes`, `expiredCodes`, `mu`, HTTP client, `PlayerStore`)
- Interacts with the KingShot API (`login`, `redeemGiftCode`)
- Returns structured result types (`CodeResult`, `RegisterResult`) — no Discord-specific formatting

`PlayerStore` is an interface so the backing store can be swapped (e.g. CSV today, BoltDB later):
```go
type PlayerStore interface {
    PlayerIDs() ([]string, error)
    FindByPlayerID(playerID string) (externalID string, found bool, err error)
    AddPlayer(playerID, externalID string) error
}
```

`discord` package wraps `GiftCodeService` and handles all Discord-specific concerns (interaction deferral, permission checks, message formatting, chunking). A future `cli` package would do the same for terminal I/O without touching the service.

### How the bot is wired

`cmd/discord/main.go` builds dependencies in this order:

```go
store := csvstore.New(*playerIDFile)
svc   := kingshot.New(store, activeCodeList...)
discord.Register(session, svc)
commands := discord.GiftCodeCommands()
```

Slash commands (`/register`, `/code`) are registered in the single `Ready` callback.
**Never call `s.AddHandler` inside a `Ready` callback** — it duplicates handlers on reconnect.

### Local development

Create a `.env` file in the project root:

```
bot_token=YOUR_DISCORD_BOT_TOKEN
player_id_file=player_ids.csv       # optional, defaults to player_ids.csv
active_codes=CODE1,CODE2            # optional pre-loaded codes
```

### Test discipline

- Use `httptest.NewServer` for KingShot API mocks (see `service_test.go`)
- Use the in-memory `mapStore` / `errStore` test helpers in `service_test.go` to isolate service logic from file I/O
- `go test -race ./...` must pass clean before every commit

