package youtrack

import (
	"context"
	"net/http"
	"slices"
)

// Issue holds its custom fields in the order of the project, each read by its type. Description is empty when the
// issue has none, and Links are the slots the server sends, one per link type and direction, empty ones too.
type Issue struct {
	ID          string
	IDReadable  string
	Summary     string
	Description string
	Project     Project
	Fields      []Field
	Links       []Link
}

type Link struct {
	Direction Direction
	Type      LinkType
	Issues    []IssueRef
}

// Direction is the end of a link the issue stands at; Both is either end of a link type that has no direction.
type Direction string

const (
	Outward Direction = outward
	Inward  Direction = inward
	Both    Direction = both
)

// A phrase the instance leaves null is empty.
type LinkType struct {
	Name           string
	SourceToTarget string
	TargetToSource string
}

type IssueRef struct {
	ID         string
	IDReadable string
}

type Project struct {
	ID        string
	ShortName string
	Name      string
}

type Field struct {
	Name          string
	LocalizedName string
	Type          FieldType
	Values        []Value
}

func (s *IssuesService) Get(ctx context.Context, id string) (*Issue, error) {
	return result(s.get(ctx, id))
}

// Field resolves the name by the name of a field, then by its translation, without regard to letter case.
func (i *Issue) Field(name string) (Field, bool) {
	return fieldNamed(name, i.Fields)
}

func (f Field) info() fieldInfo {
	return fieldInfo{name: f.Name, localizedName: f.LocalizedName, kind: f.Type}
}

func (f Field) Texts() []string {
	texts := make([]string, 0, len(f.Values))
	for _, v := range f.Values {
		texts = append(texts, v.Text)
	}
	return texts
}

func (s *IssuesService) get(ctx context.Context, id string) (*Issue, *Error) {
	id, fault := parseIssueID(id)
	if fault != nil {
		return nil, fault
	}
	c := s.client
	decoded, fault := c.request(ctx, issueSchema, issueRecordFields(), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetIssue(ctx, id, fields, nil)
	})
	if fault != nil {
		return nil, fault
	}
	return readIssue(decoded)
}

func issueRecordFields() []requestedField {
	named := requestedField{name: valueKey, children: []requestedField{{name: idKey}, {name: localizedNameKey}}}
	return []requestedField{
		{name: idKey},
		{name: idReadableKey},
		{name: summaryKey},
		{name: descriptionKey},
		{name: projectKey, children: []requestedField{{name: idKey}, {name: shortNameKey}, {name: nameKey}}},
		{name: customFieldsKey, children: withFields(customFieldsAsked(true), named)},
		{name: linksKey, children: []requestedField{
			{name: directionKey},
			{name: linkTypeKey, children: []requestedField{{name: nameKey}, {name: sourceToTarget}, {name: targetToSource}}},
			{name: issuesKey, children: []requestedField{{name: idKey}, {name: idReadableKey}}},
		}},
	}
}

func readIssue(a decodedResponse) (*Issue, *Error) {
	object := a.objects[0]
	id, isID := object[idKey].(string)
	readable, isReadable := object[idReadableKey].(string)
	summary, isSummary := object[summaryKey].(string)
	if !isID || !isReadable || !isSummary {
		return nil, a.invalid("the id, the readable id or the summary of the issue is not text")
	}
	description, isText := readOptionalText(object[descriptionKey])
	if !isText {
		return nil, a.invalid("the description of the issue is neither text nor null")
	}
	project, isProject := readIssueProject(object[projectKey])
	if !isProject {
		return nil, a.invalid("the project of the issue is not of the shape the specification gives it")
	}
	n := newConverter(a, wholeRecord)
	fields, fault := n.recordFields(object[customFieldsKey])
	if fault != nil {
		return nil, fault
	}
	links, fault := n.recordLinks(object[linksKey])
	if fault != nil {
		return nil, fault
	}
	return &Issue{ID: id, IDReadable: readable, Summary: summary, Description: description, Project: project,
		Fields: fields, Links: links}, nil
}

func readIssueProject(value any) (Project, bool) {
	object, isObject := value.(map[string]any)
	if !isObject {
		return Project{}, false
	}
	id, isID := object[idKey].(string)
	code, isCode := object[shortNameKey].(string)
	name, isName := object[nameKey].(string)
	if !isID || !isCode || !isName {
		return Project{}, false
	}
	return Project{ID: id, ShortName: code, Name: name}, true
}

func (n converter) recordFields(value any) ([]Field, *Error) {
	held, fault := n.readCustomFields(value)
	if fault != nil {
		return nil, fault
	}
	slices.SortStableFunc(held, inProjectOrder)
	fields := make([]Field, 0, len(held))
	for _, f := range held {
		field, fault := n.recordField(f)
		if fault != nil {
			return nil, fault
		}
		fields = append(fields, field)
	}
	return fields, nil
}

func (n converter) recordLinks(value any) ([]Link, *Error) {
	if value == nil {
		return nil, nil
	}
	if _, isList := value.([]any); !isList {
		return nil, n.response.invalid("the links of the issue are neither a JSON array nor null")
	}
	slots, fault := n.issueLinks(value)
	if fault != nil {
		return nil, fault
	}
	links := make([]Link, 0, len(slots))
	for _, slot := range slots {
		link, fault := n.recordLink(slot)
		if fault != nil {
			return nil, fault
		}
		links = append(links, link)
	}
	return links, nil
}

const brokenLinkSlot = "a link of the issue is not of the shape the specification gives it"

func (n converter) recordLink(slot map[string]any) (Link, *Error) {
	direction, isText := slot[directionKey].(string)
	kind, isObject := slot[linkTypeKey].(map[string]any)
	if !isText || !isObject {
		return Link{}, n.response.invalid(brokenLinkSlot)
	}
	name, isName := kind[nameKey].(string)
	forward, isForward := readOptionalText(kind[sourceToTarget])
	backward, isBackward := readOptionalText(kind[targetToSource])
	if !isName || !isForward || !isBackward {
		return Link{}, n.response.invalid(brokenLinkSlot)
	}
	targets, fault := n.targets(slot)
	if fault != nil {
		return Link{}, fault
	}
	issues := make([]IssueRef, 0, len(targets))
	for _, target := range targets {
		id, isID := target[idKey].(string)
		readable, isReadable := target[idReadableKey].(string)
		if !isID || !isReadable {
			return Link{}, n.response.invalid(brokenLinkSlot)
		}
		issues = append(issues, IssueRef{ID: id, IDReadable: readable})
	}
	kinds := LinkType{Name: name, SourceToTarget: forward, TargetToSource: backward}
	return Link{Direction: Direction(direction), Type: kinds, Issues: issues}, nil
}
