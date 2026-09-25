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
	spec       *schemas
	schemaSets map[*fieldNode]schemaSet
}

const absenceAccepted Code = ""

func checkMissingFields(spec *schemas, response *http.Response, responseSchema string, requested []requestedField, tree any) *Error {
	root, absences := findMissingFields(requested, tree)
	if len(absences) == 0 {
		return nil
	}
	resolver := newSchemaResolver(spec, responseSchema, root)
	var missing, unknown []*Node
	listed := map[string]bool{}
	for _, absence := range absences {
		code := resolver.classify(absence)
		if code == absenceAccepted {
			continue
		}
		field := fieldPath(absence.at.path(), absence.name)
		if listed[string(code)+" "+field] {
			continue
		}
		listed[string(code)+" "+field] = true
		if code == CodeUpstreamInvalid {
			missing = append(missing, missingEntry(field, absence))
		} else {
			unknown = append(unknown, nearestEntry(fieldKey, field, nearestNames(absence.name, resolver.schemaSets[absence.at].names)))
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
	var collector missingFieldCollector
	root := newFieldNode(nil, requestedField{children: requested})
	collector.visit(root, tree)
	return root, collector.absences
}

type missingFieldCollector struct {
	absences []missingField
}

func newFieldNode(parent *fieldNode, field requestedField) *fieldNode {
	node := &fieldNode{parent: parent, field: field}
	for _, child := range field.children {
		node.children = append(node.children, newFieldNode(node, child))
	}
	return node
}

func (node *fieldNode) path() []string {
	if node.parent == nil {
		return nil
	}
	return append(node.parent.path(), node.field.name)
}

func (collector *missingFieldCollector) visit(node *fieldNode, value any) {
	switch value := value.(type) {
	case nil:
	case []any:
		for _, item := range value {
			collector.visit(node, item)
		}
	case map[string]any:
		objectType, hasType := value["$type"].(string)
		if hasType && !slices.Contains(node.seenTypes, objectType) {
			node.seenTypes = append(node.seenTypes, objectType)
		}
		for i, field := range node.field.children {
			child, ok := value[field.name]
			switch {
			case !ok:
				collector.absences = append(collector.absences, missingField{at: node, name: field.name, objectType: objectType, hasType: hasType, askedByDefault: fromDefault(field)})
			case field.children != nil && !field.normalized:
				collector.visit(node.children[i], child)
			}
		}
	default:
		for _, field := range node.field.children {
			collector.absences = append(collector.absences, missingField{at: node, name: field.name, scalar: true, askedByDefault: fromDefault(field)})
		}
	}
}

func newSchemaResolver(spec *schemas, responseSchema string, root *fieldNode) schemaResolver {
	resolver := schemaResolver{spec: spec, schemaSets: map[*fieldNode]schemaSet{}}
	top := spec.subtree(responseSchema)
	resolver.assignSchemas(root, schemaSet{schemas: top, names: spec.names(top)})
	return resolver
}

func (resolver schemaResolver) assignSchemas(node *fieldNode, set schemaSet) {
	resolver.schemaSets[node] = set
	for _, child := range node.children {
		resolver.assignSchemas(child, resolver.childSchemas(set, child))
	}
}

func (resolver schemaResolver) childSchemas(parentSet schemaSet, node *fieldNode) schemaSet {
	var schemas []string
	untyped := false
	for _, owner := range parentSet.schemas {
		decl, declared := resolver.spec.declaration(owner, node.field.name)
		switch {
		case decl.schema != "":
			schemas = append(schemas, resolver.spec.subtree(decl.schema)...)
		case declared:
			untyped = true
		}
	}
	if untyped || schemas == nil {
		schemas = append(schemas, resolver.spec.hierarchies(node.seenTypes)...)
	}
	for _, schema := range node.field.extraSchemas {
		schemas = append(schemas, resolver.spec.subtree(schema)...)
	}
	return schemaSet{schemas: schemas, names: resolver.spec.names(schemas), untyped: untyped}
}

func (resolver schemaResolver) classify(absence missingField) Code {
	here := resolver.schemaSets[absence.at]
	typeBelongsHere := slices.Contains(here.schemas, absence.objectType)
	switch {
	case typeBelongsHere && resolver.declares(absence.objectType, absence.name):
		return CodeUpstreamInvalid
	case !slices.Contains(here.names, absence.name):
		if absence.askedByDefault {
			return CodeUpstreamInvalid
		}
		return CodeUnknownName
	case typeBelongsHere, absence.scalar && here.untyped:
		return absenceAccepted
	}
	return CodeUpstreamInvalid
}

func (resolver schemaResolver) declares(schema, name string) bool {
	_, ok := resolver.spec.declaration(schema, name)
	return ok
}

func fieldPath(parents []string, name string) string {
	return strings.Join(append(slices.Clip(parents), name), "(") + strings.Repeat(")", len(parents))
}

func missingEntry(field string, absence missingField) *Node {
	schema := NewNull()
	if absence.hasType {
		schema = NewString(absence.objectType)
	}
	return NewMap(Pair{Key: fieldKey, Value: NewString(field)}, Pair{Key: typeKey, Value: schema})
}

func nearestEntry(key, written string, nearest []string) *Node {
	return NewMap(Pair{Key: key, Value: NewString(written)}, Pair{Key: "nearest", Value: textList(nearest)})
}

func unresolvedDetails(unknown, ambiguous []*Node) []Pair {
	var details []Pair
	if len(unknown) > 0 {
		details = append(details, Pair{Key: "unknown", Value: NewList(unknown...)})
	}
	if len(ambiguous) > 0 {
		details = append(details, Pair{Key: "ambiguous", Value: NewList(ambiguous...)})
	}
	return details
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
	for _, suggested := range among {
		closest := distance(lowered, []rune(strings.ToLower(suggested.name)))
		for _, form := range suggested.also {
			closest = min(closest, distance(lowered, []rune(strings.ToLower(form))))
		}
		if closest <= 2 {
			near = append(near, candidate{name: suggested.name, distance: closest})
		}
	}
	if len(near) == 0 {
		return fallback
	}
	slices.SortFunc(near, func(a, b candidate) int {
		return cmp.Or(cmp.Compare(a.distance, b.distance), strings.Compare(a.name, b.name))
	})
	names := make([]string, 0, min(len(near), 5))
	for _, kept := range near[:min(len(near), 5)] {
		names = append(names, kept.name)
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
