package kcpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
)

const clustersPath = "/clusters/"

type Options struct {
	Kubeconfig string

	Context string

	Workspace string

	Namespace string

	QPS float32

	Burst int
}

type Client struct {
	dynamic dynamic.Interface

	namespace string

	workspace string
}

func New(opts Options) (*Client, error) {
	config, err := RestConfig(opts.Kubeconfig, opts.Context)
	if err != nil {
		return nil, err
	}
	return NewFromRestConfig(config, opts.Workspace, opts.Namespace, opts.QPS, opts.Burst)
}

func RestConfig(kubeconfig, contextName string) (*rest.Config, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		rules.ExplicitPath = kubeconfig
	}
	overrides := &clientcmd.ConfigOverrides{}
	if contextName != "" {
		overrides.CurrentContext = contextName
	}
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("kcpclient: load kubeconfig: %w", err)
	}
	return config, nil
}

func WorkspaceRestConfig(kubeconfig, contextName, workspace string, qps float32, burst int) (*rest.Config, error) {
	config, err := RestConfig(kubeconfig, contextName)
	if err != nil {
		return nil, err
	}
	tuned := rest.CopyConfig(config)
	if qps > 0 {
		tuned.QPS = qps
	}
	if burst > 0 {
		tuned.Burst = burst
	}
	tuned.Host = WorkspaceHost(tuned.Host, workspace)
	return tuned, nil
}

func NewFromRestConfig(config *rest.Config, workspace, namespace string, qps float32, burst int) (*Client, error) {
	if config == nil {
		return nil, fmt.Errorf("kcpclient: rest config is required")
	}
	tuned := rest.CopyConfig(config)
	if qps > 0 {
		tuned.QPS = qps
	}
	if burst > 0 {
		tuned.Burst = burst
	}
	tuned.Host = WorkspaceHost(tuned.Host, workspace)
	client, err := dynamic.NewForConfig(tuned)
	if err != nil {
		return nil, fmt.Errorf("kcpclient: build dynamic client: %w", err)
	}
	if namespace == "" {
		namespace = specapi.DefaultNamespace
	}
	return &Client{dynamic: client, namespace: namespace, workspace: workspace}, nil
}

func WorkspaceHost(host, workspace string) string {
	trimmed, _, _ := strings.Cut(host, clustersPath)
	trimmed = strings.TrimSuffix(trimmed, "/")
	if workspace == "" {
		return trimmed
	}
	return trimmed + clustersPath + workspace
}

func (c *Client) Workspace() string {
	return c.workspace
}

func (c *Client) Namespace() string {
	return c.namespace
}

func (c *Client) resolveNamespace(namespace string) string {
	if namespace == "" {
		return c.namespace
	}
	return namespace
}

func (c *Client) Get(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string) (*unstructured.Unstructured, error) {
	found, err := c.dynamic.Resource(gvr).Namespace(c.resolveNamespace(namespace)).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("kcpclient: get %s %s: %w", gvr.Resource, name, err)
	}
	return found, nil
}

func (c *Client) List(ctx context.Context, gvr schema.GroupVersionResource, namespace string) (*unstructured.UnstructuredList, error) {
	listed, err := c.dynamic.Resource(gvr).Namespace(c.resolveNamespace(namespace)).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("kcpclient: list %s: %w", gvr.Resource, err)
	}
	return listed, nil
}

// GetCluster reads a cluster scoped object such as a ConstraintTemplate or a
// constraint; the namespace aware methods cannot, because the request would
// carry a namespace the resource does not have.
func (c *Client) GetCluster(ctx context.Context, gvr schema.GroupVersionResource, name string) (*unstructured.Unstructured, error) {
	found, err := c.dynamic.Resource(gvr).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("kcpclient: get %s %s: %w", gvr.Resource, name, err)
	}
	return found, nil
}

func (c *Client) ListCluster(ctx context.Context, gvr schema.GroupVersionResource) (*unstructured.UnstructuredList, error) {
	listed, err := c.dynamic.Resource(gvr).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("kcpclient: list %s: %w", gvr.Resource, err)
	}
	return listed, nil
}

func (c *Client) CreateCluster(ctx context.Context, object *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	created, err := c.dynamic.Resource(gvrOf(object)).Create(ctx, object, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("kcpclient: create %s %s: %w", object.GetKind(), object.GetName(), err)
	}
	return created, nil
}

func (c *Client) UpdateCluster(ctx context.Context, object *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	updated, err := c.dynamic.Resource(gvrOf(object)).Update(ctx, object, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("kcpclient: update %s %s: %w", object.GetKind(), object.GetName(), err)
	}
	return updated, nil
}

func (c *Client) ApplyCluster(ctx context.Context, object *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	current, err := c.GetCluster(ctx, gvrOf(object), object.GetName())
	if err != nil {
		if IsNotFound(err) {
			return c.CreateCluster(ctx, object)
		}
		return nil, err
	}
	object.SetResourceVersion(current.GetResourceVersion())
	return c.UpdateCluster(ctx, object)
}

func (c *Client) DeleteCluster(ctx context.Context, gvr schema.GroupVersionResource, name string) error {
	err := c.dynamic.Resource(gvr).Delete(ctx, name, metav1.DeleteOptions{PropagationPolicy: propagationBackground()})
	if err != nil && !IsNotFound(err) {
		return fmt.Errorf("kcpclient: delete %s %s: %w", gvr.Resource, name, err)
	}
	return nil
}

