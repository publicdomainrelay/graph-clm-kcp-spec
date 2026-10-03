package kcpclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
)

type seen struct {
	Method string

	Path string

	ContentType string

	Query string

	Body string
}

func newClient(t *testing.T, respond func(w http.ResponseWriter, r *http.Request, index int)) (*Client, *[]seen) {
	t.Helper()
	requests := &[]seen{}
	index := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		*requests = append(*requests, seen{
			Method:      r.Method,
			Path:        r.URL.Path,
			ContentType: r.Header.Get("Content-Type"),
			Query:       r.URL.RawQuery,
			Body:        string(body),
		})
		respond(w, r, index)
		index++
	}))
	t.Cleanup(server.Close)
	client, err := NewFromRestConfig(&rest.Config{Host: server.URL}, "root:specs", "default", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	return client, requests
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func systemContext(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": specapi.APIVersion,
		"kind":       specapi.SystemContextKind,
		"metadata":   map[string]any{"name": name, "namespace": "default"},
		"spec":       map[string]any{"repository": "calc"},
	}}
}

func TestWorkspaceHostCarriesTheLogicalCluster(t *testing.T) {
	cases := map[string]string{
		"https://127.0.0.1:6447":                   "https://127.0.0.1:6447/clusters/root:specs",
		"https://127.0.0.1:6447/":                  "https://127.0.0.1:6447/clusters/root:specs",
		"https://127.0.0.1:6447/clusters/root":     "https://127.0.0.1:6447/clusters/root:specs",
		"https://127.0.0.1:6447/clusters/root:old": "https://127.0.0.1:6447/clusters/root:specs",
	}
	for host, want := range cases {
		if got := WorkspaceHost(host, "root:specs"); got != want {
			t.Fatalf("WorkspaceHost(%q) = %q, want %q", host, got, want)
		}
	}
	if got := WorkspaceHost("https://127.0.0.1:6447/clusters/root:specs", ""); got != "https://127.0.0.1:6447" {
		t.Fatalf("an empty workspace must leave the host bare, got %q", got)
	}
}

func TestGetUsesTheWorkspacePath(t *testing.T) {
	client, requests := newClient(t, func(w http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(w, 200, systemContext("calc").Object)
	})
	found, err := client.Get(context.Background(), specapi.SystemContextGVR, "default", "calc")
	if err != nil {
		t.Fatal(err)
	}
	if found.GetName() != "calc" || found.GetKind() != specapi.SystemContextKind {
		t.Fatalf("found = %+v", found.Object)
	}
	want := "/clusters/root:specs/apis/specs.publicdomainrelay.dev/v1alpha1/namespaces/default/systemcontexts/calc"
	if (*requests)[0].Path != want {
		t.Fatalf("path = %q, want %q", (*requests)[0].Path, want)
	}
}

func TestGetFallsBackToTheClientNamespace(t *testing.T) {
	client, requests := newClient(t, func(w http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(w, 200, systemContext("calc").Object)
	})
	if _, err := client.Get(context.Background(), specapi.SystemContextGVR, "", "calc"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains((*requests)[0].Path, "/namespaces/default/") {
		t.Fatalf("path = %q", (*requests)[0].Path)
	}
}

func TestListDecodesItems(t *testing.T) {
	client, _ := newClient(t, func(w http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(w, 200, map[string]any{
			"apiVersion": specapi.APIVersion,
			"kind":       specapi.SystemContextListKind,
			"items":      []any{systemContext("calc").Object, systemContext("calc-cli").Object},
		})
	})
	listed, err := client.List(context.Background(), specapi.SystemContextGVR, "default")
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Items) != 2 || listed.Items[0].GetName() != "calc" {
		t.Fatalf("items = %+v", listed.Items)
	}
}

func TestApplyCreatesWhenMissingAndUpdatesWhenPresent(t *testing.T) {
	client, requests := newClient(t, func(w http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			writeJSON(w, 404, map[string]any{"kind": "Status", "reason": "NotFound", "code": 404})
			return
		}
		if index == 1 {
			created := systemContext("calc")
			created.SetResourceVersion("1")
			writeJSON(w, 201, created.Object)
			return
		}
		if index == 2 {
			current := systemContext("calc")
			current.SetResourceVersion("7")
			writeJSON(w, 200, current.Object)
			return
		}
		updated := systemContext("calc")
		updated.SetResourceVersion("8")
		writeJSON(w, 200, updated.Object)
	})

	applied, err := client.Apply(context.Background(), systemContext("calc"))
	if err != nil {
		t.Fatal(err)
	}
	if applied.GetResourceVersion() != "1" {
		t.Fatalf("created resource version = %q", applied.GetResourceVersion())
	}
	if (*requests)[1].Method != http.MethodPost {
		t.Fatalf("second request = %s", (*requests)[1].Method)
	}

	applied, err = client.Apply(context.Background(), systemContext("calc"))
	if err != nil {
		t.Fatal(err)
	}
	if (*requests)[3].Method != http.MethodPut {
		t.Fatalf("fourth request = %s", (*requests)[3].Method)
	}
	if !strings.Contains((*requests)[3].Body, `"resourceVersion":"7"`) {
		t.Fatalf("update body = %s", (*requests)[3].Body)
	}
	if applied.GetResourceVersion() != "8" {
		t.Fatalf("updated resource version = %q", applied.GetResourceVersion())
	}
}

