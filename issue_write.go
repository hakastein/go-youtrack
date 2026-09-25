package youtrack

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
)

func CreateIssue(code, summary string, description *string, filled []string, expression string) (Call, *Error) {
	code, fault := parseProjectCode(code)
	if fault != nil {
		return nil, fault
	}
	parts, fault := parseIssueCreate(summary, description, filled)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	requested, fault := issueFields(spec, expression, IssueShowFields, issueCommentTarget().commentsOfAWrite())
	if fault != nil {
		return nil, fault
	}
	return func(ctx context.Context, c *Client) (*Node, *Error) {
		return c.createIssue(ctx, spec, code, parts, requested)
	}, nil
}

func UpdateIssue(id string, summary, description *string, filled, cleared []string, expression string) (Call, *Error) {
	id, fault := parseIssueID(id)
	if fault != nil {
		return nil, fault
	}
	if summary == nil && description == nil && len(filled) == 0 && len(cleared) == 0 {
		return nil, &Error{Code: CodeBadUsage, Message: nothingToWrite}
	}
	parts, fault := parseIssueUpdate(summary, description, filled, cleared)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	requested, fault := issueFields(spec, expression, IssueShowFields, issueCommentTarget().commentsOfAWrite())
	if fault != nil {
		return nil, fault
	}
	return func(ctx context.Context, c *Client) (*Node, *Error) {
		return c.updateIssue(ctx, spec, id, parts, requested)
	}, nil
}

const nothingToWrite = "the call writes nothing into the issue: an update is given --summary, --description, " +
	"--field \"Name=value\" or --clear Name, and a part it is given none of is left as the issue holds it"

const descriptionBothWays = "--description writes the prose of the issue and --clear description empties it, and the " +
	"call gives both"

func (c *Client) createIssue(ctx context.Context, spec *schemas, code string, parts issueInput, requested []requestedField) (*Node, *Error) {
	project, fault := c.readProjectMetadata(ctx, spec, code)
	if fault != nil {
		return nil, fault
	}
	var newIssueHasNoFieldTypes map[string]string
	filed, fault := parts.resolve(project, newIssueHasNoFieldTypes)
	if fault != nil {
		return nil, fault
	}
	if hidden := filed.hiddenFields(); len(hidden) > 0 {
		return nil, project.fault(CodeBadUsage, hiddenMessage, "invalid", invalidEntries(hidden))
	}
	if missing := filed.missing(); len(missing) > 0 {
		return nil, project.fault(CodeMissingRequired, missingMessage, "missing", names(missing))
	}
	if fault := c.resolveCustomFields(ctx, spec, requested); fault != nil {
		return nil, fault
	}
	asked := withFields(requested, filed.verifyFields()...)
	issueBlocks(spec, composedIssue(), asked)
	body := filed.createBody()
	return c.write(ctx, spec, issueSchema, asked, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiCreateIssue(ctx, body, fields)
	}, filed.verify, writeResultNode(requested))
}

func (c *Client) updateIssue(ctx context.Context, spec *schemas, id string, parts issueInput, requested []requestedField) (*Node, *Error) {
	issue, fault := c.readIssueToWrite(ctx, spec, id)
	if fault != nil {
		return nil, fault
	}
	changed, fault := parts.resolve(issue.project, issue.fieldTypes)
	if fault != nil {
		return nil, fault
	}
	if emptied := changed.requiredEmptied(); len(emptied) > 0 {
		return nil, issue.project.fault(CodeMissingRequired, emptiedMessage, "missing", names(emptied))
	}
	if fault := c.resolveCustomFields(ctx, spec, requested); fault != nil {
		return nil, fault
	}
	asked := withFields(requested, changed.verifyFields()...)
	issueBlocks(spec, composedIssue(), asked)
	body := changed.updateBody()
	return c.write(ctx, spec, issueSchema, asked, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiUpdateIssue(ctx, issue.readable, body, fields)
	}, changed.verify, writeResultNode(requested))
}

type issueInput struct {
	summary           *string
	description       *string
	named             []namedValue
	clearsDescription bool
	cleared           []string
}

