# CodeStore Refactor Plan

## Problem

`GiftCodeService` stores active and expired codes in private in-memory slices
(`activeCodes []string`, `expiredCodes []string`) on the service struct. Every
time the bot restarts those slices are empty. The only mitigation today is
seeding active codes at startup through the `New(store, activeCodes...)` variadic
parameter, which requires a code change and a redeploy each time the set of
active codes changes.

## Target Architecture

Introduce a `CodeStore` interface in the `kingshot` package. `GiftCodeService`
depends on the interface rather than on concrete slices, so the storage
strategy can be swapped without touching service logic:

```
GiftCodeService
  └─ codeStore CodeStore  ←── interface
                               ├── inMemoryCodeStore   (in-memory, default)
                               └── <future: FirestoreCodeStore / SQLiteCodeStore>
```

### `CodeStore` interface (summary)

```go
type CodeStore interface {
    IsActive(ctx context.Context, code string) bool
    IsExpired(ctx context.Context, code string) bool
    AddActive(ctx context.Context, code string)
    AddExpired(ctx context.Context, code string)
    ActiveCodes(ctx context.Context) []string
    RemoveActive(ctx context.Context, codes ...string)
}
```

Context is included on every method so that persistent implementations can
respect cancellation and deadlines without an interface change.

## Migration Phases

### Phase 1 — Interface + in-memory implementation (this PR)

1. Add `CodeStore` interface in `code_store.go`.
2. Add `inMemoryCodeStore` in `in_memory_code_store.go`, backed by maps for O(1)
   membership checks. Uniqueness is enforced — adding a duplicate is a no-op.
3. Refactor `GiftCodeService` to hold `codeStore CodeStore` and call the
   interface methods in place of direct slice operations.
4. The `New(store PlayerStore, activeCodes ...string)` constructor signature is
   preserved: seeds are loaded into a default `inMemoryCodeStore`.
5. Update tests: remove direct access to `svc.activeCodes`/`svc.expiredCodes`;
   add focused unit tests for `inMemoryCodeStore`.

**Expected outcome:** no behaviour change; all existing tests pass.

### Phase 2 — Persistent implementation (future PR)

Implement a `FirestoreCodeStore` (or `SQLiteCodeStore`) that satisfies
`CodeStore`. On construction:

- Load all active codes from storage so the service resumes correctly after
  restarts.
- Writes go to the backing store synchronously (or buffered, if latency is a
  concern).

Wire the new store into `cmd/discord/main.go` alongside the existing
`firestore.PlayerStore`.

### Phase 3 — Optional admin tooling (future)

Add a Discord slash command (e.g. `/addcode`, `/removecode`) or a simple HTTP
admin endpoint backed by the persistent `CodeStore` so active codes can be
managed without redeploying.

## Risks and Compatibility Notes

| Risk | Mitigation |
|------|-----------|
| Service behaviour changes during refactor | Extensive existing tests; behaviour is preserved at the interface boundary. |
| Persistent store write failures during redemption | The `CodeStore` interface methods currently return nothing (like the slice appends they replace). A future persistent store should handle errors internally (log and continue, or return an error to the caller by changing the interface). |
| Race conditions | `GiftCodeService` already serialises all code-store access through `s.mu`. `inMemoryCodeStore` does not need its own lock. A future persistent store must be safe for the single goroutine that holds `s.mu`. |
| Increased startup time | Persistent store bootstrap (`ListActive` on startup) will add I/O. Acceptable given that it solves the restart problem. |

## Test Strategy

- Unit tests for `inMemoryCodeStore`: add/check/remove semantics, duplicate
  handling, empty-state behaviour.
- Existing service tests updated to construct `GiftCodeService` via `New` or
  with an explicit `inMemoryCodeStore`; no direct field access to `activeCodes`
  or `expiredCodes`.
- Race detector (`go test -race ./...`) must pass on every commit.
- For the future persistent store: integration tests behind a build tag (e.g.
  `//go:build integration`) to avoid requiring external infrastructure in CI.

## Rollout Notes

- Phase 1 is a pure refactor and can be merged to main immediately.
- Phase 2 can be deployed behind a feature flag or by switching the wiring in
  `cmd/discord/main.go` once the persistent store is tested.
- Existing `active_codes` env-var seeding in `cmd/discord/main.go` remains
  useful as a fallback for local development and can be removed once Phase 2 is
  live and verified.
