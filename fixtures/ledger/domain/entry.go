package domain

// Entry is one amount posted to one account.
type Entry struct {
	Account string `json:"account"`

	Amount int `json:"amount"`

	Memo string `json:"memo,omitempty"`
}
