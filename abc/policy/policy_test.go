package policy_test

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
)

const headerYAML = `
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: relayonly
  annotations:
    specs.publicdomainrelay.dev/title: ssh over the relay only
    specs.publicdomainrelay.dev/level: MUST
    specs.publicdomainrelay.dev/requirements: market#r.relay-only,market#r.no-direct-ssh
spec:
  crd:
    spec:
      names:
        kind: RelayOnly
      validation:
        openAPIV3Schema:
          type: object
          properties:
            globs:
              type: array
              items:
                type: string
  targets:
    - target: admission.k8s.gatekeeper.sh
`

func TestParseTemplateReadsAnnotations(t *testing.T) {
	template, err := policy.ParseTemplate([]byte(headerYAML), []byte("package relayonly\n"))
	if err != nil {
		t.Fatal(err)
	}
	if template.Name != "relayonly" || template.Kind != "RelayOnly" {
		t.Fatalf("unexpected template identity: %+v", template)
	}
	if template.Title != "ssh over the relay only" {
		t.Fatalf("title not read: %q", template.Title)
	}
	if template.Level != policy.LevelMust {
		t.Fatalf("level not read: %q", template.Level)
	}
	if template.Severity != policy.SeverityError {
		t.Fatalf("severity not derived from level: %q", template.Severity)
	}
	if len(template.Requirements) != 2 || template.Requirements[0] != "market#r.relay-only" {
		t.Fatalf("requirements not read: %v", template.Requirements)
	}
	if template.Parameters == nil {
		t.Fatal("parameters schema not read")
	}
	if template.Rego != "package relayonly\n" {
		t.Fatalf("rego not read: %q", template.Rego)
	}
}

func TestParseTemplateSeverityOverridesLevel(t *testing.T) {
	header := `
metadata:
  name: quiet
  annotations:
    specs.publicdomainrelay.dev/level: MUST
    specs.publicdomainrelay.dev/severity: warning
spec:
  crd:
    spec:
      names:
        kind: Quiet
  targets:
    - target: admission.k8s.gatekeeper.sh
`
	template, err := policy.ParseTemplate([]byte(header), nil)
	if err != nil {
		t.Fatal(err)
	}
	if template.Severity != policy.SeverityWarning {
		t.Fatalf("explicit severity ignored: %q", template.Severity)
	}
}

func TestTemplateHeaderRoundTrip(t *testing.T) {
	template, err := policy.ParseTemplate([]byte(headerYAML), []byte("package relayonly\n"))
	if err != nil {
		t.Fatal(err)
	}
	template.Libs = []string{"package lib.specd\n"}
	encoded, err := template.Header()
	if err != nil {
		t.Fatal(err)
	}
	again, err := policy.ParseTemplate(encoded, nil)
	if err != nil {
		t.Fatal(err)
	}
	if again.Kind != template.Kind || again.Rego != template.Rego || len(again.Libs) != 1 {
		t.Fatalf("round trip lost data: %+v", again)
	}
	if again.Annotations[policy.AnnotationTitle] != "ssh over the relay only" {
		t.Fatalf("round trip lost annotations: %v", again.Annotations)
	}
}

func TestConstraintRoundTrip(t *testing.T) {
	template, err := policy.ParseTemplate([]byte(headerYAML), nil)
	if err != nil {
		t.Fatal(err)
	}
	constraint := policy.Constraint{
		Name:        "relay-only",
		Kind:        template.Kind,
		Template:    template.Name,
		Enforcement: policy.EnforcementWarn,
		Match: policy.Match{
			Kinds: []policy.MatchKind{{APIGroups: []string{policy.Group}, Kinds: []string{policy.CodeGraphKind}}},
		},
		Parameters: map[string]any{"globs": []any{"**/*_test.ts"}},
	}
	encoded, err := constraint.Document()
	if err != nil {
		t.Fatal(err)
	}
	again, err := policy.ParseConstraint(encoded, template.Name)
	if err != nil {
		t.Fatal(err)
	}
	if again.Name != constraint.Name || again.Enforcement != policy.EnforcementWarn {
		t.Fatalf("round trip lost identity: %+v", again)
	}
	if again.Template != template.Name {
		t.Fatalf("template not carried: %q", again.Template)
	}
	if len(again.Match.Kinds) != 1 || again.Match.Kinds[0].Kinds[0] != policy.CodeGraphKind {
		t.Fatalf("match not carried: %+v", again.Match)
	}
}

