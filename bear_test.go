package kingshot

import (
	"context"
	"testing"
	"time"
)

type memoryBearStore struct {
	statuses map[string]BearStatus
}

func newMemoryBearStore() *memoryBearStore {
	return &memoryBearStore{statuses: make(map[string]BearStatus)}
}

func (s *memoryBearStore) GetBearStatus(_ context.Context, guildID, bearID string) (*BearStatus, error) {
	status, found := s.statuses[bearKey(guildID, bearID)]
	if !found {
		return nil, ErrNotFound
	}
	return &status, nil
}

func (s *memoryBearStore) SetBear(_ context.Context, guildID, bearID string, setTime time.Time, setBy string, reminderLeadTime time.Duration) error {
	s.statuses[bearKey(guildID, bearID)] = BearStatus{
		Bear:             bearID,
		GuildID:          guildID,
		SetAt:            time.Now(),
		SetBy:            setBy,
		Next:             setTime,
		RemindersEnabled: true,
		ReminderLeadTime: reminderLeadTime,
	}
	return nil
}

func (s *memoryBearStore) SetBearRemindersEnabled(_ context.Context, guildID, bearID string, enabled bool) error {
	key := bearKey(guildID, bearID)
	status, found := s.statuses[key]
	if !found {
		return ErrNotFound
	}
	status.RemindersEnabled = enabled
	s.statuses[key] = status
	return nil
}

func (s *memoryBearStore) UpdateBearNext(_ context.Context, guildID, bearID string, next time.Time) error {
	key := bearKey(guildID, bearID)
	status, found := s.statuses[key]
	if !found {
		return ErrNotFound
	}
	status.Next = next
	s.statuses[key] = status
	return nil
}

func (s *memoryBearStore) GetAllBearStatuses(_ context.Context) ([]BearStatus, error) {
	statuses := make([]BearStatus, 0, len(s.statuses))
	for _, status := range s.statuses {
		statuses = append(statuses, status)
	}
	return statuses, nil
}

func TestBearServiceSetAndGetBearWithoutCache(t *testing.T) {
	store := newMemoryBearStore()
	service := NewBearService(store, nil)
	setTime := time.Now().Add(time.Hour)

	if err := service.SetBear(context.Background(), "guild-1", "1", setTime, "user-1", DefaultReminderLeadTime); err != nil {
		t.Fatalf("SetBear() error = %v", err)
	}

	status, err := service.GetBearStatus(context.Background(), "guild-1", "1")
	if err != nil {
		t.Fatalf("GetBearStatus() error = %v", err)
	}
	if status.Bear != "1" || status.GuildID != "guild-1" || status.SetBy != "user-1" || !status.Next.Equal(setTime) {
		t.Errorf("GetBearStatus() = %#v, want configured bear status", *status)
	}
}

func TestBearStatusReminders(t *testing.T) {
	tests := []struct {
		name   string
		status BearStatus
		want   string
	}{
		{name: "enabled", status: BearStatus{RemindersEnabled: true}, want: "Enabled"},
		{name: "disabled", status: BearStatus{}, want: "Disabled"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.status.Reminders(); got != test.want {
				t.Errorf("Reminders() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestBearServiceTickPersistsNextBearTime(t *testing.T) {
	store := newMemoryBearStore()
	service := NewBearService(store, nil)
	previous := time.Now().Add(-time.Hour)
	status := BearStatus{Bear: "1", GuildID: "guild-1", Next: previous, RemindersEnabled: true}
	store.statuses[bearKey(status.GuildID, status.Bear)] = status

	if err := service.tick(context.Background(), time.Now()); err != nil {
		t.Fatalf("tick() error = %v", err)
	}

	got, err := store.GetBearStatus(context.Background(), status.GuildID, status.Bear)
	if err != nil {
		t.Fatalf("GetBearStatus() error = %v", err)
	}
	want := previous.Add(bearInterval)
	if !got.Next.Equal(want) {
		t.Errorf("persisted Next = %v, want %v", got.Next, want)
	}
}

func TestBearServiceTickSendsReminderOncePerOccurrence(t *testing.T) {
	store := newMemoryBearStore()
	service := NewBearService(store, nil)
	now := time.Now()
	status := BearStatus{Bear: "1", GuildID: "guild-1", Next: now.Add(10 * time.Minute), RemindersEnabled: true}
	store.statuses[bearKey(status.GuildID, status.Bear)] = status

	if err := service.tick(context.Background(), now); err != nil {
		t.Fatalf("first tick() error = %v", err)
	}
	if err := service.tick(context.Background(), now.Add(time.Minute)); err != nil {
		t.Fatalf("second tick() error = %v", err)
	}

	select {
	case reminder := <-service.ReminderChannel():
		if reminder.Next != status.Next {
			t.Errorf("reminder Next = %v, want %v", reminder.Next, status.Next)
		}
	default:
		t.Fatal("expected reminder")
	}
	select {
	case reminder := <-service.ReminderChannel():
		t.Errorf("unexpected duplicate reminder: %#v", reminder)
	default:
	}
}

func TestBearServiceDisableBearReminders(t *testing.T) {
	store := newMemoryBearStore()
	service := NewBearService(store, nil)
	status := BearStatus{Bear: "1", GuildID: "guild-1", Next: time.Now().Add(10 * time.Minute), RemindersEnabled: true}
	store.statuses[bearKey(status.GuildID, status.Bear)] = status

	if err := service.DisableBearReminders(context.Background(), status.GuildID, status.Bear); err != nil {
		t.Fatalf("DisableBearReminders() error = %v", err)
	}

	got, err := service.GetBearStatus(context.Background(), status.GuildID, status.Bear)
	if err != nil {
		t.Fatalf("GetBearStatus() error = %v", err)
	}
	if got.RemindersEnabled {
		t.Error("RemindersEnabled = true, want false")
	}
	if err := service.tick(context.Background(), time.Now()); err != nil {
		t.Fatalf("tick() error = %v", err)
	}
	select {
	case reminder := <-service.ReminderChannel():
		t.Errorf("unexpected reminder for disabled bear: %#v", reminder)
	default:
	}
}
