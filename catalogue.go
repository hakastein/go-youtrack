package youtrack

import (
	"maps"
	"slices"
	"strings"
)

//go:generate go run scripts/catalogue.go api/openapi.json catalogue.gen.go

type schema struct {
	parent     string
	properties map[string]string
}

const (
	timeKind = "!time"
	textKind = "!text"
)

type typeRef struct {
	schema string
	list   bool
	kind   string
}

func parseTypeRef(written string) typeRef {
	written, list := strings.CutPrefix(written, "[]")
	ref := typeRef{list: list}
	switch {
	case written == "{}":
	case strings.HasPrefix(written, "!"):
		ref.kind = written
	default:
		ref.schema = written
	}
	return ref
}

type schemas struct {
	byName   map[string]schema
	children map[string][]string
}

func loadSchemas() *schemas {
	byName := catalogue()
	children := map[string][]string{}
	for name, s := range byName {
		if s.parent != "" {
			children[s.parent] = append(children[s.parent], name)
		}
	}
	return &schemas{byName: byName, children: children}
}

func (spec *schemas) declaration(name, property string) (typeRef, bool) {
	for s, ok := spec.byName[name]; ok; s, ok = spec.byName[s.parent] {
		if written, declared := s.properties[property]; declared {
			return parseTypeRef(written), true
		}
	}
	return typeRef{}, false
}

func (spec *schemas) isSubtypeOf(name, ancestor string) bool {
	for {
		if name == ancestor {
			return true
		}
		s, known := spec.byName[name]
		if !known || s.parent == "" {
			return false
		}
		name = s.parent
	}
}

func (spec *schemas) subtree(name string) []string {
	set := []string{name}
	for i := 0; i < len(set); i++ {
		set = append(set, spec.children[set[i]]...)
	}
	return set
}

func (spec *schemas) hierarchies(named []string) []string {
	var roots []string
	for _, name := range named {
		if _, known := spec.byName[name]; !known {
			continue
		}
		for spec.byName[name].parent != "" {
			name = spec.byName[name].parent
		}
		if !slices.Contains(roots, name) {
			roots = append(roots, name)
		}
	}
	var set []string
	for _, root := range roots {
		set = append(set, spec.subtree(root)...)
	}
	return set
}

func (spec *schemas) names(set []string) []string {
	var names []string
	for _, name := range set {
		for s, ok := spec.byName[name]; ok; s, ok = spec.byName[s.parent] {
			names = slices.AppendSeq(names, maps.Keys(s.properties))
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}