func (c *Client) PatchStatusCluster(ctx context.Context, gvr schema.GroupVersionResource, name string, status map[string]any) (*unstructured.Unstructured, error) {
	patch, err := json.Marshal(map[string]any{"status": status})
	if err != nil {
		return nil, fmt.Errorf("kcpclient: encode status patch: %w", err)
	}
	updated, err := c.dynamic.Resource(gvr).
		Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{}, "status")
	if err != nil {
		return nil, fmt.Errorf("kcpclient: patch %s %s status: %w", gvr.Resource, name, err)
	}
	return updated, nil
}

func (c *Client) ListAll(ctx context.Context, gvr schema.GroupVersionResource) (*unstructured.UnstructuredList, error) {
	listed, err := c.dynamic.Resource(gvr).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("kcpclient: list %s in every namespace: %w", gvr.Resource, err)
	}
	return listed, nil
}

func (c *Client) Create(ctx context.Context, object *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	namespace := c.resolveNamespace(object.GetNamespace())
	created, err := c.dynamic.Resource(gvrOf(object)).Namespace(namespace).Create(ctx, object, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("kcpclient: create %s %s: %w", object.GetKind(), object.GetName(), err)
	}
	return created, nil
}

func (c *Client) Update(ctx context.Context, object *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	namespace := c.resolveNamespace(object.GetNamespace())
	updated, err := c.dynamic.Resource(gvrOf(object)).Namespace(namespace).Update(ctx, object, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("kcpclient: update %s %s: %w", object.GetKind(), object.GetName(), err)
	}
	return updated, nil
}

func (c *Client) Apply(ctx context.Context, object *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	if object.GetNamespace() == "" {
		object.SetNamespace(c.namespace)
	}
	current, err := c.Get(ctx, gvrOf(object), object.GetNamespace(), object.GetName())
	if err != nil {
		if IsNotFound(err) {
			return c.Create(ctx, object)
		}
		return nil, err
	}
	object.SetResourceVersion(current.GetResourceVersion())
	return c.Update(ctx, object)
}

func (c *Client) ServerSideApply(ctx context.Context, object *unstructured.Unstructured, fieldManager string) (*unstructured.Unstructured, error) {
	if object.GetNamespace() == "" {
		object.SetNamespace(c.namespace)
	}
	encoded, err := json.Marshal(object.Object)
	if err != nil {
		return nil, fmt.Errorf("kcpclient: encode %s %s: %w", object.GetKind(), object.GetName(), err)
	}
	force := true
	applied, err := c.dynamic.Resource(gvrOf(object)).Namespace(object.GetNamespace()).
		Patch(ctx, object.GetName(), types.ApplyPatchType, encoded, metav1.PatchOptions{
			FieldManager: fieldManager,
			Force:        &force,
		})
	if err != nil {
		return nil, fmt.Errorf("kcpclient: server side apply %s %s: %w", object.GetKind(), object.GetName(), err)
	}
	return applied, nil
}

func (c *Client) Delete(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string) error {
	err := c.dynamic.Resource(gvr).Namespace(c.resolveNamespace(namespace)).
		Delete(ctx, name, metav1.DeleteOptions{PropagationPolicy: propagationBackground()})
	if err != nil && !IsNotFound(err) {
		return fmt.Errorf("kcpclient: delete %s %s: %w", gvr.Resource, name, err)
	}
	return nil
}

func (c *Client) UpdateStatus(ctx context.Context, object *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	namespace := c.resolveNamespace(object.GetNamespace())
	updated, err := c.dynamic.Resource(gvrOf(object)).Namespace(namespace).
		UpdateStatus(ctx, object, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("kcpclient: update %s %s status: %w", object.GetKind(), object.GetName(), err)
	}
	return updated, nil
}

func (c *Client) PatchStatus(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string, status map[string]any) (*unstructured.Unstructured, error) {
	patch, err := json.Marshal(map[string]any{"status": status})
	if err != nil {
		return nil, fmt.Errorf("kcpclient: encode status patch: %w", err)
	}
	updated, err := c.dynamic.Resource(gvr).Namespace(c.resolveNamespace(namespace)).
		Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{}, "status")
	if err != nil {
		return nil, fmt.Errorf("kcpclient: patch %s %s status: %w", gvr.Resource, name, err)
	}
	return updated, nil
}

func (c *Client) Ping(ctx context.Context) error {
	if _, err := c.List(ctx, specapi.SystemContextGVR, c.namespace); err != nil {
		return fmt.Errorf("kcpclient: workspace %q is not serving %s: %w", c.workspace, specapi.SystemContextGVR, err)
	}
	return nil
}

func IsNotFound(err error) bool {
	return apierrors.IsNotFound(err)
}

func IsAlreadyExists(err error) bool {
	return apierrors.IsAlreadyExists(err)
}

func propagationBackground() *metav1.DeletionPropagation {
	policy := metav1.DeletePropagationBackground
	return &policy
}

func gvrOf(object *unstructured.Unstructured) schema.GroupVersionResource {
	gvk := object.GroupVersionKind()
	return schema.GroupVersionResource{
		Group:    gvk.Group,
		Version:  gvk.Version,
		Resource: specapi.ResourceForKind(gvk.Kind),
	}
}
