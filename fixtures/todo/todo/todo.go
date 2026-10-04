package todo

import "errors"

var ErrNotFound = errors.New("todo: no such task")

type Task struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
}

type Store struct {
	tasks  []Task
	nextID int
}

func NewStore() *Store {
	return &Store{nextID: 1}
}

func (s *Store) Add(title string) Task {
	task := Task{ID: s.nextID, Title: title}
	s.nextID++
	s.tasks = append(s.tasks, task)
	return task
}

func (s *Store) List() []Task {
	out := make([]Task, len(s.tasks))
	copy(out, s.tasks)
	return out
}

func (s *Store) Get(id int) (Task, error) {
	for _, task := range s.tasks {
		if task.ID == id {
			return task, nil
		}
	}
	return Task{}, ErrNotFound
}

func (s *Store) Complete(id int) (Task, error) {
	for index, task := range s.tasks {
		if task.ID == id {
			s.tasks[index].Done = true
			return s.tasks[index], nil
		}
	}
	return Task{}, ErrNotFound
}
