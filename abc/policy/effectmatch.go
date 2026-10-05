package policy

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
)

type ClassifierPack struct {
	Language string `json:"language"`

	Version string `json:"version,omitempty"`

	Rules []EffectRule `json:"rules"`
}

type EffectRule struct {
	ID string `json:"id"`

	Kind EffectKind `json:"kind"`

	Call *CallRule `json:"call,omitempty"`

	Command *CommandRule `json:"command,omitempty"`

	Route *RouteRule `json:"route,omitempty"`

	Import *ImportRule `json:"import,omitempty"`

	Args *ArgsRule `json:"args,omitempty"`

	RequiresImport string `json:"requiresImport,omitempty"`

	Extra bool `json:"extra,omitempty"`

	InStrings bool `json:"inStrings,omitempty"`

	Target string `json:"target,omitempty"`

	Attrs map[string]string `json:"attrs,omitempty"`

	Extract []ExtractRule `json:"extract,omitempty"`
}

func (r EffectRule) MatcherName() string {
	switch {
	case r.Call != nil:
		return r.Call.Name
	case r.Command != nil:
		return r.Command.Name
	case r.Route != nil:
		return "route"
	case r.Import != nil:
		return r.Import.Specifier
	}
	return r.ID
}

type CallRule struct {
	Name string `json:"name"`

	Member bool `json:"member,omitempty"`

	Any bool `json:"any,omitempty"`

	New bool `json:"new,omitempty"`

	Argv0 []string `json:"argv0,omitempty"`

	TargetArg int `json:"targetArg,omitempty"`
}

type CommandRule struct {
	Name string `json:"name"`

	Verbs []string `json:"verbs,omitempty"`
}

type RouteRule struct {
	Methods []string `json:"methods,omitempty"`

	Receiver string `json:"receiver,omitempty"`

	PathRegex string `json:"pathRegex,omitempty"`

	Node bool `json:"node,omitempty"`
}

type ImportRule struct {
	Specifier string `json:"specifier"`
}

type ArgsRule struct {
	Regex string `json:"regex"`

	Any bool `json:"any,omitempty"`
}

type ExtractRule struct {
	Attr string `json:"attr"`

	Regex string `json:"regex"`

	Group int `json:"group,omitempty"`

	Trim bool `json:"trim,omitempty"`
}

type MatchOptions struct {
	IncludeExtras bool
}

type Matcher struct {
	packs []compiledPack
}

type compiledPack struct {
	language string
	version  string
	rules    []compiledRule
}

type compiledRule struct {
	id             string
	kind           EffectKind
	extra          bool
	inStrings      bool
	attrs          map[string]string
	target         string
	extract        []compiledExtract
	importSpec     *regexp.Regexp
	requiresImport *regexp.Regexp
	call           *regexp.Regexp
	member         bool
	argv0          map[string]bool
	targetArg      int
	command        *regexp.Regexp
	verbs          map[string]bool
	args           *regexp.Regexp
	route          *regexp.Regexp
	routeMethods   map[string]string
	routeReceiver  *regexp.Regexp
	routePath      *regexp.Regexp
}

type compiledExtract struct {
	attr  string
	regex *regexp.Regexp
	group int
	trim  bool
}

func Compile(packs ...ClassifierPack) (*Matcher, error) {
	compiled := make([]compiledPack, 0, len(packs))
	for _, pack := range packs {
		one, err := compilePack(pack)
		if err != nil {
			return nil, err
		}
		compiled = append(compiled, one)
	}
	return &Matcher{packs: compiled}, nil
}

func compilePack(pack ClassifierPack) (compiledPack, error) {
	if strings.TrimSpace(pack.Language) == "" {
		return compiledPack{}, fmt.Errorf("effects: classifier pack has no language")
	}
	out := compiledPack{language: languageFamily(pack.Language), version: pack.Version}
	for _, rule := range pack.Rules {
		compiled, err := compileRule(rule)
		if err != nil {
			return compiledPack{}, err
		}
		out.rules = append(out.rules, compiled)
	}
	return out, nil
}

