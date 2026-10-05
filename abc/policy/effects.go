package policy

import (
	"slices"
	"sort"
	"strconv"
	"strings"
)

type EffectKind string

const (
	EffectNetDial       EffectKind = "net.dial"
	EffectNetListen     EffectKind = "net.listen"
	EffectHTTPRequest   EffectKind = "http.request"
	EffectHTTPHandle    EffectKind = "http.handle"
	EffectProcExec      EffectKind = "proc.exec"
	EffectSSHConnect    EffectKind = "ssh.connect"
	EffectContainerExec EffectKind = "container.exec"
	EffectEventEmit     EffectKind = "event.emit"
	EffectEventReceive  EffectKind = "event.receive"
	EffectSecretRead    EffectKind = "secret.read"
	EffectFileWrite     EffectKind = "file.write"
	EffectFileRead      EffectKind = "file.read"
)

var EffectVocabulary = []EffectKind{
	EffectNetDial,
	EffectNetListen,
	EffectHTTPRequest,
	EffectHTTPHandle,
	EffectProcExec,
	EffectSSHConnect,
	EffectContainerExec,
	EffectEventEmit,
	EffectEventReceive,
	EffectSecretRead,
	EffectFileWrite,
	EffectFileRead,
}

func KnownEffectKind(kind EffectKind) bool {
	return slices.Contains(EffectVocabulary, kind)
}

type Effect struct {
	ID string `json:"id"`

	Kind EffectKind `json:"kind"`

	Component string `json:"component,omitempty"`

	Context string `json:"context,omitempty"`

	Attrs map[string]string `json:"attrs,omitempty"`

	File string `json:"file"`

	Line int `json:"line,omitempty"`

	Node string `json:"node,omitempty"`
}

func (e Effect) Attr(name string) string {
	return e.Attrs[name]
}

func EffectID(kind EffectKind, file string, line int, attrs map[string]string) string {
	return ViolationID("effect", string(kind), file, strconv.Itoa(line), attrSignature(attrs))
}

func attrSignature(attrs map[string]string) string {
	keys := make([]string, 0, len(attrs))
	for key := range attrs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+attrs[key])
	}
	return strings.Join(parts, ",")
}

func SortEffects(effects []Effect) {
	sort.SliceStable(effects, func(left, right int) bool {
		a, b := effects[left], effects[right]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Node != b.Node {
			return a.Node < b.Node
		}
		return a.ID < b.ID
	})
}

func EffectsOf(effects []Effect, kind EffectKind) []Effect {
	out := []Effect{}
	for _, effect := range effects {
		if effect.Kind == kind {
			out = append(out, effect)
		}
	}
	return out
}
