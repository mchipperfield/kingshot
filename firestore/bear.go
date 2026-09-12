package firestore

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/mchipperfield/kingshot"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type BearStore struct {
	client *firestore.Client
}

func NewBearStore(client *firestore.Client) *BearStore {
	return &BearStore{
		client: client,
	}
}

type bear struct {
	Bear             string        `firestore:"bear"`
	GuildID          string        `firestore:"guild_id"`
	SetBy            string        `firestore:"set_by"`
	SetAt            time.Time     `firestore:"set_at"`
	Next             time.Time     `firestore:"next"`
	RemindersEnabled bool          `firestore:"reminders_enabled"`
	ReminderLeadTime time.Duration `firestore:"reminder_lead_time"`
	CreatedAt        time.Time     `firestore:"created_at"`
	UpdatedAt        time.Time     `firestore:"updated_at"`
}

func (s *BearStore) GetBearStatus(ctx context.Context, guildId string, bearID string) (*kingshot.BearStatus, error) {
	docRef := s.client.Collection("alliances").Doc(guildId).Collection("bears").Doc(bearID)
	docSnap, err := docRef.Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, kingshot.ErrNotFound
		}
		return nil, fmt.Errorf("firestore: get bear doc: %w", err)
	}

	// Map the document snapshot to a BearStatus
	var b bear
	if err := docSnap.DataTo(&b); err != nil {
		return nil, fmt.Errorf("firestore: decode bear doc: %w", err)
	}
	return &kingshot.BearStatus{
		Next:             b.Next,
		SetBy:            b.SetBy,
		SetAt:            b.SetAt,
		Bear:             b.Bear,
		GuildID:          b.GuildID,
		RemindersEnabled: b.RemindersEnabled,
		ReminderLeadTime: b.ReminderLeadTime,
	}, nil
}

func (s *BearStore) SetBear(ctx context.Context, guildId string, bearID string, setTime time.Time, setBy string, reminderLeadTime time.Duration) error {
	docRef := s.client.Collection("alliances").Doc(guildId).Collection("bears").Doc(bearID)
	now := time.Now().UTC()
	err := s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		docSnap, err := tx.Get(docRef)
		if err != nil && status.Code(err) != codes.NotFound {
			return fmt.Errorf("firestore: get bear doc: %w", err)
		}
		if err != nil || docSnap == nil || !docSnap.Exists() {
			b := bear{
				Bear:             bearID,
				GuildID:          guildId,
				SetBy:            setBy,
				SetAt:            now,
				Next:             setTime,
				RemindersEnabled: true,
				ReminderLeadTime: reminderLeadTime,
				CreatedAt:        now,
				UpdatedAt:        now,
			}
			err = tx.Set(docRef, b)
			if err != nil {
				return fmt.Errorf("firestore: create bear doc: %w", err)
			}
			return nil
		}
		if err := tx.Update(docRef, []firestore.Update{
			{Path: "set_by", Value: setBy},
			{Path: "set_at", Value: now},
			{Path: "next", Value: setTime},
			{Path: "reminders_enabled", Value: true},
			{Path: "reminder_lead_time", Value: reminderLeadTime},
			{Path: "updated_at", Value: now},
		}); err != nil {
			return fmt.Errorf("firestore: update bear doc: %w", err)
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("firestore: set bear transaction: %w", err)
	}
	return nil
}

func (s *BearStore) SetBearRemindersEnabled(ctx context.Context, guildID, bearID string, enabled bool) error {
	docRef := s.client.Collection("alliances").Doc(guildID).Collection("bears").Doc(bearID)
	if err := s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		docSnap, err := tx.Get(docRef)
		if err != nil {
			if status.Code(err) == codes.NotFound {
				return kingshot.ErrNotFound
			}
			return fmt.Errorf("firestore: get bear doc: %w", err)
		}
		if !docSnap.Exists() {
			return kingshot.ErrNotFound
		}
		return tx.Update(docRef, []firestore.Update{
			{Path: "reminders_enabled", Value: enabled},
			{Path: "updated_at", Value: time.Now().UTC()},
		})
	}); err != nil {
		return fmt.Errorf("firestore: set bear reminders enabled transaction: %w", err)
	}
	return nil
}

func (s *BearStore) UpdateBearNext(ctx context.Context, guildId, bearID string, next time.Time) error {
	docRef := s.client.Collection("alliances").Doc(guildId).Collection("bears").Doc(bearID)
	if err := s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		docSnap, err := tx.Get(docRef)
		if err != nil {
			if status.Code(err) == codes.NotFound {
				return kingshot.ErrNotFound
			}
			return fmt.Errorf("firestore: get bear doc: %w", err)
		}
		if !docSnap.Exists() {
			return kingshot.ErrNotFound
		}
		return tx.Update(docRef, []firestore.Update{
			{Path: "next", Value: next},
			{Path: "updated_at", Value: time.Now().UTC()},
		})
	}); err != nil {
		return fmt.Errorf("firestore: update bear next transaction: %w", err)
	}
	return nil
}

func (s *BearStore) GetAllBearStatuses(ctx context.Context) ([]kingshot.BearStatus, error) {
	var bears []kingshot.BearStatus
	iter := s.client.CollectionGroup("bears").Where("reminders_enabled", "==", true).Documents(ctx)
	defer iter.Stop()

	for {
		docSnap, err := iter.Next()
		if err != nil {
			if err == iterator.Done {
				break
			}
			return nil, fmt.Errorf("firestore: iterate bears: %w", err)
		}

		var b bear
		if err := docSnap.DataTo(&b); err != nil {
			return nil, fmt.Errorf("firestore: decode bear doc: %w", err)
		}

		bears = append(bears, kingshot.BearStatus{
			Bear:             b.Bear,
			GuildID:          b.GuildID,
			SetBy:            b.SetBy,
			SetAt:            b.SetAt,
			Next:             b.Next,
			RemindersEnabled: b.RemindersEnabled,
			ReminderLeadTime: b.ReminderLeadTime,
		})
	}
	return bears, nil
}
