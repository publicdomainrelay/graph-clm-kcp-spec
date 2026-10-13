package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/rego"
	"github.com/open-policy-agent/opa/storage/inmem"
	"sigs.k8s.io/yaml"
)

const apiVersion = "specs.publicdomainrelay.dev/v1alpha1"

func main() {
	ctx := context.Background()
	var opts []func(*rego.Rego)
	var testSource string
	var modules, models []string
	for index := 1; index < len(os.Args); index++ {
		if os.Args[index] == "--model" {
			models = append(models, os.Args[index+1]+" "+os.Args[index+2])
			index += 2
			continue
		}
		modules = append(modules, os.Args[index])
	}
	for _, p := range modules {
		b, err := os.ReadFile(p)
		if err != nil {
			fmt.Println("READ FAIL", p, err)
			os.Exit(1)
		}
		name := filepath.Base(p)
		if _, err := ast.ParseModule(name, string(b)); err != nil {
			fmt.Println("PARSE FAIL", p, err)
			os.Exit(1)
		}
		if strings.HasSuffix(name, "_test.rego") {
			testSource = string(b)
		}
		opts = append(opts, rego.Module(name, string(b)))
	}

	testCount, fail := 0, false

	mod, err := ast.ParseModule("src_test.rego", testSource)
	if err != nil {
		fmt.Println("PARSE FAIL tests", err)
		os.Exit(1)
	}
	for _, rule := range mod.Rules {
		name := rule.Head.Name.String()
		if !strings.HasPrefix(name, "test_") {
			continue
		}
		testCount++
		q := rego.New(append(opts, rego.Query("data.relayonlyssh."+name))...)
		rs, err := q.Eval(ctx)
		if err != nil {
			fmt.Println("EVAL FAIL", name, err)
			fail = true
			continue
		}
		if len(rs) != 1 || len(rs[0].Expressions) != 1 || rs[0].Expressions[0].Value != true {
			fmt.Println("FAIL", name)
			fail = true
			continue
		}
		fmt.Println("ok", name)
	}

	for i := 0; i+1 < len(os.Args); i++ {
		if os.Args[i] != "--model" {
			continue
		}
		want := os.Args[i+2]
		got, err := review(ctx, opts, os.Args[i+1])
		if err != nil {
			fmt.Println("MODEL FAIL", os.Args[i+1], err)
			fail = true
			continue
		}
		status := "ok"
		if fmt.Sprint(got) != want {
			status = "FAIL"
			fail = true
		}
		fmt.Printf("%s %s: violations=%d want=%s\n", status, filepath.Base(os.Args[i+1]), got, want)
	}

	if fail {
		os.Exit(1)
	}
	fmt.Println("all", testCount, "unit tests and every model case pass")
}

func review(ctx context.Context, opts []func(*rego.Rego), path string) (int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	object := map[string]interface{}{}
	if err := yaml.Unmarshal(raw, &object); err != nil {
		return 0, err
	}
	meta, _ := object["metadata"].(map[string]interface{})
	name, _ := meta["name"].(string)
	inventory := map[string]interface{}{
		"namespace": map[string]interface{}{
			"default": map[string]interface{}{
				apiVersion: map[string]interface{}{
					"ArchitectureModel": map[string]interface{}{name: object},
				},
			},
		},
	}
	input := map[string]interface{}{
		"parameters": map[string]interface{}{
			"requesterRole": "requester", "hostRole": "host", "testRole": "test", "relayChannelClass": "relay",
		},
		"review": map[string]interface{}{"kind": map[string]interface{}{"kind": "ArchitectureModel"}, "object": object},
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return 0, err
	}
	q := rego.New(append(opts,
		rego.Query("count(data.relayonlyssh.violation)"),
		rego.Store(inmem.NewFromObject(map[string]interface{}{"inventory": inventory})),
		rego.Input(json.RawMessage(encoded)),
	)...)
	rs, err := q.Eval(ctx)
	if err != nil {
		return 0, err
	}
	value, ok := rs[0].Expressions[0].Value.(json.Number)
	if !ok {
		return 0, fmt.Errorf("unexpected result %v", rs[0].Expressions[0].Value)
	}
	n, err := value.Int64()
	return int(n), err
}
