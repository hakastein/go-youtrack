package youtrack

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
)

// Values are value keys of the field's type; a field of many values is written whole, and writes of one name add up.
type FieldWrite struct {
	Name   string
	Values []string
	Clear  bool
}

type IssueInput struct {
	Summary     string
	Description string
	Fields      []FieldWrite
}

// IssueUpdate: a nil part is left as the issue holds it. YouTrack keeps an empty description as none, so an empty
// Description is refused and ClearDescription empties it.
type IssueUpdate struct {
	Summary          *string
	Description      *string
	ClearDescription bool
	Fields           []FieldWrite
}

// Create checks the call against the metadata of the project before the write: the names and the values of the
// fields, the fields the project requires and the ones its conditions hide on the new issue.
func (s *IssuesService) Create(ctx context.Context, project string, in *IssueInput, opts *WriteOptions) (*Node, error) {
	return result(s.create(ctx, project, optionsOf(in), optionsOf(opts)))
}

// Update sends a field under the class the issue holds it in, as StateMachineIssueCustomField for a state under a
// workflow.
func (s *IssuesService) Update(ctx context.Context, id string, in *IssueUpdate, opts *WriteOptions) (*Node, error) {
	return result(s.update(ctx, id, optionsOf(in), optionsOf(opts)))
}

func (s *IssuesService) WriteFields(ctx context.Context, id string, writes []FieldWrite) (*Issue, error) {
	return result(s.writeFields(ctx, id, writes))
}

func (s *IssuesService) create(ctx context.Context, project string, in IssueInput, opts WriteOptions) (*Node, *Error) {
	code, fault := parseProjectCode(project)
	if fault != nil {
		return nil, fault
	}
	parts, fault := in.parse()
	if fault != nil {
		return nil, fault
	}
	c := s.client
	requested, fault := issueFields(c.spec, opts.Fields, IssueShowFields, issueCommentTarget().commentsOfAWrite())
	if fault != nil {
		return nil, fault
	}
	return createIssue(ctx, c, code, parts, issueDocument(c, requested))
}

func (s *IssuesService) update(ctx context.Context, id string, in IssueUpdate, opts WriteOptions) (*Node, *Error) {
	id, fault := parseIssueID(id)
	if fault != nil {
		return nil, fault
	}
	parts, fault := in.parse()
	if fault != nil {
		return nil, fault
	}
	c := s.client
	requested, fault := issueFields(c.spec, opts.Fields, IssueShowFields, issueCommentTarget().commentsOfAWrite())
	if fault != nil {
		return nil, fault
	}
	return updateIssue(ctx, c, id, parts, issueDocument(c, requested))
}

func (s *IssuesService) writeFields(ctx context.Context, id string, writes []FieldWrite) (*Issue, *Error) {
	id, fault := parseIssueID(id)
	if fault != nil {
		return nil, fault
	}
	if len(writes) == 0 {
		return nil, &Error{Code: CodeBadUsage, Message: noFieldToWrite}
	}
	named, cleared, fault := parseFieldWrites(writes)
	if fault != nil {
		return nil, fault
	}
	return updateIssue(ctx, s.client, id, issueInput{named: named, cleared: cleared}, issueRecordAnswer())
}

const (
	nothingToWrite = "the call writes nothing into the issue: an update names at least one part to write, and a " +
		"part it does not name is left as the issue holds it"
	noFieldToWrite      = "the call writes no custom field into the issue, and a write of the fields names at least one"
	descriptionBothWays = "the call both writes the description of the issue and empties it"
	clearOnCreate       = "the call empties the custom field %s of an issue it files: a field a new issue is given " +
		"no value for is filed as the project fills it"
)

type issueAnswer[T any] struct {
	fields func(ctx context.Context, verified []requestedField) ([]requestedField, *Error)
	read   func(decodedResponse) (T, *Error)
}

func issueDocument(c *Client, requested []requestedField) issueAnswer[*Node] {
	return issueAnswer[*Node]{
		fields: func(ctx context.Context, verified []requestedField) ([]requestedField, *Error) {
			if fault := c.resolveCustomFields(ctx, requested); fault != nil {
				return nil, fault
			}
			asked := withFields(requested, verified...)
			issueBlocks(c.spec, composedIssue(), asked)
			return asked, nil
		},
		read: writeResultNode(requested),
	}
}