func compileRule(rule EffectRule) (compiledRule, error) {
	out := compiledRule{
		id:        rule.ID,
		kind:      rule.Kind,
		extra:     rule.Extra,
		inStrings: rule.InStrings,
		attrs:     rule.Attrs,
		target:    rule.Target,
	}
	if !KnownEffectKind(rule.Kind) {
		return out, fmt.Errorf("effects: rule %s: unknown effect kind %q", rule.ID, rule.Kind)
	}
	for _, extraction := range rule.Extract {
		regex, err := regexp.Compile(extraction.Regex)
		if err != nil {
			return out, fmt.Errorf("effects: rule %s: extract %s: %w", rule.ID, extraction.Attr, err)
		}
		out.extract = append(out.extract, compiledExtract{
			attr:  extraction.Attr,
			regex: regex,
			group: extraction.Group,
			trim:  extraction.Trim,
		})
	}
	if rule.Call != nil {
		name := strings.TrimSpace(rule.Call.Name)
		if name == "" {
			return out, fmt.Errorf("effects: rule %s: call has no name", rule.ID)
		}
		pattern := `\b` + regexp.QuoteMeta(name) + `\s*\(`
		switch {
		case rule.Call.Member:
			pattern = `\.` + regexp.QuoteMeta(lastSegment(name)) + `\s*\(`
		case rule.Call.New:
			pattern = `\bnew\s+` + regexp.QuoteMeta(name) + `\s*\(`
		case rule.Call.Any:
			pattern = `\b` + regexp.QuoteMeta(lastSegment(name)) + `\s*\(`
		}
		regex, err := regexp.Compile(pattern)
		if err != nil {
			return out, fmt.Errorf("effects: rule %s: %w", rule.ID, err)
		}
		out.call = regex
		out.member = rule.Call.Member || rule.Call.Any
		out.targetArg = rule.Call.TargetArg
		if len(rule.Call.Argv0) > 0 {
			out.argv0 = map[string]bool{}
			for _, value := range rule.Call.Argv0 {
				out.argv0[value] = true
			}
		}
	}
	if rule.Command != nil {
		name := strings.TrimSpace(rule.Command.Name)
		if name == "" {
			return out, fmt.Errorf("effects: rule %s: command has no name", rule.ID)
		}
		regex, err := regexp.Compile(`(?:^|[\s;&|('"\x60])(` + name + `)(?:\s|$)`)
		if err != nil {
			return out, fmt.Errorf("effects: rule %s: command: %w", rule.ID, err)
		}
		out.command = regex
		if len(rule.Command.Verbs) > 0 {
			out.verbs = map[string]bool{}
			for _, verb := range rule.Command.Verbs {
				out.verbs[verb] = true
			}
		}
	}
	if rule.Route != nil {
		alternation := `[A-Za-z_$][\w$]*`
		if len(rule.Route.Methods) > 0 {
			alternation = strings.Join(rule.Route.Methods, "|")
		}
		regex, err := regexp.Compile(`\.(` + alternation + `)\s*\(`)
		if err != nil {
			return out, fmt.Errorf("effects: rule %s: %w", rule.ID, err)
		}
		out.route = regex
		out.routeMethods = map[string]string{}
		for _, method := range rule.Route.Methods {
			out.routeMethods[strings.ToLower(method)] = strings.ToUpper(method)
		}
		if rule.Route.Receiver != "" {
			receiver, err := regexp.Compile(rule.Route.Receiver)
			if err != nil {
				return out, fmt.Errorf("effects: rule %s: receiver: %w", rule.ID, err)
			}
			out.routeReceiver = receiver
		}
		path := `^/`
		if rule.Route.PathRegex != "" {
			path = rule.Route.PathRegex
		}
		pathRegex, err := regexp.Compile(path)
		if err != nil {
			return out, fmt.Errorf("effects: rule %s: pathRegex: %w", rule.ID, err)
		}
		out.routePath = pathRegex
	}
	if rule.Import != nil {
		specifier, err := regexp.Compile(rule.Import.Specifier)
		if err != nil {
			return out, fmt.Errorf("effects: rule %s: import specifier: %w", rule.ID, err)
		}
		out.importSpec = specifier
	}
	if rule.RequiresImport != "" {
		required, err := regexp.Compile(rule.RequiresImport)
		if err != nil {
			return out, fmt.Errorf("effects: rule %s: requiresImport: %w", rule.ID, err)
		}
		out.requiresImport = required
	}
	if rule.Args != nil {
		args, err := regexp.Compile(rule.Args.Regex)
		if err != nil {
			return out, fmt.Errorf("effects: rule %s: args: %w", rule.ID, err)
		}
		out.args = args
	}
	return out, nil
}

func lastSegment(name string) string {
	if index := strings.LastIndex(name, "."); index >= 0 {
		return name[index+1:]
	}
	return name
}

func languageFamily(language string) string {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "typescript", "ts", "tsx", "javascript", "js", "jsx", "deno":
		return "typescript"
	case "go", "golang":
		return "go"
	case "shell", "sh", "bash", "zsh", "console":
		return "shell"
	}
	return strings.ToLower(strings.TrimSpace(language))
}

