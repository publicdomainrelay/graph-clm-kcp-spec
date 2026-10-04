package domain

type Account struct {
	ID string `json:"id"`

	Name string `json:"name"`
}

func (a Account) Valid() bool {
	return a.ID != ""
}
