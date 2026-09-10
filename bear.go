package kingshot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

type BearService struct {
	store         BearStore
	sentReminders map[string]time.Time
	reminderChan  chan Reminder
	mu            sync.Mutex
	logger        *slog.Logger
}

// NewBearService returns a BearService using the supplied BearStore.
// A nil logger falls back to slog.Default(). The logger is tagged with a
// "component" attribute so its log lines can be attributed to this service.
func NewBearService(store BearStore, logger *slog.Logger) *BearService {
	if logger == nil {
		logger = slog.Default()
	}
	return &BearService{
		store:         store,
		sentReminders: make(map[string]time.Time),
		reminderChan:  make(chan Reminder, 64),
		logger:        logger.With("component", "bear_service"),
	}
}

type BearStatus struct {
	Bear             string
	SetAt            time.Time
	SetBy            string
	Next             time.Time
	GuildID          string
	RemindersEnabled bool
}

func (s BearStatus) Reminders() string {
	if s.RemindersEnabled {
		return "Enabled"
	}
	return "Disabled"
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
	s.mu.Lock()
	defer s.mu.Unlock()

	err := s.store.SetBear(ctx, guildId, bearID, setTime, setBy)
	if err != nil {
		return fmt.Errorf("kingshot: set bear: %w", err)
	}
	delete(s.sentReminders, bearKey(guildId, bearID))

	return nil
}

func (s *BearService) DisableBearReminders(ctx context.Context, guildID, bearID string) error {
	if bearID != "1" && bearID != "2" {
		return ErrInvalidBear
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.store.SetBearRemindersEnabled(ctx, guildID, bearID, false); err != nil {
		return fmt.Errorf("kingshot: disable bear reminders: %w", err)
	}
	delete(s.sentReminders, bearKey(guildID, bearID))
	return nil
}

type Reminder struct {
	BearID  string
	GuildID string
	Next    time.Time
	Sent    bool
}

const bearInterval time.Duration = 48 * time.Hour

// Start checks the store for due bear events every minute until ctx is canceled.
func (s *BearService) Start(ctx context.Context) error {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	s.logger.Info("bear reminder scheduler started", "interval", time.Minute)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case now := <-ticker.C:
			if err := s.tick(ctx, now); err != nil {
				s.logger.Info("kingshot: tick bears", "error", err)
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

	statuses, err := s.store.GetAllBearStatuses(ctx)
	if err != nil {
		return fmt.Errorf("get all bear statuses: %w", err)
	}

	for _, status := range statuses {
		if !status.RemindersEnabled {
			continue
		}
		key := bearKey(status.GuildID, status.Bear)
		if !status.Next.After(now) {
			steps := int64(now.Sub(status.Next)/bearInterval) + 1
			next := status.Next.Add(time.Duration(steps) * bearInterval)
			if err := s.store.UpdateBearNext(ctx, status.GuildID, status.Bear, next); err != nil {
				return fmt.Errorf("update bear next: %w", err)
			}
			s.logger.Info("bear event advanced", "guild_id", status.GuildID, "bear_id", status.Bear, "next", next)
			status.Next = next
			delete(s.sentReminders, key)
		}
		if status.Next.Sub(now) < time.Minute*30 && !s.sentReminders[key].Equal(status.Next) {
			reminder := Reminder{
				BearID:  status.Bear,
				GuildID: status.GuildID,
				Next:    status.Next,
				Sent:    true,
			}
			select {
			case s.reminderChan <- reminder:
				s.sentReminders[key] = status.Next
				s.logger.Info("bear reminder queued", "guild_id", status.GuildID, "bear_id", status.Bear, "next", status.Next)
			default:
			}
		}
	}
	return nil
}

func bearKey(guildID, bearID string) string {
	return guildID + "/" + bearID
}