func (m *Matcher) Effects(graph CodeGraph, opts MatchOptions) []Effect {
	out := []Effect{}
	contexts := newContextCache(graph)
	importConsts := importConstants(graph)
	claimed := map[string]map[int]bool{}
	for _, file := range graph.Spec.Files {
		source := sourceOf(graph, file.Path)
		if source == "" {
			continue
		}
		family := languageFamily(file.Language)
		if claimed[file.Path] == nil {
			claimed[file.Path] = map[int]bool{}
		}
		scan := newScanner(source, family, importConsts[file.Path])
		imports := scan.imports()
		for index := range m.packs {
			pack := &m.packs[index]
			if pack.language != family {
				continue
			}
			for ruleIndex := range pack.rules {
				rule := &pack.rules[ruleIndex]
				if rule.extra && !opts.IncludeExtras {
					continue
				}
				if rule.requiresImport != nil && !matchesAny(rule.requiresImport, imports) {
					continue
				}
				sites := scan.sites(rule, claimed[file.Path])
				for _, site := range sites {
					node := nodeAt(graph, file.Path, site.line)
					effect := m.effect(rule, file, site, node, contexts.site(file.Path, node, source, site.line))
					out = append(out, effect)
				}
			}
		}
	}
	out = append(out, m.routeNodeEffects(graph)...)
	return dedupeEffects(out)
}

func (m *Matcher) EffectsIn(file, language, source string, opts MatchOptions) []Effect {
	out := []Effect{}
	scan := newScanner(source, languageFamily(language), nil)
	imports := scan.imports()
	claimed := map[int]bool{}
	for index := range m.packs {
		pack := &m.packs[index]
		if pack.language != scan.family {
			continue
		}
		for ruleIndex := range pack.rules {
			rule := &pack.rules[ruleIndex]
			if rule.extra && !opts.IncludeExtras {
				continue
			}
			if rule.requiresImport != nil && !matchesAny(rule.requiresImport, imports) {
				continue
			}
			for _, site := range scan.sites(rule, claimed) {
				out = append(out, m.effect(rule, CodeGraphFile{Path: file, Language: language}, site, CodeGraphNode{}, site.args))
			}
		}
	}
	return dedupeEffects(out)
}

func (m *Matcher) effect(rule *compiledRule, file CodeGraphFile, site effectSite, node CodeGraphNode, context string) Effect {
	attrs := map[string]string{}
	maps.Copy(attrs, rule.attrs)
	switch {
	case rule.call != nil:
		if rule.target != "" {
			if literal, ok := stringLiteral(nthArg(site.args, rule.targetArg)); ok {
				attrs[rule.target] = literal
			}
		}
	case rule.route != nil:
		attrs["method"] = site.method
		attrs["path"] = site.path
	case rule.importSpec != nil:
		attrs["specifier"] = site.specifier
	}
	if site.argv0 != "" {
		attrs["argv0"] = site.argv0
	}
	for _, extraction := range rule.extract {
		if value, ok := extraction.value(site.args); ok {
			attrs[extraction.attr] = value
			continue
		}
		if context != "" {
			if value, ok := extraction.value(context); ok {
				attrs[extraction.attr] = value
			}
		}
	}
	if len(attrs) == 0 {
		attrs = nil
	}
	contextName := node.Context
	if contextName == "" {
		contextName = file.Context
	}
	return Effect{
		ID:        EffectID(rule.kind, file.Path, site.line, attrs),
		Kind:      rule.kind,
		Component: contextName,
		Context:   contextName,
		Attrs:     attrs,
		File:      file.Path,
		Line:      site.line,
		Node:      node.ID,
	}
}

func (e compiledExtract) value(text string) (string, bool) {
	match := e.regex.FindStringSubmatch(text)
	if match == nil {
		return "", false
	}
	group := e.group
	if group == 0 && len(match) > 1 {
		group = 1
	}
	if group >= len(match) || match[group] == "" {
		return "", false
	}
	value := match[group]
	if e.trim {
		value = strings.Trim(value, "\"'`{}()[], \t")
	}
	if value == "" {
		return "", false
	}
	return value, true
}

func (m *Matcher) routeNodeEffects(graph CodeGraph) []Effect {
	enabled := false
	for index := range m.packs {
		for ruleIndex := range m.packs[index].rules {
			if m.packs[index].rules[ruleIndex].route != nil {
				enabled = true
			}
		}
	}
	if !enabled {
		return nil
	}
	out := []Effect{}
	for _, node := range graph.Spec.Nodes {
		if node.Kind != "route" {
			continue
		}
		method, routePath, ok := splitRouteName(node.Name)
		if !ok {
			continue
		}
		attrs := map[string]string{"method": method, "path": routePath}
		contextName := node.Context
		if contextName == "" {
			contextName = contextOfFile(graph, node.File)
		}
		out = append(out, Effect{
			ID:        EffectID(EffectHTTPHandle, node.File, node.StartLine, attrs),
			Kind:      EffectHTTPHandle,
			Component: contextName,
			Context:   contextName,
			Attrs:     attrs,
			File:      node.File,
			Line:      node.StartLine,
			Node:      node.ID,
		})
	}
	return out
}

