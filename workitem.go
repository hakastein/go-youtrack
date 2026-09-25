package youtrack

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

const (
	WorkItemListFields  = "id,duration,type(name),attributes,author(login),date,text"
	WorkItemWriteFields = "id,duration,type(name),attributes,author(login),date,issue(idReadable,customFields),text"
)

const (
	workItemSchema          = "IssueWorkItem"
	workItemsPlural         = "workItems"
	workItemsListing        = "[]" + workItemSchema
	durationKey             = "duration"
	dateKey                 = "date"
	typeKey                 = "type"
	workItemNoun            = "work item"
	workItemOwnerNoun       = "the issue"
	pluginsKey              = "plugins"
	timeTrackingSettingsKey = "timeTrackingSettings"
	workItemTypesKey        = "workItemTypes"
)

// An empty Fields means WorkItemListFields; +x adds x to them.
type ListWorkItemsOptions struct {
	Fields string
	Page   Page
}

// Oldest first.
func (s *WorkItemsService) List(ctx context.Context, issue string, opts *ListWorkItemsOptions) (*Node, error) {
	return result(s.list(ctx, issue, optionsOf(opts)))
}

// Create logs time as the owner of the token. A type of work or an attribute is resolved among the time tracking
// settings of the project, read before the write.
func (s *WorkItemsService) Create(ctx context.Context, issue string, in *WorkItemInput, opts *WriteOptions) (*Node, error) {
	return result(s.create(ctx, issue, optionsOf(in), optionsOf(opts)))
}

// The work item is addressed by its internal id, as 7-1.
func (s *WorkItemsService) Update(ctx context.Context, issue, id string, in *WorkItemUpdate, opts *WriteOptions) (*Node, error) {
	return result(s.update(ctx, issue, id, optionsOf(in), optionsOf(opts)))
}

// Delete answers with the work item as read just before the deletion, which goes to the issue the read named.
func (s *WorkItemsService) Delete(ctx context.Context, issue, id string) (*Node, error) {
	return result(s.delete(ctx, issue, id))
}

func (s *WorkItemsService) list(ctx context.Context, issue string, opts ListWorkItemsOptions) (*Node, *Error) {
	id, fault := parseIssueID(issue)
	if fault != nil {
		return nil, fault
	}
	page, fault := opts.Page.parse()
	if fault != nil {
		return nil, fault
	}
	c := s.client
	requested, fault := workItemFields(c.spec, opts.Fields, WorkItemListFields)
	if fault != nil {
		return nil, fault
	}
	ask := func(ctx context.Context, fields string, w window) (*http.Response, error) {
		return c.apiGetIssueWorkItems(ctx, id, fields, w)
	}
	selection := c.newList(workItemsPlural, workItemsListing, requested, workItemRequestFields(c.spec, requested), page, ask)
	return selection.fetch(ctx)
}

func (s *WorkItemsService) create(ctx context.Context, issue string, in WorkItemInput, opts WriteOptions) (*Node, *Error) {
	id, fault := parseIssueID(issue)
	if fault != nil {
		return nil, fault
	}
	written, fault := parseWorkItemCreate(in)
	if fault != nil {
		return nil, fault
	}
	c := s.client
	requested, fault := workItemFields(c.spec, opts.Fields, WorkItemWriteFields)
	if fault != nil {
		return nil, fault
	}
	at, workType, attributes, fault := c.resolveWorkItemSettings(ctx, id, written.workType, written.attributes)
	if fault != nil {
		return nil, fault
	}
	filed := workItemCreate{input: written, workType: workType, attributes: attributes}
	asked := withFields(requested, filed.verifyFields()...)
	issueBlocks(c.spec, composedWorkItem(), asked)
	body := filed.body()
	return writeAs(ctx, c, workItemSchema, asked, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiCreateIssueWorkItem(ctx, at, body, fields)
	}, filed.verify, writeResultNode(requested))
}

func (c *Client) resolveWorkItemSettings(ctx context.Context, id string, named *string, written []AttributeWrite) (string, *resolvedWorkType, []resolvedAttribute, *Error) {
	withAttributes := len(written) > 0
	if named == nil && !withAttributes {
		return id, nil, nil, nil
	}
	readable, project, fault := c.readWorkItemTypes(ctx, id, withAttributes)
	if fault != nil {
		return "", nil, nil, fault
	}
	var workType *resolvedWorkType
	if named != nil {
		resolved, fault := project.resolve(*named)
		if fault != nil {
			return "", nil, nil, fault
		}
		workType = &resolved
	}
	attributes, fault := project.resolveAttributes(written)
	if fault != nil {
		return "", nil, nil, fault
	}
	return readable.String(), workType, attributes, nil
}

