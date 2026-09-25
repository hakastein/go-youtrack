package youtrack

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
)

// FieldWrite is one custom field of an issue to fill or to empty. Values are the value keys of the field's
// type: names for a bundle or group field, logins for a user field, the string or text itself, a period as
// PT1H30M, a day as 2026-09-16, a moment as 2026-08-31T03:00:00.123+03:00 or a number. A field of many
// values is written whole. Clear empties the field instead.
type FieldWrite struct {
	Name   string
	Values []string
	Clear  bool
}

// WriteFields writes the custom fields into the issue and answers with the issue as the server holds it
// after the write. The $type of each field comes from the issue when it holds the field and from the
// field's type otherwise; a value that comes back as something other than what was written is a *MismatchError.
func (c *Client) WriteFields(ctx context.Context, id string, writes []FieldWrite) (*Issue, error) {
	id, err := parseIssueID(id)
	if err != nil {
		return nil, err
	}
	if err := checkWrites(writes); err != nil {
		return nil, err
	}
	held, err := c.readIssueToWrite(ctx, id)
	if err != nil {
		return nil, err
	}
	resolved, err := held.resolve(writes)
	if err != nil {
		return nil, err
	}
	body := resolved.body()
	a, err := c.send(ctx, func(ctx context.Context) (*http.Response, error) {
		return c.apiUpdateIssue(ctx, held.idReadable, body, formatFields(issueFields()))
	})
	if err != nil {
		return nil, err
	}
	issue, err := readIssue(a)
	if err != nil {
		return nil, err
	}
	if err := resolved.verify(a.request, issue); err != nil {
		return nil, err
	}
	return issue, nil
}

func checkWrites(writes []FieldWrite) error {
	if len(writes) == 0 {
		return &ArgumentError{Argument: "writes", Value: "", Reason: "name no custom field: a write fills or empties at least one"}
	}
	for _, w := range writes {
		switch {
		case w.Name == "":
			return &ArgumentError{Argument: "field", Value: w.Name, Reason: "names no custom field"}
		case w.Clear && len(w.Values) > 0:
			return &ArgumentError{Argument: "field", Value: w.Name, Reason: "is given values and Clear both, and one write leaves it one way"}
		case !w.Clear && len(w.Values) == 0:
			return &ArgumentError{Argument: "field", Value: w.Name, Reason: "is given no value: Values fill a field and Clear empties it"}
		}
	}
	return nil
}

type issueToWrite struct {
	idReadable string
	project    projectMetadata
	classes    map[string]string
	answer     answer
}

func issueToWriteFields() []field {
	return []field{
		{name: idReadableKey},
		{name: customFieldsKey, children: []field{{name: "$type"}, {name: nameKey}, {name: bindingKey, children: []field{{name: idKey}}}}},
		{name: projectKey, children: projectFields()},
	}
}

func (c *Client) readIssueToWrite(ctx context.Context, id string) (issueToWrite, error) {
	a, err := c.read(ctx, func(ctx context.Context) (*http.Response, error) {
		return c.apiGetIssue(ctx, id, formatFields(issueToWriteFields()))
	})
	if err != nil {
		return issueToWrite{}, err
	}
	object, err := a.object()
	if err != nil {
		return issueToWrite{}, err
	}
	idReadable, isText := object[idReadableKey].(string)
	if !isText {
		return issueToWrite{}, a.invalid("the readable id of the issue arrived as something other than a string")
	}
	if _, err := parseIssueID(idReadable); err != nil {
		return issueToWrite{}, a.invalid(fmt.Sprintf("the issue arrived with %s for a readable id, and the write is addressed by the readable id the server gave", quote(idReadable)))
	}
	project, isObject := object[projectKey].(map[string]any)
	if !isObject {
		return issueToWrite{}, a.invalid("the project of the issue is not a JSON object")
	}
	metadata, err := readProjectMetadata(a, project)
	if err != nil {
		return issueToWrite{}, err
	}
	classes, err := readIssueClasses(a, object[customFieldsKey])
	if err != nil {
		return issueToWrite{}, err
	}
	return issueToWrite{idReadable: idReadable, project: metadata, classes: classes, answer: a}, nil
}

// The class a field is held in on the issue is the one the server named, as in StateMachineIssueCustomField
// for a state under a workflow, and the write sends it back as is.
func readIssueClasses(a answer, value any) (map[string]string, error) {
	items, isList := value.([]any)
	if !isList {
		return nil, a.invalid("the custom fields of the issue are not a JSON array")
	}
	classByBinding := make(map[string]string, len(items))
	for _, item := range items {
		object, isObject := item.(map[string]any)
		if !isObject {
			return nil, a.invalid("a custom field of the issue is not a JSON object")
		}
		name, isNamed := object[nameKey].(string)
		class, isText := object["$type"].(string)
		if !isNamed || !isText {
			return nil, a.invalid("the name or the class of a custom field of the issue is not text")
		}
		binding, isObject := object[bindingKey].(map[string]any)
		if !isObject {
			return nil, a.invalid(brokenIssueBinding(name))
		}
		bindingID, isText := binding[idKey].(string)
		if !isText {
			return nil, a.invalid(brokenIssueBinding(name))
		}
		classByBinding[bindingID] = class
	}
	return classByBinding, nil
}

type resolvedWrite struct {
	field   ProjectField
	kind    fieldKind
	class   string
	values  []string
	sent    []any
	keys    []string
	cleared bool
}

