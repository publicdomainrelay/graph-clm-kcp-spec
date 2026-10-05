package policy

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
)

const DefaultMaxReach = 4096

const DefaultMaxReachHops = 3

const defaultHintHops = 3

const defaultHintNodes = 256

var initiatingEffectKinds = []EffectKind{
	EffectNetDial,
	EffectHTTPRequest,
	EffectSSHConnect,
	EffectContainerExec,
	EffectEventEmit,
}

var triggerRootEffectKinds = []EffectKind{
	EffectHTTPHandle,
	EffectEventReceive,
	EffectProcExec,
	EffectContainerExec,
	EffectHTTPRequest,
}

var DefaultTriggerEdgeKinds = []string{"calls", "instantiates"}

type ModelContext struct {
	Name string

	Labels map[string]string

	Interactions []DeclaredInteraction
}

type DeclaredInteraction struct {
	Self string `json:"self,omitempty"`

	Peer string `json:"peer,omitempty"`

	Initiator string `json:"initiator,omitempty"`

	Channel string `json:"channel,omitempty"`

	Carries []string `json:"carries,omitempty"`

	Purpose string `json:"purpose,omitempty"`

	Level string `json:"level,omitempty"`

	Forbidden bool `json:"forbidden,omitempty"`
}

type ModelInput struct {
	Repository string

	Graph CodeGraph

	Effects []Effect

	Contexts []ModelContext

	Binding Binding

	Interactions []DeclaredInteraction

	TriggerEdgeKinds []string

	MaxReach int

	MaxReachHops int
}

type compiledRole struct {
	name string

	contexts []string

	labels map[string]string

	globs []string

	symbols []*regexp.Regexp

	hintHosts []string

	hintNSIDs []string

	hintRoutes []string

	hintSymbols []*regexp.Regexp

	hintAttrs []*regexp.Regexp
}

func compileRoles(binding Binding) ([]compiledRole, error) {
	out := make([]compiledRole, 0, len(binding.Roles))
	for _, name := range binding.RoleNames() {
		role := binding.Roles[name]
		compiled := compiledRole{
			name:     name,
			contexts: role.Contexts,
			labels:   role.Labels,
			globs:    role.Globs,
		}
		for _, pattern := range role.Symbols {
			regex, err := regexp.Compile(pattern)
			if err != nil {
				return nil, fmt.Errorf("policy: role %s: symbol %q: %w", name, pattern, err)
			}
			compiled.symbols = append(compiled.symbols, regex)
		}
		if role.Targets != nil {
			compiled.hintHosts = role.Targets.Hosts
			compiled.hintNSIDs = role.Targets.NSIDs
			compiled.hintRoutes = role.Targets.Routes
			for _, pattern := range role.Targets.Symbols {
				regex, err := regexp.Compile(pattern)
				if err != nil {
					return nil, fmt.Errorf("policy: role %s: target symbol %q: %w", name, pattern, err)
				}
				compiled.hintSymbols = append(compiled.hintSymbols, regex)
			}
			for _, pattern := range role.Targets.Attrs {
				regex, err := regexp.Compile(pattern)
				if err != nil {
					return nil, fmt.Errorf("policy: role %s: target attr %q: %w", name, pattern, err)
				}
				compiled.hintAttrs = append(compiled.hintAttrs, regex)
			}
		}
		out = append(out, compiled)
	}
	return out, nil
}

