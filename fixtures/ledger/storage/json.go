package storage

import (
	"encoding/json"
	"os"

	"example.com/ledger/domain"
)

func (s *Store) Save(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	encoded, err := json.MarshalIndent(s.entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(encoded, '\n'), 0o644)
}

func Load(path string) (*Store, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	entries := []domain.Entry{}
	if err := json.Unmarshal(contents, &entries); err != nil {
		return nil, err
	}
	return &Store{entries: entries}, nil
}