func issueRecordAnswer() issueAnswer[*Issue] {
	return issueAnswer[*Issue]{
		fields: func(_ context.Context, verified []requestedField) ([]requestedField, *Error) {
			return withFields(issueRecordFields(), verified...), nil
		},
		read: readIssue,
	}
}

func createIssue(ctx context.Context, c *Client, code string, parts issueInput, answer issueAnswer[*Node]) (*Node, *Error) {
	project, fault := c.readProjectMetadata(ctx, code)
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
		return nil, project.fault(CodeMissingRequired, missingMessage, "missing", textList(missing))
	}
	asked, fault := answer.fields(ctx, filed.verifyFields())
	if fault != nil {
		return nil, fault
	}
	body := filed.createBody()
	return writeAs(ctx, c, issueSchema, asked, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiCreateIssue(ctx, body, fields)
	}, filed.verify, answer.read)
}

func updateIssue[T any](ctx context.Context, c *Client, id string, parts issueInput, answer issueAnswer[T]) (T, *Error) {
	var none T
	issue, fault := c.readIssueToWrite(ctx, id)
	if fault != nil {
		return none, fault
	}
	changed, fault := parts.resolve(issue.project, issue.fieldTypes)
	if fault != nil {
		return none, fault
	}
	if emptied := changed.requiredEmptied(); len(emptied) > 0 {
		return none, issue.project.fault(CodeMissingRequired, emptiedMessage, "missing", textList(emptied))
	}
	asked, fault := answer.fields(ctx, changed.verifyFields())
	if fault != nil {
		return none, fault
	}
	body := changed.updateBody()
	return writeAs(ctx, c, issueSchema, asked, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiUpdateIssue(ctx, issue.readable, body, fields)
	}, changed.verify, answer.read)
}

type issueInput struct {
	summary           *string
	description       *string
	named             []namedValue
	clearsDescription bool
	cleared           []string
}

func (in IssueInput) parse() (issueInput, *Error) {
	var description *string
	if in.Description != "" {
		description = &in.Description
	}
	if fault := rejectReplacedText(&in.Summary, description); fault != nil {
		return issueInput{}, fault
	}
	named, cleared, fault := parseFieldWrites(in.Fields)
	if fault != nil {
		return issueInput{}, fault
	}
	if len(cleared) > 0 {
		return issueInput{}, &Error{Code: CodeBadUsage, Message: fmt.Sprintf(clearOnCreate, quote(cleared[0]))}
	}
	return issueInput{summary: &in.Summary, description: description, named: named}, nil
}

func (in IssueUpdate) parse() (issueInput, *Error) {
	if in.Summary == nil && in.Description == nil && !in.ClearDescription && len(in.Fields) == 0 {
		return issueInput{}, &Error{Code: CodeBadUsage, Message: nothingToWrite}
	}
	if in.ClearDescription && in.Description != nil {
		return issueInput{}, &Error{Code: CodeBadUsage, Message: descriptionBothWays}
	}
	if fault := rejectReplacedText(in.Summary, in.Description); fault != nil {
		return issueInput{}, fault
	}
	named, cleared, fault := parseFieldWrites(in.Fields)
	if fault != nil {
		return issueInput{}, fault
	}
	return issueInput{summary: in.Summary, description: in.Description, named: named,
		clearsDescription: in.ClearDescription, cleared: cleared}, nil
}

type namedValue struct {
	name  string
	value string
}

func parseFieldWrites(writes []FieldWrite) ([]namedValue, []string, *Error) {
	var named []namedValue
	var cleared []string
	for _, w := range writes {
		if fault := w.check(); fault != nil {
			return nil, nil, fault
		}
		for _, value := range w.Values {
			named = append(named, namedValue{name: w.Name, value: value})
		}
		if w.Clear {
			cleared = append(cleared, w.Name)
		}
	}
	return named, cleared, nil
}

