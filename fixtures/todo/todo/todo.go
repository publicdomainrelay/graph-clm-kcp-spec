// Package todo holds the task store behind the todo HTTP service. It is the
// in-memory half: no HTTP, no JSON, so the handlers can be tested against it
// directly.
package todo

import "errors"

// ErrNotFound is returned for an id the store does not hold.
var ErrNotFound = errors.New("todo: no such task")

// Task is one item of the list.
type Task struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
}

// Store is an in-memory task list. The zero value is not usable; call
// NewStore.
type Store struct {
	tasks  []Task
	nextID int
}

// NewStore returns an empty store whose first task gets id 1.
func NewStore() *Store {
	return &Store{nextID: 1}
}

// Add appends a task with the given title and returns it.
func (s *Store) Add(title string) Task {
	task := Task{ID: s.nextID, Title: title}
	s.nextID++
	s.tasks = append(s.tasks, task)
	return task
}

// List returns every task, oldest first.
func (s *Store) List() []Task {
	out := make([]Task, len(s.tasks))
	copy(out, s.tasks)
	return out
}

// Get returns one task by id.
func (s *Store) Get(id int) (Task, error) {
	for _, task := range s.tasks {
		if task.ID == id {
			return task, nil
		}
	}
	return Task{}, ErrNotFound
}

// Complete marks a task done and returns it.
func (s *Store) Complete(id int) (Task, error) {
	for index, task := range s.tasks {
		if task.ID == id {
			s.tasks[index].Done = true
			return s.tasks[index], nil
		}
	}
	return Task{}, ErrNotFound
}
