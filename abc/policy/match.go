package policy

import (
	"path"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	ScopeAll        = "*"
	ScopeCluster    = "Cluster"
	ScopeNamespaced = "Namespaced"
)

type MatchTarget struct {
	Object metav1.Object

	GVK schema.GroupVersionKind

	NamespaceLabels map[string]string
}

func (m Match) Matches(target MatchTarget) bool {
	if target.Object == nil {
		return false
	}
	if !kindsMatch(m.Kinds, target.GVK) {
		return false
	}
	if !scopeMatch(m.Scope, target.Object.GetNamespace()) {
		return false
	}
	namespace := target.Object.GetNamespace()
	if !namespacesMatch(m.Namespaces, m.ExcludedNamespaces, namespace) {
		return false
	}
	if !m.labelSelectorMatches(target.Object.GetLabels()) {
		return false
	}
	if !m.namespaceSelectorMatches(target.NamespaceLabels) {
		return false
	}
	return nameMatch(m.Name, target.Object.GetName())
}

func kindsMatch(kinds []MatchKind, gvk schema.GroupVersionKind) bool {
	if len(kinds) == 0 {
		return true
	}
	for _, entry := range kinds {
		if len(entry.Kinds) == 0 || len(entry.APIGroups) == 0 {
			continue
		}
		if !anyMatch(entry.APIGroups, gvk.Group) {
			continue
		}
		if !anyMatch(entry.Kinds, gvk.Kind) {
			continue
		}
		return true
	}
	return false
}

func anyMatch(patterns []string, value string) bool {
	for _, pattern := range patterns {
		if pattern == ScopeAll || pattern == value {
			return true
		}
	}
	return false
}

func scopeMatch(scope, namespace string) bool {
	switch scope {
	case "", ScopeAll:
		return true
	case ScopeCluster:
		return namespace == ""
	case ScopeNamespaced:
		return namespace != ""
	}
	return true
}

func namespacesMatch(included, excluded []string, namespace string) bool {
	if len(included) > 0 && !containsString(included, namespace) {
		return false
	}
	if containsString(excluded, namespace) {
		return false
	}
	return true
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func nameMatch(pattern, name string) bool {
	if pattern == "" {
		return true
	}
	if !strings.ContainsAny(pattern, "*?[") {
		return pattern == name
	}
	matched, err := path.Match(pattern, name)
	return err == nil && matched
}

func (m Match) labelSelectorMatches(objectLabels map[string]string) bool {
	if m.LabelSelector == nil {
		return true
	}
	selector, err := metav1.LabelSelectorAsSelector(m.LabelSelector)
	if err != nil {
		return false
	}
	return selector.Matches(labels.Set(objectLabels))
}

func (m Match) namespaceSelectorMatches(namespaceLabels map[string]string) bool {
	if m.NamespaceSelector == nil {
		return true
	}
	selector, err := metav1.LabelSelectorAsSelector(m.NamespaceSelector)
	if err != nil {
		return false
	}
	return selector.Matches(labels.Set(namespaceLabels))
}

func (m Match) SelectsNamespace(namespace string) bool {
	return namespacesMatch(m.Namespaces, m.ExcludedNamespaces, namespace)
}

func (m Match) HasNameGlob() bool {
	return strings.ContainsAny(m.Name, "*?[")
}
