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
// code value.
// Expired codes are retained in the collection with is_active=false so that they can be prevented from being re-added.
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

// Find looks up a code by value.
// The caller should check code expiration with code.IsExpired().
func (cs *CodeStore) Find(ctx context.Context, code string) (*kingshot.Code, bool, error) {
	snap, err := cs.client.Collection("codes").Doc(code).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, false, nil
		}
		slog.Error("CodeStore.Find: failed to get code", "code", code, "error", err)
		return nil, false, err
	}

	var d codeDoc
	if err := snap.DataTo(&d); err != nil {
		slog.Error("CodeStore.Find: failed to decode code document", "code", code, "error", err)
		return nil, false, err
	}

	return &kingshot.Code{Value: d.Value, ExpiredAt: d.ExpiredAt}, true, nil
}

// Add stores code. If a document with the same Value already exists its state
// is overwritten. Codes whose ExpiredAt is non-zero are stored with
// is_active=false so they are excluded from ActiveCodes queries.
func (cs *CodeStore) Add(ctx context.Context, code kingshot.Code) error {
	_, err := cs.client.Collection("codes").Doc(code.Value).Set(ctx, map[string]any{
		"value":      code.Value,
		"expired_at": code.ExpiredAt,
		"is_active":  !code.IsExpired(),
	})
	if err != nil {
		slog.Error("CodeStore.Add: failed to store code", "code", code.Value, "error", err)
	}
	return err
}

// ActiveCodes returns the values of all codes that are currently active (i.e.
// not expired and not removed).
func (cs *CodeStore) ActiveCodes(ctx context.Context) ([]string, error) {
	var codes []string
	iter := cs.client.Collection("codes").Where("is_active", "==", true).Documents(ctx)
	for {
		snap, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			slog.Error("CodeStore.ActiveCodes: iteration error", "error", err)
			return nil, err
		}
		var d codeDoc
		if err := snap.DataTo(&d); err != nil {
			slog.Error("CodeStore.ActiveCodes: failed to decode code document", "error", err)
			return nil, err
		}
		codes = append(codes, d.Value)
	}
	return codes, nil
}

// RemoveActive marks the named codes as inactive (is_active=false) with an
// expired_at timestamp so that subsequent Find calls correctly identify them
// as expired. Codes that are not present are silently ignored.
func (cs *CodeStore) RemoveActive(ctx context.Context, codes ...string) error {
	now := time.Now()
	for _, code := range codes {
		_, err := cs.client.Collection("codes").Doc(code).Set(ctx, map[string]any{
			"is_active":  false,
			"expired_at": now,
		}, firestore.MergeAll)
		if err != nil {
			slog.Error("CodeStore.RemoveActive: failed to mark code inactive", "code", code, "error", err)
			return err
		}
	}
	return nil
}
