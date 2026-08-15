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
	status, found := s.statuses[bearStoreKey(guildID, bearID)]
	if !found {
		return nil, ErrNotFound
	}
	return &status, nil
}

func (s *memoryBearStore) SetBear(_ context.Context, guildID, bearID string, setTime time.Time, setBy string) error {
	s.statuses[bearStoreKey(guildID, bearID)] = BearStatus{
		Bear:    bearID,
		GuildID: guildID,
		SetAt:   time.Now(),
		SetBy:   setBy,
		Next:    setTime,
	}
	return nil
}

func (s *memoryBearStore) UpdateBearNext(_ context.Context, guildID, bearID string, next time.Time) error {
	key := bearStoreKey(guildID, bearID)
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

func bearStoreKey(guildID, bearID string) string {
	return guildID + "/" + bearID
}

func TestBearServiceSetAndGetBearWithoutCache(t *testing.T) {
	store := newMemoryBearStore()
	service := NewBearService(store)
	setTime := time.Now().Add(time.Hour)

	if err := service.SetBear(context.Background(), "guild-1", "1", setTime, "user-1"); err != nil {
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

func TestBearServiceTickPersistsNextBearTime(t *testing.T) {
	store := newMemoryBearStore()
	service := NewBearService(store)
	previous := time.Now().Add(-time.Hour)
	status := BearStatus{Bear: "1", GuildID: "guild-1", Next: previous}
	store.statuses[bearStoreKey(status.GuildID, status.Bear)] = status
	service.upsertBear(status)

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
