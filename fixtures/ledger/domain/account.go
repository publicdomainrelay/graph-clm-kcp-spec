package domain

// Account is one named place entries are posted to.
type Account struct {
	ID string `json:"id"`

	Name string `json:"name"`
}

// Valid reports whether the account can be posted to.
func (a Account) Valid() bool {
	return a.ID != ""
}
