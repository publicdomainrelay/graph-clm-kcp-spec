// Package archyaml reads an open architecture document (arch.yaml) into a flat
// node model and writes one back out.
//
// The document is a small header (schema, apiVersion, kind, metadata and any
// other top-level key that is not a node section) plus sections of nodes. A
// node is any mapping with a string id; nodes nest inside each other through
// the upstream, overlay, orchestrator, children or context keys, and a nested
// node is either inline (a mapping with an id) or a plain id ref. Parse keeps
// that tree as one flat list: every node knows its parent id and the slot path
// it occupied inside the parent, and its body holds everything the node owned,
// with the inline children replaced by their id refs. Marshal walks the flat
// list back into the tree, so import and export are inverse for the ids, the
// refs and the data the document carries.
package archyaml

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"
)

const SystemContexts = "system_contexts"

type Document struct {
	Header map[string]any

	Sections []*Section
}

type Section struct {
	Key string

	Form string

	Nodes []*Node
}

const (
	FormMap  = "map"
	FormList = "list"
)

type Node struct {
	ID string

	Section string

	Form string

	Position int

	Parent string

	Slot string

	Body map[string]any

	Upstream     string
	Overlay      []string
	Orchestrator string
	DependsOn    []string
	Introduces   []string
	Code         []string
}

func (d *Document) Section(key string) *Section {
	for _, section := range d.Sections {
		if section.Key == key {
			return section
		}
	}
	return nil
}

func (d *Document) Nodes() []*Node {
	out := []*Node{}
	for _, section := range d.Sections {
		out = append(out, section.Nodes...)
	}
	return out
}

func (d *Document) Node(id string) *Node {
	for _, node := range d.Nodes() {
		if node.ID == id {
			return node
		}
	}
	return nil
}

// Roots are the nodes a section holds directly; a node with a parent is placed
// inside that parent instead.
func (s *Section) Roots() []*Node {
	out := []*Node{}
	for _, node := range s.Nodes {
		if node.Parent == "" {
			out = append(out, node)
		}
	}
	return out
}

func Parse(data []byte) (*Document, error) {
	raw := map[string]any{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("archyaml: parse: %w", err)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("archyaml: the document is empty")
	}
	document := &Document{Header: map[string]any{}}
	for _, key := range sortedKeys(raw) {
		value := raw[key]
		switch {
		case key == SystemContexts:
			section, err := flattenList(key, value)
			if err != nil {
				return nil, err
			}
			if section != nil {
				document.Sections = append(document.Sections, section)
			}
		case isNodeMap(value):
			document.Sections = append(document.Sections, flattenMap(key, value.(map[string]any)))
		case isNodeList(value):
			section, err := flattenList(key, value)
			if err != nil {
				return nil, err
			}
			if section != nil {
				document.Sections = append(document.Sections, section)
			}
		default:
			document.Header[key] = value
		}
	}
	return document, nil
}

// isNodeMap matches the upstreams/orchestrators/types shape: a mapping whose
// values are all mappings, so an arch id is the key and the node is the value.
func isNodeMap(value any) bool {
	mapping, ok := value.(map[string]any)
	if !ok || len(mapping) == 0 {
		return false
	}
	for _, entry := range mapping {
		if _, ok := entry.(map[string]any); !ok {
			return false
		}
	}
	return true
}

// isNodeList matches the trust_boundaries/crossings/flows shape: a list of
// mappings that each carry an id.
func isNodeList(value any) bool {
	list, ok := value.([]any)
	if !ok || len(list) == 0 {
		return false
	}
	for _, entry := range list {
		mapping, ok := entry.(map[string]any)
		if !ok {
			return false
		}
		if id, ok := mapping["id"].(string); !ok || id == "" {
			return false
		}
	}
	return true
}

