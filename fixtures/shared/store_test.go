package shared

import (
	"reflect"
	"testing"
)

func TestIndexerKeepsTheOrder(t *testing.T) {
	indexer := NewIndexer()
	indexer.Add("b")
	indexer.Add("a")
	if got := indexer.List(); !reflect.DeepEqual(got, []string{"b", "a"}) {
		t.Fatalf("List() = %v, want b, a", got)
	}
}

func TestSetIsSortedAndUnique(t *testing.T) {
	set := NewSet()
	set.Add("b")
	set.Add("a")
	set.Add("b")
	if got := set.List(); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("List() = %v, want a, b", got)
	}
}
