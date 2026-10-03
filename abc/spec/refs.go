package spec

import (
	"hash/fnv"
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	RefSelf = "self"

	RefPrefixContext      = "sc."
	RefPrefixUpstream     = "up."
	RefPrefixOverlay      = "ov."
	RefPrefixOrchestrator = "orch."
)

var RefPrefixes = []string{RefPrefixContext, RefPrefixUpstream, RefPrefixOverlay, RefPrefixOrchestrator}

// IsRef accepts the plain vocabulary (self, sc.<name>, up.<name>, ov.<name>,
// orch.<name> with a DNS-1123 label payload) and the open architecture ids the
// arch.yaml importer keeps verbatim (sc.kind.denopod, up.hono-pds), whose
// payload is a DNS-1123 subdomain so the dots survive.
func IsRef(value string) bool {
	if value == RefSelf {
		return true
	}
	for _, prefix := range RefPrefixes {
		if payload, ok := strings.CutPrefix(value, prefix); ok {
			return len(validation.IsDNS1123Subdomain(payload)) == 0
		}
	}
	return false
}

// RefName is the object name a plain ref points at. An open architecture id
// needs ArchName instead, because its dots are part of the id, not a separator:
// the importer names sc.kind.denopod "sc-kind-denopod".
func RefName(value string) (string, bool) {
	for _, prefix := range RefPrefixes {
		if payload, ok := strings.CutPrefix(value, prefix); ok {
			return payload, true
		}
	}
	return "", false
}

var archIDPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*\.[A-Za-z0-9][A-Za-z0-9._-]*$`)

// IsArchID reports whether a value is an open architecture id: a prefix, a
// dot, then a payload (sc.kind.denopod, up.kcp, type.DenoPermissions, x.1). The
// plain vocabulary and the arch vocabulary overlap (sc.calc is both), so a
// caller that knows the source is arch.yaml uses this, and a caller that does
// not keeps RefName.
func IsArchID(value string) bool {
	return value != RefSelf && archIDPattern.MatchString(value)
}

// ArchName maps an open architecture id to the DNS-1123 object name the
// importer uses for it. The mapping is a pure function of the id, so a reimport
// lands on the same object.
func ArchName(id string) string {
	lowered := strings.ToLower(id)
	builder := strings.Builder{}
	lastDash := false
	for _, char := range lowered {
		switch {
		case char >= 'a' && char <= 'z', char >= '0' && char <= '9':
			builder.WriteRune(char)
			lastDash = false
		case char == '-' || char == '_' || char == '.':
			if !lastDash && builder.Len() > 0 {
				builder.WriteByte('-')
				lastDash = true
			}
		}
	}
	name := strings.Trim(builder.String(), "-")
	if name == "" {
		name = "node"
	}
	if len(name) <= maxArchName {
		return name
	}
	return name[:maxArchName-9] + "-" + archHash(id)
}

const maxArchName = 63

func archHash(id string) string {
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(id))
	digest := hasher.Sum32()
	const hex = "0123456789abcdef"
	out := make([]byte, 8)
	for index := range out {
		out[index] = hex[(digest>>(uint(index)*4))&0xf]
	}
	return string(out)
}

const (
	CodeRefPrefixFile     = "file:"
	CodeRefPrefixFunction = "function:"
	CodeRefPrefixMethod   = "method:"
	CodeRefPrefixType     = "type:"
	CodeRefPrefixPackage  = "package:"
)

// CodeRefKinds are the CodeGraph node kinds a code ref may name. The list is
// the index's own vocabulary, because a ref the model copied from an observed
// fact is a valid ref by construction: a TypeScript interface is
// interface:<hex> and a class is class:<hex>, not type:<hex>. A parser that
// canonicalizes a bare name to the observed CodeGraph id and a validator that
// only knew a shorter list would disagree about the model's own answer.
var CodeRefKinds = []string{
	"file", "package", "module",
	"function", "method", "constructor",
	"struct", "interface", "class", "type_alias", "enum", "type",
	"variable", "constant", "import",
}

var CodeRefPrefixes = codeRefPrefixes()

func codeRefPrefixes() []string {
	out := make([]string, 0, len(CodeRefKinds))
	for _, kind := range CodeRefKinds {
		out = append(out, kind+":")
	}
	return out
}

func IsCodeRef(value string) bool {
	for _, prefix := range CodeRefPrefixes {
		if payload, ok := strings.CutPrefix(value, prefix); ok {
			return payload != "" && !strings.ContainsAny(payload, " \t\n")
		}
	}
	return false
}
