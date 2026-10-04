package storage

import "example.com/ledger/domain"

// Add appends one entry.
func (s *Store) Add(entry domain.Entry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, entry)
}

// For returns a copy of the entries of one account, oldest first.
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

// All returns a copy of every entry.
func (s *Store) All() []domain.Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Entry, len(s.entries))
	copy(out, s.entries)
	return out
}
