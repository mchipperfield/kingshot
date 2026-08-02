package kingshot

import "context"

// inMemoryCodeStore is a CodeStore backed by an in-memory map. It is not
// safe for concurrent use; callers are expected to serialise access (e.g.
// via GiftCodeService.mu).
type inMemoryCodeStore struct {
	codes map[string]Code
}

// newInMemoryCodeStore returns an inMemoryCodeStore pre-seeded with any
// provided active codes.
func newInMemoryCodeStore(activeCodes ...string) *inMemoryCodeStore {
	s := &inMemoryCodeStore{
		codes: make(map[string]Code, len(activeCodes)),
	}
	for _, c := range activeCodes {
		s.codes[c] = Code{Value: c}
	}
	return s
}

func (s *inMemoryCodeStore) Find(_ context.Context, code string) (Code, bool) {
	c, ok := s.codes[code]
	return c, ok
}

func (s *inMemoryCodeStore) Add(_ context.Context, code Code) {
	s.codes[code.Value] = code
}

func (s *inMemoryCodeStore) ActiveCodes(_ context.Context) []string {
	codes := make([]string, 0, len(s.codes))
	for _, c := range s.codes {
		if c.IsActive() {
			codes = append(codes, c.Value)
		}
	}
	return codes
}

func (s *inMemoryCodeStore) RemoveActive(_ context.Context, codes ...string) {
	for _, v := range codes {
		delete(s.codes, v)
	}
}