func parseIssueCreate(summary string, description *string, filled []string) (issueInput, *Error) {
	if fault := rejectReplacedText(&summary, description, descriptionOfANewIssue); fault != nil {
		return issueInput{}, fault
	}
	named, fault := parseFieldValues(filled)
	if fault != nil {
		return issueInput{}, fault
	}
	return issueInput{summary: &summary, description: description, named: named}, nil
}

func parseIssueUpdate(summary, description *string, filled, cleared []string) (issueInput, *Error) {
	emptied, clearsDescription, fault := clearedFields(cleared)
	if fault != nil {
		return issueInput{}, fault
	}
	if clearsDescription && description != nil {
		return issueInput{}, &Error{Code: CodeBadUsage, Message: descriptionBothWays}
	}
	if fault := rejectReplacedText(summary, description, descriptionOfAnIssue); fault != nil {
		return issueInput{}, fault
	}
	named, fault := parseFieldValues(filled)
	if fault != nil {
		return issueInput{}, fault
	}
	return issueInput{summary: summary, description: description, named: named, clearsDescription: clearsDescription, cleared: emptied}, nil
}

type namedValue struct {
	name  string
	value string
}

// The name is not trimmed: a project may name a field with a trailing space.
func parseFieldValues(filled []string) ([]namedValue, *Error) {
	named := make([]namedValue, 0, len(filled))
	for _, flag := range filled {
		name, value, split := strings.Cut(flag, "=")
		switch {
		case !split:
			message := fmt.Sprintf("--field %s holds no =: a custom field is filled by writing its name, an = "+
				"and the value, as in --field Type=Task", quote(flag))
			return nil, &Error{Code: CodeBadUsage, Message: message}
		case name == "":
			message := fmt.Sprintf("--field %s names no custom field: the name stands before the =", quote(flag))
			return nil, &Error{Code: CodeBadUsage, Message: message}
		}
		if fault := rejectOwnName(flag, name); fault != nil {
			return nil, fault
		}
		named = append(named, namedValue{name: name, value: value})
	}
	return named, nil
}

func clearedFields(cleared []string) ([]string, bool, *Error) {
	names := make([]string, 0, len(cleared))
	clearsDescription := false
	for _, name := range cleared {
		switch {
		case name == "":
			message := `--clear "" names no custom field: it takes the name of the field to empty, as in --clear Assignee`
			return nil, false, &Error{Code: CodeBadUsage, Message: message}
		case strings.EqualFold(name, summaryKey):
			message := fmt.Sprintf("--clear %s names the summary of the issue, which YouTrack files none "+
				"without: a title is written with --summary and cannot be taken away", quote(name))
			return nil, false, &Error{Code: CodeBadUsage, Message: message}
		case strings.EqualFold(name, descriptionKey):
			clearsDescription = true
		default:
			names = append(names, name)
		}
	}
	return names, clearsDescription, nil
}

func rejectOwnName(flag, name string) *Error {
	for _, own := range []string{summaryKey, descriptionKey} {
		if !strings.EqualFold(name, own) {
			continue
		}
		message := fmt.Sprintf("--field %s names the %s of the issue, which is no custom field of it: it is "+
			"written with --%s", quote(flag), own, own)
		return &Error{Code: CodeBadUsage, Message: message}
	}
	return nil
}

func rejectReplacedText(summary, description *string, emptyDescription string) *Error {
	if summary != nil {
		if fault := rejectReplaced("--summary", *summary, summaryEmpty, summaryRewrites()); fault != nil {
			return fault
		}
	}
	if description != nil {
		if fault := rejectReplaced("--description", *description, emptyDescription, descriptionRewrites()); fault != nil {
			return fault
		}
	}
	return nil
}

func summaryRewrites() []charReplacement {
	return []charReplacement{
		{rune: '\n', into: "a space"},
		{rune: '\r', into: "a space"},
		{rune: 0x85, into: "nothing at all"},
		{rune: 0x2028, into: "a space"},
		{rune: 0x2029, into: "a space"},
	}
}

func descriptionRewrites() []charReplacement {
	return []charReplacement{{rune: '\r', into: "nothing at all"}}
}

const (
	summaryEmpty           = "is empty, and YouTrack files no issue without a title"
	descriptionOfANewIssue = "is empty, and YouTrack keeps an empty description as none: leave the flag out to " +
		"file the issue with no description at all"
	descriptionOfAnIssue = "is empty, and YouTrack keeps an empty description as none: --clear description " +
		"empties it outright, and a description the call does not write is left as the issue holds it"
)

