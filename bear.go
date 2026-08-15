package kingshot

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type BearService struct {
	store        BearStore
	bears        map[string]*scheduledBear
	reminderChan chan Reminder
	mu           sync.Mutex
}

type scheduledBear struct {
	status BearStatus
	sent   bool
}

func NewBearService(store BearStore) *BearService {
	return &BearService{
		store:        store,
		bears:        make(map[string]*scheduledBear),
		reminderChan: make(chan Reminder, 64),
		mu:           sync.Mutex{},
	}
}

type BearStatus struct {
	Bear    string
	SetAt   time.Time
	SetBy   string
	Next    time.Time
	GuildID string
}

var (
	ErrSetTimeInPast = errors.New("set time cannot be in the past")
	ErrInvalidBear   = errors.New("invalid bear trap")
)

func (s *BearService) GetBearStatus(ctx context.Context, guildId, bearID string) (*BearStatus, error) {
	return s.store.GetBearStatus(ctx, guildId, bearID)
}

func (s *BearService) SetBear(ctx context.Context, guildId, bearID string, setTime time.Time, setBy string) error {
	if setTime.Before(time.Now()) {
		return ErrSetTimeInPast
	}
	if bearID != "1" && bearID != "2" {
		return ErrInvalidBear
	}
	err := s.store.SetBear(ctx, guildId, bearID, setTime, setBy)
	if err != nil {
		return fmt.Errorf("kingshot: set bear: %w", err)
	}
	s.upsertBear(BearStatus{
		Bear:    bearID,
		GuildID: guildId,
		SetBy:   setBy,
		SetAt:   time.Now(),
		Next:    setTime,
	})
	return nil
}

type Reminder struct {
	BearID  string
	GuildID string
	Next    time.Time
	Sent    bool
}

const bearInterval time.Duration = 48 * time.Hour

func (s *BearService) Start(ctx context.Context) error {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	if err := s.loadBears(ctx); err != nil {
		return fmt.Errorf("kingshot: load bears: %w", err)
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case now := <-ticker.C:
			if err := s.loadBears(ctx); err != nil {
				return fmt.Errorf("kingshot: refresh bears: %w", err)
			}
			if err := s.tick(ctx, now); err != nil {
				return fmt.Errorf("kingshot: advance bears: %w", err)
			}
		}
	}
}

func (s *BearService) ReminderChannel() <-chan Reminder {
	return s.reminderChan
}

func (s *BearService) tick(ctx context.Context, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, bear := range s.bears {
		if !bear.status.Next.After(now) {
			steps := int64(now.Sub(bear.status.Next)/bearInterval) + 1
			next := bear.status.Next.Add(time.Duration(steps) * bearInterval)
			if err := s.store.UpdateBearNext(ctx, bear.status.GuildID, bear.status.Bear, next); err != nil {
				return fmt.Errorf("update bear next: %w", err)
			}
			bear.status.Next = next
			bear.sent = false
		}
		if bear.status.Next.Sub(now) < time.Minute*30 && !bear.sent {
			bear.sent = true
			s.reminderChan <- Reminder{
				BearID:  bear.status.Bear,
				GuildID: bear.status.GuildID,
				Next:    bear.status.Next,
				Sent:    true,
			}
		}

	}
	return nil
}

func (s *BearService) loadBears(ctx context.Context) error {
	statuses, err := s.store.GetAllBearStatuses(ctx)
	if err != nil {
		return fmt.Errorf("kingshot: get all bear statuses: %w", err)
	}

	for _, status := range statuses {
		s.upsertBear(status)
	}
	return nil
}

func (s *BearService) upsertBear(status BearStatus) {
	key := status.GuildID + "/" + status.Bear
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, found := s.bears[key]; found && existing.status.Next.Equal(status.Next) {
		existing.status = status
		return
	}
	s.bears[key] = &scheduledBear{status: status}
}
