package firestore

import (
	"context"
	"log/slog"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/mchipperfield/kingshot"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// CodeStore is a Firestore-backed implementation of kingshot.CodeStore.
// Each code is stored as a document in the "codes" collection, keyed by the
// code value. Expired codes (added via Add with a non-zero ExpiredAt) are
// retained so Find can detect them; codes removed via RemoveActive are deleted
// entirely, matching the in-memory semantics.
type CodeStore struct {
	client *firestore.Client
}

// codeDoc is the Firestore document shape for a gift code.
type codeDoc struct {
	Value     string    `firestore:"value"`
	ExpiredAt time.Time `firestore:"expired_at"`
	IsActive  bool      `firestore:"is_active"`
}

// NewCodeStore returns a CodeStore that stores codes in the given Firestore
// client. The client is typically shared with the PlayerStore for the same
// GCP project.
func NewCodeStore(client *firestore.Client) *CodeStore {
	return &CodeStore{client: client}
}

func (cs *CodeStore) collection() *firestore.CollectionRef {
	return cs.client.Collection("codes")
}

// Find looks up a code by value. found is false when the code is not tracked
// at all (never added, or previously removed via RemoveActive). Expired codes
// that were added explicitly are returned with found=true so callers can
// detect the AlreadyExpired case.
func (cs *CodeStore) Find(ctx context.Context, code string) (kingshot.Code, bool) {
	snap, err := cs.collection().Doc(code).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return kingshot.Code{}, false
		}
		slog.Error("CodeStore.Find: failed to get code", "code", code, "error", err)
		return kingshot.Code{}, false
	}
	var d codeDoc
	if err := snap.DataTo(&d); err != nil {
		slog.Error("CodeStore.Find: failed to decode code document", "code", code, "error", err)
		return kingshot.Code{}, false
	}
	return kingshot.Code{Value: d.Value, ExpiredAt: d.ExpiredAt}, true
}

// Add stores code. If a document with the same Value already exists its state
// is overwritten. Codes whose ExpiredAt is non-zero are stored with
// is_active=false so they are excluded from ActiveCodes queries.
func (cs *CodeStore) Add(ctx context.Context, code kingshot.Code) {
	_, err := cs.collection().Doc(code.Value).Set(ctx, map[string]any{
		"value":      code.Value,
		"expired_at": code.ExpiredAt,
		"is_active":  !code.IsExpired(),
	})
	if err != nil {
		slog.Error("CodeStore.Add: failed to store code", "code", code.Value, "error", err)
	}
}

// ActiveCodes returns the values of all codes that are currently active (i.e.
// not expired and not removed).
func (cs *CodeStore) ActiveCodes(ctx context.Context) []string {
	var codes []string
	iter := cs.collection().Where("is_active", "==", true).Documents(ctx)
	for {
		snap, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			slog.Error("CodeStore.ActiveCodes: iteration error", "error", err)
			break
		}
		var d codeDoc
		if err := snap.DataTo(&d); err != nil {
			slog.Error("CodeStore.ActiveCodes: failed to decode code document", "error", err)
			continue
		}
		codes = append(codes, d.Value)
	}
	return codes
}

// RemoveActive deletes the named codes from Firestore entirely. Codes that are
// not present are silently ignored. After removal, Find will return found=false
// for those codes, matching the in-memory semantics.
func (cs *CodeStore) RemoveActive(ctx context.Context, codes ...string) {
	for _, code := range codes {
		_, err := cs.collection().Doc(code).Delete(ctx)
		if err != nil {
			slog.Error("CodeStore.RemoveActive: failed to delete code", "code", code, "error", err)
		}
	}
}
