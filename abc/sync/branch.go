package specsync

import "fmt"

type BranchStatus struct {
	Mismatch bool

	Message string
}

func BranchCheck(head, want string) BranchStatus {
	if want == "" || head == want {
		return BranchStatus{}
	}
	if head == "" || head == "HEAD" {
		head = "a detached HEAD"
	}
	return BranchStatus{
		Mismatch: true,
		Message: fmt.Sprintf("the checkout is on %s but the repository's branch is %s; %s",
			head, want, "specd does not index, populate, realize or persist until they match"),
	}
}
