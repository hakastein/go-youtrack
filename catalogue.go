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
	e := typeRef{list: list}
	switch {
	case written == "{}":
	case strings.HasPrefix(written, "!"):
		e.kind = written
	default:
		e.schema = written
	}
	return e
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

func (c *schemas) declaration(name, property string) (typeRef, bool) {
	for s, ok := c.byName[name]; ok; s, ok = c.byName[s.parent] {
		if written, declared := s.properties[property]; declared {
			return parseTypeRef(written), true
		}
	}
	return typeRef{}, false
}

func (c *schemas) isSubtypeOf(name, ancestor string) bool {
	for {
		if name == ancestor {
			return true
		}
		s, known := c.byName[name]
		if !known || s.parent == "" {
			return false
		}
		name = s.parent
	}
}

func (c *schemas) subtree(name string) []string {
	set := []string{name}
	for i := 0; i < len(set); i++ {
		set = append(set, c.children[set[i]]...)
	}
	return set
}

func (c *schemas) hierarchies(named []string) []string {
	var roots []string
	for _, name := range named {
		if _, known := c.byName[name]; !known {
			continue
		}
		for c.byName[name].parent != "" {
			name = c.byName[name].parent
		}
		if !slices.Contains(roots, name) {
			roots = append(roots, name)
		}
	}
	var set []string
	for _, root := range roots {
		set = append(set, c.subtree(root)...)
	}
	return set
}

func (c *schemas) names(set []string) []string {
	var names []string
	for _, name := range set {
		for s, ok := c.byName[name]; ok; s, ok = c.byName[s.parent] {
			names = slices.AppendSeq(names, maps.Keys(s.properties))
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}
