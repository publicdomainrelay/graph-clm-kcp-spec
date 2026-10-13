// Package finding names a Finding and builds one from a proposal a code to
// spec writer made. It holds no I/O: the controller hands it the repository,
// the change and the proposal, and writes what it returns.
package finding

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
)

const (
	Open      = "Open"
	Planned   = "Planned"
	Resolved  = "Resolved"
	Dismissed = "Dismissed"
)

// SourceProposal marks a finding a code to spec writer raised: the code does
// not say what a declared unit says.
const SourceProposal = "proposal"

// SeverityInfo is the severity of a proposal: the code suggests a value, it
// does not break anything.
const SeverityInfo = "info"

const (
	nameLimit  = 63
	namePrefix = 54
	hashLength = 8

	summaryLimit  = 500
	summaryPrefix = "the code suggests: "
)

var unsafeNameCharacters = regexp.MustCompile(`[^a-z0-9.-]+`)

// Name is the object name of the finding a proposal raises. The context and
// the unit are slugged into a valid object name; a slug too long for an object
// name keeps its first 54 characters and a hash of the input, so the name is
// stable and a changed context or unit gets a new one.
func Name(context, unit string) string {
	input := "proposal-" + context + "-" + unit
	slug := slugify(input)
	if len(slug) <= nameLimit {
		return slug
	}
	sum := sha256.Sum256([]byte(input))
	return slug[:namePrefix] + "-" + hex.EncodeToString(sum[:])[:hashLength]
}

func slugify(value string) string {
	slug := unsafeNameCharacters.ReplaceAllString(strings.ToLower(value), "-")
	return strings.Trim(slug, "-.")
}

// FromProposal builds the finding a proposal raises: the repository and the
// context it concerns, the change that made it, the unit the writer would have
// written, and the value the code suggests.
func FromProposal(repository, change, context string, proposal spec.Proposal) spec.Finding {
	proposed := []rune(proposal.Proposed)
	if len(proposed) > summaryLimit {
		proposed = proposed[:summaryLimit]
	}
	finding := spec.Finding{
		ObjectMeta: metav1.ObjectMeta{Name: Name(context, proposal.Unit)},
		Spec: spec.FindingSpec{
			Repository: repository,
			Source:     SourceProposal,
			Ref:        change,
			Context:    context,
			Unit:       proposal.Unit,
			Severity:   SeverityInfo,
			Summary:    summaryPrefix + string(proposed),
		},
	}
	spec.SetDefaults(&finding)
	return finding
}
