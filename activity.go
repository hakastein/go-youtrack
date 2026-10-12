package youtrack

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strings"
)

const (
	activitySchema    = "ActivityItem"
	categorySchema    = "ActivityCategory"
	customFilterField = "CustomFilterField"
	activitiesPlural  = "activities"
	categoryKey       = "category"
	customFieldKey    = "customField"
	timestampKey      = "timestamp"
)

// ActivityCategory is a category of the history of an issue or of an article; only IssueActivityCategory and
// ArticleActivityCategory are one.
type ActivityCategory interface {
	activityCategory() (ownerKind, string)
}

// IssueActivityCategory is a category of the history of an issue.
type IssueActivityCategory string

// ArticleActivityCategory is a category of the history of an article.
type ArticleActivityCategory string

func (c IssueActivityCategory) activityCategory() (ownerKind, string) { return issueOwner, string(c) }

func (c ArticleActivityCategory) activityCategory() (ownerKind, string) {
	return articleOwner, string(c)
}

const (
	AttachmentsCategory   IssueActivityCategory = "AttachmentsCategory"
	CommentTextCategory   IssueActivityCategory = "CommentTextCategory"
	CommentsCategory      IssueActivityCategory = "CommentsCategory"
	CustomFieldCategory   IssueActivityCategory = "CustomFieldCategory"
	DescriptionCategory   IssueActivityCategory = "DescriptionCategory"
	IssueCreatedCategory  IssueActivityCategory = "IssueCreatedCategory"
	IssueResolvedCategory IssueActivityCategory = "IssueResolvedCategory"
	LinksCategory         IssueActivityCategory = "LinksCategory"
	SummaryCategory       IssueActivityCategory = "SummaryCategory"
	TagsCategory          IssueActivityCategory = "TagsCategory"
	VcsChangeCategory     IssueActivityCategory = "VcsChangeCategory"
	WorkItemCategory      IssueActivityCategory = "WorkItemCategory"
)

const (
	ArticleCreatedCategory     ArticleActivityCategory = "ArticleCreatedCategory"
	ArticleDeletedCategory     ArticleActivityCategory = "ArticleDeletedCategory"
	ArticleRestoredCategory    ArticleActivityCategory = "ArticleRestoredCategory"
	ArticleSummaryCategory     ArticleActivityCategory = "ArticleSummaryCategory"
	ArticleDescriptionCategory ArticleActivityCategory = "ArticleDescriptionCategory"
	ArticleProjectCategory     ArticleActivityCategory = "ArticleProjectCategory"
	ArticleAttachmentsCategory ArticleActivityCategory = "ArticleAttachmentsCategory"
	ArticleVisibilityCategory  ArticleActivityCategory = "ArticleVisibilityCategory"
	ArticleTagsCategory        ArticleActivityCategory = "ArticleTagsCategory"
	ArticleCommentsCategory    ArticleActivityCategory = "ArticleCommentsCategory"
)

const customFieldCategory = string(CustomFieldCategory)

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

func issueActivityTable() []activityCategory {
	return []activityCategory{
		{id: string(AttachmentsCategory)},
		{id: string(CommentTextCategory)},
		{id: string(CommentsCategory)},
		{id: customFieldCategory, field: fieldCustom},
		{id: string(DescriptionCategory)},
		{id: string(IssueCreatedCategory)},
		{id: string(IssueResolvedCategory)},
		{id: string(LinksCategory), field: fieldLinkPhrase},
		{id: string(SummaryCategory)},
		{id: string(TagsCategory)},
		{id: string(VcsChangeCategory)},
		{id: string(WorkItemCategory)},
	}
}

// The history of an article names no field of its own: field is null under each of these categories.
func articleActivityTable() []activityCategory {
	rows := make([]activityCategory, 0, len(ArticleActivityCategories()))
	for _, category := range ArticleActivityCategories() {
		rows = append(rows, activityCategory{id: string(category)})
	}
	return rows
}

func activityTable(kind ownerKind) []activityCategory {
	if kind == articleOwner {
		return articleActivityTable()
	}
	return issueActivityTable()
}

func IssueActivityCategories() []IssueActivityCategory {
	return []IssueActivityCategory{
		AttachmentsCategory, CommentTextCategory, CommentsCategory, CustomFieldCategory, DescriptionCategory,
		IssueCreatedCategory, IssueResolvedCategory, LinksCategory, SummaryCategory, TagsCategory, VcsChangeCategory,
		WorkItemCategory,
	}
}