func BuildModel(input ModelInput) (ArchitectureModel, error) {
	roles, err := compileRoles(input.Binding)
	if err != nil {
		return ArchitectureModel{}, err
	}
	graph := input.Graph
	effects := input.Effects
	if effects == nil {
		effects = graph.Spec.Effects
	}

	contextLabels := map[string]map[string]string{}
	contextNames := []string{}
	for _, context := range input.Contexts {
		contextLabels[context.Name] = context.Labels
		if !slices.Contains(contextNames, context.Name) {
			contextNames = append(contextNames, context.Name)
		}
	}
	slices.Sort(contextNames)

	owner := assignComponents(graph, roles)
	filesOf := map[string][]string{}
	for _, file := range graph.Spec.Files {
		name := file.Context
		if name == "" {
			name = owner[file.Path]
		}
		if name == "" {
			continue
		}
		filesOf[name] = append(filesOf[name], file.Path)
	}
	for name := range filesOf {
		slices.Sort(filesOf[name])
	}

	names := map[string]bool{}
	for _, name := range contextNames {
		names[name] = true
	}
	for name := range filesOf {
		names[name] = true
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	slices.Sort(ordered)

	nodesByFile := map[string][]CodeGraphNode{}
	for _, node := range graph.Spec.Nodes {
		nodesByFile[node.File] = append(nodesByFile[node.File], node)
	}

	components := make([]ModelComponent, 0, len(ordered))
	rolesOf := map[string][]string{}
	for _, name := range ordered {
		selected := selectRoles(name, filesOf[name], contextLabels[name], nodesByFile, roles)
		rolesOf[name] = selected
		source := SourceObserved
		_, declared := contextLabels[name]
		switch {
		case declared && len(filesOf[name]) > 0:
			source = SourceBoth
		case declared:
			source = SourceDeclared
		}
		context := ""
		if declared {
			context = name
		}
		components = append(components, ModelComponent{
			Name:    name,
			Roles:   selected,
			Globs:   globsOfRoles(selected, roles),
			Context: context,
			Source:  source,
		})
	}

	normalized := make([]Effect, 0, len(effects))
	for _, effect := range effects {
		if effect.Component == "" {
			effect.Component = owner[effect.File]
		}
		normalized = append(normalized, effect)
	}

	model := ArchitectureModel{
		APIVersion: APIVersion,
		Kind:       ArchitectureModelKind,
		Metadata:   ObjectMeta{Name: input.Repository, Namespace: specapi.DefaultNamespace},
		Spec: ArchitectureModelSpec{
			Repository: input.Repository,
			Roles:      input.Binding.RoleNames(),
			Vocabulary: input.Binding.Vocabulary,
			Components: components,
			Effects:    normalized,
			Flows:      []ModelFlow{},
			Triggers:   []ModelTrigger{},
		},
	}
	model.Spec.Flows = observedFlows(model, graph, rolesOf, input.Binding)
	model.Spec.Flows = mergeDeclaredFlows(model.Spec.Flows, declaredInteractions(input), rolesOf)
	model.Spec.Triggers = effectTriggers(graph, normalized, input.TriggerEdgeKinds, input.MaxReach, input.MaxReachHops)
	model.Sort()
	return model, nil
}

// declaredInteractions is every interaction the model was given: the ones
// passed for the whole model, plus each context's own. A per-context
// interaction inherits the context as its self, so a spec author writes
// `interactions` on the context and the peer alone.
func declaredInteractions(input ModelInput) []DeclaredInteraction {
	out := append([]DeclaredInteraction{}, input.Interactions...)
	for _, context := range input.Contexts {
		for _, interaction := range context.Interactions {
			if interaction.Self == "" {
				interaction.Self = context.Name
			}
			out = append(out, interaction)
		}
	}
	return out
}

func assignComponents(graph CodeGraph, roles []compiledRole) map[string]string {
	owner := map[string]string{}
	for _, file := range graph.Spec.Files {
		if file.Context != "" {
			owner[file.Path] = file.Context
			continue
		}
		if name, ok := bestGlobRole(file.Path, roles); ok {
			owner[file.Path] = name
		}
	}
	return owner
}

func bestGlobRole(file string, roles []compiledRole) (string, bool) {
	best := ""
	bestLength := -1
	for _, role := range roles {
		for _, pattern := range role.globs {
			matched, err := path.Match(pattern, file)
			if err != nil || !matched {
				continue
			}
			if len(pattern) > bestLength || (len(pattern) == bestLength && role.name < best) {
				best = role.name
				bestLength = len(pattern)
			}
		}
	}
	return best, best != ""
}

func selectRoles(name string, files []string, labels map[string]string, nodesByFile map[string][]CodeGraphNode, roles []compiledRole) []string {
	out := []string{}
	for _, role := range roles {
		if slices.Contains(role.contexts, name) || labelsMatch(role.labels, labels) || labels[RoleLabel] == role.name {
			out = append(out, role.name)
			continue
		}
		if roleMatchesFiles(role, files, nodesByFile) {
			out = append(out, role.name)
		}
	}
	slices.Sort(out)
	return out
}

func globsOfRoles(selected []string, roles []compiledRole) []string {
	out := []string{}
	for _, role := range roles {
		if !slices.Contains(selected, role.name) {
			continue
		}
		out = append(out, role.globs...)
	}
	sort.Strings(out)
	return slices.Compact(out)
}

func labelsMatch(wanted, labels map[string]string) bool {
	if len(wanted) == 0 {
		return false
	}
	for key, value := range wanted {
		if labels[key] != value {
			return false
		}
	}
	return true
}

func roleMatchesFiles(role compiledRole, files []string, nodesByFile map[string][]CodeGraphNode) bool {
	for _, file := range files {
		for _, pattern := range role.globs {
			if matched, err := path.Match(pattern, file); err == nil && matched {
				return true
			}
		}
		for _, node := range nodesByFile[file] {
			for _, regex := range role.symbols {
				if regex.MatchString(node.QualifiedName) || regex.MatchString(node.Name) {
					return true
				}
			}
		}
	}
	return false
}

type flowIndex struct {
	rolesOf map[string][]string

	handlesByPath map[string][]Effect

	handlesByNSID map[string][]Effect

	roles []compiledRole

	channels map[string][]string

	payloads map[string][]string

	purposes map[string][]string

	nodeText map[string]string

	adjacency map[string][]string

	neighborhoods map[string]string
}

func newFlowIndex(graph CodeGraph, model ArchitectureModel, rolesOf map[string][]string, binding Binding) *flowIndex {
	index := &flowIndex{
		rolesOf:       rolesOf,
		handlesByPath: map[string][]Effect{},
		handlesByNSID: map[string][]Effect{},
		channels:      binding.Vocabulary.Channels,
		payloads:      binding.Vocabulary.Payloads,
		purposes:      binding.Vocabulary.Purposes,
		nodeText:      map[string]string{},
		adjacency:     map[string][]string{},
		neighborhoods: map[string]string{},
	}
	for _, node := range graph.Spec.Nodes {
		if node.ID != "" {
			index.nodeText[node.ID] = node.Text
		}
	}
	for _, edge := range graph.Spec.Edges {
		if edge.Kind != "calls" {
			continue
		}
		index.adjacency[edge.Source] = append(index.adjacency[edge.Source], edge.Target)
	}
	for _, effect := range model.Spec.Effects {
		if effect.Kind != EffectHTTPHandle {
			continue
		}
		if effectPath := effect.Attr("path"); effectPath != "" {
			index.handlesByPath[effectPath] = append(index.handlesByPath[effectPath], effect)
		}
		if nsid := effect.Attr("nsid"); nsid != "" {
			index.handlesByNSID[nsid] = append(index.handlesByNSID[nsid], effect)
		}
	}
	compiled, _ := compileRoles(binding)
	index.roles = compiled
	return index
}

func observedFlows(model ArchitectureModel, graph CodeGraph, rolesOf map[string][]string, binding Binding) []ModelFlow {
	index := newFlowIndex(graph, model, rolesOf, binding)
	out := []ModelFlow{}
	for _, effect := range model.Spec.Effects {
		if !slices.Contains(initiatingEffectKinds, effect.Kind) {
			continue
		}
		from := componentRoles(effect.Component, rolesOf)
		if len(from) == 0 {
			continue
		}
		to := index.targetRoles(effect, from)
		for _, source := range from {
			for _, dest := range to {
				if source == dest {
					continue
				}
				out = append(out, ModelFlow{
					From:      source,
					To:        dest,
					Initiator: source,
					Channel:   index.channel(effect),
					Carries:   index.carries(effect),
					Purpose:   index.purpose(effect),
					Source:    SourceObserved,
					Evidence:  []string{effect.ID},
				})
			}
		}
	}
	return dedupeFlows(out)
}

func componentRoles(component string, rolesOf map[string][]string) []string {
	if roles := rolesOf[component]; len(roles) > 0 {
		return roles
	}
	if component == "" {
		return nil
	}
	return []string{component}
}

func (i *flowIndex) targetRoles(effect Effect, from []string) []string {
	text := i.siteText(effect)
	host, effectPath, nsid := effectTarget(effect)

	if effectPath != "" {
		if roles := i.handleRoles(effectPath, from); len(roles) > 0 {
			return roles
		}
	}
	if nsid != "" {
		if roles := i.handleNSIDRoles(nsid, from); len(roles) > 0 {
			return roles
		}
	}
	if roles := i.hintRoles(host, effectPath, nsid, from); len(roles) > 0 {
		return roles
	}
	bySymbol := []string{}
	for _, role := range i.roles {
		if slices.Contains(from, role.name) {
			continue
		}
		matched := false
		for _, regex := range role.hintSymbols {
			if regex.MatchString(text) {
				matched = true
				break
			}
		}
		if !matched {
			matched = matchesAnyAttr(role.hintAttrs, effect.Attrs)
		}
		if matched {
			bySymbol = append(bySymbol, role.name)
		}
	}
	if len(bySymbol) > 0 {
		return dedupeStrings(bySymbol)
	}
	return []string{RoleUnknown}
}

func (i *flowIndex) handleRoles(effectPath string, from []string) []string {
	out := []string{}
	for _, handle := range i.handlesByPath[effectPath] {
		out = append(out, i.componentRoles(handle.Component, from)...)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func (i *flowIndex) handleNSIDRoles(nsid string, from []string) []string {
	out := []string{}
	for _, handle := range i.handlesByNSID[nsid] {
		out = append(out, i.componentRoles(handle.Component, from)...)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func (i *flowIndex) componentRoles(component string, from []string) []string {
	roles := i.rolesOf[component]
	if len(roles) == 0 {
		if component == "" || slices.Contains(from, component) {
			return nil
		}
		return []string{component}
	}
	// a component talking to itself is not a flow
	if slices.Equal(roles, from) {
		return nil
	}
	return roles
}

func (i *flowIndex) hintRoles(host, effectPath, nsid string, from []string) []string {
	out := []string{}
	for _, role := range i.roles {
		if slices.Contains(from, role.name) {
			continue
		}
		if host != "" && matchesAnyPattern(role.hintHosts, host) {
			out = append(out, role.name)
			continue
		}
		if effectPath != "" && matchesAnyPattern(role.hintRoutes, effectPath) {
			out = append(out, role.name)
			continue
		}
		if nsid != "" && matchesAnyPattern(role.hintNSIDs, nsid) {
			out = append(out, role.name)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func matchesAnyAttr(patterns []*regexp.Regexp, attrs map[string]string) bool {
	values := make([]string, 0, len(attrs))
	for key := range attrs {
		values = append(values, attrs[key])
	}
	sort.Strings(values)
	for _, pattern := range patterns {
		for _, value := range values {
			if pattern.MatchString(value) {
				return true
			}
		}
	}
	return false
}

func matchesAnyPattern(patterns []string, value string) bool {
	for _, pattern := range patterns {
		if pattern == value {
			return true
		}
		if matched, err := path.Match(pattern, value); err == nil && matched {
			return true
		}
	}
	return false
}

// hintText is the text a vocabulary term is looked for in: the effect's site,
// its attributes and the declarations its site calls. A channel or a payload
// class is often not written at the site itself -- an ssh site builds its
// ProxyCommand through one helper that calls another -- so the vocabulary
// resolves a delegated channel.
func (i *flowIndex) hintText(effect Effect) string {
	return i.siteText(effect) + "\n" + i.neighborhood(effect.Node)
}

// siteText is the effect's own site and attributes. It is what a role's target
// symbol hint is matched against: a hint names something the caller says, not
// something a declaration it happens to call says.
func (i *flowIndex) siteText(effect Effect) string {
	parts := []string{i.nodeText[effect.Node]}
	keys := make([]string, 0, len(effect.Attrs))
	for key := range effect.Attrs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts = append(parts, effect.Attrs[key])
	}
	return strings.Join(parts, "\n")
}

// neighborhood is the text of the effect's site and of the declarations it
// calls, a few hops out. A channel or a payload class is often not written at
// the site itself: an ssh site builds its ProxyCommand through one helper that
// calls another. The walk is bounded in hops and nodes, and it is what makes
// the vocabulary resolve a delegated channel.
func (i *flowIndex) neighborhood(node string) string {
	if node == "" {
		return ""
	}
	if cached, ok := i.neighborhoods[node]; ok {
		return cached
	}
	seen := map[string]bool{node: true}
	depth := map[string]int{node: 0}
	queue := []string{node}
	parts := []string{}
	for len(queue) > 0 && len(seen) < defaultHintNodes {
		current := queue[0]
		queue = queue[1:]
		if text := i.nodeText[current]; text != "" {
			parts = append(parts, text)
		}
		if depth[current] >= defaultHintHops {
			continue
		}
		for _, next := range i.adjacency[current] {
			if seen[next] {
				continue
			}
			seen[next] = true
			depth[next] = depth[current] + 1
			queue = append(queue, next)
			if len(seen) >= defaultHintNodes {
				break
			}
		}
	}
	out := strings.Join(parts, "\n")
	i.neighborhoods[node] = out
	return out
}

func effectTarget(effect Effect) (host, effectPath, nsid string) {
	nsid = effect.Attr("nsid")
	if raw := effect.Attr("url"); raw != "" {
		if parsed, err := url.Parse(raw); err == nil && parsed.Host != "" {
			host = parsed.Hostname()
			effectPath = parsed.Path
		} else {
			effectPath = raw
		}
	}
	if host == "" {
		host = effect.Attr("host")
	}
	if effectPath == "" {
		effectPath = effect.Attr("path")
	}
	if host == "" {
		for _, key := range []string{"target", "address"} {
			value := effect.Attr(key)
			if value == "" {
				continue
			}
			if parsed, err := url.Parse(value); err == nil && parsed.Host != "" {
				host = parsed.Hostname()
				if effectPath == "" {
					effectPath = parsed.Path
				}
				break
			}
			if strings.Contains(value, ":") && !strings.Contains(value, "/") {
				host = value
				break
			}
		}
	}
	return host, effectPath, nsid
}

func (i *flowIndex) channel(effect Effect) string {
	text := i.hintText(effect)
	names := make([]string, 0, len(i.channels))
	for name := range i.channels {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, term := range i.channels[name] {
			if containsFold(text, term) {
				return name
			}
		}
	}
	return ""
}

func (i *flowIndex) carries(effect Effect) []string {
	text := i.hintText(effect)
	out := []string{}
	names := make([]string, 0, len(i.payloads))
	for name := range i.payloads {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, term := range i.payloads[name] {
			if containsFold(text, term) {
				out = append(out, name)
				break
			}
		}
	}
	return out
}

func (i *flowIndex) purpose(effect Effect) string {
	text := i.hintText(effect)
	names := make([]string, 0, len(i.purposes))
	for name := range i.purposes {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, term := range i.purposes[name] {
			if containsFold(text, term) {
				return name
			}
		}
	}
	return ""
}

func containsFold(text, term string) bool {
	if term == "" {
		return false
	}
	return strings.Contains(strings.ToLower(text), strings.ToLower(term))
}

func mergeDeclaredFlows(observed []ModelFlow, interactions []DeclaredInteraction, rolesOf map[string][]string) []ModelFlow {
	out := observed
	for _, interaction := range interactions {
		from := componentRoles(interaction.Self, rolesOf)
		to := componentRoles(interaction.Peer, rolesOf)
		if len(from) == 0 || len(to) == 0 {
			continue
		}
		for _, source := range from {
			for _, dest := range to {
				initiator := ""
				switch interaction.Initiator {
				case "self":
					initiator = source
				case "peer":
					initiator = dest
				default:
					initiator = interaction.Initiator
				}
				flow := ModelFlow{
					From:      source,
					To:        dest,
					Initiator: initiator,
					Channel:   interaction.Channel,
					Carries:   interaction.Carries,
					Purpose:   interaction.Purpose,
					Level:     interaction.Level,
					Forbidden: interaction.Forbidden,
					Source:    SourceDeclared,
				}
				out = append(out, flow)
			}
		}
	}
	return dedupeFlows(out)
}

func dedupeFlows(flows []ModelFlow) []ModelFlow {
	index := map[string]int{}
	out := []ModelFlow{}
	for _, flow := range flows {
		// A forbidden marker never merges with a flow that is not forbidden:
		// the marker is what the conformance rule matches a real flow against.
		key := dedupeKey(flow)
		if position, ok := index[key]; ok {
			merged := out[position]
			merged.Evidence = dedupeStrings(append(merged.Evidence, flow.Evidence...))
			if merged.Source != flow.Source {
				merged.Source = SourceBoth
			}
			if len(merged.Carries) == 0 {
				merged.Carries = flow.Carries
			}
			merged.Level = strongerLevel(merged.Level, flow.Level)
			out[position] = merged
			continue
		}
		index[key] = len(out)
		flow.Evidence = dedupeStrings(flow.Evidence)
		out = append(out, flow)
	}
	sort.SliceStable(out, func(left, right int) bool {
		return dedupeKey(out[left]) < dedupeKey(out[right])
	})
	return out
}

func dedupeKey(flow ModelFlow) string {
	key := flowKey(flow)
	if flow.Forbidden {
		return key + "\x00forbidden"
	}
	return key
}

func strongerLevel(left, right string) string {
	if levelRank(right) > levelRank(left) {
		return right
	}
	return left
}

func levelRank(level string) int {
	switch level {
	case "MUST":
		return 3
	case "SHOULD":
		return 2
	case "MAY":
		return 1
	}
	return 0
}

func dedupeStrings(values []string) []string {
	out := []string{}
	for _, value := range values {
		if value == "" || slices.Contains(out, value) {
			continue
		}
		out = append(out, value)
	}
	slices.Sort(out)
	return out
}

func effectTriggers(graph CodeGraph, effects []Effect, edgeKinds []string, maxReach, maxHops int) []ModelTrigger {
	if len(edgeKinds) == 0 {
		edgeKinds = DefaultTriggerEdgeKinds
	}
	if maxReach <= 0 {
		maxReach = DefaultMaxReach
	}
	if maxHops <= 0 {
		maxHops = DefaultMaxReachHops
	}
	adjacency := map[string][]string{}
	for _, edge := range graph.Spec.Edges {
		if !slices.Contains(edgeKinds, edge.Kind) {
			continue
		}
		adjacency[edge.Source] = append(adjacency[edge.Source], edge.Target)
	}
	for key := range adjacency {
		slices.Sort(adjacency[key])
		adjacency[key] = slices.Compact(adjacency[key])
	}

	roots := []Effect{}
	leaves := []Effect{}
	for _, effect := range effects {
		if effect.Node == "" {
			continue
		}
		if slices.Contains(triggerRootEffectKinds, effect.Kind) {
			roots = append(roots, effect)
		}
		if slices.Contains(initiatingEffectKinds, effect.Kind) {
			leaves = append(leaves, effect)
		}
	}

	out := []ModelTrigger{}
	seen := map[string]bool{}
	for _, root := range roots {
		reachable := reachableNodes(root.Node, adjacency, maxReach, maxHops)
		for _, leaf := range leaves {
			if root.ID == leaf.ID || !reachable[leaf.Node] {
				continue
			}
			key := root.ID + "\x00" + leaf.ID
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, ModelTrigger{From: root.ID, To: leaf.ID})
		}
	}
	sort.SliceStable(out, func(left, right int) bool {
		if out[left].From != out[right].From {
			return out[left].From < out[right].From
		}
		return out[left].To < out[right].To
	})
	return out
}

func reachableNodes(root string, adjacency map[string][]string, maxReach, maxHops int) map[string]bool {
	seen := map[string]bool{root: true}
	queue := []string{root}
	depth := map[string]int{root: 0}
	for len(queue) > 0 && len(seen) < maxReach {
		current := queue[0]
		queue = queue[1:]
		if depth[current] >= maxHops {
			continue
		}
		for _, next := range adjacency[current] {
			if seen[next] {
				continue
			}
			seen[next] = true
			depth[next] = depth[current] + 1
			queue = append(queue, next)
			if len(seen) >= maxReach {
				break
			}
		}
	}
	return seen
}
