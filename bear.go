package kingshot

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type BearService struct {
	store BearStore
}

func NewBearService(store BearStore) *BearService {
	return &BearService{store: store}
}

type BearStatus struct {
	Bear  string
	SetAt time.Time
	SetBy string
	Next  time.Time
}

func (s *BearService) GetBearStatus(ctx context.Context, guildId, bearID string) (*BearStatus, error) {
	switch bearID {
	case "1":
		return &BearStatus{
			Bear:  "1",
			SetAt: time.Now().Add(-1 * time.Hour),
			SetBy: "359734862141194251",
			Next:  time.Now().Add(1 * time.Hour),
		}, nil
	case "2":
		return &BearStatus{

			Bear:  "2",
			SetAt: time.Now().Add(-1 * time.Hour),
			SetBy: "359734862141194251",
			Next:  time.Now().Add(1 * time.Hour),
		}, nil
	default:
		return nil, errors.New("invalid bear trap")
	}

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