// flattenMap handles the upstreams/orchestrators/types shape, where the key is
// the id and the value is the node, so the value carries no id of its own.
func flattenMap(key string, value map[string]any) *Section {
	section := &Section{Key: key, Form: FormMap}
	walker := &walker{section: section}
	for position, id := range sortedKeys(value) {
		entry, ok := value[id].(map[string]any)
		if !ok {
			continue
		}
		node := &Node{ID: id, Section: key, Form: FormMap, Position: position}
		body := map[string]any{}
		for _, bodyKey := range sortedKeys(entry) {
			body[bodyKey] = walker.transform(entry[bodyKey], id, bodyKey)
		}
		node.Body = body
		walker.derive(node, entry)
		section.Nodes = append(section.Nodes, node)
	}
	return section
}

func flattenList(key string, value any) (*Section, error) {
	list, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("archyaml: %s must be a list", key)
	}
	section := &Section{Key: key, Form: FormList}
	walker := &walker{section: section}
	for position, entry := range list {
		mapping, ok := entry.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("archyaml: %s[%d] is not a mapping", key, position)
		}
		transformed := walker.transform(mapping, "", "")
		node := walker.last
		if node == nil {
			return nil, fmt.Errorf("archyaml: %s[%d] has no id", key, position)
		}
		node.Position = position
		if body, ok := transformed.(map[string]any); ok {
			node.Body = body
		}
	}
	return section, nil
}

// walker turns one section into flat nodes. It records the node it created last
// so the section loop can stamp the position, which is the only thing the walk
// itself does not know.
type walker struct {
	section *Section

	last *Node
}

// transform copies a value, replacing every inline node with its id ref and
// appending the node to the section. `parent` is the id of the nearest
// enclosing node and `slot` is the path from that node to this value.
func (w *walker) transform(value any, parent, slot string) any {
	switch typed := value.(type) {
	case map[string]any:
		if id, ok := typed["id"].(string); ok && id != "" {
			node := &Node{ID: id, Section: w.section.Key, Form: w.section.Form, Parent: parent, Slot: slot}
			body := map[string]any{}
			for _, key := range sortedKeys(typed) {
				body[key] = w.transform(typed[key], id, key)
			}
			node.Body = body
			w.derive(node, typed)
			w.section.Nodes = append(w.section.Nodes, node)
			w.last = node
			return id
		}
		out := map[string]any{}
		for _, key := range sortedKeys(typed) {
			out[key] = w.transform(typed[key], parent, joinSlot(slot, key))
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for index, entry := range typed {
			out = append(out, w.transform(entry, parent, fmt.Sprintf("%s[%d]", slot, index)))
		}
		return out
	default:
		return value
	}
}

func (w *walker) derive(node *Node, original map[string]any) {
	node.Upstream = refOf(original["upstream"])
	node.Overlay = refsOf(original["overlay"])
	node.Orchestrator = refOf(original["orchestrator"])
	node.DependsOn = refsOf(original["depends_on"])
	node.Introduces = refsOf(original["introduces"])
	node.Code = codePaths(original["code"])
}

func joinSlot(slot, key string) string {
	if slot == "" {
		return key
	}
	return slot + "/" + key
}

func refOf(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case map[string]any:
		if id, ok := typed["id"].(string); ok {
			return id
		}
	case []any:
		for _, entry := range typed {
			if ref := refOf(entry); ref != "" {
				return ref
			}
		}
	}
	return ""
}

