package domain

import (
	"errors"
	"testing"
)

func TestValidateNeedsAnAccount(t *testing.T) {
	if err := Validate(Entry{Amount: 100}); !errors.Is(err, ErrEmptyAccount) {
		t.Fatalf("Validate with no account = %v, want ErrEmptyAccount", err)
	}
	if err := Validate(Entry{Account: "cash", Amount: 100}); err != nil {
		t.Fatalf("Validate with an account = %v, want nil", err)
	}
}

func TestPostAppendsAndBalanceTotals(t *testing.T) {
	ledger := NewLedger()
	for _, entry := range []Entry{
		{Account: "cash", Amount: 100},
		{Account: "cash", Amount: 250},
		{Account: "fees", Amount: 10},
	} {
		if err := ledger.Post(entry); err != nil {
			t.Fatalf("Post(%+v) = %v", entry, err)
		}
	}
	balance, err := ledger.Balance("cash")
	if err != nil {
		t.Fatal(err)
	}
	if balance != 350 {
		t.Fatalf("Balance(cash) = %d, want 350", balance)
	}
	if _, err := ledger.Balance(""); !errors.Is(err, ErrUnknownAccount) {
		t.Fatalf("Balance with no account = %v, want ErrUnknownAccount", err)
	}
}

func TestEntriesFiltersByAccount(t *testing.T) {
	ledger := NewLedger()
	_ = ledger.Post(Entry{Account: "cash", Amount: 1})
	_ = ledger.Post(Entry{Account: "fees", Amount: 2})
	if got := ledger.Entries("fees"); len(got) != 1 || got[0].Amount != 2 {
		t.Fatalf("Entries(fees) = %+v", got)
	}
	if got := ledger.Entries(""); len(got) != 2 {
		t.Fatalf("Entries(\"\") = %+v, want every entry", got)
	}
}

func TestAccountValid(t *testing.T) {
	if (Account{}).Valid() {
		t.Error("an account with no id is valid")
	}
	if !(Account{ID: "cash"}).Valid() {
		t.Error("an account with an id is not valid")
	}
}