func splitRouteName(name string) (string, string, bool) {
	fields := strings.Fields(name)
	switch len(fields) {
	case 1:
		if strings.HasPrefix(fields[0], "/") {
			return "", fields[0], true
		}
	case 2:
		if strings.HasPrefix(fields[1], "/") {
			return strings.ToUpper(fields[0]), fields[1], true
		}
	}
	return "", "", false
}

func contextOfFile(graph CodeGraph, path string) string {
	for _, file := range graph.Spec.Files {
		if file.Path == path {
			return file.Context
		}
	}
	return ""
}

func sourceOf(graph CodeGraph, path string) string {
	if text, ok := graph.Spec.Texts[path]; ok && text != "" {
		return text
	}
	for _, node := range graph.Spec.Nodes {
		if node.Kind == "file" && node.File == path && node.Text != "" {
			return node.Text
		}
	}
	return ""
}

func nodeAt(graph CodeGraph, file string, line int) CodeGraphNode {
	best := CodeGraphNode{}
	found := false
	for _, node := range graph.Spec.Nodes {
		if node.File != file || node.StartLine <= 0 {
			continue
		}
		if line < node.StartLine || line > node.EndLine {
			continue
		}
		if !found || smallerSpan(node, best) {
			best = node
			found = true
		}
	}
	return best
}

func smallerSpan(candidate, current CodeGraphNode) bool {
	left := candidate.EndLine - candidate.StartLine
	right := current.EndLine - current.StartLine
	if left != right {
		return left < right
	}
	return candidate.ID < current.ID
}

func dedupeEffects(effects []Effect) []Effect {
	seen := map[string]bool{}
	out := make([]Effect, 0, len(effects))
	for _, effect := range effects {
		key := strings.Join([]string{
			string(effect.Kind),
			effect.File,
			fmt.Sprint(effect.Line),
			attrSignature(effect.Attrs),
		}, "\x00")
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, effect)
	}
	SortEffects(out)
	return out
}

func matchesAny(regex *regexp.Regexp, values []string) bool {
	return slices.ContainsFunc(values, regex.MatchString)
}

type effectSite struct {
	line      int
	args      string
	argv0     string
	method    string
	path      string
	specifier string
}

type maskClass uint8

const (
	maskQuoted maskClass = iota
	maskCode
	maskComment
)

type scanner struct {
	source string
	family string
	mask   []maskClass
	consts map[string]string
}

func newScanner(source, family string, imported map[string]string) *scanner {
	mask := codeMask(source, family)
	consts := constTable(source, family, mask)
	if len(imported) > 0 {
		if consts == nil {
			consts = map[string]string{}
		}
		for name, value := range imported {
			if _, clash := consts[name]; !clash {
				consts[name] = value
			}
		}
	}
	scan := &scanner{source: source, family: family, mask: mask, consts: consts}
	if len(scan.consts) > 0 {
		scan.source = scan.propagate()
		scan.mask = codeMask(scan.source, family)
	}
	return scan
}

// importConstants resolves the named constants a file imports from another file
// in the same graph, so `createSignedRepoRecord(EVENT_NSID, ...)` names the
// NSID the constant stands for even when the constant lives in a sibling
// module. The module is found by the specifier's last segment, which is how a
// repository's own packages are named.
func importConstants(graph CodeGraph) map[string]map[string]string {
	locals := map[string]map[string]string{}
	sources := map[string]string{}
	for _, file := range graph.Spec.Files {
		source := sourceOf(graph, file.Path)
		if source == "" {
			continue
		}
		sources[file.Path] = source
		locals[file.Path] = constTable(source, languageFamily(file.Language), codeMask(source, languageFamily(file.Language)))
	}
	out := map[string]map[string]string{}
	for path, source := range sources {
		family := languageFamily(languageOf(graph, path))
		table := map[string]string{}
		maps.Copy(table, locals[path])
		for _, imported := range namedImports(source, family) {
			target := resolveModule(graph, path, imported.Specifier)
			if target == "" {
				continue
			}
			for local, remote := range imported.Names {
				if _, clash := table[local]; clash {
					continue
				}
				if value, ok := locals[target][remote]; ok {
					table[local] = value
				}
			}
		}
		if len(table) > 0 {
			out[path] = table
		}
	}
	return out
}

type namedImport struct {
	Specifier string
	Names     map[string]string
}

func namedImports(source, family string) []namedImport {
	if family != "typescript" {
		return nil
	}
	regex := regexp.MustCompile(`(?m)^\s*import\b[^;]*?\{([^}]*)\}\s*from\s*["']([^"'\n]+)["']`)
	out := []namedImport{}
	for _, loc := range regex.FindAllStringSubmatchIndex(source, -1) {
		names := map[string]string{}
		for _, entry := range strings.Split(source[loc[2]:loc[3]], ",") {
			fields := strings.Fields(strings.TrimSpace(entry))
			switch len(fields) {
			case 1:
				names[fields[0]] = fields[0]
			case 3:
				if fields[1] == "as" {
					names[fields[2]] = fields[0]
				}
			}
		}
		out = append(out, namedImport{Specifier: source[loc[4]:loc[5]], Names: names})
	}
	return out
}

