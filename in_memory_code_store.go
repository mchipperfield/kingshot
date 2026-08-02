package kingshot

import "context"

// inMemoryCodeStore is a CodeStore backed by in-memory maps. It is not
// safe for concurrent use; callers are expected to serialise access (e.g.
// via GiftCodeService.mu).
type inMemoryCodeStore struct {
	active  map[string]struct{}
	expired map[string]struct{}
}

// newInMemoryCodeStore returns an inMemoryCodeStore pre-seeded with any
// provided active codes.
func newInMemoryCodeStore(activeCodes ...string) *inMemoryCodeStore {
	s := &inMemoryCodeStore{
		active:  make(map[string]struct{}, len(activeCodes)),
		expired: make(map[string]struct{}),
	}
	for _, c := range activeCodes {
		s.active[c] = struct{}{}
	}
	return s
}

func (s *inMemoryCodeStore) IsActive(_ context.Context, code string) bool {
	_, ok := s.active[code]
	return ok
}

func (s *inMemoryCodeStore) IsExpired(_ context.Context, code string) bool {
	_, ok := s.expired[code]
	return ok
}

func (s *inMemoryCodeStore) AddActive(_ context.Context, code string) {
	s.active[code] = struct{}{}
}

func (s *inMemoryCodeStore) AddExpired(_ context.Context, code string) {
	s.expired[code] = struct{}{}
}

func (s *inMemoryCodeStore) ActiveCodes(_ context.Context) []string {
	codes := make([]string, 0, len(s.active))
	for c := range s.active {
		codes = append(codes, c)
	}
	return codes
}

func (s *inMemoryCodeStore) RemoveActive(_ context.Context, codes ...string) {
	for _, c := range codes {
		delete(s.active, c)
	}
}
