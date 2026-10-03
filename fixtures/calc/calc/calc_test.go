package calc

import "testing"

func TestAdd(t *testing.T) {
	if got := Add(2, 3); got != 5 {
		t.Fatalf("Add(2, 3) = %d, want 5", got)
	}
}

func TestMultiply(t *testing.T) {
	if got := Multiply(2, 3); got != 6 {
		t.Fatalf("Multiply(2, 3) = %d, want 6", got)
	}
}