type createIssueBody struct {
	Project      projectIDBody     `json:"project"`
	Summary      string            `json:"summary"`
	Description  *string           `json:"description,omitempty"`
	CustomFields []customFieldBody `json:"customFields,omitempty"`
}

type projectIDBody struct {
	ID string `json:"id"`
}

type updateIssueBody struct {
	Summary      *string           `json:"summary,omitempty"`
	Description  json.RawMessage   `json:"description,omitempty"`
	CustomFields []customFieldBody `json:"customFields,omitempty"`
}

type customFieldBody struct {
	Type  string `json:"$type"`
	Name  string `json:"name"`
	Value any    `json:"value"`
}

type issueWrite struct {
	text    issueInput
	fields  []resolvedField
	project projectMetadata
}

type resolvedField struct {
	field    projectField
	kind     FieldType
	class    string
	values   []string
	sent     []any
	sentKeys []string
	cleared  bool
}

func (w issueWrite) createBody() []byte {
	body, _ := json.Marshal(createIssueBody{
		Project:      projectIDBody{ID: w.project.id},
		Summary:      *w.text.summary,
		Description:  w.text.description,
		CustomFields: w.bodies(),
	})
	return body
}

func (w issueWrite) updateBody() []byte {
	body, _ := json.Marshal(updateIssueBody{
		Summary:      w.text.summary,
		Description:  w.text.descriptionJSON(),
		CustomFields: w.bodies(),
	})
	return body
}

func (w issueInput) descriptionJSON() json.RawMessage {
	switch {
	case w.clearsDescription:
		return json.RawMessage("null")
	case w.description == nil:
		return nil
	}
	encoded, _ := json.Marshal(*w.description)
	return encoded
}

func (w issueWrite) bodies() []customFieldBody {
	fields := make([]customFieldBody, 0, len(w.fields))
	for _, field := range w.fields {
		fields = append(fields, field.body())
	}
	return fields
}

func (f resolvedField) body() customFieldBody {
	var value any
	switch {
	case f.kind.Multi:
		value = f.sent
	case len(f.sent) > 0:
		value = f.sent[0]
	}
	return customFieldBody{Type: f.class, Name: f.field.info.name, Value: value}
}

func (w issueWrite) verifyFields() []requestedField {
	own := []requestedField{{name: idReadableKey}}
	if w.text.summary != nil {
		own = append(own, requestedField{name: summaryKey})
	}
	if w.text.description != nil || w.text.clearsDescription {
		own = append(own, requestedField{name: descriptionKey})
	}
	if len(w.fields) > 0 {
		own = append(own, requestedField{name: customFieldsKey})
	}
	return own
}

func (w issueWrite) verify(a decodedResponse) *Error {
	issue := a.objects[0]
	var wrong []mismatch
	if w.text.summary != nil {
		wrong = textMismatch(wrong, summaryKey, *w.text.summary, issue[summaryKey])
	}
	switch {
	case w.text.description != nil:
		wrong = textMismatch(wrong, descriptionKey, *w.text.description, issue[descriptionKey])
	case w.text.clearsDescription:
		wrong = emptyMismatch(wrong, descriptionKey, issue[descriptionKey])
	}
	wrong, fault := w.verifyCustomFields(a, wrong)
	switch {
	case fault != nil:
		return fault
	case len(wrong) == 0:
		return nil
	}
	return mismatchFault(a, knownAs(issueOwner.String(), responseID(a, idReadableKey)), wrong)
}

func (w issueWrite) verifyCustomFields(a decodedResponse, wrong []mismatch) ([]mismatch, *Error) {
	if len(w.fields) == 0 {
		return wrong, nil
	}
	n := converter{response: a}
	received, fault := n.readCustomFields(a.objects[0][customFieldsKey])
	if fault != nil {
		return nil, fault
	}
	held := make(map[string]issueCustomField, len(received))
	for _, field := range received {
		held[field.name] = field
	}
	for _, written := range w.fields {
		name := written.field.info.name
		field, onTheIssue := held[name]
		if !onTheIssue {
			if written.cleared {
				continue
			}
			wrong = append(wrong, mismatch{field: name, expected: written.node(), actual: NewNull()})
			continue
		}
		texts, fault := n.valueKeys(field)
		if fault != nil {
			return nil, fault
		}
		if sameValueSet(written.kind, written.sentKeys, texts) {
			continue
		}
		wrong = append(wrong, mismatch{field: name, expected: written.node(), actual: valueNode(texts, written.kind)})
	}
	return wrong, nil
}

