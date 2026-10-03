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
)

const (
	RepositoryResource    = "repositories"
	SystemContextResource = "systemcontexts"
	SpecChangeResource    = "specchanges"
)

const (
	RepositoryListKind    = "RepositoryList"
	SystemContextListKind = "SystemContextList"
	SpecChangeListKind    = "SpecChangeList"
)

const DefaultNamespace = "default"

const (
	OriginAnnotation = Group + "/origin"

	// OriginHashAnnotation is the hash of the spec the tool itself wrote. A
	// spec write and the status write that acknowledges it are two API calls,
	// and a reconcile can see the object between them; the annotation is what
	// tells that reconcile the spec it is reading is the tool's own write and
	// not a human edit.
	OriginHashAnnotation = Group + "/origin-hash"

	OriginIngest  = "ingest"
	OriginRealize = "realize"
	OriginHuman   = "human"

	// PopulateRequestAnnotation is a caller asking the controller to index a
	// Repository again right now, even though its git HEAD has not moved. The
	// value is any token the caller can recognise; the controller records the
	// one it handled in status.populateRequest, so a request is answered once
	// and a restart does not answer it twice.
	PopulateRequestAnnotation = Group + "/populate-request"
)

const (
	ArchIDLabel   = Group + "/arch-id"
	ArchKindLabel = Group + "/arch-kind"
)

const (
	ConditionSpecValid  = "SpecValid"
	ConditionCodeSynced = "CodeSynced"
	ConditionDrifted    = "Drifted"
	ConditionIndexed    = "Indexed"
	ConditionPopulated  = "Populated"
)

// Phases of Repository.status.phase: one manifest takes an unknown codebase
// from clone to populated specs. A failed populate ends in PhaseFailed, which
// is also a SpecChange phase.
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

func GVRForKind(kind string) (schema.GroupVersionResource, error) {
	switch kind {
	case RepositoryKind, RepositoryResource:
		return RepositoryGVR, nil
	case SystemContextKind, SystemContextResource:
		return SystemContextGVR, nil
	case SpecChangeKind, SpecChangeResource:
		return SpecChangeGVR, nil
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
	}
	return "", fmt.Errorf("specapi: unknown resource %q", resource)
}
