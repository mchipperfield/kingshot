# kingshot

Discord bot tooling for KingShot gift code registration and redemption.

## Binary layout

- `cmd/discord` — Discord entrypoint that registers `/register` and `/code` commands and processes gift codes via the KingShot API.

## Run

```bash
go run ./cmd/discord
```

Configuration is read from flags or `.env`:

- `bot_token` (required)
- `guild_id` (optional, defaults to NXG guild)
- `gift_code_channel_id` (required)
- `player_id_file` (optional, defaults to `player_ids.csv`)
