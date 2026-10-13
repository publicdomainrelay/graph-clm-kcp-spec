package finding

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
)

func TestFindingNameSlugsTheContextAndTheUnit(t *testing.T) {
	got := Name("Calc Service", "requirement/r.add")
	if got != "proposal-calc-service-requirement-r.add" {
		t.Errorf("Name = %q, want the context and the unit slugged", got)
	}
	if strings.ContainsAny(got, "/_ ") {
		t.Errorf("Name = %q, want a valid object name", got)
	}
	if strings.HasPrefix(got, "-") || strings.HasPrefix(got, ".") {
		t.Errorf("Name = %q, want the leading separators trimmed", got)
	}
}

func TestFindingNameCapsALongContextWithAStableHash(t *testing.T) {
	unit := "requirement/r.a-very-long-requirement-name-that-keeps-going"
	context := strings.Repeat("a-long-context-name/", 4)
	got := Name(context, unit)

	input := "proposal-" + context + "-" + unit
	sum := sha256.Sum256([]byte(input))
	wantHash := hex.EncodeToString(sum[:])[:hashLength]
	want := slugify(input)[:namePrefix] + "-" + wantHash
	if got != want {
		t.Errorf("Name = %q, want %q", got, want)
	}
	if len(got) != nameLimit {
		t.Errorf("len(Name) = %d, want the cap %d", len(got), nameLimit)
	}
	if !strings.HasSuffix(got, wantHash) {
		t.Errorf("Name = %q, want the hash %q it ends with", got, wantHash)
	}
	if again := Name(context, unit); again != got {
		t.Errorf("Name is not stable: %q then %q", got, again)
	}
	if other := Name(context, unit+"-b"); other == got {
		t.Errorf("a changed unit kept the name %q", got)
	}
}

func TestFindingFromProposalFillsTheFinding(t *testing.T) {
	built := FromProposal("calc", "change-7", "Calc Service", spec.Proposal{
		Unit:     "requirement/r.add",
		Current:  "Add returns the sum.",
		Proposed: "Add returns the sum of two integers.",
	})

	if want := Name("Calc Service", "requirement/r.add"); built.Name != want {
		t.Errorf("name = %q, want %q", built.Name, want)
	}
	if built.Namespace != specapi.DefaultNamespace {
		t.Errorf("namespace = %q, want the default", built.Namespace)
	}
	if built.Kind != specapi.FindingKind || built.APIVersion != specapi.APIVersion {
		t.Errorf("type = %s/%s, want a Finding", built.APIVersion, built.Kind)
	}
	fields := built.Spec
	if fields.Source != SourceProposal {
		t.Errorf("source = %q, want proposal", fields.Source)
	}
	if fields.Repository != "calc" {
		t.Errorf("repository = %q, want calc", fields.Repository)
	}
	if fields.Ref != "change-7" {
		t.Errorf("ref = %q, want the change name", fields.Ref)
	}
	if fields.Context != "Calc Service" {
		t.Errorf("context = %q, want the context", fields.Context)
	}
	if fields.Unit != "requirement/r.add" {
		t.Errorf("unit = %q, want the proposed unit", fields.Unit)
	}
	if fields.Severity != SeverityInfo {
		t.Errorf("severity = %q, want info", fields.Severity)
	}
	if want := summaryPrefix + "Add returns the sum of two integers."; fields.Summary != want {
		t.Errorf("summary = %q, want %q", fields.Summary, want)
	}
}

func TestFindingFromProposalCutsALongValue(t *testing.T) {
	proposed := strings.Repeat("x", summaryLimit+50)
	built := FromProposal("calc", "change-7", "calc", spec.Proposal{Unit: "requirement/r.add", Proposed: proposed})

	if want := summaryPrefix + strings.Repeat("x", summaryLimit); built.Spec.Summary != want {
		t.Errorf("summary = %q (len %d), want the value cut to %d characters", built.Spec.Summary, len(built.Spec.Summary), summaryLimit)
	}
}
