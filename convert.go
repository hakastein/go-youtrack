package youtrack

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

type layout int

const (
	blockLayout layout = iota
	inlineLayout
)

type converter struct {
	response     decodedResponse
	layout       layout
	at           []string
	row          activityCategory
	activityRoot bool
	phrases      linkPhrases
}

func newConverter(a decodedResponse, l layout) converter {
	return converter{response: a, layout: l}
}

func (n converter) child(name string) converter {
	n.activityRoot = false
	n.at = append(slices.Clip(n.at), name)
	return n
}

func (n converter) objectsAt(schema string, requested []requestedField, objects []map[string]any) ([]*Node, *Error) {
	printed := make([]*Node, 0, len(objects))
	for _, object := range objects {
		node, fault := n.object(schema, requested, object)
		if fault != nil {
			return nil, fault
		}
		printed = append(printed, node)
	}
	return printed, nil
}

func objectNode(a decodedResponse, requested []requestedField, object map[string]any, own []Pair) (*Node, *Error) {
	n := newConverter(a, blockLayout)
	pairs, fault := n.pairs(a.schema, requested, object)
	if fault != nil {
		return nil, fault
	}
	return NewMap(append(pairs, own...)...), nil
}

func (n converter) object(schema string, requested []requestedField, object map[string]any) (*Node, *Error) {
	pairs, fault := n.pairs(schema, requested, object)
	if fault != nil {
		return nil, fault
	}
	return NewMap(pairs...), nil
}

func (n converter) pairs(schema string, requested []requestedField, object map[string]any) ([]Pair, *Error) {
	if named, typed := object["$type"].(string); typed {
		schema = named
	}
	pairs := make([]Pair, 0, len(requested))
	for _, field := range requested {
		value, ok := object[field.name]
		if !ok {
			continue
		}
		node, fault := n.property(schema, field, value)
		if fault != nil {
			return nil, fault
		}
		pairs = append(pairs, Pair{Key: field.name, Value: node})
	}
	return pairs, nil
}

func (n converter) property(schema string, field requestedField, value any) (*Node, *Error) {
	if n.response.schemas.isInstanceURL(schema, field.name) {
		return n.resolveInstancePath(field, value)
	}
	decl, _ := n.response.schemas.declaration(schema, field.name)
	return n.value(decl, field, value)
}

func (n converter) value(decl typeRef, field requestedField, value any) (*Node, *Error) {
	if n.hasIssueBlocks() && decl.schema == customFieldSchema {
		return n.customFields(field, value)
	}
	if n.hasIssueBlocks() && decl.schema == linkSchema {
		return n.links(field, value)
	}
	if n.hasIssueBlocks() && decl.schema == workItemAttributeSchema {
		return n.attributes(value)
	}
	if n.activityRoot && decl.schema == categorySchema {
		return n.category(), nil
	}
	if n.activityRoot && field.name == fieldKey {
		return n.changedField(value)
	}
	if n.activityRoot && (field.name == addedKey || field.name == removedKey) {
		return n.values(decl, field, value)
	}
	switch value := value.(type) {
	case []any:
		items := make([]*Node, 0, len(value))
		for _, item := range value {
			node, fault := n.value(decl, field, item)
			if fault != nil {
				return nil, fault
			}
			items = append(items, node)
		}
		return NewList(items...), nil
	case nil:
		return NewNull(), nil
	}
	if decl.schema == durationSchema {
		return n.durationNode(value)
	}
	if decl.kind == timeKind {
		return n.instant(field.name, value)
	}
	switch value := value.(type) {
	case string:
		if decl.kind == textKind {
			return n.textNode(value), nil
		}
		return NewString(value), nil
	case bool:
		return NewBool(value), nil
	case json.Number:
		return NewNumber(value), nil
	case map[string]any:
		return n.child(field.name).object(decl.schema, field.children, value)
	}
	return NewNull(), nil
}

func (n converter) hasIssueBlocks() bool {
	return hasIssueBlocks(n.response.schema)
}

const (
	issueAttachmentSchema   = "IssueAttachment"
	articleAttachmentSchema = "ArticleAttachment"
	userSchema              = "User"
	urlKey                  = "url"
	thumbnailURLKey         = "thumbnailURL"
	avatarURLKey            = "avatarUrl"
	iconURLKey              = "iconUrl"
)

func (c *schemas) isInstanceURL(schema, property string) bool {
	switch property {
	case urlKey, thumbnailURLKey:
		return c.isSubtypeOf(schema, issueAttachmentSchema) || c.isSubtypeOf(schema, articleAttachmentSchema)
	case avatarURLKey:
		return c.isSubtypeOf(schema, userSchema)
	case iconURLKey:
		return c.isSubtypeOf(schema, projectSchema)
	}
	return false
}