func sameValueSet(kind FieldType, sent, received []string) bool {
	return covers(kind, sent, received) && covers(kind, received, sent)
}

func covers(kind FieldType, all, some []string) bool {
	for _, value := range some {
		if !slices.ContainsFunc(all, func(held string) bool { return kind.Same(held, value) }) {
			return false
		}
	}
	return true
}

func (f resolvedField) node() *Node {
	return valueNode(f.values, f.kind)
}

func valueNode(values []string, kind FieldType) *Node {
	items := make([]*Node, 0, len(values))
	for _, value := range values {
		items = append(items, NewString(value))
	}
	switch {
	case kind.Multi:
		return NewList(items...)
	case len(items) == 0:
		return NewNull()
	}
	return items[0]
}

type projectMetadata struct {
	id       string
	code     string
	fields   []projectField
	response decodedResponse
}

type projectField struct {
	customField
	canBeEmpty bool
	defaults   []string
	condition  fieldCondition
}

type fieldCondition struct {
	kind             string
	controls         string
	values           []string
	showForNullValue bool
	given            bool
}

func writeMetadataFields() []requestedField {
	return []requestedField{
		{name: idKey},
		{name: "shortName"},
		{name: customFieldsKey, children: []requestedField{
			{name: idKey},
			{name: "canBeEmpty"},
			{name: "defaultValues", children: []requestedField{{name: nameKey}}},
			{name: "condition", children: []requestedField{
				{name: "$type"},
				{name: "showForNullValue"},
				{name: "field", children: []requestedField{{name: idKey}}},
				{name: "values", children: []requestedField{{name: nameKey}}},
			}},
			fieldInfoFields(),
		}},
	}
}

func (c *Client) readProjectMetadata(ctx context.Context, spec *schemas, code string) (projectMetadata, *Error) {
	a, fault := c.request(ctx, spec, projectSchema, writeMetadataFields(), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetProject(ctx, code, fields)
	})
	if fault != nil {
		return projectMetadata{}, fault
	}
	return readWriteMetadata(a, a.objects[0])
}

type issueForUpdate struct {
	readable   readableID
	project    projectMetadata
	fieldTypes map[string]string
}

func issueToWriteFields() []requestedField {
	return []requestedField{
		{name: idReadableKey},
		{name: customFieldsKey, children: []requestedField{
			{name: "$type"},
			{name: nameKey},
			{name: "projectCustomField", children: []requestedField{{name: idKey}}},
		}},
		{name: "project", children: writeMetadataFields()},
	}
}

func (c *Client) readIssueToWrite(ctx context.Context, spec *schemas, id string) (issueForUpdate, *Error) {
	a, fault := c.request(ctx, spec, issueSchema, issueToWriteFields(), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetIssue(ctx, id, fields, nil)
	})
	if fault != nil {
		return issueForUpdate{}, fault
	}
	readable, fault := readableIDAt(a, a.objects[0], issueOwner, "an update")
	if fault != nil {
		return issueForUpdate{}, fault
	}
	held, isObject := a.objects[0]["project"].(map[string]any)
	if !isObject {
		return issueForUpdate{}, shapeFailure(a.httpResponse, a.body, "the project of the issue is not a JSON object")
	}
	project, fault := readWriteMetadata(a, held)
	if fault != nil {
		return issueForUpdate{}, fault
	}
	fieldTypes, fault := readIssueFieldTypes(a)
	if fault != nil {
		return issueForUpdate{}, fault
	}
	return issueForUpdate{readable: readable, project: project, fieldTypes: fieldTypes}, nil
}