// The name is not trimmed: a project may name a field with a trailing space.
func (w FieldWrite) check() *Error {
	switch {
	case w.Name == "":
		return &Error{Code: CodeBadUsage, Message: "a write of a custom field names no field"}
	case !w.Clear && len(w.Values) == 0:
		message := fmt.Sprintf("the write of the custom field %s neither gives it a value nor empties it", quote(w.Name))
		return &Error{Code: CodeBadUsage, Message: message}
	}
	for _, own := range []string{summaryKey, descriptionKey} {
		if strings.EqualFold(w.Name, own) {
			message := fmt.Sprintf("the custom field %s names the %s of the issue, which is a part of the issue "+
				"written on its own and no custom field", quote(w.Name), own)
			return &Error{Code: CodeBadUsage, Message: message}
		}
	}
	return nil
}

func rejectReplacedText(summary, description *string) *Error {
	if summary != nil {
		if fault := rejectReplaced("the summary", *summary, summaryEmpty, append(lineBreakRewrites(), stringFieldRewrites()...)); fault != nil {
			return fault
		}
	}
	if description != nil {
		if fault := rejectReplaced("the description", *description, descriptionEmpty, descriptionRewrites()); fault != nil {
			return fault
		}
	}
	return nil
}

func descriptionRewrites() []charReplacement {
	return []charReplacement{{rune: '\r', into: "nothing at all"}}
}

const (
	summaryEmpty     = "is empty, and YouTrack files no issue without a title"
	descriptionEmpty = "is empty, and YouTrack keeps an empty description as none: the description is emptied " +
		"outright by clearing it, and a description the call does not write is left as the issue holds it"
)

type createIssueBody struct {
	Project      idBody            `json:"project"`
	Summary      string            `json:"summary"`
	Description  *string           `json:"description,omitempty"`
	CustomFields []customFieldBody `json:"customFields,omitempty"`
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
		Project:      idBody{ID: w.project.id},
		Summary:      *w.text.summary,
		Description:  w.text.description,
		CustomFields: w.bodies(),
	})
	return body
}

func (w issueWrite) updateBody() []byte {
	body, _ := json.Marshal(updateIssueBody{
		Summary:      w.text.summary,
		Description:  optionalJSON(w.text.clearsDescription, w.text.description),
		CustomFields: w.bodies(),
	})
	return body
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
	if fault != nil {
		return fault
	}
	return mismatchFault(a, wrong, Pair{Key: issueOwner.String(), Value: responseID(a, idReadableKey)})
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
		record, fault := n.recordField(field)
		if fault != nil {
			return nil, fault
		}
		texts := record.Texts()
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
	switch {
	case kind.Multi:
		return textList(values)
	case len(values) == 0:
		return NewNull()
	}
	return NewString(values[0])
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
		{name: shortNameKey},
		{name: customFieldsKey, children: []requestedField{
			{name: idKey},
			{name: canBeEmptyKey},
			{name: "defaultValues", children: []requestedField{{name: nameKey}}},
			{name: "condition", children: []requestedField{
				{name: "$type"},
				{name: "showForNullValue"},
				{name: fieldKey, children: []requestedField{{name: idKey}}},
				{name: "values", children: []requestedField{{name: nameKey}}},
			}},
			fieldInfoFields(),
		}},
	}
}

func (c *Client) readProjectMetadata(ctx context.Context, code string) (projectMetadata, *Error) {
	a, fault := c.request(ctx, projectSchema, writeMetadataFields(), func(ctx context.Context, fields string) (*http.Response, error) {
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
		{name: projectKey, children: writeMetadataFields()},
	}
}

func (c *Client) readIssueToWrite(ctx context.Context, id string) (issueForUpdate, *Error) {
	a, fault := c.request(ctx, issueSchema, issueToWriteFields(), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetIssue(ctx, id, fields, nil)
	})
	if fault != nil {
		return issueForUpdate{}, fault
	}
	readable, fault := readableIDOf(a, issueOwner, "an update")
	if fault != nil {
		return issueForUpdate{}, fault
	}
	held, isObject := a.objects[0][projectKey].(map[string]any)
	if !isObject {
		return issueForUpdate{}, a.invalid("the project of the issue is not a JSON object")
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
		return nil, a.invalid("the custom fields of the issue are not a JSON array")
	}
	typeByProjectFieldID := make(map[string]string, len(items))
	for _, item := range items {
		object, isObject := item.(map[string]any)
		if !isObject {
			return nil, a.invalid("a custom field of the issue is not a JSON object")
		}
		name, isNamed := object[nameKey].(string)
		kind, isText := object["$type"].(string)
		if !isNamed || !isText {
			return nil, a.invalid(brokenIssueField)
		}
		projectFieldID, isText := memberOf(object["projectCustomField"], idKey).(string)
		if !isText {
			return nil, a.invalid(brokenBinding(name))
		}
		typeByProjectFieldID[projectFieldID] = kind
	}
	return typeByProjectFieldID, nil
}

