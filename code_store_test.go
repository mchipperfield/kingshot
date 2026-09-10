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

func (s *inMemoryCodeStore) Find(_ context.Context, code string) (*Code, bool, error) {
	c, ok := s.codes[code]
	return &c, ok, nil
}

func (s *inMemoryCodeStore) Add(_ context.Context, code Code) error {
	s.codes[code.Value] = code
	return nil
}

func (s *inMemoryCodeStore) ActiveCodes(_ context.Context) ([]string, error) {
	codes := make([]string, 0, len(s.codes))
	for _, c := range s.codes {
		if !c.IsExpired() {
			codes = append(codes, c.Value)
		}
	}
	return codes, nil
}

func (s *inMemoryCodeStore) RemoveActive(_ context.Context, codes ...string) error {
	for _, v := range codes {
		delete(s.codes, v)
	}
	return nil
}
