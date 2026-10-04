package httpapi

import (
	"encoding/json"
	"net/http"
)

func decode(request *http.Request, into any) error {
	return json.NewDecoder(request.Body).Decode(into)
}

func writeJSON(writer http.ResponseWriter, status int, body any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(body)
}

func writeError(writer http.ResponseWriter, status int, message string) {
	writeJSON(writer, status, map[string]string{"error": message})
}
