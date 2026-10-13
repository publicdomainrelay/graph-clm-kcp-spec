package persist

import (
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
)

func TestUnionDeclaredKeepsEitherSidesUnits(t *testing.T) {
	ours := map[string]string{specapi.DeclaredAnnotation: `["intent"]`}
	theirs := map[string]string{specapi.DeclaredAnnotation: `["requirement/r.add"]`}
	joined, changed := unionDeclared(ours, theirs)
	if !changed || joined != `["intent","requirement/r.add"]` {
		t.Errorf("union = %s %v", joined, changed)
	}
	same, changed := unionDeclared(theirs, theirs)
	if changed || same != `["requirement/r.add"]` {
		t.Errorf("an equal side changed the units: %s %v", same, changed)
	}
	kept, changed := unionDeclared(ours, map[string]string{specapi.DeclaredAnnotation: "not json"})
	if changed || kept != `["intent"]` {
		t.Errorf("a broken branch annotation replaced ours: %s %v", kept, changed)
	}
	none, changed := unionDeclared(nil, nil)
	if changed || none != "" {
		t.Errorf("nothing declared on either side: %q %v", none, changed)
	}
}