func (s *WorkItemsService) update(ctx context.Context, issue, item string, in WorkItemUpdate, opts WriteOptions) (*Node, *Error) {
	id, fault := parseIssueID(issue)
	if fault != nil {
		return nil, fault
	}
	at, fault := parseChildID(workItemNoun, workItemOwnerNoun, item)
	if fault != nil {
		return nil, fault
	}
	written, fault := parseWorkItemUpdate(in)
	if fault != nil {
		return nil, fault
	}
	c := s.client
	requested, fault := workItemFields(c.spec, opts.Fields, WorkItemWriteFields)
	if fault != nil {
		return nil, fault
	}
	owner, workType, attributes, fault := c.resolveWorkItemSettings(ctx, id, written.workType, written.attributes)
	if fault != nil {
		return nil, fault
	}
	changed := workItemUpdate{input: written, issue: owner, at: at, workType: workType, attributes: attributes}
	asked := withFields(requested, changed.verifyFields()...)
	issueBlocks(c.spec, composedWorkItem(), asked)
	body := changed.body()
	return writeAs(ctx, c, workItemSchema, asked, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiUpdateIssueWorkItem(ctx, owner, at, body, fields)
	}, changed.verify, writeResultNode(requested))
}

func removedWorkItemFields() []requestedField {
	return []requestedField{
		{name: idKey},
		{name: issueOwner.String(), children: []requestedField{{name: idReadableKey}}},
	}
}

func (s *WorkItemsService) delete(ctx context.Context, issue, item string) (*Node, *Error) {
	id, fault := parseIssueID(issue)
	if fault != nil {
		return nil, fault
	}
	at, fault := parseChildID(workItemNoun, workItemOwnerNoun, item)
	if fault != nil {
		return nil, fault
	}
	c := s.client
	requested := removedWorkItemFields()
	a, fault := c.request(ctx, workItemSchema, requested, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetIssueWorkItem(ctx, id, at, fields)
	})
	if fault != nil {
		return nil, fault
	}
	owner, fault := owningIssueID(a)
	if fault != nil {
		return nil, fault
	}
	known, fault := workItemID(a)
	if fault != nil {
		return nil, fault
	}
	if fault := writeEmpty(ctx, func(ctx context.Context) (*http.Response, error) {
		return c.apiDeleteIssueWorkItem(ctx, owner, known)
	}); fault != nil {
		return nil, fault
	}
	return objectNode(a, requested, a.objects[0], nil)
}

func owningIssueID(a decodedResponse) (readableID, *Error) {
	issue, isObject := a.objects[0][issueOwner.String()].(map[string]any)
	if !isObject {
		return readableID{}, shapeFailure(a.httpResponse, a.body, "the issue the work item hangs from is not a JSON object")
	}
	return readableIDAt(a, issue, issueOwner, "a removal")
}

func workItemID(a decodedResponse) (childID, *Error) {
	id, isText := a.objects[0][idKey].(string)
	if !isText {
		return childID{}, shapeFailure(a.httpResponse, a.body, "the id of the work item arrived as something other than a string")
	}
	known, fault := parseChildID(workItemNoun, workItemOwnerNoun, id)
	if fault != nil {
		message := fmt.Sprintf("the work item arrived with %s for an id, and a removal is addressed by the id the "+
			"server gave", quote(id))
		return childID{}, shapeFailure(a.httpResponse, a.body, message)
	}
	return known, nil
}

type projectWorkItemTypes struct {
	project    string
	types      []workItemType
	attributes []projectAttribute
	response   decodedResponse
}

type workItemType struct {
	id   string
	name string
}

func workItemTypesFields(withAttributes bool) []requestedField {
	settings := []requestedField{{name: workItemTypesKey, children: []requestedField{{name: idKey}, {name: nameKey}}}}
	if withAttributes {
		settings = append(settings, requestedField{name: attributesKey, children: []requestedField{
			{name: idKey},
			{name: nameKey},
			{name: "values", children: []requestedField{{name: idKey}, {name: nameKey}}},
		}})
	}
	return []requestedField{
		{name: idReadableKey},
		{name: projectKey, children: []requestedField{
			{name: shortNameKey},
			{name: pluginsKey, children: []requestedField{
				{name: timeTrackingSettingsKey, children: settings},
			}},
		}},
	}
}