func refsOf(value any) []string {
	switch typed := value.(type) {
	case string:
		if typed == "" {
			return nil
		}
		return []string{typed}
	case map[string]any:
		if id, ok := typed["id"].(string); ok && id != "" {
			return []string{id}
		}
	case []any:
		out := []string{}
		for _, entry := range typed {
			if ref := refOf(entry); ref != "" {
				out = append(out, ref)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}
	return nil
}

// codePaths turns the code field into file paths. The field is a path, a list
// of paths, or a mapping of names to either, and a path may carry a symbol
// after a colon (deploy/start-kcp.sh:KCP_BIN). A value that is not a path at
// all (a shell command, say) is dropped rather than turned into a broken code
// ref.
func codePaths(value any) []string {
	seen := map[string]bool{}
	out := []string{}
	var add func(any)
	add = func(entry any) {
		switch typed := entry.(type) {
		case string:
			if path, ok := codePath(typed); ok && !seen[path] {
				seen[path] = true
				out = append(out, path)
			}
		case []any:
			for _, item := range typed {
				add(item)
			}
		case map[string]any:
			for _, key := range sortedKeys(typed) {
				add(typed[key])
			}
		}
	}
	add(value)
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

func codePath(value string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", false
	}
	if index := strings.IndexAny(trimmed, " \t\n'\"$()"); index >= 0 {
		return "", false
	}
	path, _, _ := strings.Cut(trimmed, ":")
	if path == "" || !strings.ContainsAny(path, "/.") {
		return "", false
	}
	return path, true
}

// Marshal writes the document back out. Top level keys come out sorted, which
// is the canonical order for a semantic comparison; the order that matters, the
// order of a list section, is kept by the node positions.
func Marshal(document *Document) ([]byte, error) {
	out := map[string]any{}
	for key, value := range document.Header {
		out[key] = value
	}
	for _, section := range document.Sections {
		children := childrenByParent(section)
		switch section.Form {
		case FormMap:
			mapping := map[string]any{}
			for _, node := range section.Roots() {
				mapping[node.ID] = rebuild(node, children)
			}
			out[section.Key] = mapping
		default:
			ordered := append([]*Node{}, section.Roots()...)
			sort.SliceStable(ordered, func(left, right int) bool {
				return ordered[left].Position < ordered[right].Position
			})
			list := make([]any, 0, len(ordered))
			for _, node := range ordered {
				list = append(list, rebuild(node, children))
			}
			out[section.Key] = list
		}
	}
	encoded, err := yaml.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("archyaml: marshal: %w", err)
	}
	return encoded, nil
}

func childrenByParent(section *Section) map[string][]*Node {
	out := map[string][]*Node{}
	for _, node := range section.Nodes {
		if node.Parent == "" {
			continue
		}
		out[node.Parent] = append(out[node.Parent], node)
	}
	for parent := range out {
		sort.SliceStable(out[parent], func(left, right int) bool {
			return slotLess(out[parent][left].Slot, out[parent][right].Slot)
		})
	}
	return out
}

// rebuild puts a node back together, re-inlining every node that was nested
// inside it at the slot the node recorded.
func rebuild(node *Node, children map[string][]*Node) map[string]any {
	out := deepCopy(node.Body).(map[string]any)
	for _, child := range children[node.ID] {
		setSlot(out, child.Slot, rebuild(child, children))
	}
	return out
}

// slotLess orders two slots by list index, so inserting children into the same
// list happens left to right and the indices stay the ones the nodes recorded.
func slotLess(left, right string) bool {
	leftName, leftIndex, leftIsIndex := parseStep(lastStep(left))
	rightName, rightIndex, rightIsIndex := parseStep(lastStep(right))
	if leftName != rightName {
		return left < right
	}
	if leftIsIndex && rightIsIndex {
		return leftIndex < rightIndex
	}
	return left < right
}

func lastStep(slot string) string {
	steps := strings.Split(slot, "/")
	return steps[len(steps)-1]
}

func setSlot(root map[string]any, slot string, value any) {
	steps := strings.Split(slot, "/")
	var current any = root
	for index, step := range steps {
		name, position, isIndex := parseStep(step)
		last := index == len(steps)-1
		if name == "" {
			list, ok := current.([]any)
			if !ok || position < 0 || position >= len(list) {
				return
			}
			if last {
				list[position] = value
				return
			}
			current = list[position]
			continue
		}
		mapping, ok := current.(map[string]any)
		if !ok {
			return
		}
		if !isIndex {
			if last {
				mapping[name] = value
				return
			}
			current = mapping[name]
			continue
		}
		list, ok := mapping[name].([]any)
		if !ok || position < 0 || position >= len(list) {
			return
		}
		if last {
			list[position] = value
			return
		}
		current = list[position]
	}
}

func parseStep(step string) (name string, position int, isIndex bool) {
	if open := strings.Index(step, "["); open >= 0 && strings.HasSuffix(step, "]") {
		name = step[:open]
		index := 0
		if _, err := fmt.Sscanf(step[open+1:len(step)-1], "%d", &index); err != nil {
			return "", 0, false
		}
		return name, index, true
	}
	return step, 0, false
}

func deepCopy(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, entry := range typed {
			out[key] = deepCopy(entry)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, entry := range typed {
			out = append(out, deepCopy(entry))
		}
		return out
	default:
		return value
	}
}

// Diff compares two documents by ids, refs, placement and preserved data. It
// returns one line per difference, so an empty result is a semantic match.
func Diff(left, right *Document) []string {
	differences := []string{}
	if !equalJSON(left.Header, right.Header) {
		differences = append(differences, fmt.Sprintf("header differs: %v != %v", left.Header, right.Header))
	}
	if len(left.Sections) != len(right.Sections) {
		differences = append(differences, fmt.Sprintf("sections = %d, want %d", len(right.Sections), len(left.Sections)))
	}
	for index := 0; index < len(left.Sections) && index < len(right.Sections); index++ {
		expected, actual := left.Sections[index], right.Sections[index]
		if expected.Key != actual.Key || expected.Form != actual.Form {
			differences = append(differences, fmt.Sprintf("section %d = %s/%s, want %s/%s", index, actual.Key, actual.Form, expected.Key, expected.Form))
		}
	}
	leftNodes, rightNodes := byID(left.Nodes()), byID(right.Nodes())
	for id, expected := range leftNodes {
		actual, ok := rightNodes[id]
		if !ok {
			differences = append(differences, fmt.Sprintf("node %s is missing", id))
			continue
		}
		differences = append(differences, diffNode(expected, actual)...)
	}
	for id := range rightNodes {
		if _, ok := leftNodes[id]; !ok {
			differences = append(differences, fmt.Sprintf("node %s is unexpected", id))
		}
	}
	sort.Strings(differences)
	return differences
}

func diffNode(expected, actual *Node) []string {
	differences := []string{}
	at := func(format string, args ...any) {
		differences = append(differences, fmt.Sprintf("%s: ", expected.ID)+fmt.Sprintf(format, args...))
	}
	if expected.Section != actual.Section || expected.Form != actual.Form || expected.Position != actual.Position {
		at("place = %s/%s/%d, want %s/%s/%d", actual.Section, actual.Form, actual.Position, expected.Section, expected.Form, expected.Position)
	}
	if expected.Parent != actual.Parent || expected.Slot != actual.Slot {
		at("parent/slot = %q/%q, want %q/%q", actual.Parent, actual.Slot, expected.Parent, expected.Slot)
	}
	if !equalJSON(expected.Body, actual.Body) {
		at("body differs: %v != %v", actual.Body, expected.Body)
	}
	for _, pair := range []struct {
		field    string
		expected any
		actual   any
	}{
		{"upstream", expected.Upstream, actual.Upstream},
		{"overlay", expected.Overlay, actual.Overlay},
		{"orchestrator", expected.Orchestrator, actual.Orchestrator},
		{"dependsOn", expected.DependsOn, actual.DependsOn},
		{"introduces", expected.Introduces, actual.Introduces},
		{"code", expected.Code, actual.Code},
	} {
		if !equalJSON(pair.expected, pair.actual) {
			at("%s = %v, want %v", pair.field, pair.actual, pair.expected)
		}
	}
	return differences
}

func byID(nodes []*Node) map[string]*Node {
	out := make(map[string]*Node, len(nodes))
	for _, node := range nodes {
		out[node.ID] = node
	}
	return out
}

// equalJSON compares through JSON, the same path the kcp client takes, so two
// values that made the same trip compare equal.
func equalJSON(left, right any) bool {
	leftJSON, err := json.Marshal(left)
	if err != nil {
		return false
	}
	rightJSON, err := json.Marshal(right)
	if err != nil {
		return false
	}
	return string(leftJSON) == string(rightJSON)
}

func sortedKeys(mapping map[string]any) []string {
	keys := make([]string, 0, len(mapping))
	for key := range mapping {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