func TestConstraintDefaultsToDeny(t *testing.T) {
	doc := `
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: RelayOnly
metadata:
  name: relay-only
spec:
  match:
    kinds:
      - apiGroups: ["specs.publicdomainrelay.dev"]
        kinds: ["CodeGraph"]
`
	constraint, err := policy.ParseConstraint([]byte(doc), "relayonly")
	if err != nil {
		t.Fatal(err)
	}
	if constraint.Enforcement != policy.EnforcementDeny {
		t.Fatalf("missing enforcementAction must default to deny: %q", constraint.Enforcement)
	}
}

func TestMatchKindsScopeAndNamespace(t *testing.T) {
	graph := &metav1.PartialObjectMetadata{
		ObjectMeta: metav1.ObjectMeta{Name: "atproto-market", Namespace: "default"},
	}
	gvk := schema.GroupVersionKind{Group: policy.Group, Version: policy.Version, Kind: policy.CodeGraphKind}

	match := policy.Match{
		Kinds:      []policy.MatchKind{{APIGroups: []string{policy.Group}, Kinds: []string{policy.CodeGraphKind}}},
		Namespaces: []string{"default"},
	}
	if !match.Matches(policy.MatchTarget{Object: graph, GVK: gvk}) {
		t.Fatal("a matching object was rejected")
	}
	if match.Matches(policy.MatchTarget{Object: graph, GVK: schema.GroupVersionKind{Group: policy.Group, Kind: "SystemContext"}}) {
		t.Fatal("a kind outside the match was accepted")
	}
	other := graph.DeepCopy()
	other.Namespace = "other"
	if match.Matches(policy.MatchTarget{Object: other, GVK: gvk}) {
		t.Fatal("a namespace outside the match was accepted")
	}
	excluded := policy.Match{
		Kinds:              match.Kinds,
		ExcludedNamespaces: []string{"kube-system"},
	}
	if !excluded.Matches(policy.MatchTarget{Object: graph, GVK: gvk}) {
		t.Fatal("an object outside the excluded namespace was rejected")
	}
}

func TestMatchNameGlob(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: policy.Group, Version: policy.Version, Kind: policy.CodeGraphKind}
	named := func(name string) *metav1.PartialObjectMetadata {
		return &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: name}}
	}
	exact := policy.Match{Name: "atproto-market"}
	if !exact.Matches(policy.MatchTarget{Object: named("atproto-market"), GVK: gvk}) {
		t.Fatal("exact name did not match")
	}
	if exact.Matches(policy.MatchTarget{Object: named("atproto-market-2"), GVK: gvk}) {
		t.Fatal("exact name matched a longer name")
	}
	glob := policy.Match{Name: "atproto-*"}
	if !glob.Matches(policy.MatchTarget{Object: named("atproto-market"), GVK: gvk}) {
		t.Fatal("name glob did not match")
	}
	if glob.Matches(policy.MatchTarget{Object: named("market"), GVK: gvk}) {
		t.Fatal("name glob matched a name outside the pattern")
	}
	if !glob.HasNameGlob() {
		t.Fatal("glob not detected")
	}
}

func TestMatchLabelSelector(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: policy.Group, Version: policy.Version, Kind: policy.CodeGraphKind}
	object := &metav1.PartialObjectMetadata{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "atproto-market",
			Labels: map[string]string{"team": "market"},
		},
	}
	match := policy.Match{LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"team": "market"}}}
	if !match.Matches(policy.MatchTarget{Object: object, GVK: gvk}) {
		t.Fatal("label selector did not match")
	}
	other := policy.Match{LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"team": "relay"}}}
	if other.Matches(policy.MatchTarget{Object: object, GVK: gvk}) {
		t.Fatal("label selector matched the wrong labels")
	}
}

func TestMatchScope(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: policy.Group, Version: policy.Version, Kind: policy.CodeGraphKind}
	namespaced := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "default"}}
	cluster := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: "a"}}
	if !(policy.Match{Scope: policy.ScopeNamespaced}).Matches(policy.MatchTarget{Object: namespaced, GVK: gvk}) {
		t.Fatal("namespaced scope rejected a namespaced object")
	}
	if (policy.Match{Scope: policy.ScopeNamespaced}).Matches(policy.MatchTarget{Object: cluster, GVK: gvk}) {
		t.Fatal("namespaced scope accepted a cluster object")
	}
	if !(policy.Match{Scope: policy.ScopeCluster}).Matches(policy.MatchTarget{Object: cluster, GVK: gvk}) {
		t.Fatal("cluster scope rejected a cluster object")
	}
}

