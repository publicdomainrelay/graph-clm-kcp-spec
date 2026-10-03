package spec

import (
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	RefSelf = "self"

	RefPrefixContext  = "sc."
	RefPrefixUpstream = "up."
)

var RefPrefixes = []string{RefPrefixContext, RefPrefixUpstream}

func IsRef(value string) bool {
	if value == RefSelf {
		return true
	}
	for _, prefix := range RefPrefixes {
		if name, ok := strings.CutPrefix(value, prefix); ok {
			return len(validation.IsDNS1123Label(name)) == 0
		}
	}
	return false
}

func RefName(value string) (string, bool) {
	for _, prefix := range RefPrefixes {
		if name, ok := strings.CutPrefix(value, prefix); ok {
			return name, true
		}
	}
	return "", false
}

const (
	CodeRefPrefixFile     = "file:"
	CodeRefPrefixFunction = "function:"
	CodeRefPrefixMethod   = "method:"
	CodeRefPrefixType     = "type:"
	CodeRefPrefixPackage  = "package:"
)

var CodeRefPrefixes = []string{
	CodeRefPrefixFile,
	CodeRefPrefixFunction,
	CodeRefPrefixMethod,
	CodeRefPrefixType,
	CodeRefPrefixPackage,
}

func IsCodeRef(value string) bool {
	for _, prefix := range CodeRefPrefixes {
		if payload, ok := strings.CutPrefix(value, prefix); ok {
			return payload != "" && !strings.ContainsAny(payload, " \t\n")
		}
	}
	return false
}