func readIssueFieldTypes(a decodedResponse) (map[string]string, *Error) {
	items, isList := a.objects[0][customFieldsKey].([]any)
	if !isList {
		return nil, shapeFailure(a.httpResponse, a.body, "the custom fields of the issue are not a JSON array")
	}
	typeByProjectFieldID := make(map[string]string, len(items))
	for _, item := range items {
		object, isObject := item.(map[string]any)
		if !isObject {
			return nil, shapeFailure(a.httpResponse, a.body, "a custom field of the issue is not a JSON object")
		}
		name, isNamed := object[nameKey].(string)
		kind, isText := object["$type"].(string)
		if !isNamed || !isText {
			return nil, shapeFailure(a.httpResponse, a.body, brokenIssueField)
		}
		place, isObject := object["projectCustomField"].(map[string]any)
		if !isObject {
			return nil, shapeFailure(a.httpResponse, a.body, brokenBinding(name))
		}
		projectFieldID, isText := place[idKey].(string)
		if !isText {
			return nil, shapeFailure(a.httpResponse, a.body, brokenBinding(name))
		}
		typeByProjectFieldID[projectFieldID] = kind
	}
	return typeByProjectFieldID, nil
}

const brokenIssueField = "the name or the class of a custom field of the issue is not text"

const brokenProject = "the id or the short name of the project is not text"

func readWriteMetadata(a decodedResponse, project map[string]any) (projectMetadata, *Error) {
	id, isText := project[idKey].(string)
	code, isName := project["shortName"].(string)
	if !isText || !isName {
		return projectMetadata{}, shapeFailure(a.httpResponse, a.body, brokenProject)
	}
	items, isList := project[customFieldsKey].([]any)
	if !isList {
		return projectMetadata{}, shapeFailure(a.httpResponse, a.body, "the custom fields of the project are not a JSON array")
	}
	fields := make([]projectField, 0, len(items))
	for _, item := range items {
		object, isObject := item.(map[string]any)
		if !isObject {
			return projectMetadata{}, shapeFailure(a.httpResponse, a.body, brokenField)
		}
		field, ok := readProjectField(object)
		if !ok {
			return projectMetadata{}, shapeFailure(a.httpResponse, a.body, brokenFieldInfo)
		}
		fields = append(fields, field)
	}
	return projectMetadata{id: id, code: code, fields: fields, response: a}, nil
}

func readProjectField(object map[string]any) (projectField, bool) {
	id, isText := object[idKey].(string)
	named, isNamed := readFieldInfo(object)
	canBeEmpty, isFlag := object["canBeEmpty"].(bool)
	if !isText || !isNamed || !isFlag {
		return projectField{}, false
	}
	defaults, ok := readValueNames(object["defaultValues"])
	if !ok {
		return projectField{}, false
	}
	shown, ok := readCondition(object["condition"])
	if !ok {
		return projectField{}, false
	}
	field := customField{id: id, info: named}
	return projectField{customField: field, canBeEmpty: canBeEmpty, defaults: defaults, condition: shown}, true
}

func readValueNames(value any) ([]string, bool) {
	if value == nil {
		return nil, true
	}
	items, isList := value.([]any)
	if !isList {
		return nil, false
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		object, isObject := item.(map[string]any)
		if !isObject {
			return nil, false
		}
		name, isText := object[nameKey].(string)
		if !isText {
			return nil, false
		}
		names = append(names, name)
	}
	return names, true
}

func readCondition(value any) (fieldCondition, bool) {
	if value == nil {
		return fieldCondition{}, true
	}
	object, isObject := value.(map[string]any)
	if !isObject {
		return fieldCondition{}, false
	}
	kind, isText := object["$type"].(string)
	if !isText {
		return fieldCondition{}, false
	}
	if kind != fieldBasedCondition {
		return fieldCondition{kind: kind, given: true}, true
	}
	shown, isFlag := object["showForNullValue"].(bool)
	values, isList := readValueNames(object["values"])
	if !isFlag || !isList {
		return fieldCondition{}, false
	}
	controls := ""
	if watched, isObject := object["field"].(map[string]any); isObject {
		if controls, isText = watched[idKey].(string); !isText {
			return fieldCondition{}, false
		}
	}
	return fieldCondition{kind: kind, controls: controls, values: values, showForNullValue: shown, given: true}, true
}