func TestBranchLayoutAndResolve(t *testing.T) {
	if got, want := policy.Branch("atproto-market"), "open-policy/atproto-market"; got != want {
		t.Fatalf("branch: got %q want %q", got, want)
	}
	if got, want := policy.Resolve("atproto-market", "main", "main"), "open-policy/atproto-market"; got != want {
		t.Fatalf("default branch resolve: got %q want %q", got, want)
	}
	if got, want := policy.Resolve("atproto-market", "", "main"), "open-policy/atproto-market"; got != want {
		t.Fatalf("empty branch resolve: got %q want %q", got, want)
	}
	if got, want := policy.Resolve("atproto-market", "spec/iroh", "main"), "open-policy/atproto-market--spec-iroh"; got != want {
		t.Fatalf("feature branch resolve: got %q want %q", got, want)
	}
	if got, want := policy.TemplateSourcePath("relay-only"), "templates/relay-only/src.rego"; got != want {
		t.Fatalf("source path: got %q want %q", got, want)
	}
	if got, want := policy.SuitePath("relay-only"), "tests/relay-only/suite.yaml"; got != want {
		t.Fatalf("suite path: got %q want %q", got, want)
	}
	if got, want := policy.ReportPath("spec/iroh"), "reports/spec-iroh.yaml"; got != want {
		t.Fatalf("report path: got %q want %q", got, want)
	}
	if repository, ok := policy.RepositoryOf("open-policy/atproto-market--spec-iroh"); !ok || repository != "atproto-market--spec-iroh" {
		t.Fatalf("repository of branch: %q %v", repository, ok)
	}
	if _, ok := policy.RepositoryOf("open-architecture/x"); ok {
		t.Fatal("an architecture branch was read as a policy branch")
	}
}

func decisionReport() policy.Report {
	report := policy.Report{Violations: []policy.Violation{
		{Constraint: "relay-only", Enforcement: policy.EnforcementDeny, Severity: policy.SeverityError, Msg: "direct ssh", Object: policy.ObjectRef{Kind: policy.CodeGraphKind, Name: "a"}},
		{Constraint: "guest-reports", Enforcement: policy.EnforcementWarn, Severity: policy.SeverityWarning, Msg: "reach-in", Object: policy.ObjectRef{Kind: policy.CodeGraphKind, Name: "a"}},
		{Constraint: "no-tls-off", Enforcement: policy.EnforcementDryRun, Severity: policy.SeverityInfo, Msg: "tls off", Object: policy.ObjectRef{Kind: policy.CodeGraphKind, Name: "a"}},
	}}
	report.Tally()
	return report
}

func TestDecideBlocksOnDeny(t *testing.T) {
	decision := policy.Decide(decisionReport(), policy.RepositoryPolicy{}, nil)
	if !decision.Blocked {
		t.Fatal("a deny violation must block")
	}
	if len(decision.Denied) != 1 || len(decision.Warned) != 1 || len(decision.DryRun) != 1 {
		t.Fatalf("unexpected grouping: %+v", decision)
	}
	if messages := decision.Messages(); len(messages) != 1 || messages[0] != "relay-only: direct ssh" {
		t.Fatalf("unexpected messages: %v", messages)
	}
}

func TestDecideCapDowngrades(t *testing.T) {
	decision := policy.Decide(decisionReport(), policy.RepositoryPolicy{Enforcement: policy.EnforcementWarn}, nil)
	if decision.Blocked {
		t.Fatal("a warn cap must not block")
	}
	if len(decision.Denied) != 0 {
		t.Fatalf("deny survived the cap: %+v", decision.Denied)
	}
	if len(decision.Warned) != 2 {
		t.Fatalf("downgraded deny must join warn: %+v", decision.Warned)
	}
	if len(decision.Capped) != 1 {
		t.Fatalf("the downgrade must be recorded: %+v", decision.Capped)
	}
}

func TestDecideDisabledIsDryRun(t *testing.T) {
	decision := policy.Decide(decisionReport(), policy.RepositoryPolicy{Disabled: true}, nil)
	if decision.Blocked || len(decision.DryRun) != 3 {
		t.Fatalf("a disabled policy must only record: %+v", decision)
	}
}

