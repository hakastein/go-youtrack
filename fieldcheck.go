package youtrack

import (
	"cmp"
	"net/http"
	"slices"
	"strings"
)

type fieldNode struct {
	parent    *fieldNode
	field     requestedField
	children  []*fieldNode
	seenTypes []string
}

type missingField struct {
	at             *fieldNode
	name           string
	objectType     string
	hasType        bool
	scalar         bool
	askedByDefault bool
}

type schemaSet struct {
	schemas []string
	names   []string
	untyped bool
}

type schemaResolver struct {
	schemas    *schemas
	schemaSets map[*fieldNode]schemaSet
}

const absenceAccepted Code = ""

func checkMissingFields(spec *schemas, response *http.Response, responseSchema string, requested []requestedField, tree any) *Error {
	root, absences := findMissingFields(requested, tree)
	if len(absences) == 0 {
		return nil
	}
	j := newSchemaResolver(spec, responseSchema, root)
	var missing, unknown []*Node
	listed := map[string]bool{}
	for _, a := range absences {
		code := j.classify(a)
		if code == absenceAccepted {
			continue
		}
		field := fieldPath(a.at.path(), a.name)
		if listed[string(code)+" "+field] {
			continue
		}
		listed[string(code)+" "+field] = true
		if code == CodeUpstreamInvalid {
			missing = append(missing, missingEntry(field, a))
		} else {
			unknown = append(unknown, unknownEntry(field, nearestNames(a.name, j.schemaSets[a.at].names)))
		}
	}
	details := []Pair{
		sentRequest(response),
		{Key: "fields", Value: NewString(formatFields(requested))},
	}
	if len(missing) > 0 {
		message := "the fields under missing were asked for and did not arrive: the caller's rights may hide them"
		details = append(details, Pair{Key: "missing", Value: NewList(missing...)})
		return &Error{Code: CodeUpstreamInvalid, Message: message, Details: details}
	}
	if len(unknown) > 0 {
		message := "the names under unknown are not declared where they were asked for"
		details = append(details, Pair{Key: "unknown", Value: NewList(unknown...)})
		return &Error{Code: CodeUnknownName, Message: message, Details: details}
	}
	return nil
}

func findMissingFields(requested []requestedField, tree any) (*fieldNode, []missingField) {
	s := missingFieldCollector{found: map[missingField]bool{}}
	root := newFieldNode(nil, requestedField{children: requested})
	s.visit(root, tree)
	return root, s.absences
}

type missingFieldCollector struct {
	absences []missingField
	found    map[missingField]bool
}

func newFieldNode(parent *fieldNode, field requestedField) *fieldNode {
	p := &fieldNode{parent: parent, field: field}
	for _, child := range field.children {
		p.children = append(p.children, newFieldNode(p, child))
	}
	return p
}

func (p *fieldNode) path() []string {
	if p.parent == nil {
		return nil
	}
	return append(p.parent.path(), p.field.name)
}

func (s *missingFieldCollector) visit(p *fieldNode, value any) {
	switch value := value.(type) {
	case nil:
	case []any:
		for _, item := range value {
			s.visit(p, item)
		}
	case map[string]any:
		objectType, hasType := value["$type"].(string)
		if hasType && !slices.Contains(p.seenTypes, objectType) {
			p.seenTypes = append(p.seenTypes, objectType)
		}
		for i, field := range p.field.children {
			child, ok := value[field.name]
			switch {
			case !ok:
				s.add(missingField{at: p, name: field.name, objectType: objectType, hasType: hasType, askedByDefault: fromDefault(field)})
			case field.children != nil && !field.normalized:
				s.visit(p.children[i], child)
			}
		}
	default:
		for _, field := range p.field.children {
			s.add(missingField{at: p, name: field.name, scalar: true, askedByDefault: fromDefault(field)})
		}
	}
}

func (s *missingFieldCollector) add(a missingField) {
	if !s.found[a] {
		s.found[a] = true
		s.absences = append(s.absences, a)
	}
}

func newSchemaResolver(schemas *schemas, responseSchema string, root *fieldNode) schemaResolver {
	j := schemaResolver{schemas: schemas, schemaSets: map[*fieldNode]schemaSet{}}
	top := schemas.subtree(responseSchema)
	j.assignSchemas(root, schemaSet{schemas: top, names: schemas.names(top)})
	return j
}

