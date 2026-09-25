package youtrack

import (
	"fmt"
	"slices"
	"strings"
)

const (
	attributesKey           = "attributes"
	workItemAttributeSchema = "WorkItemAttribute"
	attributeFlag           = "--attribute"
	attributeValueKey       = "value"
)

func attributesAsked() []requestedField {
	return []requestedField{
		{name: idKey},
		{name: nameKey},
		{name: attributeValueKey, children: []requestedField{{name: idKey}, {name: nameKey}}},
	}
}

func eachAttributes(spec *schemas, at string, requested []requestedField, visit func(parents []string, field *requestedField)) {
	fieldsOfType(spec, at, workItemAttributeSchema, requested, nil, visit)
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
		return nil, n.malformed("the attributes of the work item arrived as something other than an array")
	}
	pairs := make([]Pair, 0, len(received))
	named := make(map[string]bool, len(received))
	for _, item := range received {
		name, isText := memberOf(item, nameKey).(string)
		if !isText {
			return nil, n.malformed("an attribute of the work item arrived without a name")
		}
		if named[name] {
			return nil, n.malformed(fmt.Sprintf("two attributes of the work item are named %s", quote(name)))
		}
		named[name] = true
		held := NewNull()
		if value := memberOf(item, attributeValueKey); value != nil {
			valueName, isText := memberOf(value, nameKey).(string)
			if !isText {
				return nil, n.malformed(fmt.Sprintf("the value of the attribute %s arrived without a name", quote(name)))
			}
			held = NewString(valueName)
		}
		pairs = append(pairs, DataPair(name, held))
	}
	return NewMap(pairs...), nil
}

func attributeValues(filled []string) ([]namedValue, *Error) {
	named := make([]namedValue, 0, len(filled))
	for _, flag := range filled {
		name, value, split := strings.Cut(flag, "=")
		switch {
		case !split:
			message := fmt.Sprintf("%s %s holds no =: an attribute is set by writing its name, an = and the value, "+
				"as in %s 'Формат работы=ИИагент'", attributeFlag, quote(flag), attributeFlag)
			return nil, &Error{Code: CodeBadUsage, Message: message}
		case name == "":
			message := fmt.Sprintf("%s %s names no attribute: the name stands before the =", attributeFlag, quote(flag))
			return nil, &Error{Code: CodeBadUsage, Message: message}
		case value == "":
			message := fmt.Sprintf("%s %s names no value: --clear takes an attribute away", attributeFlag, quote(flag))
			return nil, &Error{Code: CodeBadUsage, Message: message}
		}
		if slices.ContainsFunc(named, func(earlier namedValue) bool { return strings.EqualFold(earlier.name, name) }) {
			message := fmt.Sprintf("%s names the attribute %s twice", attributeFlag, quote(name))
			return nil, &Error{Code: CodeBadUsage, Message: message}
		}
		named = append(named, namedValue{name: name, value: value})
	}
	return named, nil
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
	ID    string          `json:"id"`
	Value *workItemIDBody `json:"value"`
}

func attributeBodies(filed []resolvedAttribute) []attributeBody {
	written := make([]attributeBody, 0, len(filed))
	for _, attribute := range filed {
		sent := attributeBody{ID: attribute.id}
		if attribute.value != nil {
			sent.Value = &workItemIDBody{ID: attribute.value.id}
		}
		written = append(written, sent)
	}
	return written
}

func (p projectWorkItemTypes) resolveAttributes(set []namedValue, cleared []string) ([]resolvedAttribute, *Error) {
	catalogue := make([]fieldInfo, 0, len(p.attributes))
	for _, attribute := range p.attributes {
		catalogue = append(catalogue, fieldInfo{name: attribute.name})
	}
	var filed []resolvedAttribute
	var unknown []*Node
	for _, written := range set {
		at, found := matchName(written.name, catalogue)
		if !found {
			unknown = append(unknown, unknownAttribute(written.name, catalogue))
			continue
		}
		attribute := p.attributes[at]
		values := make([]fieldInfo, 0, len(attribute.values))
		for _, value := range attribute.values {
			values = append(values, fieldInfo{name: value.name})
		}
		place, found := matchName(written.value, values)
		if !found {
			unknown = append(unknown, NewMap(
				Pair{Key: "attribute", Value: NewString(attribute.name)},
				Pair{Key: attributeValueKey, Value: NewString(written.value)},
				Pair{Key: "nearest", Value: NewList(names(nearestNamed(written.value, values))...)}))
			continue
		}
		value := resolvedWorkType{id: attribute.values[place].id, name: written.value}
		filed = append(filed, resolvedAttribute{id: attribute.id, name: attribute.name, value: &value})
	}
	for _, name := range cleared {
		at, found := matchName(name, catalogue)
		if !found {
			unknown = append(unknown, unknownAttribute(name, catalogue))
			continue
		}
		filed = append(filed, resolvedAttribute{id: p.attributes[at].id, name: p.attributes[at].name})
	}
	if len(unknown) > 0 {
		message := "the names under unknown are not attributes of the work items of the project, or values they take"
		sent := requestDetail(p.response.httpResponse.Request.Method, p.response.httpResponse.Request.URL.Redacted())
		return nil, unknownNames(sent, Pair{Key: projectKey, Value: NewString(p.project)}, "unknown", message, unknown)
	}
	return filed, nil
}

func attributeBothWays(name string) *Error {
	message := fmt.Sprintf("%s sets the attribute %s and --clear takes it away, and the call gives both",
		attributeFlag, quote(name))
	return &Error{Code: CodeBadUsage, Message: message}
}

func unknownAttribute(name string, catalogue []fieldInfo) *Node {
	return NewMap(
		Pair{Key: "attribute", Value: NewString(name)},
		Pair{Key: "nearest", Value: NewList(names(nearestNamed(name, catalogue))...)})
}

func matchName(name string, catalogue []fieldInfo) (int, bool) {
	places := findMatches(name, catalogue)
	if len(places) > 1 {
		places = slices.DeleteFunc(places, func(at int) bool { return catalogue[at].name != name })
	}
	if len(places) != 1 {
		return 0, false
	}
	return places[0], true
}

func attributesOf(a decodedResponse, settings map[string]any) ([]projectAttribute, *Error) {
	items, isList := settings[attributesKey].([]any)
	if !isList {
		return nil, shapeFailure(a.httpResponse, a.body, "the attributes of work items of the project are not a JSON array")
	}
	attributes := make([]projectAttribute, 0, len(items))
	for _, item := range items {
		id, isText := memberOf(item, idKey).(string)
		name, isNamed := memberOf(item, nameKey).(string)
		values, isList := memberOf(item, "values").([]any)
		if !isText || !isNamed || !isList {
			return nil, shapeFailure(a.httpResponse, a.body, brokenAttribute)
		}
		attribute := projectAttribute{id: id, name: name}
		for _, value := range values {
			id, isText := memberOf(value, idKey).(string)
			name, isNamed := memberOf(value, nameKey).(string)
			if !isText || !isNamed {
				return nil, shapeFailure(a.httpResponse, a.body, brokenAttribute)
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
			kept = memberOf(received[at], attributeValueKey)
		}
		if attribute.value == nil {
			if kept != nil {
				wrong = append(wrong, mismatch{field: attribute.name, expected: NewNull(), actual: rawValueNode(memberOf(kept, nameKey))})
			}
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
