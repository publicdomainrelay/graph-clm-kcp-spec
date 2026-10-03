package codegraphsqlite

import "testing"

func TestExportedMethodFollowsGoCapitalisation(t *testing.T) {
	cases := []struct {
		node Node
		want bool
	}{
		{Node{Kind: "method", Language: "go", Name: "Add"}, true},
		{Node{Kind: "method", Language: "go", Name: "add"}, false},
		{Node{Kind: "method", Language: "go", Name: "_hidden"}, false},
		{Node{Kind: "method", Language: "typescript", Name: "Add"}, false},
		{Node{Kind: "function", Language: "go", Name: "Add"}, false},
	}
	for _, testCase := range cases {
		if got := exportedMethod(testCase.node); got != testCase.want {
			t.Errorf("exportedMethod(%+v) = %v, want %v", testCase.node, got, testCase.want)
		}
	}
}
