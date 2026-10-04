package domain

// Ledger is an append only list of entries.
type Ledger struct {
	entries []Entry
}

// NewLedger returns an empty ledger.
func NewLedger() *Ledger {
	return &Ledger{}
}

// Post validates an entry and appends it.
func (l *Ledger) Post(entry Entry) error {
	if err := Validate(entry); err != nil {
		return err
	}
	l.entries = append(l.entries, entry)
	return nil
}

// Balance returns the sum of the entries of one account.
func (l *Ledger) Balance(account string) (int, error) {
	if account == "" {
		return 0, ErrUnknownAccount
	}
	total := 0
	for _, entry := range l.entries {
		if entry.Account == account {
			total += entry.Amount
		}
	}
	return total, nil
}

// Entries returns a copy of the entries of one account, oldest first. An empty
// account means every account.
func (l *Ledger) Entries(account string) []Entry {
	out := []Entry{}
	for _, entry := range l.entries {
		if account == "" || entry.Account == account {
			out = append(out, entry)
		}
	}
	return out
}