func resolveModule(graph CodeGraph, from, specifier string) string {
	tail := specifier
	if index := strings.LastIndex(specifier, "/"); index >= 0 {
		tail = specifier[index+1:]
	}
	tail = strings.TrimSuffix(tail, ".ts")
	if tail == "" || tail == "." || tail == ".." {
		return ""
	}
	if strings.HasPrefix(specifier, ".") {
		base := from
		if index := strings.LastIndex(base, "/"); index >= 0 {
			base = base[:index]
		}
		candidate := base + "/" + specifier
		for _, suffix := range []string{"", ".ts", "/mod.ts", "/index.ts"} {
			if sourceOf(graph, candidate+suffix) != "" {
				return candidate + suffix
			}
		}
		return ""
	}
	for _, suffix := range []string{"/" + tail + "/mod.ts", "/" + tail + "/index.ts", "/" + tail + ".ts"} {
		for _, file := range graph.Spec.Files {
			if strings.HasSuffix(file.Path, suffix) {
				return file.Path
			}
		}
	}
	return ""
}

func languageOf(graph CodeGraph, path string) string {
	for _, file := range graph.Spec.Files {
		if file.Path == path {
			return file.Language
		}
	}
	return ""
}

// constTable reads the simple string constants of one file: `const SSH = "ssh"`
// in TypeScript, `sshBin = "ssh"` in Go. It is what lets a call whose argv0 or
// host is held in a name still name the effect.
func constTable(source, family string, mask []maskClass) map[string]string {
	regex := constRegex(family)
	if regex == nil {
		return nil
	}
	out := map[string]string{}
	for _, loc := range regex.FindAllStringSubmatchIndex(source, -1) {
		if loc[2] < 0 || loc[4] < 0 || classAtIn(mask, loc[2]) != maskCode {
			continue
		}
		name := source[loc[2]:loc[3]]
		value := source[loc[4]:loc[5]]
		if name == "" || value == "" {
			continue
		}
		if _, clash := out[name]; !clash {
			out[name] = value
		}
	}
	return out
}

func constRegex(family string) *regexp.Regexp {
	switch family {
	case "typescript":
		return regexp.MustCompile("\\b(?:const|let|var)\\s+([A-Za-z_$][\\w$]*)\\s*(?::[^=\\n]+)?=\\s*(\"(?:[^\"\\\\\\n]|\\\\.)*\"|'(?:[^'\\\\\\n]|\\\\.)*'|`[^`$\\n]*`)")
	case "go":
		return regexp.MustCompile("(?:^|\\n)\\s*(?:const\\s+)?([A-Za-z_]\\w*)\\s*(?:string\\s*)?=\\s*(\"(?:[^\"\\\\\\n]|\\\\.)*\")")
	}
	return nil
}

func classAtIn(mask []maskClass, offset int) maskClass {
	if offset < 0 || offset >= len(mask) {
		return maskComment
	}
	return mask[offset]
}

// propagate rewrites each constant name that appears in code with its literal,
// so the extract rules read a value a name held. An occurrence that is a member
// name, part of a longer word, or the left side of the declaration stays.
func (s *scanner) propagate() string {
	var out strings.Builder
	out.Grow(len(s.source))
	index := 0
	for index < len(s.source) {
		char := s.source[index]
		if !isIdentStart(char) || (index > 0 && (isIdentifierByte(s.source[index-1]) || s.source[index-1] == '.')) {
			out.WriteByte(char)
			index++
			continue
		}
		end := index
		for end < len(s.source) && isIdentifierByte(s.source[end]) {
			end++
		}
		name := s.source[index:end]
		value, ok := s.consts[name]
		if !ok || s.classAt(index) != maskCode || assignedAhead(s.source, end) {
			out.WriteString(name)
			index = end
			continue
		}
		out.WriteString(value)
		index = end
	}
	return out.String()
}

func assignedAhead(source string, offset int) bool {
	for offset < len(source) && isSpaceByte(source[offset]) {
		offset++
	}
	return offset < len(source) && source[offset] == '='
}

