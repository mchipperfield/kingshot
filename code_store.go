package kingshot

import "context"

// CodeStore manages the lifecycle of gift codes tracked by GiftCodeService.
// Implementations must be safe to call from a single goroutine at a time;
// GiftCodeService serialises all access through its own mutex.
type CodeStore interface {
	// IsActive reports whether code is currently active.
	IsActive(ctx context.Context, code string) bool
	// IsExpired reports whether code is known to be expired.
	IsExpired(ctx context.Context, code string) bool
	// AddActive records code as active. Adding a code that is already active
	// is a no-op.
	AddActive(ctx context.Context, code string)
	// AddExpired records code as expired. Adding a code that is already
	// expired is a no-op.
	AddExpired(ctx context.Context, code string)
	// ActiveCodes returns a snapshot of all currently active codes.
	ActiveCodes(ctx context.Context) []string
	// RemoveActive removes the named codes from the active set. Codes that are
	// not present are silently ignored.
	RemoveActive(ctx context.Context, codes ...string)
}