type resolvedWrites struct {
	idReadable string
	fields     []resolvedWrite
}

func (held issueToWrite) resolve(writes []FieldWrite) (resolvedWrites, error) {
	fields := held.project.fields
	given := make([][]string, len(fields))
	emptied := make([]bool, len(fields))
	var unknown []string
	var ambiguous []AmbiguousName
	reported := make(map[string]bool, len(writes))
	for _, w := range writes {
		at := matchFields(w.Name, fields)
		switch {
		case len(at) == 1:
			given[at[0]] = append(given[at[0]], w.Values...)
			emptied[at[0]] = emptied[at[0]] || w.Clear
			continue
		case reported[w.Name]:
		case len(at) == 0:
			unknown = append(unknown, w.Name)
		default:
			ambiguous = append(ambiguous, AmbiguousName{Name: w.Name, Candidates: fieldNames(fields, at)})
		}
		reported[w.Name] = true
	}
	if len(unknown) > 0 || len(ambiguous) > 0 {
		return resolvedWrites{}, &FieldNameError{Request: held.answer.request, Project: held.project.code,
			Unknown: unknown, Ambiguous: ambiguous, Known: allFieldNames(fields)}
	}
	return held.encode(given, emptied)
}

const setAndClearedReason = "the call writes a value into the custom field and empties it both, and one write leaves it one way"

func (held issueToWrite) encode(given [][]string, emptied []bool) (resolvedWrites, error) {
	resolved := resolvedWrites{idReadable: held.idReadable}
	var invalid []InvalidValue
	var required []string
	for at, values := range given {
		if len(values) == 0 && !emptied[at] {
			continue
		}
		f := held.project.fields[at]
		kind, known := f.Type.kind()
		if !known {
			return resolvedWrites{}, held.answer.invalid(unmodelled(f.Type))
		}
		class := kind.class
		if onTheIssue, held := held.classes[f.ID]; held {
			class = onTheIssue
		}
		written := resolvedWrite{field: f, kind: kind, class: class, values: values, cleared: emptied[at]}
		switch {
		case emptied[at] && len(values) > 0:
			invalid = append(invalid, InvalidValue{Field: f.Name, Value: values[0], Reason: setAndClearedReason})
			continue
		case emptied[at]:
			if !f.CanBeEmpty {
				required = append(required, f.Name)
			}
			written.sent = []any{}
			resolved.fields = append(resolved.fields, written)
			continue
		case !kind.multi && len(values) > 1:
			reason := fmt.Sprintf("the custom field holds one value by its type, and the call gives it %d", len(values))
			invalid = append(invalid, InvalidValue{Field: f.Name, Value: values[1], Reason: reason})
			continue
		}
		for _, value := range values {
			encoded, reason := kind.encode(value)
			if reason != "" {
				invalid = append(invalid, InvalidValue{Field: f.Name, Value: value, Reason: reason})
				continue
			}
			written.sent = append(written.sent, encoded.body)
			written.keys = append(written.keys, encoded.key)
		}
		resolved.fields = append(resolved.fields, written)
	}
	switch {
	case len(invalid) > 0:
		return resolvedWrites{}, &ValueError{Request: held.answer.request, Project: held.project.code, Invalid: invalid}
	case len(required) > 0:
		return resolvedWrites{}, &RequiredFieldError{Request: held.answer.request, Project: held.project.code, Fields: required}
	}
	return resolved, nil
}

type customFieldBody struct {
	Type  string `json:"$type"`
	Name  string `json:"name"`
	Value any    `json:"value"`
}

type updateIssueBody struct {
	CustomFields []customFieldBody `json:"customFields"`
}

// An emptied field of one value is written null and an emptied field of many values is written [].
func (r resolvedWrites) body() []byte {
	fields := make([]customFieldBody, 0, len(r.fields))
	for _, w := range r.fields {
		var value any
		switch {
		case w.kind.multi:
			value = w.sent
		case len(w.sent) > 0:
			value = w.sent[0]
		}
		fields = append(fields, customFieldBody{Type: w.class, Name: w.field.Name, Value: value})
	}
	body, _ := json.Marshal(updateIssueBody{CustomFields: fields})
	return body
}

// The server fixes the letter case of a name and returns a set of values in an order of its own, so a
// named value is compared without regard to case and a field of many values as a set.
func (r resolvedWrites) verify(request Request, issue *Issue) error {
	var wrong []Mismatch
	for _, w := range r.fields {
		at := slices.IndexFunc(issue.Fields, func(f Field) bool { return f.Name == w.field.Name })
		if at < 0 {
			if !w.cleared {
				wrong = append(wrong, Mismatch{Field: w.field.Name, Type: w.field.Type, Expected: w.keys})
			}
			continue
		}
		texts := issue.Fields[at].Texts()
		if covers(w.kind, w.keys, texts) && covers(w.kind, texts, w.keys) {
			continue
		}
		wrong = append(wrong, Mismatch{Field: w.field.Name, Type: w.field.Type, Expected: w.keys, Actual: texts})
	}
	if len(wrong) == 0 {
		return nil
	}
	return &MismatchError{Request: request, Issue: issue.IDReadable, Mismatches: wrong}
}

func covers(kind fieldKind, all, some []string) bool {
	for _, value := range some {
		if !slices.ContainsFunc(all, func(held string) bool { return kind.sameValue(held, value) }) {
			return false
		}
	}
	return true
}