func isIdentStart(char byte) bool {
	return char == '_' || char == '$' ||
		(char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z')
}

func (s *scanner) sites(rule *compiledRule, claimed map[int]bool) []effectSite {
	switch {
	case rule.call != nil:
		return s.callSites(rule, claimed)
	case rule.command != nil:
		return s.commandSites(rule, claimed)
	case rule.route != nil:
		return s.routeSites(rule, claimed)
	case rule.importSpec != nil:
		return s.importSites(rule)
	}
	return nil
}

func (s *scanner) commandSites(rule *compiledRule, claimed map[int]bool) []effectSite {
	out := []effectSite{}
	for _, loc := range rule.command.FindAllStringSubmatchIndex(s.source, -1) {
		wordStart, wordEnd := loc[2], loc[3]
		if claimed[wordStart] || !s.allowed(wordStart, rule.inStrings) {
			continue
		}
		if len(rule.verbs) > 0 && !rule.verbs[nextCommandWord(s.source[loc[1]:])] {
			continue
		}
		line := lineAt(s.source, wordStart)
		text := lineText(s.source, line)
		if rule.args != nil && !rule.args.MatchString(text) {
			continue
		}
		claimed[wordStart] = true
		out = append(out, effectSite{line: line, args: text, argv0: s.source[wordStart:wordEnd]})
	}
	return out
}

func nextCommandWord(rest string) string {
	index := 0
	for index < len(rest) {
		for index < len(rest) && (rest[index] == ' ' || rest[index] == '\t') {
			index++
		}
		start := index
		for index < len(rest) && rest[index] != ' ' && rest[index] != '\t' && rest[index] != '\n' {
			index++
		}
		word := rest[start:index]
		if word == "" || word == "\\" {
			return ""
		}
		if !strings.HasPrefix(word, "-") {
			return word
		}
	}
	return ""
}

func lineText(source string, line int) string {
	if line < 1 {
		return ""
	}
	lines := strings.Split(source, "\n")
	if line > len(lines) {
		return ""
	}
	return lines[line-1]
}

func (s *scanner) callSites(rule *compiledRule, claimed map[int]bool) []effectSite {
	out := []effectSite{}
	for _, loc := range rule.call.FindAllStringIndex(s.source, -1) {
		start, end := loc[0], loc[1]
		if claimed[start] || !s.allowed(start, rule.inStrings) {
			continue
		}
		if !rule.member && !s.notMember(start) {
			continue
		}
		args, close := balancedArgs(s.source, end-1)
		if close < 0 {
			continue
		}
		if !rule.matchesArgs(args) {
			continue
		}
		claimed[start] = true
		out = append(out, effectSite{line: lineAt(s.source, start), args: args})
	}
	return out
}

func (s *scanner) routeSites(rule *compiledRule, claimed map[int]bool) []effectSite {
	out := []effectSite{}
	for _, loc := range rule.route.FindAllStringSubmatchIndex(s.source, -1) {
		start, end := loc[0], loc[1]
		if claimed[start] || !s.allowed(start, rule.inStrings) {
			continue
		}
		method := strings.ToLower(s.source[loc[2]:loc[3]])
		if len(rule.routeMethods) > 0 {
			upper, ok := rule.routeMethods[method]
			if !ok {
				continue
			}
			method = upper
		} else {
			method = strings.ToUpper(method)
		}
		if rule.routeReceiver != nil && !rule.routeReceiver.MatchString(receiverBefore(s.source, start)) {
			continue
		}
		args, close := balancedArgs(s.source, end-1)
		if close < 0 {
			continue
		}
		path, ok := stringLiteral(firstArg(args))
		if !ok || !rule.routePath.MatchString(path) {
			continue
		}
		claimed[start] = true
		out = append(out, effectSite{line: lineAt(s.source, start), args: args, method: method, path: path})
	}
	return out
}

func (s *scanner) importSites(rule *compiledRule) []effectSite {
	out := []effectSite{}
	for _, specifier := range s.importSpans() {
		if !rule.importSpec.MatchString(specifier.text) {
			continue
		}
		out = append(out, effectSite{line: lineAt(s.source, specifier.start), specifier: specifier.text})
	}
	return out
}

func (s *scanner) imports() []string {
	out := []string{}
	for _, specifier := range s.importSpans() {
		out = append(out, specifier.text)
	}
	return out
}

type importSpan struct {
	start int
	text  string
}

func (s *scanner) importSpans() []importSpan {
	if s.family == "shell" {
		return nil
	}
	regex := importSpecifierRegex(s.family)
	if regex == nil {
		return nil
	}
	out := []importSpan{}
	for _, loc := range regex.FindAllStringSubmatchIndex(s.source, -1) {
		if !s.isCode(loc[0]) {
			continue
		}
		out = append(out, importSpan{start: loc[2], text: s.source[loc[2]:loc[3]]})
	}
	return out
}

func importSpecifierRegex(family string) *regexp.Regexp {
	if family == "go" {
		return regexp.MustCompile(`(?m)^\s*(?:[\w.]+\s+)?"([^"\n]+)"\s*$`)
	}
	return regexp.MustCompile(`(?m)^\s*(?:import|export)\b[^\n]*?["']([^"'\n]+)["']`)
}

func (rule *compiledRule) matchesArgs(args string) bool {
	if len(rule.argv0) > 0 {
		literal, ok := stringLiteral(firstArg(args))
		if !ok || !rule.argv0[literal] {
			return false
		}
	}
	if rule.args != nil && !rule.args.MatchString(args) {
		return false
	}
	return true
}

func (s *scanner) isCode(offset int) bool {
	return s.classAt(offset) == maskCode
}

func (s *scanner) classAt(offset int) maskClass {
	if offset < 0 || offset >= len(s.mask) {
		return maskComment
	}
	return s.mask[offset]
}

func (s *scanner) allowed(offset int, inStrings bool) bool {
	class := s.classAt(offset)
	if inStrings {
		return class == maskQuoted
	}
	return class == maskCode
}

func (s *scanner) notMember(offset int) bool {
	if offset == 0 {
		return true
	}
	previous := s.source[offset-1]
	return previous != '.'
}

func receiverBefore(source string, dot int) string {
	index := dot
	for index > 0 && isReceiverByte(source[index-1]) {
		index--
	}
	return source[index:dot]
}

func isReceiverByte(char byte) bool {
	return char == '.' || char == '_' || char == '$' ||
		(char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9')
}

func lineAt(source string, offset int) int {
	if offset > len(source) {
		offset = len(source)
	}
	return strings.Count(source[:offset], "\n") + 1
}

func splitArgs(args string) []string {
	out := []string{}
	depth := 0
	start := 0
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case '"', '\'', '`':
			index = skipString(args, index)
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, args[start:index])
				start = index + 1
			}
		}
	}
	out = append(out, args[start:])
	return out
}

