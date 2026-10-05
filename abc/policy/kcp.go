package policy

import (
	"context"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type ConstraintCRDBuilder interface {
	ConstraintCRD(ctx context.Context, template Template) (*unstructured.Unstructured, error)
}

const (
	ConstraintTemplateKind = "ConstraintTemplate"

	ConstraintTemplateGroup = "templates.gatekeeper.sh"

	ConstraintTemplateVersion = "v1"

	ConstraintTemplateResource = "constrainttemplates"

	ConstraintTemplateAPIVersion = ConstraintTemplateGroup + "/" + ConstraintTemplateVersion

	ConstraintGroup = "constraints.gatekeeper.sh"

	ConstraintVersion = "v1beta1"
)

func ConstraintTemplateGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group: ConstraintTemplateGroup, Version: ConstraintTemplateVersion, Resource: ConstraintTemplateResource,
	}
}

func ConstraintTemplateGVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group: ConstraintTemplateGroup, Version: ConstraintTemplateVersion, Kind: ConstraintTemplateKind,
	}
}

func ConstraintResource(kind string) string {
	return strings.ToLower(kind)
}

func ConstraintCRDName(kind string) string {
	return ConstraintResource(kind) + "." + ConstraintGroup
}

func ConstraintGVR(kind string) schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group: ConstraintGroup, Version: ConstraintVersion, Resource: ConstraintResource(kind),
	}
}

func ConstraintGVK(kind string) schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group: ConstraintGroup, Version: ConstraintVersion, Kind: kind,
	}
}

func ConstraintListKind(kind string) string {
	return kind + "List"
}
