# Guild-Aware Redemption Refactor Plan

## Goal
Change code redemption so active players are grouped by guild/server, redeemed per guild, and reported back per guild instead of as one flat list.

## Current Constraint
The current redemption flow only works with a flat `[]*Player` list and the domain `Player` type does not carry `GuildID`. That means the service can redeem codes for all active players, but it cannot group or deliver results by server.

## Proposed Shape
1. Keep `is_active` as the source of truth in Firestore.
2. Carry `GuildID` through the domain model so the service can group active players by guild.
3. Change code redemption to operate on `map[guildID][]players` rather than one global list.
4. Produce per-guild redemption results so Discord can post separately to each server.
5. Add a delivery target per guild if separate posting is required outside the command invocation channel.

## Refactor Steps
1. Update `kingshot.Player` to include `GuildID`.
2. Map `GuildID` in the Firestore store for `Players`, `FindByPlayerID`, and `FindByUser`.
3. Introduce a grouped redemption result type that records guild, code, and per-player outcome.
4. Refactor `ProcessNewCode` to:
   - load all active players,
   - group them by `GuildID`,
   - redeem the code once per guild group,
   - collect separate results for each guild.
5. Update Discord formatting so each guild gets its own message block.
6. Decide how to post into each server:
   - reply only in the invoking interaction, or
   - resolve a configured channel/webhook per guild and send the guild-specific summary there.

## Risks
- If guild routing is not configured, the bot can only reply in the originating interaction channel.
- Adding guild-aware result types will touch service, store, formatting, and tests together.
- Any Firestore query changes should preserve the existing `is_active` behavior.

## Validation Plan
1. Add unit tests for grouping by guild.
2. Add tests for per-guild redemption summaries.
3. Run `go test -race ./...`.
4. Verify the bot still registers and re-registers players correctly.

## Suggested Rollout
1. Land the data-model change first.
2. Land the service grouping/refactor second.
3. Land Discord delivery changes last.
4. Backfill or migrate any guild routing config before enabling separate server posting.
