package youtrack

import (
	"fmt"
	"slices"
	"strings"
)

const (
	attributesKey           = "attributes"
	workItemAttributeSchema = "WorkItemAttribute"
)

// Name and Value resolve in any letter case, an exact spelling settling a tie; Clear takes no Value.
type AttributeWrite struct {
	Name  string
	Value string
	Clear bool
}

func attributesAsked() []requestedField {
	return []requestedField{
		{name: idKey},
		{name: nameKey},
		{name: valueKey, children: []requestedField{{name: idKey}, {name: nameKey}}},
	}
}

func eachAttributes(spec *schemas, at string, requested []requestedField, visit func(parents []string, field *requestedField)) {
	fieldsOfType(spec, at, workItemAttributeSchema, requested, visit)
}

func rejectAttributeNames(spec *schemas, at, expression string, requested []requestedField) *Error {
	var fault *Error
	eachAttributes(spec, at, requested, func(parents []string, field *requestedField) {
		if field.children == nil || fault != nil {
			return
		}
		message := fmt.Sprintf("fields %s: %s holds the attributes of a work item, which are printed as the value "+
			"each holds under its name, so no name stands under it", quote(expression), fieldPath(parents, field.name))
		fault = &Error{Code: CodeBadUsage, Message: message}
	})
	return fault
}

func (n converter) attributes(value any) (*Node, *Error) {
	received, isList := value.([]any)
	if !isList {
		return nil, n.response.invalid("the attributes of the work item arrived as something other than an array")
	}
	pairs := make([]Pair, 0, len(received))
	named := make(map[string]bool, len(received))
	for _, item := range received {
		name, isText := memberOf(item, nameKey).(string)
		if !isText {
			return nil, n.response.invalid("an attribute of the work item arrived without a name")
		}
		if named[name] {
			return nil, n.response.invalid(fmt.Sprintf("two attributes of the work item are named %s", quote(name)))
		}
		named[name] = true
		held := NewNull()
		if value := memberOf(item, valueKey); value != nil {
			valueName, isText := memberOf(value, nameKey).(string)
			if !isText {
				return nil, n.response.invalid(fmt.Sprintf("the value of the attribute %s arrived without a name", quote(name)))
			}
			held = NewString(valueName)
		}
		pairs = append(pairs, DataPair(name, held))
	}
	return NewMap(pairs...), nil
}

func checkAttributes(written []AttributeWrite) *Error {
	for at, attribute := range written {
		var message string
		switch {
		case attribute.Name == "":
			message = "an attribute to write has no name, and every attribute of the work items of a project has one"
		case attribute.Clear && attribute.Value != "":
			message = fmt.Sprintf("the call both sets the attribute %s and empties it", quote(attribute.Name))
		case !attribute.Clear && attribute.Value == "":
			message = fmt.Sprintf("the attribute %s is set to a value of no name: an attribute holds one of the "+
				"values it takes, or is emptied", quote(attribute.Name))
		case slices.ContainsFunc(written[:at], func(earlier AttributeWrite) bool {
			return strings.EqualFold(earlier.Name, attribute.Name)
		}):
			message = fmt.Sprintf("the call names the attribute %s twice, in any letter case", quote(attribute.Name))
		default:
			continue
		}
		return &Error{Code: CodeBadUsage, Message: message}
	}
	return nil
}

type projectAttribute struct {
	id     string
	name   string
	values []workItemType
}

type resolvedAttribute struct {
	id    string
	name  string
	value *resolvedWorkType
}

type attributeBody struct {
	ID    string  `json:"id"`
	Value *idBody `json:"value"`
}

func attributeBodies(filed []resolvedAttribute) []attributeBody {
	written := make([]attributeBody, 0, len(filed))
	for _, attribute := range filed {
		sent := attributeBody{ID: attribute.id}
		if attribute.value != nil {
			sent.Value = &idBody{ID: attribute.value.id}
		}
		written = append(written, sent)
	}
	return written
}

