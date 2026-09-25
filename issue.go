package youtrack

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

// Issue is an issue with its custom fields read by their types. Description is empty when the issue has none.
// Links are the slots the server sends, one per link type and direction, empty ones too. Tree is the answer of
// the server as it came, with what the caller's fields expression asked for beyond the members here.
type Issue struct {
	ID          string
	IDReadable  string
	Summary     string
	Description string
	Project     Project
	Fields      []Field
	Links       []Link
	Tree        map[string]any
}

// Link is one slot of the links of an issue: the issues at the other end of one link type in one direction.
type Link struct {
	Direction Direction
	Type      LinkType
	Issues    []IssueRef
}

// Direction is the end of a link the issue stands at.
type Direction string

const (
	Outward Direction = "OUTWARD"
	Inward  Direction = "INWARD"
	Both    Direction = "BOTH"
)

// LinkType names a type of link and its two phrases; a phrase the instance does not translate is empty.
type LinkType struct {
	Name           string
	SourceToTarget string
	TargetToSource string
}

// IssueRef names an issue at the other end of a link.
type IssueRef struct {
	ID         string
	IDReadable string
}

// Project names the project an issue stands in.
type Project struct {
	ID        string
	ShortName string
	Name      string
}

// Field is a custom field on an issue with the values it holds, none when it holds nothing.
type Field struct {
	Name          string
	LocalizedName string
	Type          FieldType
	Values        []Value
}

// Value is one value of a custom field. Text is the value key of the field's type: the name of a bundle
// value or group, the login of a user, a period as PT1H30M, a day as 2026-09-16, a moment in UTC, a number in
// its shortest decimal form, or the string or text itself. ID is the internal id of a bundle value, user or
// group, and LocalizedName the translation the interface shows for a bundle value; both are empty otherwise.
type Value struct {
	ID            string
	Text          string
	LocalizedName string
}

// Field is the custom field the name resolves to, by name and then by translation, without regard to letter case.
func (i *Issue) Field(name string) (Field, bool) {
	var byName, byTranslation []int
	for at, f := range i.Fields {
		switch {
		case strings.EqualFold(name, f.Name):
			byName = append(byName, at)
		case f.LocalizedName != "" && strings.EqualFold(name, f.LocalizedName):
			byTranslation = append(byTranslation, at)
		}
	}
	switch {
	case len(byName) > 0:
		return i.Fields[byName[0]], true
	case len(byTranslation) > 0:
		return i.Fields[byTranslation[0]], true
	}
	return Field{}, false
}

// Texts are the value keys of the values the field holds.
func (f Field) Texts() []string {
	texts := make([]string, 0, len(f.Values))
	for _, v := range f.Values {
		texts = append(texts, v.Text)
	}
	return texts
}

const (
	idReadableKey   = "idReadable"
	summaryKey      = "summary"
	descriptionKey  = "description"
	projectKey      = "project"
	customFieldsKey = "customFields"
	bindingKey      = "projectCustomField"
	ordinalKey      = "ordinal"
	valueKey        = "value"
	linksKey        = "links"
)

func issueFields() []field {
	return []field{
		{name: idKey},
		{name: idReadableKey},
		{name: summaryKey},
		{name: descriptionKey},
		{name: projectKey, children: []field{{name: idKey}, {name: "shortName"}, {name: nameKey}}},
		{name: customFieldsKey, children: []field{
			{name: nameKey},
			{name: valueKey, children: valueMembers()},
			{name: bindingKey, children: []field{
				{name: idKey},
				{name: ordinalKey},
				{name: "field", children: []field{
					{name: localizedNameKey},
					{name: "fieldType", children: []field{{name: "valueType"}, {name: "isMultiValue"}}},
				}},
			}},
		}},
		{name: linksKey, children: []field{
			{name: "direction"},
			{name: "linkType", children: []field{{name: nameKey}, {name: "sourceToTarget"}, {name: "targetToSource"}}},
			{name: "issues", children: []field{{name: idKey}, {name: idReadableKey}}},
		}},
	}
}

