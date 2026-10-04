package domain

type Entry struct {
	Account string `json:"account"`

	Amount int `json:"amount"`

	Memo string `json:"memo,omitempty"`
}
