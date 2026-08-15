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
	reminders    map[string]*Reminder
	reminderChan chan Reminder
	mu           sync.Mutex
}

func NewBearService(store BearStore) *BearService {
	return &BearService{
		store:        store,
		reminders:    make(map[string]*Reminder),
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

func (s *BearService) GetBearStatus(ctx context.Context, guildId, bearID string) (*BearStatus, error) {
	return s.store.GetBearStatus(ctx, guildId, bearID)
}

func (s *BearService) SetBear(ctx context.Context, guildId, bearID string, setTime time.Time, setBy string) error {
	if setTime.Before(time.Now()) {
		return errors.New("set time cannot be in the past")
	}
	if bearID != "1" && bearID != "2" {
		return errors.New("invalid bear trap")
	}
	err := s.store.SetBear(ctx, guildId, bearID, setTime, setBy)
	if err != nil {
		return fmt.Errorf("kingshot: set bear: %w", err)
	}
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
			s.tick(now)
		}
	}
}

func (s *BearService) ReminderChannel() <-chan Reminder {
	return s.reminderChan
}

func (s *BearService) tick(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, reminder := range s.reminders {
		// If reminder.Next is in the past, calculate the next reminder time based on the bearInterval
		if reminder.Next.Before(now) {
			steps := int64(now.Sub(reminder.Next)/bearInterval) + 1
			reminder.Next = reminder.Next.Add(time.Duration(steps) * bearInterval)
			reminder.Sent = false
		}
		// Time until next bear is less than 30 minutes and reminder has not been sent yet, send the reminder
		if reminder.Next.Sub(now) < time.Minute*30 && !reminder.Sent {
			reminder.Sent = true
			s.reminderChan <- *reminder
		}

	}
}

func (s *BearService) loadBears(ctx context.Context) error {
	reminders, err := s.store.GetAllBears(ctx)
	if err != nil {
		return fmt.Errorf("kingshot: get all bears: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range reminders {
		s.reminders[r.GuildID+"/"+r.BearID] = r
	}
	return nil
}
