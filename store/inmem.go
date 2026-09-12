package inmem

import (
	"context"
	"sync"
	"time"

	"github.com/mchipperfield/kingshot"
)

type BearStore struct {
	cache        map[string]cacheEntry
	mu           sync.RWMutex
	ttl          time.Duration
	allExpiresAt time.Time
	store        kingshot.BearStore
}

type cacheEntry struct {
	status    kingshot.BearStatus
	expiresAt time.Time
}

func NewBearStore(store kingshot.BearStore) *BearStore {
	return &BearStore{
		cache: make(map[string]cacheEntry),
		ttl:   30 * time.Minute,
		store: store,
	}
}

func (s *BearStore) GetBearStatus(ctx context.Context, guildId, bearID string) (*kingshot.BearStatus, error) {
	key := guildId + "/" + bearID
	now := time.Now()

	s.mu.RLock()
	entry, found := s.cache[key]
	s.mu.RUnlock()
	if found && now.Before(entry.expiresAt) {
		return &entry.status, nil
	}

	status, err := s.store.GetBearStatus(ctx, guildId, bearID)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.cache[key] = cacheEntry{
		status:    *status,
		expiresAt: now.Add(s.ttl),
	}
	s.mu.Unlock()
	return status, nil
}

func (s *BearStore) SetBear(ctx context.Context, guildId, bearID string, setTime time.Time, setBy string, reminderLeadTime time.Duration) error {
	if err := s.store.SetBear(ctx, guildId, bearID, setTime, setBy, reminderLeadTime); err != nil {
		return err
	}

	now := time.Now()
	s.mu.Lock()
	s.cache[guildId+"/"+bearID] = cacheEntry{
		status: kingshot.BearStatus{
			Bear:             bearID,
			SetAt:            now,
			SetBy:            setBy,
			Next:             setTime,
			GuildID:          guildId,
			RemindersEnabled: true,
			ReminderLeadTime: reminderLeadTime,
		},
		expiresAt: now.Add(s.ttl),
	}
	s.mu.Unlock()
	return nil
}

func (s *BearStore) SetBearRemindersEnabled(ctx context.Context, guildID, bearID string, enabled bool) error {
	if err := s.store.SetBearRemindersEnabled(ctx, guildID, bearID, enabled); err != nil {
		return err
	}

	key := guildID + "/" + bearID
	s.mu.Lock()
	if entry, found := s.cache[key]; found {
		entry.status.RemindersEnabled = enabled
		entry.expiresAt = time.Now().Add(s.ttl)
		s.cache[key] = entry
	}
	s.mu.Unlock()
	return nil
}

func (s *BearStore) UpdateBearNext(ctx context.Context, guildId, bearID string, next time.Time) error {
	if err := s.store.UpdateBearNext(ctx, guildId, bearID, next); err != nil {
		return err
	}

	key := guildId + "/" + bearID
	s.mu.Lock()
	if entry, found := s.cache[key]; found {
		entry.status.Next = next
		entry.expiresAt = time.Now().Add(s.ttl)
		s.cache[key] = entry
	}
	s.mu.Unlock()
	return nil
}

func (s *BearStore) GetAllBearStatuses(ctx context.Context) ([]kingshot.BearStatus, error) {
	now := time.Now()
	s.mu.RLock()
	if now.Before(s.allExpiresAt) {
		statuses := make([]kingshot.BearStatus, 0, len(s.cache))
		for _, entry := range s.cache {
			if entry.status.RemindersEnabled {
				statuses = append(statuses, entry.status)
			}
		}
		s.mu.RUnlock()
		return statuses, nil
	}
	s.mu.RUnlock()

	statuses, err := s.store.GetAllBearStatuses(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	for _, status := range statuses {
		s.cache[status.GuildID+"/"+status.Bear] = cacheEntry{
			status:    status,
			expiresAt: now.Add(s.ttl),
		}
	}
	s.allExpiresAt = now.Add(s.ttl)
	s.mu.Unlock()
	return statuses, nil
}
