package domain

func Validate(entry Entry) error {
	if entry.Account == "" {
		return ErrEmptyAccount
	}
	return nil
}

func Sum(entries []Entry) int {
	total := 0
	for _, entry := range entries {
		total += entry.Amount
	}
	return total
}
