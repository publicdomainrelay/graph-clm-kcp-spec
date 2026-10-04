// Package httpapi is the JSON front end of the ledger: three routes over the
// domain's ledger, no framework, so the handlers are the whole surface a client
// sees.
package httpapi

import (
	"net/http"

	"example.com/ledger/domain"
)

// NewHandler returns the service's routes over one ledger.
func NewHandler(ledger *domain.Ledger) http.Handler {
	mux := http.NewServeMux()
	routes(mux, ledger)
	return mux
}