func (w issueInput) resolve(project projectMetadata, issueFieldTypes map[string]string) (issueWrite, *Error) {
	fields, fault := project.resolveFields(w.named, w.cleared, issueFieldTypes)
	if fault != nil {
		return issueWrite{}, fault
	}
	return issueWrite{text: w, fields: fields, project: project}, nil
}

func (p projectMetadata) resolveFields(named []namedValue, cleared []string, issueFieldTypes map[string]string) ([]resolvedField, *Error) {
	catalogue := make([]fieldInfo, 0, len(p.fields))
	for _, field := range p.fields {
		catalogue = append(catalogue, field.info)
	}
	given := make([][]string, len(p.fields))
	emptied := make([]bool, len(p.fields))
	var unknown, ambiguous []*Node
	reported := make(map[string]bool, len(named)+len(cleared))
	place := func(name string) (int, bool) {
		places := findMatches(name, catalogue)
		switch {
		case len(places) == 1:
			return places[0], true
		case reported[name]:
		case len(places) == 0:
			unknown = append(unknown, unknownEntry(name, nearestNamed(name, catalogue)))
		default:
			ambiguous = append(ambiguous, ambiguousEntry(name, canonical(pick(catalogue, places))))
		}
		reported[name] = true
		return 0, false
	}
	for _, addressed := range named {
		if at, found := place(addressed.name); found {
			given[at] = append(given[at], addressed.value)
		}
	}
	for _, name := range cleared {
		if at, found := place(name); found {
			emptied[at] = true
		}
	}
	switch {
	case len(unknown) > 0:
		return nil, p.fault(CodeUnknownName, unknownMessage, "unknown", unknown)
	case len(ambiguous) > 0:
		return nil, p.fault(CodeUnknownName, ambiguousMessage, "ambiguous", ambiguous)
	}
	return p.encodeValues(given, emptied, issueFieldTypes)
}

func (p projectMetadata) encodeValues(given [][]string, emptied []bool, issueFieldTypes map[string]string) ([]resolvedField, *Error) {
	fields := make([]resolvedField, 0, len(p.fields))
	var invalid []*Node
	for at, values := range given {
		if len(values) == 0 && !emptied[at] {
			continue
		}
		field := p.fields[at]
		kind := field.info.kind
		if !kind.Known() {
			return nil, shapeFailure(p.response.httpResponse, p.response.body, unmodelledType(field.info))
		}
		class := kind.Class()
		if typeOnIssue, onTheIssue := issueFieldTypes[field.id]; onTheIssue {
			class = typeOnIssue
		}
		written := resolvedField{field: field, kind: kind, class: class, values: values, cleared: emptied[at]}
		switch {
		case emptied[at] && len(values) > 0:
			invalid = append(invalid, invalidEntry(field.info.name, values[0], setAndClearedMessage))
			continue
		case emptied[at]:
			written.sent = []any{}
			fields = append(fields, written)
			continue
		case !kind.Multi && len(values) > 1:
			reason := fmt.Sprintf("the custom field holds one value by its type, and the call gives it %d", len(values))
			invalid = append(invalid, invalidEntry(field.info.name, values[1], reason))
			continue
		}
		for _, value := range values {
			sent, reason := encodeValue(kind, value)
			if reason != "" {
				invalid = append(invalid, invalidEntry(field.info.name, value, reason))
				continue
			}
			written.sent = append(written.sent, sent.Body)
			written.sentKeys = append(written.sentKeys, sent.Key)
		}
		fields = append(fields, written)
	}
	if len(invalid) > 0 {
		return nil, p.fault(CodeBadUsage, invalidMessage, "invalid", invalid)
	}
	return fields, nil
}

func invalidEntry(field, value, reason string) *Node {
	return NewMap(
		Pair{Key: "field", Value: NewString(field)},
		Pair{Key: "value", Value: NewString(value)},
		Pair{Key: "reason", Value: NewString(reason)})
}

func (w issueWrite) missing() []string {
	var missing []string
	for _, field := range w.project.fields {
		if field.canBeEmpty || len(field.defaults) > 0 || w.sets(field) {
			continue
		}
		if _, hidden := w.hiddenReason(field); hidden {
			continue
		}
		missing = append(missing, field.info.name)
	}
	return missing
}

func (w issueWrite) requiredEmptied() []string {
	var required []string
	for _, written := range w.fields {
		if written.cleared && !written.field.canBeEmpty {
			required = append(required, written.field.info.name)
		}
	}
	return required
}

