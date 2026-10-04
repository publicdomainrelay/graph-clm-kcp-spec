package storage

import "example.com/ledger/domain"

func (s *Store) Add(entry domain.Entry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, entry)
}

func (s *Store) For(account string) []domain.Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []domain.Entry{}
	for _, entry := range s.entries {
		if entry.Account == account {
			out = append(out, entry)
		}
	}
	return out
}

func (s *Store) All() []domain.Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Entry, len(s.entries))
	copy(out, s.entries)
	return out
}
