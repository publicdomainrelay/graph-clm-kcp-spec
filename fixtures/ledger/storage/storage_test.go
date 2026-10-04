package storage

import (
	"path/filepath"
	"testing"

	"example.com/ledger/domain"
)

func TestAddAndFor(t *testing.T) {
	store := New()
	store.Add(domain.Entry{Account: "cash", Amount: 100})
	store.Add(domain.Entry{Account: "fees", Amount: 5})
	if got := store.For("cash"); len(got) != 1 || got[0].Amount != 100 {
		t.Fatalf("For(cash) = %+v", got)
	}
	if got := store.All(); len(got) != 2 {
		t.Fatalf("All() = %+v", got)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.json")
	store := New()
	store.Add(domain.Entry{Account: "cash", Amount: 100, Memo: "opening"})
	if err := store.Save(path); err != nil {
		t.Fatalf("Save = %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load = %v", err)
	}
	got := loaded.All()
	if len(got) != 1 || got[0].Account != "cash" || got[0].Amount != 100 || got[0].Memo != "opening" {
		t.Fatalf("loaded = %+v", got)
	}
}
