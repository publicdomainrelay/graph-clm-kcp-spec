package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"example.com/ledger/domain"
)

func TestPostEntryAndReadBalance(t *testing.T) {
	handler := NewHandler(domain.NewLedger())
	post := httptest.NewRequest(http.MethodPost, "/entries", strings.NewReader(`{"account":"cash","amount":100}`))
	posted := httptest.NewRecorder()
	handler.ServeHTTP(posted, post)
	if posted.Code != http.StatusCreated {
		t.Fatalf("POST /entries = %d: %s", posted.Code, posted.Body)
	}

	get := httptest.NewRequest(http.MethodGet, "/accounts/cash/balance", nil)
	balance := httptest.NewRecorder()
	handler.ServeHTTP(balance, get)
	if balance.Code != http.StatusOK {
		t.Fatalf("GET balance = %d: %s", balance.Code, balance.Body)
	}
	body := map[string]int{}
	if err := json.Unmarshal(balance.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["balance"] != 100 {
		t.Fatalf("balance = %v, want 100", body)
	}
}

func TestPostRejectsABodyThatIsNotAnEntry(t *testing.T) {
	handler := NewHandler(domain.NewLedger())
	post := httptest.NewRequest(http.MethodPost, "/entries", strings.NewReader("not json"))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, post)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("POST /entries = %d, want 400", recorder.Code)
	}
}

func TestEntriesListsWhatWasPosted(t *testing.T) {
	ledger := domain.NewLedger()
	_ = ledger.Post(domain.Entry{Account: "cash", Amount: 100})
	handler := NewHandler(ledger)
	get := httptest.NewRequest(http.MethodGet, "/entries?account=cash", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, get)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /entries = %d", recorder.Code)
	}
	entries := []domain.Entry{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Amount != 100 {
		t.Fatalf("entries = %+v", entries)
	}
}