func nthArg(args string, index int) string {
	parts := splitArgs(args)
	if index < 0 || index >= len(parts) {
		return ""
	}
	return parts[index]
}

func firstArg(args string) string {
	return nthArg(args, 0)
}

func stringLiteral(text string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) < 2 {
		return "", false
	}
	quote := trimmed[0]
	if quote != '"' && quote != '\'' && quote != '`' {
		return "", false
	}
	if trimmed[len(trimmed)-1] != quote {
		return "", false
	}
	return trimmed[1 : len(trimmed)-1], true
}

func balancedArgs(source string, open int) (string, int) {
	if open < 0 || open >= len(source) || source[open] != '(' {
		return "", -1
	}
	depth := 0
	for index := open; index < len(source); index++ {
		switch source[index] {
		case '"', '\'', '`':
			index = skipString(source, index)
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
			if depth == 0 {
				return source[open+1 : index], index
			}
		}
	}
	return "", -1
}

func skipString(source string, open int) int {
	quote := source[open]
	for index := open + 1; index < len(source); index++ {
		if source[index] == '\\' {
			index++
			continue
		}
		if source[index] == quote {
			return index
		}
	}
	return len(source) - 1
}

func codeMask(source, family string) []maskClass {
	if family == "shell" {
		return shellMask(source)
	}
	mask := make([]maskClass, len(source))
	index := 0
	for index < len(source) {
		char := source[index]
		switch {
		case char == '/' && index+1 < len(source) && source[index+1] == '/':
			for index < len(source) && source[index] != '\n' {
				mask[index] = maskComment
				index++
			}
		case char == '/' && index+1 < len(source) && source[index+1] == '*':
			mask[index] = maskComment
			mask[index+1] = maskComment
			index += 2
			for index < len(source) && !(source[index] == '*' && index+1 < len(source) && source[index+1] == '/') {
				mask[index] = maskComment
				index++
			}
			if index < len(source) {
				mask[index] = maskComment
				mask[index+1] = maskComment
				index += 2
			}
		case family == "typescript" && char == '/' && regexCanStart(mask, source, index):
			if close := skipRegex(source, index); close > index {
				index = close + 1
				continue
			}
			mask[index] = maskCode
			index++
		case char == '"' || char == '\'' || char == '`':
			index = skipString(source, index) + 1
		default:
			mask[index] = maskCode
			index++
		}
	}
	return mask
}

func shellMask(source string) []maskClass {
	mask := make([]maskClass, len(source))
	index := 0
	for index < len(source) {
		char := source[index]
		switch {
		case char == '#' && (index == 0 || source[index-1] == ' ' || source[index-1] == '\t' || source[index-1] == '\n'):
			for index < len(source) && source[index] != '\n' {
				mask[index] = maskComment
				index++
			}
		case char == '\'':
			index = skipString(source, index) + 1
		case char == '"':
			close := skipString(source, index)
			markSubstitutions(source, index+1, close, mask)
			index = close + 1
		case char == '\\':
			index += 2
		default:
			mask[index] = maskCode
			index++
		}
	}
	return mask
}