func (j schemaResolver) assignSchemas(p *fieldNode, f schemaSet) {
	j.schemaSets[p] = f
	for _, child := range p.children {
		j.assignSchemas(child, j.childSchemas(f, child))
	}
}

func (j schemaResolver) childSchemas(parentSet schemaSet, p *fieldNode) schemaSet {
	var schemas []string
	untyped := false
	for _, owner := range parentSet.schemas {
		decl, declared := j.schemas.declaration(owner, p.field.name)
		switch {
		case decl.schema != "":
			schemas = append(schemas, j.schemas.subtree(decl.schema)...)
		case declared:
			untyped = true
		}
	}
	if untyped || schemas == nil {
		schemas = append(schemas, j.schemas.hierarchies(p.seenTypes)...)
	}
	for _, schema := range p.field.extraSchemas {
		schemas = append(schemas, j.schemas.subtree(schema)...)
	}
	return schemaSet{schemas: schemas, names: j.schemas.names(schemas), untyped: untyped}
}

func (j schemaResolver) classify(a missingField) Code {
	here := j.schemaSets[a.at]
	typeBelongsHere := slices.Contains(here.schemas, a.objectType)
	switch {
	case typeBelongsHere && j.declares(a.objectType, a.name):
		return CodeUpstreamInvalid
	case !slices.Contains(here.names, a.name):
		if a.askedByDefault {
			return CodeUpstreamInvalid
		}
		return CodeUnknownName
	case typeBelongsHere, a.scalar && here.untyped:
		return absenceAccepted
	}
	return CodeUpstreamInvalid
}

func (j schemaResolver) declares(schema, name string) bool {
	_, ok := j.schemas.declaration(schema, name)
	return ok
}

func fieldPath(parents []string, name string) string {
	return strings.Join(append(slices.Clip(parents), name), "(") + strings.Repeat(")", len(parents))
}

func missingEntry(field string, a missingField) *Node {
	schema := NewNull()
	if a.hasType {
		schema = NewString(a.objectType)
	}
	return NewMap(Pair{Key: "field", Value: NewString(field)}, Pair{Key: "type", Value: schema})
}

func unknownEntry(field string, nearest []string) *Node {
	return nearestEntry("field", field, nearest)
}

func nearestEntry(key, written string, nearest []string) *Node {
	names := make([]*Node, 0, len(nearest))
	for _, name := range nearest {
		names = append(names, NewString(name))
	}
	return NewMap(Pair{Key: key, Value: NewString(written)}, Pair{Key: "nearest", Value: NewList(names...)})
}

type suggestion struct {
	name string
	also []string
}

func nearest(asked string, among []suggestion, fallback []string) []string {
	type candidate struct {
		name     string
		distance int
	}
	lowered := []rune(strings.ToLower(asked))
	var near []candidate
	for _, s := range among {
		d := distance(lowered, []rune(strings.ToLower(s.name)))
		for _, form := range s.also {
			d = min(d, distance(lowered, []rune(strings.ToLower(form))))
		}
		if d <= 2 {
			near = append(near, candidate{name: s.name, distance: d})
		}
	}
	if len(near) == 0 {
		return fallback
	}
	slices.SortFunc(near, func(a, b candidate) int {
		return cmp.Or(cmp.Compare(a.distance, b.distance), strings.Compare(a.name, b.name))
	})
	names := make([]string, 0, min(len(near), 5))
	for _, c := range near[:min(len(near), 5)] {
		names = append(names, c.name)
	}
	return names
}

func nearestNames(asked string, names []string) []string {
	among := make([]suggestion, 0, len(names))
	for _, name := range names {
		among = append(among, suggestion{name: name})
	}
	return nearest(asked, among, names)
}

func distance(a, b []rune) int {
	previous, current := make([]int, len(b)+1), make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current[0] = i
		for j := 1; j <= len(b); j++ {
			substitution := previous[j-1]
			if a[i-1] != b[j-1] {
				substitution++
			}
			current[j] = min(previous[j]+1, current[j-1]+1, substitution)
		}
		previous, current = current, previous
	}
	return previous[len(b)]
}
