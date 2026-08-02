package kingshot

import (
	"context"
	"time"
)

// Code represents a tracked gift code and its current state.
type Code struct {
	Value     string
	ExpiredAt time.Time // zero value means the code is still active
}

// IsExpired reports whether the code is known to be expired.
func (c Code) IsExpired() bool { return !c.ExpiredAt.IsZero() }

// CodeStore manages the lifecycle of gift codes tracked by GiftCodeService.
// Implementations must be safe to call from a single goroutine at a time;
// GiftCodeService serialises all access through its own mutex.
type CodeStore interface {
	// Find looks up a code by value. found is false when the code is not
	// tracked at all.
	Find(ctx context.Context, code string) (Code, bool)
	// Add stores a code. If a code with the same Value already exists its
	// state is updated.
	Add(ctx context.Context, code Code)
	// ActiveCodes returns a snapshot of all currently active codes.
	ActiveCodes(ctx context.Context) []string
	// RemoveActive removes the named codes from the active set. Codes that are
	// not present are silently ignored.
	RemoveActive(ctx context.Context, codes ...string)
}