func ArticleActivityCategories() []ArticleActivityCategory {
	return []ArticleActivityCategory{
		ArticleCreatedCategory, ArticleDeletedCategory, ArticleRestoredCategory, ArticleSummaryCategory,
		ArticleDescriptionCategory, ArticleProjectCategory, ArticleAttachmentsCategory, ArticleVisibilityCategory,
		ArticleTagsCategory, ArticleCommentsCategory,
	}
}

func categoryIDs(rows []activityCategory) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.id)
	}
	return ids
}

// ListActivitiesOptions: added and removed are lists even where the server sends one value or null. Categories are
// of the kind of the owner, in any letter case, empty for all of them.
type ListActivitiesOptions struct {
	Fields     string
	Page       Page
	Categories []ActivityCategory
}

// List runs newest first; the total is null unless the page ends where the history does, and a limit is at most
// MaxInt32-1.
func (s *ActivitiesService) List(ctx context.Context, owner Owner, opts *ListActivitiesOptions) (*Node, error) {
	return result(s.list(ctx, owner, optionsOf(opts)))
}

func (s *ActivitiesService) list(ctx context.Context, owner Owner, opts ListActivitiesOptions) (*Node, *Error) {
	c := s.client
	at, fault := ownerOf(owner)
	if fault != nil {
		return nil, fault
	}
	page, fault := opts.Page.validate(activityLimit)
	if fault != nil {
		return nil, fault
	}
	categories, fault := resolveCategories(at.kind, opts.Categories)
	if fault != nil {
		return nil, fault
	}
	requested, fault := c.parseFields(activitySchema, opts.Fields)
	if fault != nil {
		return nil, fault
	}
	return c.listActivities(ctx, at, requested, categories, page)
}