func (p projectWorkItemTypes) resolveAttributes(written []AttributeWrite) ([]resolvedAttribute, *Error) {
	catalogue := make([]fieldInfo, 0, len(p.attributes))
	for _, attribute := range p.attributes {
		catalogue = append(catalogue, fieldInfo{name: attribute.name})
	}
	var filed []resolvedAttribute
	var unknown []*Node
	for _, asked := range written {
		at, found := matchName(asked.Name, catalogue)
		if !found {
			unknown = append(unknown, nearestEntry("attribute", asked.Name, nearestNamed(asked.Name, catalogue)))
			continue
		}
		attribute := p.attributes[at]
		if asked.Clear {
			filed = append(filed, resolvedAttribute{id: attribute.id, name: attribute.name})
			continue
		}
		values := make([]fieldInfo, 0, len(attribute.values))
		for _, value := range attribute.values {
			values = append(values, fieldInfo{name: value.name})
		}
		place, found := matchName(asked.Value, values)
		if !found {
			unknown = append(unknown, NewMap(
				Pair{Key: "attribute", Value: NewString(attribute.name)},
				Pair{Key: valueKey, Value: NewString(asked.Value)},
				Pair{Key: "nearest", Value: textList(nearestNamed(asked.Value, values))}))
			continue
		}
		value := resolvedWorkType{id: attribute.values[place].id, name: asked.Value}
		filed = append(filed, resolvedAttribute{id: attribute.id, name: attribute.name, value: &value})
	}
	if len(unknown) > 0 {
		message := "the names under unknown are not attributes of the work items of the project, or values they take"
		return nil, p.response.fault(CodeUnknownName, message,
			Pair{Key: projectKey, Value: NewString(p.project)},
			Pair{Key: "unknown", Value: NewList(unknown...)})
	}
	return filed, nil
}

func matchName(name string, catalogue []fieldInfo) (int, bool) {
	return soleMatch(findMatches(name, catalogue), name, func(at int) string { return catalogue[at].name })
}

func soleMatch[E any](candidates []E, name string, spelling func(E) string) (E, bool) {
	if len(candidates) > 1 {
		candidates = slices.DeleteFunc(slices.Clone(candidates), func(candidate E) bool { return spelling(candidate) != name })
	}
	if len(candidates) != 1 {
		var none E
		return none, false
	}
	return candidates[0], true
}

func attributesOf(a decodedResponse, settings map[string]any) ([]projectAttribute, *Error) {
	items, isList := settings[attributesKey].([]any)
	if !isList {
		return nil, a.invalid("the attributes of work items of the project are not a JSON array")
	}
	attributes := make([]projectAttribute, 0, len(items))
	for _, item := range items {
		id, isText := memberOf(item, idKey).(string)
		name, isNamed := memberOf(item, nameKey).(string)
		values, isList := memberOf(item, "values").([]any)
		if !isText || !isNamed || !isList {
			return nil, a.invalid(brokenAttribute)
		}
		attribute := projectAttribute{id: id, name: name}
		for _, value := range values {
			id, isText := memberOf(value, idKey).(string)
			name, isNamed := memberOf(value, nameKey).(string)
			if !isText || !isNamed {
				return nil, a.invalid(brokenAttribute)
			}
			attribute.values = append(attribute.values, workItemType{id: id, name: name})
		}
		attributes = append(attributes, attribute)
	}
	return attributes, nil
}

const brokenAttribute = "an attribute of work items of the project, or a value of one, arrived without its id or its name"

func attributeMismatches(wrong []mismatch, filed []resolvedAttribute, value any) []mismatch {
	received, _ := value.([]any)
	for _, attribute := range filed {
		at := slices.IndexFunc(received, func(item any) bool { return memberOf(item, idKey) == attribute.id })
		var kept any
		if at >= 0 {
			kept = memberOf(received[at], valueKey)
		}
		if attribute.value == nil {
			wrong = emptyObjectMismatch(wrong, attribute.name, kept, nameKey)
			continue
		}
		if at >= 0 && memberOf(kept, idKey) == attribute.value.id {
			continue
		}
		wrong = append(wrong, mismatch{
			field:    attribute.name,
			expected: NewString(attribute.value.name),
			actual:   rawValueNode(memberOf(kept, nameKey)),
		})
	}
	return wrong
}
