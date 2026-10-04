package httpapi

import (
	"net/http"

	"example.com/ledger/domain"
)

func NewHandler(ledger *domain.Ledger) http.Handler {
	mux := http.NewServeMux()
	routes(mux, ledger)
	return mux
}
