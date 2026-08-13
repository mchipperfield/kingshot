# Discord Bot Refactor Plan

This plan addresses the Discord bot review findings without adding product features. Each phase should leave the bot behavior unchanged except where the current behavior is incorrect or failure-prone.

## Goals

- Prevent duplicate global slash commands after reconnects or restarts.
- Make Firestore failures visible to the service and Discord users.
- Preserve request cancellation and deadlines through service, Firestore, and HTTP calls.
- Make channel fallback deterministic and resilient to send failures.
- Handle malformed interaction payloads without panics.
- Reduce repeated handler and service construction code.
- Add focused tests around corrected behavior.

## Non-goals

- Adding new Discord commands or user-facing capabilities.
- Changing player limits, redemption rules, or Firestore data ownership.
- Replacing DiscordGo, Firestore, or the existing service architecture.
- Broad formatting or unrelated package rewrites.

## Phase 1: Command Reconciliation

**Owner:** `cmd/discord/main.go`

### Work

- Fetch the existing global commands before creating or editing commands.
- Match commands by name.
- Update existing commands instead of creating duplicates.
- Create only commands that do not exist.
- Delete stale commands that are no longer in `GiftCodeCommands`.
- Decide whether to use DiscordGo's bulk overwrite operation if it gives the same behavior with less code.

### Acceptance criteria

- A reconnect does not create duplicate `player` or `code` commands.
- Changed command definitions are applied.
- Stale commands are removed.
- A fetch or update failure is logged with enough context to diagnose it.

### Tests

- Existing desired commands are updated, not created again.
- Missing desired commands are created.
- Stale commands are deleted.
- Fetch, create, update, and delete failures are handled without preventing unrelated reconciliation work where appropriate.

## Phase 2: Persistence Error Propagation

**Owners:** `store.go`, `service.go`, `firestore/code.go`, `firestore/player.go`

### Work

- Change `CodeStore` methods that perform or depend on persistence to return errors:

```go
Find(ctx context.Context, code string) (*Code, bool, error)
Add(ctx context.Context, code Code) error
ActiveCodes(ctx context.Context) ([]string, error)
RemoveActive(ctx context.Context, codes ...string) error
```

- Update the in-memory implementation and all tests.
- Propagate Firestore read, decode, and write failures through `GiftCodeService`.
- Make `FindByUser` return decode failures instead of logging and continuing with partial data.
- Preserve existing result types where possible; add only the smallest error representation needed by callers.

### Acceptance criteria

- A Firestore read/write failure never appears as “code added,” “code already known,” or another successful result.
- A player decode failure cannot silently reduce the player list used for limits or status.
- Existing successful workflows retain their current behavior.

### Tests

- Code-store read failure.
- Code-store add failure.
- Active-code read failure.
- Remove-active failure.
- Player decode failure from `FindByUser`.
- Service result propagation for each failure.

## Phase 3: Context Propagation

**Owners:** `discord/handler.go`, `service.go`

### Work

- Create a timeout context at each Discord command boundary.
- Pass that context to `ProcessNewCode`, `SetRedemptionChannel`, and all other service/store calls.
- Replace `context.Background()` inside service operations with the received context.
- Ensure the first KingShot validation request in `ProcessNewCode` uses the caller's context.
- Pass a context into channel lookup and result posting where the APIs permit it.
- Keep timeouts centralized rather than introducing a different timeout per handler.

### Acceptance criteria

- Cancellation stops in-flight Firestore and HTTP work.
- No request-scoped operation uses `context.Background()` after receiving a caller context.
- Existing timeout behavior remains bounded and predictable.

### Tests

- Cancelled context reaches the store.
- Cancelled context reaches the KingShot HTTP request.
- Code processing does not continue after its command context expires.

## Phase 4: Redemption Channel Resolution

**Owner:** `discord/handler.go`

### Work

- Represent the configured channel and default candidates as an ordered list.
- Check that candidate channels are usable before sending where practical.
- If sending to a candidate fails, continue to the next eligible candidate.
- Preserve the current order: configured channel, system channel, public-updates channel, then a suitable guild text channel.
- Report a guild as posted only after the message send succeeds.

### Acceptance criteria

- A configured but unusable channel does not prevent fallback.
- A failed send is not counted as a successful guild post.
- The fallback order remains stable.

### Tests

- Configured channel succeeds.
- Configured channel fails and a default channel succeeds.
- System channel is unavailable or unusable and the next candidate is tried.
- No usable channel produces a logged failure and no successful guild count.

## Phase 5: Interaction Input Guards

**Owner:** `discord/handler.go`

### Work

- Guard access to command options and subcommand options before indexing.
- Validate the expected command and subcommand shape before dispatch.
- Handle malformed or unknown component IDs consistently.
- Return a controlled response or log-and-ignore malformed payloads instead of panicking.
- Keep Discord's registered command definitions as the source of expected input shape.

### Acceptance criteria

- Malformed application-command payloads do not panic.
- Missing player IDs, kingdom IDs, codes, or channels produce a controlled failure.
- Unknown commands and component IDs remain harmless.

### Tests

- Missing top-level options.
- Missing subcommand options.
- Missing required option values.
- Unknown command and component payloads.

## Phase 6: Handler Consolidation

**Owner:** `discord/handler.go`

### Work

- Extract shared interaction response/defer logic.
- Extract creation of the standard service timeout context.
- Add small typed option helpers for the repeated player and code argument extraction.
- Centralize repeated service-error response behavior where the user-facing wording is identical.
- Keep command-specific validation and formatting in the individual handlers.

### Acceptance criteria

- Repeated lifecycle code is reduced without hiding command behavior behind a large generic abstraction.
- The handlers remain easy to read from Discord command to service call to response.
- User-facing messages remain unchanged unless a current error path is being corrected.

### Tests

- Preserve the existing command shape and formatting tests.
- Add focused tests for option extraction helpers.

## Phase 7: Constructor and Startup Cleanup

**Owners:** `service.go`, `cmd/discord/main.go`

### Work

- Validate the single `NewService` constructor and keep its dependencies explicit.
- Validate all required Discord bot startup configuration, including `firestore_project_id`.
- Use consistent configuration error messages and logger behavior.
- Keep client ownership and shutdown behavior explicit.

### Acceptance criteria

- Both service constructors configure the same HTTP timeout, rate limiter, and redemption URL.
- Missing required startup configuration fails before opening the Discord session.
- Firestore and Discord clients close during normal shutdown.

### Tests

- Constructor equivalence for shared defaults.
- Configuration validation for missing token and project ID.
- Existing startup helper tests continue to pass.

## Validation After Each Phase

Run the narrowest relevant tests first, then the full suite:

```bash
go test ./discord ./cmd/discord

go test ./...
go vet ./...
```

Review the diff after each phase and keep each phase in a separate commit if the work is later committed. Do not combine behavior fixes with broad formatting changes.
