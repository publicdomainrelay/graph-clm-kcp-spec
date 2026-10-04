// Package storage keeps entries between runs. It holds no rules: what may be
// posted is decided by the domain package, and the store only remembers what it
// was handed.
package storage

import (
	"sync"

	"example.com/ledger/domain"
)

// Store is an in-memory set of entries behind one lock.
type Store struct {
	mu sync.Mutex

	entries []domain.Entry
}

// New returns an empty store.
func New() *Store {
	return &Store{}
}
