package ids

import "testing"

func TestStableMatchesFNV1aMaskedTo53Bits(t *testing.T) {
	cases := map[string]int64{
		"":                          5239054864098085,
		"a":                         1086646154030220,
		"coderef:file:calc/calc.go": 8260045628217466,
		"context:calc":              5473876716581127,
	}
	for key, want := range cases {
		if got := Stable(key); got != want {
			t.Errorf("Stable(%q) = %d, want %d", key, got, want)
		}
	}
}

func TestStableIsWithinMaxNodeID(t *testing.T) {
	for _, key := range []string{"", "x", "context:calc", "requirement:calc:r.add"} {
		if got := Stable(key); got < 0 || got > MaxNodeID {
			t.Errorf("Stable(%q) = %d, outside [0, %d]", key, got, MaxNodeID)
		}
	}
}

func TestCypherLiteral(t *testing.T) {
	cases := []struct {
		value any
		want  string
	}{
		{"plain", "'plain'"},
		{"it's", `'it\'s'`},
		{"a\\b", `'a\\b'`},
		{"two\nlines", `'two\nlines'`},
		{true, "true"},
		{7, "7"},
		{int64(9), "9"},
	}
	for _, testCase := range cases {
		if got := CypherLiteral(testCase.value); got != testCase.want {
			t.Errorf("CypherLiteral(%#v) = %s, want %s", testCase.value, got, testCase.want)
		}
	}
}