// Issue reads the issue by its readable id. fields is a fields= expression of the YouTrack REST API for
// members beyond the ones every Issue carries, or empty; what it names comes back in Tree.
func (c *Client) Issue(ctx context.Context, id, fields string) (*Issue, error) {
	id, err := parseIssueID(id)
	if err != nil {
		return nil, err
	}
	asked := issueFields()
	if fields != "" {
		extra, err := parseFields(fields)
		if err != nil {
			return nil, err
		}
		asked = withFields(asked, extra...)
	}
	a, err := c.read(ctx, func(ctx context.Context) (*http.Response, error) {
		return c.apiGetIssue(ctx, id, formatFields(asked))
	})
	if err != nil {
		return nil, err
	}
	return readIssue(a)
}

func readIssue(a answer) (*Issue, error) {
	object, err := a.object()
	if err != nil {
		return nil, err
	}
	id, isID := object[idKey].(string)
	idReadable, isReadable := object[idReadableKey].(string)
	summary, isSummary := object[summaryKey].(string)
	if !isID || !isReadable || !isSummary {
		return nil, a.invalid("the id, the readable id or the summary of the issue is not text")
	}
	description, isText := readLocalizedName(object[descriptionKey])
	if !isText {
		return nil, a.invalid("the description of the issue is neither text nor null")
	}
	project, ok := readProject(object[projectKey])
	if !ok {
		return nil, a.invalid("the project of the issue is not of the shape the specification gives it")
	}
	fields, err := readCustomFields(a, object[customFieldsKey])
	if err != nil {
		return nil, err
	}
	links, err := readLinks(a, object[linksKey])
	if err != nil {
		return nil, err
	}
	return &Issue{ID: id, IDReadable: idReadable, Summary: summary, Description: description, Project: project,
		Fields: fields, Links: links, Tree: object}, nil
}

// A slot the server left null would hide a link, so it is refused rather than skipped.
func readLinks(a answer, value any) ([]Link, error) {
	if value == nil {
		return nil, nil
	}
	items, isList := value.([]any)
	if !isList {
		return nil, a.invalid("the links of the issue are neither a JSON array nor null")
	}
	links := make([]Link, 0, len(items))
	for _, item := range items {
		link, ok := readLink(item)
		if !ok {
			return nil, a.invalid("a link of the issue is not of the shape the specification gives it")
		}
		links = append(links, link)
	}
	return links, nil
}

func readLink(item any) (Link, bool) {
	object, isObject := item.(map[string]any)
	if !isObject {
		return Link{}, false
	}
	direction, isText := object["direction"].(string)
	linkType, isObject := object["linkType"].(map[string]any)
	if !isText || !isObject {
		return Link{}, false
	}
	name, isName := linkType[nameKey].(string)
	outward, isOutward := readLocalizedName(linkType["sourceToTarget"])
	inward, isInward := readLocalizedName(linkType["targetToSource"])
	if !isName || !isOutward || !isInward {
		return Link{}, false
	}
	held, isList := object["issues"].([]any)
	if !isList {
		return Link{}, false
	}
	issues := make([]IssueRef, 0, len(held))
	for _, ref := range held {
		refObject, isObject := ref.(map[string]any)
		if !isObject {
			return Link{}, false
		}
		id, isID := refObject[idKey].(string)
		idReadable, isReadable := refObject[idReadableKey].(string)
		if !isID || !isReadable {
			return Link{}, false
		}
		issues = append(issues, IssueRef{ID: id, IDReadable: idReadable})
	}
	return Link{Direction: Direction(direction), Type: LinkType{Name: name, SourceToTarget: outward, TargetToSource: inward}, Issues: issues}, true
}

func readProject(value any) (Project, bool) {
	object, isObject := value.(map[string]any)
	if !isObject {
		return Project{}, false
	}
	id, isID := object[idKey].(string)
	shortName, isCode := object["shortName"].(string)
	name, isName := object[nameKey].(string)
	if !isID || !isCode || !isName {
		return Project{}, false
	}
	return Project{ID: id, ShortName: shortName, Name: name}, true
}

type placedIssueField struct {
	field   Field
	ordinal int64
	binding string
}

func readCustomFields(a answer, value any) ([]Field, error) {
	items, isList := value.([]any)
	if !isList {
		return nil, a.invalid("the custom fields of the issue arrived as something other than an array")
	}
	placed := make([]placedIssueField, 0, len(items))
	named := make(map[string]bool, len(items))
	for _, item := range items {
		p, err := readCustomField(a, item)
		if err != nil {
			return nil, err
		}
		if named[p.field.Name] {
			return nil, a.invalid(fmt.Sprintf("two custom fields of the issue are named %s", quote(p.field.Name)))
		}
		named[p.field.Name] = true
		placed = append(placed, p)
	}
	slices.SortStableFunc(placed, inProjectOrder)
	fields := make([]Field, 0, len(placed))
	for _, p := range placed {
		fields = append(fields, p.field)
	}
	return fields, nil
}