func TestDecideOverrideWaivesOneConstraint(t *testing.T) {
	override, ok := policy.ParseOverride("policy:relay-only", "migration", "alice")
	if !ok {
		t.Fatal("override step not parsed")
	}
	if override.Step() != "policy:relay-only" {
		t.Fatalf("override step round trip: %q", override.Step())
	}
	decision := policy.Decide(decisionReport(), policy.RepositoryPolicy{}, []policy.Override{override})
	if decision.Blocked {
		t.Fatal("the waived constraint must not block")
	}
	if len(decision.Waived) != 1 || decision.Waived[0].Constraint != "relay-only" {
		t.Fatalf("waiver not recorded: %+v", decision.Waived)
	}
	if waived := decision.WaivedConstraints(); len(waived) != 1 || waived[0] != "relay-only" {
		t.Fatalf("waived constraints: %v", waived)
	}
	if _, ok := policy.ParseOverride("acceptance:unit", "", ""); ok {
		t.Fatal("a non-policy override was read as a policy override")
	}
}

func TestReportSortAndTally(t *testing.T) {
	report := policy.Report{Violations: []policy.Violation{
		{Constraint: "b", Object: policy.ObjectRef{Kind: policy.CodeGraphKind, Name: "z"}, Msg: "m", Enforcement: policy.EnforcementDeny, Severity: policy.SeverityError},
		{Constraint: "a", Object: policy.ObjectRef{Kind: policy.CodeGraphKind, Name: "b"}, Msg: "m", Enforcement: policy.EnforcementWarn, Severity: policy.SeverityWarning},
		{Constraint: "a", Object: policy.ObjectRef{Kind: policy.CodeGraphKind, Name: "a"}, Msg: "m", Enforcement: policy.EnforcementWarn, Severity: policy.SeverityWarning},
	}}
	report.Sort()
	report.Tally()
	if report.Violations[0].Constraint != "a" || report.Violations[0].Object.Name != "a" {
		t.Fatalf("sort is not by constraint then object: %+v", report.Violations)
	}
	if report.Totals[policy.EnforcementDeny] != 1 || report.Totals[policy.EnforcementWarn] != 2 {
		t.Fatalf("totals: %v", report.Totals)
	}
	if report.Severity[policy.SeverityWarning] != 2 {
		t.Fatalf("severity totals: %v", report.Severity)
	}
	if report.Empty() {
		t.Fatal("a report with violations is not empty")
	}
}

func TestCodeGraphSortAndObject(t *testing.T) {
	graph := policy.CodeGraph{
		APIVersion: policy.APIVersion,
		Kind:       policy.CodeGraphKind,
		Metadata: policy.ObjectMeta{
			Name:      "atproto-market",
			Namespace: "default",
			Labels:    map[string]string{policy.CommitLabel: "deadbeef"},
		},
		Spec: policy.CodeGraphSpec{
			Repository: "atproto-market",
			Files: []policy.CodeGraphFile{
				{Path: "b.ts"},
				{Path: "a.ts", Test: true},
			},
			Nodes: []policy.CodeGraphNode{
				{ID: "fn:b", File: "b.ts", StartLine: 1},
				{ID: "fn:a", File: "a.ts", StartLine: 3},
			},
		},
	}
	graph.Sort()
	if graph.Spec.Files[0].Path != "a.ts" || graph.Spec.Nodes[0].ID != "fn:a" {
		t.Fatalf("graph not sorted: %+v", graph.Spec)
	}
	if _, ok := graph.Node("fn:a"); !ok {
		t.Fatal("node lookup failed")
	}
	if nodes := graph.NodesInFile("b.ts"); len(nodes) != 1 || nodes[0].ID != "fn:b" {
		t.Fatalf("nodes in file: %+v", nodes)
	}
	object := policy.CodeGraphObject(graph)
	if object["kind"] != policy.CodeGraphKind {
		t.Fatalf("object kind: %v", object["kind"])
	}
	metadata, ok := object["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("object metadata: %T", object["metadata"])
	}
	labels, ok := metadata["labels"].(map[string]any)
	if !ok || labels[policy.CommitLabel] != "deadbeef" {
		t.Fatalf("object labels: %v", metadata["labels"])
	}
	spec, ok := object["spec"].(map[string]any)
	if !ok || spec["repository"] != "atproto-market" {
		t.Fatalf("object spec: %v", object["spec"])
	}
}

func TestViolationIDSeparatesMessages(t *testing.T) {
	a := policy.ViolationID("relay-only", "a.ts", "12", "direct ssh")
	b := policy.ViolationID("relay-only", "a.ts", "12", "direct curl")
	if a == b {
		t.Fatal("two different messages produced the same id")
	}
	if a != policy.ViolationID("relay-only", "a.ts", "12", "direct ssh") {
		t.Fatal("the id is not deterministic")
	}
}
