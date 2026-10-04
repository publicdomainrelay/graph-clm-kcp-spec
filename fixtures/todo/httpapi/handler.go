package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"example.com/todo/todo"
)

func NewHandler(store *todo.Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /tasks", func(writer http.ResponseWriter, request *http.Request) {
		writeJSON(writer, http.StatusOK, store.List())
	})
	mux.HandleFunc("POST /tasks", func(writer http.ResponseWriter, request *http.Request) {
		body := struct {
			Title string `json:"title"`
		}{}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			writeError(writer, http.StatusBadRequest, "the body is not JSON")
			return
		}
		if strings.TrimSpace(body.Title) == "" {
			writeError(writer, http.StatusBadRequest, "a task needs a title")
			return
		}
		writeJSON(writer, http.StatusCreated, store.Add(body.Title))
	})
	mux.HandleFunc("POST /tasks/{id}/complete", func(writer http.ResponseWriter, request *http.Request) {
		id, err := strconv.Atoi(request.PathValue("id"))
		if err != nil {
			writeError(writer, http.StatusBadRequest, "the id is not a number")
			return
		}
		task, err := store.Complete(id)
		if errors.Is(err, todo.ErrNotFound) {
			writeError(writer, http.StatusNotFound, "no such task")
			return
		}
		if err != nil {
			writeError(writer, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(writer, http.StatusOK, task)
	})
	return mux
}

func writeJSON(writer http.ResponseWriter, status int, body any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(body)
}

func writeError(writer http.ResponseWriter, status int, message string) {
	writeJSON(writer, status, map[string]string{"error": message})
}
