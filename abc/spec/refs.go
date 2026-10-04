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

func RefName(value string) (string, bool) {
	for _, prefix := range RefPrefixes {
		if payload, ok := strings.CutPrefix(value, prefix); ok {
			return payload, true
		}
	}
	return "", false
}

var archIDPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*\.[A-Za-z0-9][A-Za-z0-9._-]*$`)

func IsArchID(value string) bool {
	return value != RefSelf && archIDPattern.MatchString(value)
}

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
	if len(name) <= maxDNS1123LabelLength {
		return name
	}
	return name[:maxDNS1123LabelLength-9] + "-" + archHash(id)
}

const maxDNS1123LabelLength = 63

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
