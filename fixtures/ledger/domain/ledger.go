package domain

type Ledger struct {
	entries []Entry
}

func NewLedger() *Ledger {
	return &Ledger{}
}

func (l *Ledger) Post(entry Entry) error {
	if err := Validate(entry); err != nil {
		return err
	}
	l.entries = append(l.entries, entry)
	return nil
}

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

func (l *Ledger) Entries(account string) []Entry {
	out := []Entry{}
	for _, entry := range l.entries {
		if account == "" || entry.Account == account {
			out = append(out, entry)
		}
	}
	return out
}