func (w issueWrite) sets(field projectField) bool {
	return slices.ContainsFunc(w.fields, func(written resolvedField) bool { return written.field.id == field.id })
}

const (
	missingMessage   = "the custom fields under missing are required by the project and the call fills none of them"
	emptiedMessage   = "the custom fields under missing are required by the project and the call empties them"
	unknownMessage   = "the names under unknown are not custom fields of the project"
	ambiguousMessage = "the names under ambiguous are the names of more than one custom field of the project each"
	invalidMessage   = "the values under invalid are not values the fields they name can be given, and nothing was sent"
	hiddenMessage    = "the custom fields under invalid do not stand on the issue the call would file, and " +
		"nothing was sent"
	setAndClearedMessage = "the call writes a value into the custom field and empties it both, and one write " +
		"leaves it one way"
)

func (p projectMetadata) fault(code Code, message, key string, entries []*Node) *Error {
	a := p.response
	details := []Pair{
		requestDetail(a.httpResponse.Request.Method, a.httpResponse.Request.URL.Redacted()),
		{Key: "project", Value: NewString(p.code)},
		{Key: key, Value: NewList(entries...)},
	}
	return &Error{Code: code, Message: message, Details: details}
}

func names(fields []string) []*Node {
	nodes := make([]*Node, 0, len(fields))
	for _, name := range fields {
		nodes = append(nodes, NewString(name))
	}
	return nodes
}

func invalidEntries(hidden []hiddenField) []*Node {
	entries := make([]*Node, 0, len(hidden))
	for _, field := range hidden {
		entries = append(entries, invalidEntry(field.name, field.value, field.reason))
	}
	return entries
}

func (w issueWrite) hiddenReason(field projectField) (string, bool) {
	c := field.condition
	if !c.given || c.kind != fieldBasedCondition || c.controls == "" {
		return "", false
	}
	watched, found := fieldByID(w.project.fields, c.controls)
	if !found || watched.info.kind.Multi {
		return "", false
	}
	held, filled := w.effectiveValue(watched)
	switch {
	case !filled:
		if c.showForNullValue {
			return "", false
		}
	case slices.ContainsFunc(c.values, func(name string) bool { return strings.EqualFold(name, held) }):
		return "", false
	}
	return hiddenBy(watched.info.name, c, held, filled), true
}

func (w issueWrite) hiddenFields() []hiddenField {
	var hidden []hiddenField
	for _, written := range w.fields {
		reason, kept := w.hiddenReason(written.field)
		if !kept {
			continue
		}
		hidden = append(hidden, hiddenField{name: written.field.info.name, value: written.values[0], reason: reason})
	}
	return hidden
}

type hiddenField struct {
	name   string
	value  string
	reason string
}

func (w issueWrite) effectiveValue(field projectField) (string, bool) {
	for _, written := range w.fields {
		if written.field.id == field.id && len(written.values) > 0 {
			return written.values[0], true
		}
	}
	return field.defaultValue()
}

func (f projectField) defaultValue() (string, bool) {
	if len(f.defaults) == 0 {
		return "", false
	}
	return f.defaults[0], true
}

func hiddenBy(controls string, c fieldCondition, held string, filled bool) string {
	return fmt.Sprintf("the project shows the custom field on an issue whose %s holds %s, the issue this call "+
		"files holds %s in it, and YouTrack would file the issue without the value under a 200",
		controls, c.visibleForValues(), describeValue(held, filled))
}

func (c fieldCondition) visibleForValues() string {
	shown := make([]string, 0, len(c.values)+1)
	for _, name := range c.values {
		shown = append(shown, quote(name))
	}
	if c.showForNullValue {
		shown = append(shown, emptyValueText)
	}
	if len(shown) == 0 {
		return "no value at all"
	}
	return strings.Join(shown, " or ")
}

func describeValue(held string, filled bool) string {
	if !filled {
		return emptyValueText
	}
	return quote(held)
}

const emptyValueText = "nothing at all"

func fieldByID(fields []projectField, id string) (projectField, bool) {
	for _, field := range fields {
		if field.id == id {
			return field, true
		}
	}
	return projectField{}, false
}
