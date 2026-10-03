package shared

import "sort"

type Indexer struct {
	items []string
}

func NewIndexer() *Indexer {
	return &Indexer{}
}

func (i *Indexer) Add(item string) {
	i.items = append(i.items, item)
}

func (i *Indexer) List() []string {
	out := make([]string, len(i.items))
	copy(out, i.items)
	return out
}

type Set struct {
	items map[string]bool
}

func NewSet() *Set {
	return &Set{items: map[string]bool{}}
}

func (s *Set) Add(item string) {
	s.items[item] = true
}

func (s *Set) List() []string {
	out := make([]string, 0, len(s.items))
	for item := range s.items {
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}
