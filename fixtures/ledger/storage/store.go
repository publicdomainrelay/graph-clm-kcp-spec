package storage

import (
	"sync"

	"example.com/ledger/domain"
)

type Store struct {
	mu sync.Mutex

	entries []domain.Entry
}

func New() *Store {
	return &Store{}
}
