package domain

// Validate reports whether an entry may be posted. Today it only asks for an
// account: an amount below zero is still accepted.
func Validate(entry Entry) error {
	if entry.Account == "" {
		return ErrEmptyAccount
	}
	return nil
}

// Sum totals the amounts of a set of entries.
func Sum(entries []Entry) int {
	total := 0
	for _, entry := range entries {
		total += entry.Amount
	}
	return total
}