// Fields bound through REST share the ordinal 0, and among them the project's order is that of the binding ids.
func inProjectOrder(x, y placedIssueField) int {
	return cmp.Or(cmp.Compare(x.ordinal, y.ordinal), compareBindings(x.binding, y.binding))
}

func compareBindings(x, y string) int {
	first, isNumbered := bindingNumbers(x)
	second, alsoNumbered := bindingNumbers(y)
	if !isNumbered || !alsoNumbered {
		return strings.Compare(x, y)
	}
	return cmp.Or(cmp.Compare(first[0], second[0]), cmp.Compare(first[1], second[1]))
}

func bindingNumbers(id string) ([2]int, bool) {
	before, after, dashed := strings.Cut(id, "-")
	if !dashed {
		return [2]int{}, false
	}
	first, firstErr := strconv.Atoi(before)
	second, secondErr := strconv.Atoi(after)
	if firstErr != nil || secondErr != nil {
		return [2]int{}, false
	}
	return [2]int{first, second}, true
}

func readCustomField(a answer, item any) (placedIssueField, error) {
	object, isObject := item.(map[string]any)
	if !isObject {
		return placedIssueField{}, a.invalid("a custom field of the issue is not a JSON object")
	}
	name, isText := object[nameKey].(string)
	if !isText {
		return placedIssueField{}, a.invalid("the name of a custom field of the issue is not text")
	}
	binding, isObject := object[bindingKey].(map[string]any)
	if !isObject {
		return placedIssueField{}, a.invalid(brokenIssueBinding(name))
	}
	bindingID, isText := binding[idKey].(string)
	if !isText {
		return placedIssueField{}, a.invalid(brokenIssueBinding(name))
	}
	ordinal, isWhole := parseInt64(binding[ordinalKey])
	if !isWhole {
		message := fmt.Sprintf("the place of the custom field %s among the fields of the project is no whole number", quote(name))
		return placedIssueField{}, a.invalid(message)
	}
	declared, isObject := binding["field"].(map[string]any)
	if !isObject {
		return placedIssueField{}, a.invalid(brokenIssueBinding(name))
	}
	fieldType, ok := readFieldType(declared["fieldType"])
	if !ok {
		return placedIssueField{}, a.invalid(brokenIssueBinding(name))
	}
	localized, ok := readLocalizedName(declared[localizedNameKey])
	if !ok {
		return placedIssueField{}, a.invalid(brokenIssueBinding(name))
	}
	if !fieldType.Known() {
		return placedIssueField{}, a.invalid(unmodelled(fieldType))
	}
	values, err := readValues(a, name, fieldType, object[valueKey])
	if err != nil {
		return placedIssueField{}, err
	}
	f := Field{Name: name, LocalizedName: localized, Type: fieldType, Values: values}
	return placedIssueField{field: f, ordinal: ordinal, binding: bindingID}, nil
}

func brokenIssueBinding(name string) string {
	return fmt.Sprintf("the project's field the custom field %s stands for is not of the shape the specification gives it", quote(name))
}

func readValues(a answer, name string, fieldType FieldType, value any) ([]Value, error) {
	held, isList := value.([]any)
	switch {
	case value == nil:
		return nil, nil
	case isList && !fieldType.Multi:
		return nil, a.invalid(fmt.Sprintf("the custom field %s holds one value by its type and arrived as a list", quote(name)))
	case !isList && fieldType.Multi:
		return nil, a.invalid(fmt.Sprintf("the custom field %s holds more than one value by its type and arrived as something other than a list", quote(name)))
	case !isList:
		held = []any{value}
	}
	values := make([]Value, 0, len(held))
	for _, item := range held {
		v, present, err := fieldType.ReadValue(item)
		if err != nil {
			return nil, a.invalid(fmt.Sprintf("the custom field %s: %s", quote(name), err))
		}
		if present {
			values = append(values, v)
		}
	}
	if len(values) == 0 {
		return nil, nil
	}
	return values, nil
}