func TestApplyStampsTheDefaultNamespace(t *testing.T) {
	object := systemContext("calc")
	delete(object.Object, "metadata")
	object.SetName("calc")

	client, requests := newClient(t, func(w http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			writeJSON(w, 404, map[string]any{"kind": "Status", "reason": "NotFound", "code": 404})
			return
		}
		writeJSON(w, 201, object.Object)
	})
	if _, err := client.Apply(context.Background(), object); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains((*requests)[1].Path, "/namespaces/default/") {
		t.Fatalf("path = %q", (*requests)[1].Path)
	}
}

func TestUpdateStatusUsesTheStatusSubresource(t *testing.T) {
	client, requests := newClient(t, func(w http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(w, 200, systemContext("calc").Object)
	})
	object := systemContext("calc")
	object.SetResourceVersion("3")
	if err := unstructured.SetNestedField(object.Object, "abc", "status", "observedCommit"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.UpdateStatus(context.Background(), object); err != nil {
		t.Fatal(err)
	}
	want := "/clusters/root:specs/apis/specs.publicdomainrelay.dev/v1alpha1/namespaces/default/systemcontexts/calc/status"
	if (*requests)[0].Path != want {
		t.Fatalf("path = %q, want %q", (*requests)[0].Path, want)
	}
	if (*requests)[0].Method != http.MethodPut {
		t.Fatalf("method = %s", (*requests)[0].Method)
	}
}

func TestPatchStatusSendsAMergePatch(t *testing.T) {
	client, requests := newClient(t, func(w http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(w, 200, systemContext("calc").Object)
	})
	if _, err := client.PatchStatus(context.Background(), specapi.SystemContextGVR, "default", "calc", map[string]any{"observedCommit": "abc"}); err != nil {
		t.Fatal(err)
	}
	request := (*requests)[0]
	if request.Method != http.MethodPatch || !strings.HasSuffix(request.Path, "/status") {
		t.Fatalf("request = %s %s", request.Method, request.Path)
	}
	if !strings.Contains(request.ContentType, "merge-patch") {
		t.Fatalf("content type = %q", request.ContentType)
	}
	if !strings.Contains(request.Body, `"observedCommit":"abc"`) {
		t.Fatalf("body = %s", request.Body)
	}
}

func TestDeleteIgnoresAMissingObject(t *testing.T) {
	client, requests := newClient(t, func(w http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(w, 404, map[string]any{"kind": "Status", "reason": "NotFound", "code": 404})
	})
	if err := client.Delete(context.Background(), specapi.SystemContextGVR, "default", "calc"); err != nil {
		t.Fatal(err)
	}
	if (*requests)[0].Method != http.MethodDelete {
		t.Fatalf("method = %s", (*requests)[0].Method)
	}
	if !strings.Contains((*requests)[0].Body, "Background") {
		t.Fatalf("body = %s", (*requests)[0].Body)
	}
}

func TestIsNotFound(t *testing.T) {
	client, _ := newClient(t, func(w http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(w, 404, map[string]any{"kind": "Status", "reason": "NotFound", "code": 404})
	})
	_, err := client.Get(context.Background(), specapi.SystemContextGVR, "default", "missing")
	if err == nil || !IsNotFound(err) {
		t.Fatalf("err = %v", err)
	}
	if IsNotFound(nil) {
		t.Fatal("nil is not a not-found error")
	}
}

func TestPingFailsWhenTheAPIIsMissing(t *testing.T) {
	client, _ := newClient(t, func(w http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(w, 404, map[string]any{"kind": "Status", "reason": "NotFound", "code": 404})
	})
	if err := client.Ping(context.Background()); err == nil {
		t.Fatal("ping must fail when the specs API is not served")
	}
}

func TestNewRejectsAMissingRestConfig(t *testing.T) {
	if _, err := NewFromRestConfig(nil, "root:specs", "default", 0, 0); err == nil {
		t.Fatal("a rest config is required")
	}
}
