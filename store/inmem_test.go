package inmem

import (
	"context"
	"testing"
	"time"

	"github.com/mchipperfield/kingshot"
)

type recordingBearStore struct {
	statuses       map[string]kingshot.BearStatus
	getCalls       int
	setCalls       int
	getAllCalls    int
	lastSetGuildID string
	lastSetBearID  string
	lastSetTime    time.Time
	lastSetBy      string
}

func newRecordingBearStore(statuses ...kingshot.BearStatus) *recordingBearStore {
	store := &recordingBearStore{statuses: make(map[string]kingshot.BearStatus)}
	for _, status := range statuses {
		store.statuses[bearKey(status.GuildID, status.Bear)] = status
	}
	return store
}

func (s *recordingBearStore) GetBearStatus(_ context.Context, guildID, bearID string) (*kingshot.BearStatus, error) {
	s.getCalls++
	status, found := s.statuses[bearKey(guildID, bearID)]
	if !found {
		return nil, kingshot.ErrNotFound
	}
	return &status, nil
}

func (s *recordingBearStore) SetBear(_ context.Context, guildID, bearID string, setTime time.Time, setBy string) error {
	s.setCalls++
	s.lastSetGuildID = guildID
	s.lastSetBearID = bearID
	s.lastSetTime = setTime
	s.lastSetBy = setBy
	s.statuses[bearKey(guildID, bearID)] = kingshot.BearStatus{
		Bear:    bearID,
		GuildID: guildID,
		SetAt:   time.Now(),
		SetBy:   setBy,
		Next:    setTime,
	}
	return nil
}

func (s *recordingBearStore) GetAllBearStatuses(_ context.Context) ([]kingshot.BearStatus, error) {
	s.getAllCalls++
	statuses := make([]kingshot.BearStatus, 0, len(s.statuses))
	for _, status := range s.statuses {
		statuses = append(statuses, status)
	}
	return statuses, nil
}

func bearKey(guildID, bearID string) string {
	return guildID + "/" + bearID
}

func TestBearStoreGetBearStatusCachesBackingStoreResult(t *testing.T) {
	status := kingshot.BearStatus{
		Bear:    "1",
		GuildID: "guild-1",
		SetBy:   "user-1",
		SetAt:   time.Now().Add(-time.Hour),
		Next:    time.Now().Add(time.Hour),
	}
	backingStore := newRecordingBearStore(status)
	store := NewBearStore(backingStore)

	got, err := store.GetBearStatus(context.Background(), status.GuildID, status.Bear)
	if err != nil {
		t.Fatalf("GetBearStatus() error = %v", err)
	}
	if *got != status {
		t.Errorf("GetBearStatus() = %#v, want %#v", *got, status)
	}

	got, err = store.GetBearStatus(context.Background(), status.GuildID, status.Bear)
	if err != nil {
		t.Fatalf("second GetBearStatus() error = %v", err)
	}
	if *got != status {
		t.Errorf("second GetBearStatus() = %#v, want %#v", *got, status)
	}
	if backingStore.getCalls != 1 {
		t.Errorf("backing GetBearStatus calls = %d, want 1", backingStore.getCalls)
	}
}

func TestBearStoreSetBearWritesThroughAndUpdatesCache(t *testing.T) {
	backingStore := newRecordingBearStore()
	store := NewBearStore(backingStore)
	setTime := time.Now().Add(time.Hour)

	if err := store.SetBear(context.Background(), "guild-1", "2", setTime, "user-1"); err != nil {
		t.Fatalf("SetBear() error = %v", err)
	}

	if backingStore.setCalls != 1 {
		t.Errorf("backing SetBear calls = %d, want 1", backingStore.setCalls)
	}
	if backingStore.lastSetGuildID != "guild-1" || backingStore.lastSetBearID != "2" ||
		!backingStore.lastSetTime.Equal(setTime) || backingStore.lastSetBy != "user-1" {
		t.Errorf("backing store received unexpected SetBear arguments")
	}

	status, err := store.GetBearStatus(context.Background(), "guild-1", "2")
	if err != nil {
		t.Fatalf("GetBearStatus() error = %v", err)
	}
	if status.Next != setTime || status.SetBy != "user-1" || status.GuildID != "guild-1" || status.Bear != "2" {
		t.Errorf("cached status = %#v, want configured bear status", *status)
	}
	if backingStore.getCalls != 0 {
		t.Errorf("backing GetBearStatus calls = %d, want 0 after write-through cache update", backingStore.getCalls)
	}
}

func TestBearStoreGetAllBearStatusesCachesBulkResult(t *testing.T) {
	status := kingshot.BearStatus{
		Bear:    "1",
		GuildID: "guild-1",
		SetBy:   "user-1",
		SetAt:   time.Now().Add(-time.Hour),
		Next:    time.Now().Add(time.Hour),
	}
	backingStore := newRecordingBearStore(status)
	store := NewBearStore(backingStore)

	for range 2 {
		statuses, err := store.GetAllBearStatuses(context.Background())
		if err != nil {
			t.Fatalf("GetAllBearStatuses() error = %v", err)
		}
		if len(statuses) != 1 || statuses[0] != status {
			t.Errorf("GetAllBearStatuses() = %#v, want %#v", statuses, []kingshot.BearStatus{status})
		}
	}
	if backingStore.getAllCalls != 1 {
		t.Errorf("backing GetAllBearStatuses calls = %d, want 1", backingStore.getAllCalls)
	}
}
