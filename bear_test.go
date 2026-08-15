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
