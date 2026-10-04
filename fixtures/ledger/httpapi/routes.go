package httpapi

import (
	"net/http"

	"example.com/ledger/domain"
)

func routes(mux *http.ServeMux, ledger *domain.Ledger) {
	mux.HandleFunc("GET /accounts/{id}/balance", func(writer http.ResponseWriter, request *http.Request) {
		balance, err := ledger.Balance(request.PathValue("id"))
		if err != nil {
			writeError(writer, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(writer, http.StatusOK, map[string]int{"balance": balance})
	})
	mux.HandleFunc("POST /entries", func(writer http.ResponseWriter, request *http.Request) {
		entry := domain.Entry{}
		if err := decode(request, &entry); err != nil {
			writeError(writer, http.StatusBadRequest, "the body is not an entry")
			return
		}
		if err := ledger.Post(entry); err != nil {
			writeError(writer, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(writer, http.StatusCreated, entry)
	})
	mux.HandleFunc("GET /entries", func(writer http.ResponseWriter, request *http.Request) {
		account := request.URL.Query().Get("account")
		writeJSON(writer, http.StatusOK, ledger.Entries(account))
	})
}
