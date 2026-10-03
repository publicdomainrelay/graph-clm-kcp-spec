package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"example.com/todo/todo"
)

func TestListIsEmptyAtFirst(t *testing.T) {
	server := httptest.NewServer(NewHandler(todo.NewStore()))
	defer server.Close()
	response, err := http.Get(server.URL + "/tasks")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
	tasks := []todo.Task{}
	if err := json.NewDecoder(response.Body).Decode(&tasks); err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("tasks = %+v, want none", tasks)
	}
}

func TestPostCreatesATask(t *testing.T) {
	server := httptest.NewServer(NewHandler(todo.NewStore()))
	defer server.Close()
	response, err := http.Post(server.URL+"/tasks", "application/json", strings.NewReader(`{"title":"write the spec"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", response.StatusCode)
	}
	task := todo.Task{}
	if err := json.NewDecoder(response.Body).Decode(&task); err != nil {
		t.Fatal(err)
	}
	if task.ID != 1 || task.Title != "write the spec" {
		t.Fatalf("task = %+v", task)
	}
}

func TestPostRejectsAnEmptyTitle(t *testing.T) {
	server := httptest.NewServer(NewHandler(todo.NewStore()))
	defer server.Close()
	response, err := http.Post(server.URL+"/tasks", "application/json", strings.NewReader(`{"title":"  "}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.StatusCode)
	}
}

func TestCompleteReportsAMissingTask(t *testing.T) {
	server := httptest.NewServer(NewHandler(todo.NewStore()))
	defer server.Close()
	response, err := http.Post(server.URL+"/tasks/7/complete", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", response.StatusCode)
	}
}