// A category of the other kind is unresolved like a misspelling: the history of the owner holds none of it.
func resolveCategories(kind ownerKind, asked []ActivityCategory) ([]activityCategory, *Error) {
	table := activityTable(kind)
	if len(asked) == 0 {
		return table, nil
	}
	chosen := make([]bool, len(table))
	var unresolved []string
	for _, category := range asked {
		if category == nil {
			return nil, &Error{Code: CodeBadUsage, Message: "a category is none at all"}
		}
		of, name := category.activityCategory()
		if name == "" {
			return nil, &Error{Code: CodeBadUsage, Message: "the name of a category is empty"}
		}
		at := slices.IndexFunc(table, func(row activityCategory) bool { return strings.EqualFold(row.id, name) })
		if of != kind {
			at = -1
		}
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
		message := fmt.Sprintf("the names under unknown are none of the categories the history of an %s is listed in", kind)
		details := []Pair{{Key: unknownKey, Value: NewList(unknown...)}}
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

func activityValueSchemas() []string {
	return []string{"BundleElement"}
}

func rejectUnknownValueNames(spec *schemas, root, expression string, requested []requestedField) *Error {
	resolver := schemaResolver{spec: spec}
	activity := schemaSet{schemas: spec.subtree(root)}
	var unknown []*Node
	for _, field := range requested {
		if field.name != addedKey && field.name != removedKey {
			continue
		}
		field.extraSchemas = activityValueSchemas()
		names := resolver.childSchemas(activity, &fieldNode{field: field}).names
		for _, child := range field.children {
			if !slices.Contains(names, child.name) {
				unknown = append(unknown, nearestEntry(fieldKey, fieldPath([]string{field.name}, child.name), nearestNames(child.name, names)))
			}
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	details := []Pair{
		{Key: fieldsKey, Value: NewString(expression)},
		{Key: unknownKey, Value: NewList(unknown...)},
	}
	message := "the names under unknown are declared by no value an activity holds"
	return &Error{Code: CodeUnknownName, Message: message, Details: details}
}

func rejectBlockParts(spec *schemas, root, expression string, requested []requestedField) *Error {
	blocks := []struct{ name, printedAs string }{
		{categoryKey, "the identifier YouTrack keeps the category under"},
		{fieldKey, "the name of what the change was of"},
	}
	var fault *Error
	for _, block := range blocks {
		fieldsNamed(spec, root, activitySchema, block.name, requested, func(parents []string, field *requestedField) {
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

func (c *Client) listActivities(ctx context.Context, at owner, requested []requestedField, categories []activityCategory, page Page) (*Node, *Error) {
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
		sent := strings.Join(categoryIDs(categories), ",")
		if at.kind == articleOwner {
			return c.apiGetArticleActivities(ctx, at.id, sent, fields, probing)
		}
		return c.apiGetIssueActivities(ctx, at.id, sent, fields, probing)
	})
	if fault != nil {
		return nil, fault
	}
	received := decoded.objects
	if fault := moreThanAsked(decoded, activitiesPlural, page.Limit, int(probing.top)); fault != nil {
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
	n := converter{response: decoded, record: listRecord, phrases: phrases, activityRoot: true}
	printed := make([]*Node, 0, len(received))
	for at, activity := range received {
		n.row = rows[at]
		node, fault := n.object(activitySchema, requested, activity)
		if fault != nil {
			return nil, fault
		}
		printed = append(printed, node)
	}
	return listDocument(activitiesPlural, found, truncation{left: left, known: true}, printed), nil
}

func activityRows(a decodedResponse, sent []activityCategory, printingValues bool) ([]activityCategory, *Error) {
	rows := make([]activityCategory, 0, len(a.objects))
	previous := int64(math.MaxInt64)
	for _, activity := range a.objects {
		moment, isInstant := parseInt64(activity[timestampKey])
		if !isInstant {
			return nil, a.invalid(notAnInstant(timestampKey))
		}
		if moment > previous {
			message := "an activity arrived newer than the one before it, and the newest were asked for first"
			return nil, a.invalid(message)
		}
		previous = moment
		named, reason := categoryID(activity[categoryKey])
		if reason != "" {
			return nil, a.invalid(reason)
		}
		at := slices.IndexFunc(sent, func(row activityCategory) bool { return row.id == named })
		if at < 0 {
			message := fmt.Sprintf("an activity arrived of the category %s, which was not among the categories "+
				"the request asked for", quote(named))
			return nil, a.invalid(message)
		}
		row := sent[at]
		if printingValues && row.field == fieldCustom {
			form, reason := customFieldValueForm(activity[fieldKey])
			if reason != "" {
				return nil, a.invalid(reason)
			}
			row.valueForm = form
		}
		rows = append(rows, row)
	}
	return rows, nil
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
			return nil, n.response.invalid(reason)
		}
		held, _ := filter[customFieldKey].(map[string]any)
		name, isText := held[nameKey].(string)
		if !isText {
			return nil, n.response.invalid(fmt.Sprintf("the custom field an activity of %s stands for arrived with no name "+
				"of the project's own", n.row.id))
		}
		return NewString(name), nil
	}
	filter, isObject := value.(map[string]any)
	if !isObject {
		return nil, n.response.invalid(fmt.Sprintf("an activity of %s arrived standing for no field of the issue, and a "+
			"change of that category is a change of one", n.row.id))
	}
	translatedPhrase, isText := filter[nameKey].(string)
	if !isText {
		return nil, n.response.invalid(fmt.Sprintf("an activity of %s arrived with no phrase of the link it stands for", n.row.id))
	}
	phrase, reason := n.phrases.phrase(translatedPhrase)
	if reason != "" {
		return nil, n.response.invalid(reason)
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
		return nil, n.response.invalid(fmt.Sprintf("a value under the %s of an activity of %s arrived as something other "+
			"than a JSON object, and the field it stands for holds values that carry names of their own",
			field.name, n.row.id))
	case !form.asObjects && isObject:
		return nil, n.response.invalid(fmt.Sprintf("a value under the %s of an activity of %s arrived as a JSON object, and "+
			"a change of the field it stands for carries its values bare, as numbers or texts", field.name, n.row.id))
	case isObject:
		return n.value(decl, field, value)
	}
	node, present, reason := n.readValue(form.bareKind, keyedValue(form.bareKind, value))
	switch {
	case reason != "":
		return nil, n.response.invalid(fmt.Sprintf("a value under the %s of an activity of %s: %s", field.name, n.row.id, reason))
	case !present:
		return nil, n.response.invalid(fmt.Sprintf("a value under the %s of an activity of %s is null", field.name, n.row.id))
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
