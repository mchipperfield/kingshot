package kingshot

import (
	"context"
	"errors"
	"time"
)

type BearService struct {
}

func NewBearService() *BearService {
	return &BearService{}
}

type BearStatus struct {
	Bear  int
	SetAt time.Time
	SetBy string
	Next  time.Time
}

func (s *BearService) GetBearStatus(ctx context.Context, guildId, bearID string) (*BearStatus, error) {
	switch bearID {
	case "1":
		return &BearStatus{
			Bear:  1,
			SetAt: time.Now().Add(-1 * time.Hour),
			SetBy: "359734862141194251",
			Next:  time.Now().Add(1 * time.Hour),
		}, nil
	case "2":
		return &BearStatus{

			Bear:  2,
			SetAt: time.Now().Add(-1 * time.Hour),
			SetBy: "359734862141194251",
			Next:  time.Now().Add(1 * time.Hour),
		}, nil
	default:
		return nil, errors.New("invalid bear trap")
	}

}