func (c *Client) readWorkItemTypes(ctx context.Context, id string, withAttributes bool) (readableID, projectWorkItemTypes, *Error) {
	a, fault := c.request(ctx, issueSchema, workItemTypesFields(withAttributes), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetIssue(ctx, id, fields, nil)
	})
	if fault != nil {
		return readableID{}, projectWorkItemTypes{}, fault
	}
	readable, fault := readableIDOf(a, issueOwner, "a creation")
	if fault != nil {
		return readableID{}, projectWorkItemTypes{}, fault
	}
	found, fault := workItemTypesOf(a, withAttributes)
	if fault != nil {
		return readableID{}, projectWorkItemTypes{}, fault
	}
	return readable, found, nil
}

func workItemTypesOf(a decodedResponse, withAttributes bool) (projectWorkItemTypes, *Error) {
	project, isObject := a.objects[0][projectKey].(map[string]any)
	if !isObject {
		return projectWorkItemTypes{}, shapeFailure(a.httpResponse, a.body, "the project of the issue is not a JSON object")
	}
	code, isText := project[shortNameKey].(string)
	if !isText {
		return projectWorkItemTypes{}, shapeFailure(a.httpResponse, a.body, "the short name of the project is not text")
	}
	settings, fault := timeTrackingSettingsOf(a, project)
	if fault != nil {
		return projectWorkItemTypes{}, fault
	}
	items, isList := settings[workItemTypesKey].([]any)
	if !isList {
		return projectWorkItemTypes{}, shapeFailure(a.httpResponse, a.body, "the types of work of the project are not a JSON array")
	}
	types := make([]workItemType, 0, len(items))
	for _, item := range items {
		object, isObject := item.(map[string]any)
		if !isObject {
			return projectWorkItemTypes{}, shapeFailure(a.httpResponse, a.body, brokenWorkItemType)
		}
		id, isText := object[idKey].(string)
		name, isNamed := object[nameKey].(string)
		if !isText || !isNamed {
			return projectWorkItemTypes{}, shapeFailure(a.httpResponse, a.body, brokenWorkItemType)
		}
		types = append(types, workItemType{id: id, name: name})
	}
	found := projectWorkItemTypes{project: code, types: types, response: a}
	if withAttributes {
		if found.attributes, fault = attributesOf(a, settings); fault != nil {
			return projectWorkItemTypes{}, fault
		}
	}
	return found, nil
}

func timeTrackingSettingsOf(a decodedResponse, project map[string]any) (map[string]any, *Error) {
	plugins, isObject := project[pluginsKey].(map[string]any)
	if !isObject {
		return nil, shapeFailure(a.httpResponse, a.body, "the plugins of the project are not a JSON object")
	}
	settings, isObject := plugins[timeTrackingSettingsKey].(map[string]any)
	if !isObject {
		return nil, shapeFailure(a.httpResponse, a.body, "the time tracking settings of the project are not a JSON object")
	}
	return settings, nil
}

const brokenWorkItemType = "the id or the name of a type of work of the project is not text"

func (p projectWorkItemTypes) resolve(name string) (resolvedWorkType, *Error) {
	catalogue := p.catalogue()
	at, found := matchName(name, catalogue)
	if !found {
		return resolvedWorkType{}, p.fault(name, catalogue)
	}
	return resolvedWorkType{id: p.types[at].id, name: name}, nil
}

func (p projectWorkItemTypes) catalogue() []fieldInfo {
	catalogue := make([]fieldInfo, 0, len(p.types))
	for _, found := range p.types {
		catalogue = append(catalogue, fieldInfo{name: found.name})
	}
	return catalogue
}

func (p projectWorkItemTypes) fault(name string, catalogue []fieldInfo) *Error {
	entry := NewMap(
		Pair{Key: typeKey, Value: NewString(name)},
		Pair{Key: "nearest", Value: NewList(names(nearestNamed(name, catalogue))...)})
	message := "the name under unknown is not one type of work the project writes work items against"
	sent := requestDetail(p.response.httpResponse.Request.Method, p.response.httpResponse.Request.URL.Redacted())
	return unknownNames(sent, Pair{Key: projectKey, Value: NewString(p.project)}, "unknown", message, []*Node{entry})
}

func workItemRequestFields(spec *schemas, requested []requestedField) []requestedField {
	asked := cloneFields(requested)
	issueBlocks(spec, composedWorkItem(), asked)
	return asked
}

func workItemFields(spec *schemas, expression string, defaults string) ([]requestedField, *Error) {
	written, requested, fault := fieldsOrDefault(expression, defaults, false)
	if fault != nil {
		return nil, fault
	}
	return requested, rejectIssueBlocks(spec, composedWorkItem(), written, requested)
}

type minutesBody struct {
	Minutes int64 `json:"minutes"`
}

func formatDateTime(milliseconds int64) string {
	return time.UnixMilli(milliseconds).UTC().Format(time.RFC3339Nano)
}