const brokenIssueField = "the name or the class of a custom field of the issue is not text"

const brokenProject = "the id or the short name of the project is not text"

func readWriteMetadata(a decodedResponse, project map[string]any) (projectMetadata, *Error) {
	id, isText := project[idKey].(string)
	code, isName := project[shortNameKey].(string)
	if !isText || !isName {
		return projectMetadata{}, a.invalid(brokenProject)
	}
	items, isList := project[customFieldsKey].([]any)
	if !isList {
		return projectMetadata{}, a.invalid("the custom fields of the project are not a JSON array")
	}
	fields := make([]projectField, 0, len(items))
	for _, item := range items {
		object, isObject := item.(map[string]any)
		if !isObject {
			return projectMetadata{}, a.invalid(brokenField)
		}
		field, ok := readProjectField(object)
		if !ok {
			return projectMetadata{}, a.invalid(brokenFieldInfo)
		}
		fields = append(fields, field)
	}
	return projectMetadata{id: id, code: code, fields: fields, response: a}, nil
}

func readProjectField(object map[string]any) (projectField, bool) {
	id, isText := object[idKey].(string)
	named, isNamed := readFieldInfo(object)
	canBeEmpty, isFlag := object[canBeEmptyKey].(bool)
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
	if watched, isObject := object[fieldKey].(map[string]any); isObject {
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
	names := resolvingNames(catalogue)
	for _, addressed := range named {
		if at, found := names.place(addressed.name, addressed.name); found {
			given[at] = append(given[at], addressed.value)
		}
	}
	for _, name := range cleared {
		if at, found := names.place(name, name); found {
			emptied[at] = true
		}
	}
	project := Pair{Key: projectKey, Value: NewString(p.code)}
	if fault := names.fault(p.response.sent(), project, "the project"); fault != nil {
		return nil, fault
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
		k, known := kind.kind()
		if !known {
			return nil, p.response.invalid(unmodelled(kind))
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
			sent, reason := k.encode(value)
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
		return nil, p.fault(CodeBadUsage, invalidMessage, "invalid", NewList(invalid...))
	}
	return fields, nil
}

func invalidEntry(field, value, reason string) *Node {
	return NewMap(
		Pair{Key: fieldKey, Value: NewString(field)},
		Pair{Key: valueKey, Value: NewString(value)},
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
	missingMessage = "the custom fields under missing are required by the project and the call fills none of them"
	emptiedMessage = "the custom fields under missing are required by the project and the call empties them"
	invalidMessage = "the values under invalid are not values the fields they name can be given, and nothing was sent"
	hiddenMessage  = "the custom fields under invalid do not stand on the issue the call would file, and " +
		"nothing was sent"
	setAndClearedMessage = "the call writes a value into the custom field and empties it both, and one write " +
		"leaves it one way"
)

func (p projectMetadata) fault(code Code, message, key string, entries *Node) *Error {
	return p.response.fault(code, message, Pair{Key: projectKey, Value: NewString(p.code)}, Pair{Key: key, Value: entries})
}

func invalidEntries(hidden []hiddenField) *Node {
	entries := make([]*Node, 0, len(hidden))
	for _, field := range hidden {
		entries = append(entries, invalidEntry(field.name, field.value, field.reason))
	}
	return NewList(entries...)
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