func regexCanStart(mask []maskClass, source string, index int) bool {
	previous := index - 1
	for previous >= 0 && isSpaceByte(source[previous]) {
		previous--
	}
	if previous < 0 {
		return true
	}
	if mask[previous] != maskCode {
		return false
	}
	switch source[previous] {
	case '(', ',', '=', ':', '[', '!', '&', '|', '?', '{', '}', ';', '+', '-', '*', '%', '~', '^', '<', '>':
		return true
	}
	word := ""
	for previous >= 0 && isIdentifierByte(source[previous]) {
		word = string(source[previous]) + word
		previous--
	}
	return regexKeywords[word]
}

var regexKeywords = map[string]bool{
	"return": true, "typeof": true, "case": true, "in": true, "of": true,
	"do": true, "else": true, "void": true, "delete": true, "instanceof": true,
	"new": true, "yield": true, "await": true,
}

func skipRegex(source string, open int) int {
	inClass := false
	for index := open + 1; index < len(source); index++ {
		switch source[index] {
		case '\\':
			index++
		case '\n':
			return open
		case '[':
			inClass = true
		case ']':
			inClass = false
		case '/':
			if !inClass {
				return index
			}
		}
	}
	return open
}

func isSpaceByte(char byte) bool {
	return char == ' ' || char == '\t' || char == '\n' || char == '\r'
}

func isIdentifierByte(char byte) bool {
	return char == '_' || char == '$' ||
		(char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9')
}

func markSubstitutions(source string, from, to int, mask []maskClass) {
	for index := from; index+1 < to; index++ {
		if source[index] != '$' || source[index+1] != '(' {
			continue
		}
		depth := 1
		start := index + 2
		cursor := start
		for cursor < to && depth > 0 {
			switch source[cursor] {
			case '(':
				depth++
			case ')':
				depth--
			}
			cursor++
		}
		for mark := start; mark < cursor-1 && mark < len(mask); mark++ {
			mask[mark] = maskCode
		}
		index = cursor - 1
	}
}

type contextCache struct {
	graph  CodeGraph
	nodes  map[string]CodeGraphNode
	byFile map[string][]int
	cache  map[string]string
}

func newContextCache(graph CodeGraph) *contextCache {
	cache := &contextCache{
		graph:  graph,
		nodes:  map[string]CodeGraphNode{},
		byFile: map[string][]int{},
		cache:  map[string]string{},
	}
	for index, node := range graph.Spec.Nodes {
		cache.nodes[node.ID] = node
		cache.byFile[node.File] = append(cache.byFile[node.File], index)
	}
	return cache
}

// site is the text an extract rule may fall back to. A call site inside a real
// declaration reads that declaration and the declarations it calls; a site the
// graph only knows as its file reads a window around the site, because a file's
// other calls must never lend an attribute to this one.
func (c *contextCache) site(file string, node CodeGraphNode, source string, line int) string {
	if node.Kind == "file" || node.ID == "" {
		return siteWindow(source, line)
	}
	return c.text(file, node.ID)
}

func siteWindow(source string, line int) string {
	lines := strings.Split(source, "\n")
	if line < 1 || line > len(lines) {
		return ""
	}
	start := line - 1
	if start > 0 {
		start--
	}
	end := line - 1
	if callEnd := callEndLine(source, line); callEnd > end {
		end = callEnd
	}
	if end >= len(lines) {
		end = len(lines) - 1
	}
	return strings.Join(lines[start:end+1], "\n")
}

func callEndLine(source string, line int) int {
	offset := lineOffset(source, line)
	if offset < 0 {
		return -1
	}
	rest := source[offset:]
	open := strings.IndexByte(rest, '(')
	if open < 0 {
		return -1
	}
	_, close := balancedArgs(rest, open)
	if close < 0 {
		return -1
	}
	return line - 1 + strings.Count(rest[:close], "\n")
}

func lineOffset(source string, line int) int {
	if line < 1 {
		return -1
	}
	offset := 0
	for index := 1; index < line; index++ {
		next := strings.IndexByte(source[offset:], '\n')
		if next < 0 {
			return -1
		}
		offset += next + 1
	}
	return offset
}

func (c *contextCache) text(file, id string) string {
	if id == "" {
		return ""
	}
	if cached, ok := c.cache[id]; ok {
		return cached
	}
	parts := []string{}
	if node, ok := c.nodes[id]; ok {
		parts = append(parts, node.Text)
		parts = append(parts, c.calleeTexts(file, id)...)
	}
	joined := strings.Join(parts, "\n")
	c.cache[id] = joined
	return joined
}

func (c *contextCache) calleeTexts(file, id string) []string {
	out := []string{}
	for _, edge := range c.graph.Spec.Edges {
		if edge.Kind != "calls" || edge.Source != id {
			continue
		}
		target, ok := c.nodes[edge.Target]
		if !ok || target.File != file || target.Text == "" {
			continue
		}
		out = append(out, target.Text)
	}
	return out
}
