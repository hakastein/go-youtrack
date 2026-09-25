package youtrack

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strings"
)

// added and removed print as lists even where the server sends one value or null.
const ActivityListFields = "timestamp,author(login),category,field," +
	"added(id,idReadable,login,name,urls),removed(id,idReadable,login,name,urls)"

const (
	activitySchema    = "ActivityItem"
	categorySchema    = "ActivityCategory"
	customFilterField = "CustomFilterField"
	activitiesPlural  = "activities"
	categoryKey       = "category"
	addedKey          = "added"
	removedKey        = "removed"
	fieldKey          = "field"
	customFieldKey    = "customField"
	timestampKey      = "timestamp"
)

const customFieldCategory = "CustomFieldCategory"

const truncationProbe = 1

const activityLimit = math.MaxInt32 - truncationProbe

type activityCategory struct {
	id        string
	field     activityFieldKind
	valueForm valueForm
}

type activityFieldKind int

const (
	fieldRepeatsCategory activityFieldKind = iota
	fieldCustom
	fieldLinkPhrase
)

type valueForm struct {
	fromCustomField bool
	asObjects       bool
	bareKind        FieldType
}

func activityTable() []activityCategory {
	return []activityCategory{
		{id: "AttachmentsCategory"},
		{id: "CommentTextCategory"},
		{id: "CommentsCategory"},
		{id: customFieldCategory, field: fieldCustom},
		{id: "DescriptionCategory"},
		{id: "IssueCreatedCategory"},
		{id: "IssueResolvedCategory"},
		{id: "LinksCategory", field: fieldLinkPhrase},
		{id: "SummaryCategory"},
		{id: "TagsCategory"},
		{id: "VcsChangeCategory"},
		{id: "WorkItemCategory"},
	}
}

func ActivityCategories() []string {
	return categoryIDs(activityTable())
}

func categoryIDs(rows []activityCategory) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.id)
	}
	return ids
}

// ListActivitiesOptions: Fields is a fields= expression, empty for ActivityListFields and +x for them and x.
// Categories are names from ActivityCategories in any letter case, empty for all of them.
type ListActivitiesOptions struct {
	Fields     string
	Page       Page
	Categories []string
}

// Newest first; the total is null unless the page ends where the history does, and a limit is at most MaxInt32-1.
func (s *ActivitiesService) List(ctx context.Context, issue string, opts *ListActivitiesOptions) (*Node, error) {
	return result(s.list(ctx, issue, optionsOf(opts)))
}

func (s *ActivitiesService) list(ctx context.Context, issue string, opts ListActivitiesOptions) (*Node, *Error) {
	id, fault := parseIssueID(issue)
	if fault != nil {
		return nil, fault
	}
	page, fault := opts.Page.validate(activityLimit)
	if fault != nil {
		return nil, fault
	}
	categories, fault := resolveCategories(opts.Categories)
	if fault != nil {
		return nil, fault
	}
	c := s.client
	requested, fault := activityFields(c.spec, opts.Fields)
	if fault != nil {
		return nil, fault
	}
	return c.listActivities(ctx, id, requested, categories, page)
}

func resolveCategories(asked []string) ([]activityCategory, *Error) {
	table := activityTable()
	if len(asked) == 0 {
		return table, nil
	}
	chosen := make([]bool, len(table))
	var unresolved []string
	for _, name := range asked {
		if name == "" {
			return nil, &Error{Code: CodeBadUsage, Message: "the name of a category is empty"}
		}
		at := slices.IndexFunc(table, func(row activityCategory) bool { return strings.EqualFold(row.id, name) })
		switch {
		case at >= 0:
			chosen[at] = true
		case !slices.ContainsFunc(unresolved, func(seen string) bool { return strings.EqualFold(seen, name) }):
			unresolved = append(unresolved, name)
		}
	}
	if len(unresolved) > 0 {
		unknown := make([]*Node, 0, len(unresolved))
		for _, name := range unresolved {
			unknown = append(unknown, nearestEntry(categoryKey, name, nearestNames(name, categoryIDs(table))))
		}
		message := "the names under unknown are none of the activity categories a history is listed in"
		details := []Pair{{Key: "unknown", Value: NewList(unknown...)}}
		return nil, &Error{Code: CodeUnknownName, Message: message, Details: details}
	}
	categories := make([]activityCategory, 0, len(table))
	for at, row := range table {
		if chosen[at] {
			categories = append(categories, row)
		}
	}
	return categories, nil
}