func (n converter) resolveInstancePath(field requestedField, value any) (*Node, *Error) {
	if value == nil {
		return NewNull(), nil
	}
	text, isString := value.(string)
	if !isString {
		return nil, n.invalidURL(field, value)
	}
	reference, err := url.Parse(text)
	if err != nil || !isAbsolutePath(reference) {
		return nil, n.invalidURL(field, value)
	}
	origin := *n.response.address
	origin.User = nil
	return NewString(origin.ResolveReference(reference).String()), nil
}

func isAbsolutePath(reference *url.URL) bool {
	return reference.Scheme == "" && reference.Opaque == "" && reference.User == nil &&
		reference.Host == "" && strings.HasPrefix(reference.Path, "/")
}

func (n converter) invalidURL(field requestedField, value any) *Error {
	at := fieldPath(n.at, field.name)
	message := fmt.Sprintf("%s is an address of the instance, and what arrived for it is no absolute path", at)
	return n.response.fault(CodeUpstreamInvalid, message,
		Pair{Key: "field", Value: NewString(at)},
		Pair{Key: "upstream_value", Value: rawValueNode(value)})
}

func rawValueNode(value any) *Node {
	switch received := value.(type) {
	case string:
		return NewString(received)
	case json.Number:
		return NewNumber(received)
	case bool:
		return NewBool(received)
	case nil:
		return NewNull()
	}
	written, _ := json.Marshal(value)
	return NewString(string(written))
}

func (n converter) textNode(text string) *Node {
	if n.layout == inlineLayout {
		return NewString(text)
	}
	return NewText(text)
}

const (
	durationSchema = "DurationValue"
)

func (n converter) durationNode(value any) (*Node, *Error) {
	held, isObject := value.(map[string]any)
	if !isObject {
		return nil, n.response.invalid("a duration arrived as something other than a JSON object")
	}
	minutes, ok := held[minutesKey]
	if !ok {
		return nil, n.response.invalid("a duration arrived without the minutes it holds, which is what says how long it is")
	}
	count, isWhole := parseInt64(minutes)
	if !isWhole {
		return nil, n.response.invalid("the minutes of a duration are no whole number of them")
	}
	return NewString(duration(count)), nil
}

func (c *schemas) askDurationsByMinutes(responseSchema string, requested []requestedField) ([]requestedField, *Error) {
	asked := cloneFields(requested)
	return asked, c.durationsUnder(c.subtree(parseTypeRef(responseSchema).schema), asked, nil)
}

func (c *schemas) durationsUnder(owners []string, fields []requestedField, parents []string) *Error {
	for i := range fields {
		field := &fields[i]
		inner, duration, onlyDuration := c.declarationsOf(owners, *field)
		if onlyDuration && field.children != nil {
			message := fmt.Sprintf("%s is printed as the ISO 8601 period of the minutes it holds, as in PT1H30M, so "+
				"no name stands under it", fieldPath(parents, field.name))
			return &Error{Code: CodeBadUsage, Message: message}
		}
		if fault := c.durationsUnder(inner, field.children, append(slices.Clip(parents), field.name)); fault != nil {
			return fault
		}
		if duration {
			field.children = merge(field.children, requestedField{name: minutesKey})
		}
	}
	return nil
}

func (c *schemas) declarationsOf(owners []string, field requestedField) (inner []string, duration, onlyDuration bool) {
	other := false
	declared := slices.Clone(field.extraSchemas)
	for _, owner := range owners {
		decl, found := c.declaration(owner, field.name)
		switch {
		case !found:
			continue
		case decl.schema == durationSchema:
			duration = true
		default:
			other = true
		}
		if decl.schema != "" && !slices.Contains(declared, decl.schema) {
			declared = append(declared, decl.schema)
		}
	}
	for _, schema := range declared {
		for _, name := range c.subtree(schema) {
			if !slices.Contains(inner, name) {
				inner = append(inner, name)
			}
		}
	}
	return inner, duration, duration && !other
}

func (n converter) instant(name string, value any) (*Node, *Error) {
	count, isInstant := parseInt64(value)
	if !isInstant {
		return nil, n.response.invalid(notAnInstant(name))
	}
	return NewString(formatMoment(count)), nil
}

func parseInt64(value any) (int64, bool) {
	number, isNumber := value.(json.Number)
	if !isNumber {
		return 0, false
	}
	count, err := number.Int64()
	if err != nil {
		return 0, false
	}
	return count, true
}

func notAnInstant(name string) string {
	return fmt.Sprintf("%s is a time, and what arrived for it is no whole number of milliseconds since the epoch", name)
}

func intNode(n int) *Node {
	return NewNumber(json.Number(strconv.Itoa(n)))
}

func textList(texts []string) *Node {
	items := make([]*Node, 0, len(texts))
	for _, text := range texts {
		items = append(items, NewString(text))
	}
	return NewList(items...)
}
