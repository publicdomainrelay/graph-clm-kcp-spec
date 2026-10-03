package todo

import (
	"errors"
	"testing"
)

func TestAddNumbersTasksFromOne(t *testing.T) {
	store := NewStore()
	first := store.Add("write the spec")
	second := store.Add("change the code")
	if first.ID != 1 || second.ID != 2 {
		t.Fatalf("ids = %d, %d, want 1, 2", first.ID, second.ID)
	}
}

func TestListReturnsEveryTaskOldestFirst(t *testing.T) {
	store := NewStore()
	store.Add("first")
	store.Add("second")
	tasks := store.List()
	if len(tasks) != 2 {
		t.Fatalf("len = %d, want 2", len(tasks))
	}
	if tasks[0].Title != "first" || tasks[1].Title != "second" {
		t.Fatalf("order = %q, %q, want first, second", tasks[0].Title, tasks[1].Title)
	}
}

func TestGetReturnsOneTask(t *testing.T) {
	store := NewStore()
	added := store.Add("write the spec")
	found, err := store.Get(added.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found.Title != "write the spec" {
		t.Fatalf("title = %q", found.Title)
	}
	if _, err := store.Get(99); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestCompleteMarksATaskDone(t *testing.T) {
	store := NewStore()
	added := store.Add("write the spec")
	done, err := store.Complete(added.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !done.Done {
		t.Fatal("the task is not done")
	}
}