func activityFields(spec *schemas, expression string) ([]requestedField, *Error) {
	written, requested, fault := fieldsOrDefault(expression, ActivityListFields, false)
	if fault != nil {
		return nil, fault
	}
	if fault := rejectBlockParts(spec, written, requested); fault != nil {
		return nil, fault
	}
	if fault := rejectUnknownValueNames(spec, written, requested); fault != nil {
		return nil, fault
	}
	return requested, nil
}

func activityValueSchemas() []string {
	return []string{"BundleElement"}
}

func rejectUnknownValueNames(spec *schemas, expression string, requested []requestedField) *Error {
	j := schemaResolver{schemas: spec}
	activity := schemaSet{schemas: spec.subtree(activitySchema)}
	var unknown []*Node
	for _, field := range requested {
		if field.name != addedKey && field.name != removedKey {
			continue
		}
		field.extraSchemas = activityValueSchemas()
		names := j.childSchemas(activity, &fieldNode{field: field}).names
		for _, child := range field.children {
			if !slices.Contains(names, child.name) {
				unknown = append(unknown, unknownEntry(fieldPath([]string{field.name}, child.name), nearestNames(child.name, names)))
			}
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	details := []Pair{
		{Key: "fields", Value: NewString(expression)},
		{Key: "unknown", Value: NewList(unknown...)},
	}
	message := "the names under unknown are declared by no value an activity holds"
	return &Error{Code: CodeUnknownName, Message: message, Details: details}
}

func rejectBlockParts(spec *schemas, expression string, requested []requestedField) *Error {
	blocks := []struct{ name, printedAs string }{
		{categoryKey, "the identifier YouTrack keeps the category under"},
		{fieldKey, "the name of what the change was of"},
	}
	var fault *Error
	for _, block := range blocks {
		fieldsNamed(spec, activitySchema, activitySchema, block.name, requested, nil, func(parents []string, field *requestedField) {
			if field.children == nil || fault != nil {
				return
			}
			message := fmt.Sprintf("fields %s: %s is printed as %s, so no name stands under it",
				quote(expression), fieldPath(parents, field.name), block.printedAs)
			fault = &Error{Code: CodeBadUsage, Message: message}
		})
	}
	return fault
}

func hasField(requested []requestedField, name string) bool {
	return slices.ContainsFunc(requested, func(field requestedField) bool { return field.name == name })
}

func activityFieldFields(named, values bool) []requestedField {
	asked := make([]requestedField, 0, 2)
	var held []requestedField
	if named {
		asked = append(asked, requestedField{name: nameKey})
		held = append(held, requestedField{name: nameKey})
	}
	if values {
		held = append(held, requestedField{name: fieldTypeKey, children: []requestedField{{name: valueTypeKey}}})
	}
	return append(asked, requestedField{name: customFieldKey, children: held})
}

func activityCount(page Page, received int) count {
	if page.mayHaveSkippedPastTheEnd(received) {
		return count{}
	}
	return counted(page.Skip + received)
}

func (c *Client) listActivities(ctx context.Context, id string, requested []requestedField, categories []activityCategory, page Page) (*Node, *Error) {
	own := []requestedField{
		{name: timestampKey},
		{name: categoryKey, children: []requestedField{{name: idKey}}},
	}
	values := hasField(requested, addedKey) || hasField(requested, removedKey)
	named := hasField(requested, fieldKey)
	if values || named {
		own = append(own, requestedField{name: fieldKey, normalized: true, children: activityFieldFields(named, values)})
	}
	for _, name := range []string{addedKey, removedKey} {
		if hasField(requested, name) {
			own = append(own, requestedField{name: name, extraSchemas: activityValueSchemas()})
		}
	}
	var phrases linkPhrases
	if named && slices.ContainsFunc(categories, func(row activityCategory) bool { return row.field == fieldLinkPhrase }) {
		var fault *Error
		phrases, fault = c.linkPhrases(ctx)
		if fault != nil {
			return nil, fault
		}
	}
	sent := withFields(requested, own...)
	probing := page.window()
	probing.top += truncationProbe
	decoded, fault := c.request(ctx, "[]"+activitySchema, sent, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetIssueActivities(ctx, id, strings.Join(categoryIDs(categories), ","), fields, probing)
	})
	if fault != nil {
		return nil, fault
	}
	received := decoded.objects
	if fault := moreThanAsked(activitiesPlural, page.Limit, int(probing.top), len(received)); fault != nil {
		return nil, fault
	}
	rows, fault := activityRows(decoded, categories, values)
	if fault != nil {
		return nil, fault
	}
	found, left := activityCount(page, len(received)), false
	if len(received) > page.Limit {
		received, rows, found, left = received[:page.Limit], rows[:page.Limit], count{}, true
	}
	printer := newActivityWriter(decoded, phrases)
	printed := make([]*Node, 0, len(received))
	for at, activity := range received {
		node, fault := printer.record(rows[at], requested, activity)
		if fault != nil {
			return nil, fault
		}
		printed = append(printed, node)
	}
	return truncatedListDocument(activitiesPlural, found, left, printed), nil
}

func activityRows(a decodedResponse, sent []activityCategory, printingValues bool) ([]activityCategory, *Error) {
	rows := make([]activityCategory, 0, len(a.objects))
	previous := int64(math.MaxInt64)
	for _, activity := range a.objects {
		moment, isInstant := parseInt64(activity[timestampKey])
		if !isInstant {
			return nil, shapeFailure(a.httpResponse, a.body, notAnInstant(timestampKey))
		}
		if moment > previous {
			message := "an activity arrived newer than the one before it, and the newest were asked for first"
			return nil, shapeFailure(a.httpResponse, a.body, message)
		}
		previous = moment
		named, reason := categoryID(activity[categoryKey])
		if reason != "" {
			return nil, shapeFailure(a.httpResponse, a.body, reason)
		}
		at := slices.IndexFunc(sent, func(row activityCategory) bool { return row.id == named })
		if at < 0 {
			message := fmt.Sprintf("an activity arrived of the category %s, which was not among the categories "+
				"the request asked for", quote(named))
			return nil, shapeFailure(a.httpResponse, a.body, message)
		}
		row := sent[at]
		if printingValues && row.field == fieldCustom {
			form, reason := customFieldValueForm(activity[fieldKey])
			if reason != "" {
				return nil, shapeFailure(a.httpResponse, a.body, reason)
			}
			row.valueForm = form
		}
		rows = append(rows, row)
	}
	return rows, nil
}

type activityWriter struct{ writer converter }

func newActivityWriter(a decodedResponse, phrases linkPhrases) activityWriter {
	return activityWriter{writer: converter{response: a, layout: inlineLayout, phrases: phrases}}
}

func (p activityWriter) record(row activityCategory, requested []requestedField, object map[string]any) (*Node, *Error) {
	n := p.writer
	n.row = row
	n.activityRoot = true
	return n.object(activitySchema, requested, object)
}

func customFilter(value any) (map[string]any, string) {
	field, isObject := value.(map[string]any)
	if !isObject {
		return nil, fmt.Sprintf("an activity of %s arrived standing for no field of the issue, and a change of "+
			"that category is a change of one", customFieldCategory)
	}
	if named, _ := field["$type"].(string); named != customFilterField {
		return nil, fmt.Sprintf("an activity of %s arrived for a field of %s, and a change of that category "+
			"stands for a %s", customFieldCategory, quote(named), customFilterField)
	}
	return field, ""
}

func customFieldValueForm(value any) (valueForm, string) {
	field, reason := customFilter(value)
	if reason != "" {
		return valueForm{}, reason
	}
	named, read := valueTypeOf(field)
	if !read {
		return valueForm{}, fmt.Sprintf("the custom field an activity of %s stands for arrived with no type of "+
			"value, which is what says how the change reads", customFieldCategory)
	}
	kind := FieldType{ValueType: ValueType(named)}
	if !kind.Known() {
		return valueForm{}, fmt.Sprintf("an activity of %s arrived for a field holding values of the type %s, "+
			"which is none of the custom-field types whose values the client reads", customFieldCategory, quote(named))
	}
	if kind.Named() {
		return valueForm{fromCustomField: true, asObjects: true}, ""
	}
	return valueForm{fromCustomField: true, bareKind: kind}, ""
}

func valueTypeOf(field map[string]any) (string, bool) {
	held, isObject := field[customFieldKey].(map[string]any)
	if !isObject {
		return "", false
	}
	kind, isObject := held[fieldTypeKey].(map[string]any)
	if !isObject {
		return "", false
	}
	named, isText := kind[valueTypeKey].(string)
	return named, isText
}

func categoryID(value any) (id, reason string) {
	category, isObject := value.(map[string]any)
	if !isObject {
		return "", "the category of an activity arrived as something other than a JSON object"
	}
	id, isText := category[idKey].(string)
	if !isText {
		return "", "the id of the category of an activity arrived as something other than text"
	}
	return id, ""
}

func (n converter) category() *Node {
	return NewString(n.row.id)
}

func (n converter) changedField(value any) (*Node, *Error) {
	if n.row.field == fieldRepeatsCategory {
		return NewNull(), nil
	}
	if n.row.field == fieldCustom {
		filter, reason := customFilter(value)
		if reason != "" {
			return nil, n.malformed(reason)
		}
		held, _ := filter[customFieldKey].(map[string]any)
		name, isText := held[nameKey].(string)
		if !isText {
			return nil, n.malformed(fmt.Sprintf("the custom field an activity of %s stands for arrived with no name "+
				"of the project's own", n.row.id))
		}
		return NewString(name), nil
	}
	filter, isObject := value.(map[string]any)
	if !isObject {
		return nil, n.malformed(fmt.Sprintf("an activity of %s arrived standing for no field of the issue, and a "+
			"change of that category is a change of one", n.row.id))
	}
	translatedPhrase, isText := filter[nameKey].(string)
	if !isText {
		return nil, n.malformed(fmt.Sprintf("an activity of %s arrived with no phrase of the link it stands for", n.row.id))
	}
	phrase, reason := n.phrases.phrase(translatedPhrase)
	if reason != "" {
		return nil, n.malformed(reason)
	}
	return NewString(phrase), nil
}

func (n converter) values(decl typeRef, field requestedField, value any) (*Node, *Error) {
	var items []any
	switch value := value.(type) {
	case nil:
	case []any:
		items = value
	default:
		items = []any{value}
	}
	n.activityRoot = false
	decl.list = false
	printed := make([]*Node, 0, len(items))
	for _, item := range items {
		node, fault := n.oneValue(decl, field, item)
		if fault != nil {
			return nil, fault
		}
		printed = append(printed, node)
	}
	return NewList(printed...), nil
}

func (n converter) oneValue(decl typeRef, field requestedField, value any) (*Node, *Error) {
	form := n.row.valueForm
	if !form.fromCustomField {
		return n.value(decl, field, value)
	}
	_, isObject := value.(map[string]any)
	switch {
	case form.asObjects && !isObject:
		return nil, n.malformed(fmt.Sprintf("a value under the %s of an activity of %s arrived as something other "+
			"than a JSON object, and the field it stands for holds values that carry names of their own",
			field.name, n.row.id))
	case !form.asObjects && isObject:
		return nil, n.malformed(fmt.Sprintf("a value under the %s of an activity of %s arrived as a JSON object, and "+
			"a change of the field it stands for carries its values bare, as numbers or texts", field.name, n.row.id))
	case isObject:
		return n.value(decl, field, value)
	}
	node, present, err := n.readValue(form.bareKind, keyedValue(form.bareKind, value))
	switch {
	case err != nil:
		return nil, n.malformed(fmt.Sprintf("a value under the %s of an activity of %s: %v", field.name, n.row.id, err))
	case !present:
		return nil, n.malformed(fmt.Sprintf("a value under the %s of an activity of %s is null", field.name, n.row.id))
	}
	return node, nil
}

func keyedValue(kind FieldType, key any) any {
	member := kind.ValueKey()
	if member == "" {
		return key
	}
	return map[string]any{member: key}
}
