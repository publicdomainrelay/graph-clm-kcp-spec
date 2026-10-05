package specapi

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	Group   = "specs.publicdomainrelay.dev"
	Version = "v1alpha1"

	APIVersion = Group + "/" + Version
)

const (
	RepositoryKind    = "Repository"
	SystemContextKind = "SystemContext"
	SpecChangeKind    = "SpecChange"
	PolicyChangeKind  = "PolicyChange"
)

const (
	RepositoryResource    = "repositories"
	SystemContextResource = "systemcontexts"
	SpecChangeResource    = "specchanges"
	PolicyChangeResource  = "policychanges"
)

const (
	RepositoryListKind    = "RepositoryList"
	SystemContextListKind = "SystemContextList"
	SpecChangeListKind    = "SpecChangeList"
	PolicyChangeListKind  = "PolicyChangeList"
)

const DefaultNamespace = "default"

const (
	OriginAnnotation = Group + "/origin"

	OriginHashAnnotation = Group + "/origin-hash"

	OriginIngest  = "ingest"
	OriginRealize = "realize"
	OriginHuman   = "human"
	OriginGit     = "git"
	OriginCLM     = "clm"

	PopulateRequestAnnotation = Group + "/populate-request"

	SyncedHashAnnotation = Group + "/synced-hash"
)

const (
	ArchIDLabel   = Group + "/arch-id"
	ArchKindLabel = Group + "/arch-kind"
)

const (
	ConditionSpecValid      = "SpecValid"
	ConditionCodeSynced     = "CodeSynced"
	ConditionDrifted        = "Drifted"
	ConditionIndexed        = "Indexed"
	ConditionPopulated      = "Populated"
	ConditionBranchMismatch = "BranchMismatch"

	ConditionRequirementsUnimplemented = "RequirementsUnimplemented"

	ConditionAcceptanceOverridden = "AcceptanceOverridden"

	ConditionFilesOutsideContext = "FilesOutsideContext"

	ConditionPolicyCompliant = "PolicyCompliant"

	ConditionPolicyReady = "PolicyReady"

	ConditionPolicyValid = "PolicyValid"
)

const (
	PhaseCloning    = "Cloning"
	PhaseIndexing   = "Indexing"
	PhasePopulating = "Populating"
	PhasePopulated  = "Populated"
)

const (
	ReasonValidatorPassed    = "ValidatorPassed"
	ReasonValidatorFailed    = "ValidatorFailed"
	ReasonInterfacesObserved = "InterfacesObserved"
	ReasonInterfacesMissing  = "InterfacesMissing"
	ReasonCodeRefsUnresolved = "CodeRefsUnresolved"
	ReasonFingerprintEqual   = "FingerprintEqual"
	ReasonFingerprintChanged = "FingerprintChanged"
	ReasonNotSyncedYet       = "NotSyncedYet"
	ReasonIndexed            = "Indexed"
	ReasonHeadUnavailable    = "HeadUnavailable"
	ReasonIndexFailed        = "IndexFailed"
	ReasonPathMissing        = "PathMissing"
	ReasonSourceInvalid      = "SourceInvalid"
	ReasonCloneFailed        = "CloneFailed"
	ReasonPopulated          = "Populated"
	ReasonPopulating         = "Populating"
	ReasonPopulateFailed     = "PopulateFailed"
	ReasonBranchMismatch     = "BranchMismatch"
	ReasonBranchMatches      = "BranchMatches"

	ReasonRequirementsImplemented = "RequirementsImplemented"
	ReasonRequirementsMissing     = "RequirementsUnimplemented"

	ReasonAcceptanceOverridden = "AcceptanceOverridden"

	ReasonFilesOutsideContext = "FilesOutsideContext"

	ReasonPolicyCompliant  = "PolicyCompliant"
	ReasonPolicyViolations = "PolicyViolations"
	ReasonPolicyDenied     = "PolicyDenied"
	ReasonPolicyRestored   = "PolicyRestored"
	ReasonPolicyInvalid    = "PolicyInvalid"
	ReasonPolicyConflict   = "PolicyConflict"

	ReasonPolicyDeniedAtSpec = "PolicyDeniedAtSpec"

	ReasonPolicyAllowed = "PolicyAllowed"

	ReasonPolicyGateError = "PolicyGateError"
)

const (
	DirectionSpecToCode = "SpecToCode"
	DirectionCodeToSpec = "CodeToSpec"
)

const (
	PhasePending   = "Pending"
	PhaseRunning   = "Running"
	PhaseSucceeded = "Succeeded"
	PhaseFailed    = "Failed"
)

var (
	RepositoryGVR = schema.GroupVersionResource{
		Group: Group, Version: Version, Resource: RepositoryResource,
	}
	SystemContextGVR = schema.GroupVersionResource{
		Group: Group, Version: Version, Resource: SystemContextResource,
	}
	SpecChangeGVR = schema.GroupVersionResource{
		Group: Group, Version: Version, Resource: SpecChangeResource,
	}
	PolicyChangeGVR = schema.GroupVersionResource{
		Group: Group, Version: Version, Resource: PolicyChangeResource,
	}
)

func RepositoryGVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: Group, Version: Version, Kind: RepositoryKind}
}

func SystemContextGVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: Group, Version: Version, Kind: SystemContextKind}
}

func SpecChangeGVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: Group, Version: Version, Kind: SpecChangeKind}
}

func PolicyChangeGVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: Group, Version: Version, Kind: PolicyChangeKind}
}

func GVRForKind(kind string) (schema.GroupVersionResource, error) {
	switch kind {
	case RepositoryKind, RepositoryResource:
		return RepositoryGVR, nil
	case SystemContextKind, SystemContextResource:
		return SystemContextGVR, nil
	case SpecChangeKind, SpecChangeResource:
		return SpecChangeGVR, nil
	case PolicyChangeKind, PolicyChangeResource:
		return PolicyChangeGVR, nil
	}
	return schema.GroupVersionResource{}, fmt.Errorf("specapi: unknown kind %q", kind)
}

func KindForArg(arg string) (string, error) {
	switch strings.ToLower(arg) {
	case "repository", "repositories", "repo", "repos":
		return RepositoryKind, nil
	case "systemcontext", "systemcontexts", "sc":
		return SystemContextKind, nil
	case "specchange", "specchanges", "change", "changes":
		return SpecChangeKind, nil
	case "policychange", "policychanges", "policy", "policies":
		return PolicyChangeKind, nil
	}
	return "", fmt.Errorf("specapi: unknown kind %q", arg)
}

func ResourceForKind(kind string) string {
	gvr, err := GVRForKind(kind)
	if err != nil {
		return ""
	}
	return gvr.Resource
}

func KindForResource(resource string) (string, error) {
	switch resource {
	case RepositoryResource:
		return RepositoryKind, nil
	case SystemContextResource:
		return SystemContextKind, nil
	case SpecChangeResource:
		return SpecChangeKind, nil
	case PolicyChangeResource:
		return PolicyChangeKind, nil
	}
	return "", fmt.Errorf("specapi: unknown resource %q", resource)
}
