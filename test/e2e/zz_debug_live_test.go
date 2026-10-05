package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
)

func TestDebugEnforcedBy(t *testing.T) {
	requireLive(t, "kcp", "kine", "kubectl")
	root := repoRoot(t)
	startCluster(t, root)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	client := liveClient(t, root)

	name := "debug-enforced-by"
	_ = client.Delete(ctx, specapi.SystemContextGVR, specapi.DefaultNamespace, name)
	applyTyped(t, ctx, client, &spec.SystemContext{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: specapi.DefaultNamespace},
		Spec:       spec.SystemContextSpec{Repository: "debug"},
	})
	if _, err := client.PatchStatus(ctx, specapi.SystemContextGVR, specapi.DefaultNamespace, name,
		map[string]any{"enforcedBy": []string{"relayonly"}}); err != nil {
		t.Fatalf("patch enforcedBy: %v", err)
	}
	object, err := client.Get(ctx, specapi.SystemContextGVR, specapi.DefaultNamespace, name)
	if err != nil {
		t.Fatal(err)
	}
	status, _, _ := unstructured.NestedMap(object.Object, "status")
	fmt.Printf("DEBUG status: %v\n", status)
	_ = client.Delete(ctx, specapi.SystemContextGVR, specapi.DefaultNamespace, name)
}
