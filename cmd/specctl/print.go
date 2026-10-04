package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/delta"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
)

func printObjects(out io.Writer, items []unstructured.Unstructured, format string) error {
	switch format {
	case "name":
		for _, item := range items {
			fmt.Fprintf(out, "%s/%s\n", strings.ToLower(item.GetKind()), item.GetName())
		}
		return nil
	case "yaml":
		for index, item := range items {
			if index > 0 {
				fmt.Fprintln(out, "---")
			}
			encoded, err := yaml.Marshal(item.Object)
			if err != nil {
				return err
			}
			if _, err := out.Write(encoded); err != nil {
				return err
			}
		}
		return nil
	case "json":
		list := map[string]any{
			"apiVersion": specapi.APIVersion,
			"kind":       "List",
			"items":      items,
		}
		encoded, err := json.MarshalIndent(list, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(out, string(encoded))
		return nil
	case "table", "":
		return printTable(out, items)
	}
	return fmt.Errorf("unknown output format %q", format)
}

func printTable(out io.Writer, items []unstructured.Unstructured) error {
	if len(items) == 0 {
		fmt.Fprintln(out, "No resources found")
		return nil
	}
	table := tabwriter.NewWriter(out, 0, 0, 3, ' ', 0)
	switch items[0].GetKind() {
	case specapi.SystemContextKind:
		fmt.Fprintln(table, "NAME\tREPOSITORY\tINTERFACES\tREQUIREMENTS\tVALID\tINTENT")
	case specapi.RepositoryKind:
		fmt.Fprintln(table, "NAME\tSOURCE\tPHASE\tCONTEXTS\tHEADCOMMIT")
	case specapi.SpecChangeKind:
		fmt.Fprintln(table, "NAME\tCONTEXT\tDIRECTION\tPHASE\tDELTA\tCOMMIT")
	}
	for _, item := range items {
		row, err := tableRow(item)
		if err != nil {
			return err
		}
		fmt.Fprintln(table, strings.Join(row, "\t"))
	}
	return table.Flush()
}

func tableRow(item unstructured.Unstructured) ([]string, error) {
	switch item.GetKind() {
	case specapi.SystemContextKind:
		interfaces, _, _ := unstructured.NestedSlice(item.Object, "spec", "interfaces")
		requirements, _, _ := unstructured.NestedSlice(item.Object, "spec", "requirements")
		return []string{
			item.GetName(),
			nestedString(item, "spec", "repository"),
			fmt.Sprint(len(interfaces)),
			fmt.Sprint(len(requirements)),
			conditionStatus(item, specapi.ConditionSpecValid),
			truncate(nestedString(item, "spec", "intent"), 48),
		}, nil
	case specapi.RepositoryKind:
		source := nestedString(item, "spec", "source", "path")
		if source == "" {
			source = nestedString(item, "spec", "path")
		}
		if resolved := nestedString(item, "status", "resolvedPath"); resolved != "" {
			source = resolved
		}
		total, _, _ := unstructured.NestedInt64(item.Object, "status", "contexts", "total")
		return []string{
			item.GetName(),
			truncate(source, 40),
			nestedString(item, "status", "phase"),
			fmt.Sprint(total),
			truncate(nestedString(item, "status", "headCommit"), 12),
		}, nil
	case specapi.SpecChangeKind:
		return []string{
			item.GetName(),
			nestedString(item, "spec", "systemContext"),
			nestedString(item, "spec", "direction"),
			nestedString(item, "status", "phase"),
			deltaSummary(item),
			truncate(nestedString(item, "status", "commit"), 12),
		}, nil
	}
	return nil, fmt.Errorf("no table columns for kind %q", item.GetKind())
}

func deltaSummary(item unstructured.Unstructured) string {
	raw, found, err := unstructured.NestedMap(item.Object, "spec", "delta")
	if err != nil || !found || len(raw) == 0 {
		return "-"
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return "-"
	}
	change := spec.Delta{}
	if err := json.Unmarshal(encoded, &change); err != nil {
		return "-"
	}
	return delta.Summary(change)
}

func nestedString(item unstructured.Unstructured, fields ...string) string {
	value, found, err := unstructured.NestedString(item.Object, fields...)
	if err != nil || !found {
		return ""
	}
	return value
}

func conditionStatus(item unstructured.Unstructured, conditionType string) string {
	conditions, found, err := unstructured.NestedSlice(item.Object, "status", "conditions")
	if err != nil || !found {
		return ""
	}
	for _, entry := range conditions {
		condition, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if condition["type"] == conditionType {
			status, _ := condition["status"].(string)
			return status
		}
	}
	return ""
}

func conditionStatusOf(conditions []metav1.Condition, conditionType string) string {
	for _, condition := range conditions {
		if condition.Type == conditionType {
			return string(condition.Status)
		}
	}
	return ""
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	if limit <= 3 {
		return value[:limit]
	}
	return value[:limit-3] + "..."
}
