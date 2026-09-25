package youtrack

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

const WorkItemListFields = "id,duration,type(name),attributes,author(login),date,text"

const WorkItemWriteFields = "id,duration,type(name),attributes,author(login),date,issue(idReadable,customFields),text"

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

func ListWorkItems(id string, expression string, page Page) (Call, *Error) {
	id, fault := parseIssueID(id)
	if fault != nil {
		return nil, fault
	}
	page, fault = page.parse()
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	requested, fault := workItemFields(spec, expression, WorkItemListFields)
	if fault != nil {
		return nil, fault
	}
	return func(ctx context.Context, c *Client) (*Node, *Error) {
		return c.listWorkItems(ctx, spec, id, requested, page)
	}, nil
}

func CreateWorkItem(id, spent string, day, text, workType *string, attributes []string, expression string) (Call, *Error) {
	id, fault := parseIssueID(id)
	if fault != nil {
		return nil, fault
	}
	written, fault := parseWorkItemCreate(spent, day, text, workType, attributes)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	requested, fault := workItemFields(spec, expression, WorkItemWriteFields)
	if fault != nil {
		return nil, fault
	}
	return func(ctx context.Context, c *Client) (*Node, *Error) {
		return c.createWorkItem(ctx, spec, id, written, workType, requested)
	}, nil
}

func (c *Client) createWorkItem(ctx context.Context, spec *schemas, id string, written workItemCreateInput, named *string, requested []requestedField) (*Node, *Error) {
	at, workType, attributes, fault := c.resolveWorkItemSettings(ctx, spec, id, named, written.attributes, nil)
	if fault != nil {
		return nil, fault
	}
	filed := workItemCreate{input: written, workType: workType, attributes: attributes}
	asked := withFields(requested, filed.verifyFields()...)
	fillInDurations(spec, workItemSchema, asked)
	issueBlocks(spec, composedWorkItem(), asked)
	body := filed.body()
	return c.write(ctx, spec, workItemSchema, asked, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiCreateIssueWorkItem(ctx, at, body, fields)
	}, filed.verify, writeResultNode(requested))
}

func (c *Client) resolveWorkItemSettings(ctx context.Context, spec *schemas, id string, named *string, set []namedValue, cleared []string) (string, *resolvedWorkType, []resolvedAttribute, *Error) {
	withAttributes := len(set) > 0 || len(cleared) > 0
	if named == nil && !withAttributes {
		return id, nil, nil, nil
	}
	readable, project, fault := c.readWorkItemTypes(ctx, spec, id, withAttributes)
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
	attributes, fault := project.resolveAttributes(set, cleared)
	if fault != nil {
		return "", nil, nil, fault
	}
	return readable.String(), workType, attributes, nil
}

func UpdateWorkItem(id, item string, spent, day, text, workType *string, attributes, cleared []string, expression string) (Call, *Error) {
	id, fault := parseIssueID(id)
	if fault != nil {
		return nil, fault
	}
	at, fault := parseChildID(workItemNoun, workItemOwnerNoun, item)
	if fault != nil {
		return nil, fault
	}
	written, fault := parseWorkItemUpdate(spent, day, text, workType, attributes, cleared)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	requested, fault := workItemFields(spec, expression, WorkItemWriteFields)
	if fault != nil {
		return nil, fault
	}
	return func(ctx context.Context, c *Client) (*Node, *Error) {
		return c.updateWorkItem(ctx, spec, id, at, written, workType, requested)
	}, nil
}

func (c *Client) updateWorkItem(ctx context.Context, spec *schemas, id string, at childID, written workItemUpdateInput, named *string, requested []requestedField) (*Node, *Error) {
	issue, workType, attributes, fault := c.resolveWorkItemSettings(ctx, spec, id, named, written.attributes, written.clearsAttributes)
	if fault != nil {
		return nil, fault
	}
	changed := workItemUpdate{input: written, issue: issue, at: at, workType: workType, attributes: attributes}
	asked := withFields(requested, changed.verifyFields()...)
	fillInDurations(spec, workItemSchema, asked)
	issueBlocks(spec, composedWorkItem(), asked)
	body := changed.body()
	return c.write(ctx, spec, workItemSchema, asked, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiUpdateIssueWorkItem(ctx, issue, at, body, fields)
	}, changed.verify, writeResultNode(requested))
}

func DeleteWorkItem(id, item string) (Call, *Error) {
	id, fault := parseIssueID(id)
	if fault != nil {
		return nil, fault
	}
	at, fault := parseChildID(workItemNoun, workItemOwnerNoun, item)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	return func(ctx context.Context, c *Client) (*Node, *Error) {
		return c.deleteWorkItem(ctx, spec, id, at)
	}, nil
}

func removedWorkItemFields() []requestedField {
	return []requestedField{
		{name: idKey},
		{name: issueOwner.String(), children: []requestedField{{name: idReadableKey}}},
	}
}

func (c *Client) deleteWorkItem(ctx context.Context, spec *schemas, id string, at childID) (*Node, *Error) {
	requested := removedWorkItemFields()
	a, fault := c.request(ctx, spec, workItemSchema, requested, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetIssueWorkItem(ctx, id, at, fields)
	})
	if fault != nil {
		return nil, fault
	}
	issue, fault := owningIssueID(a)
	if fault != nil {
		return nil, fault
	}
	known, fault := workItemID(a)
	if fault != nil {
		return nil, fault
	}
	if fault := writeEmpty(ctx, func(ctx context.Context) (*http.Response, error) {
		return c.apiDeleteIssueWorkItem(ctx, issue, known)
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

func (c *Client) readWorkItemTypes(ctx context.Context, spec *schemas, id string, withAttributes bool) (readableID, projectWorkItemTypes, *Error) {
	a, fault := c.request(ctx, spec, issueSchema, workItemTypesFields(withAttributes), func(ctx context.Context, fields string) (*http.Response, error) {
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

func (c *Client) listWorkItems(ctx context.Context, spec *schemas, id string, requested []requestedField, page Page) (*Node, *Error) {
	ask := func(ctx context.Context, fields string, w window) (*http.Response, error) {
		return c.apiGetIssueWorkItems(ctx, id, fields, w)
	}
	selection := c.newList(spec, workItemsPlural, workItemsListing, requested, workItemRequestFields(spec, requested), page, ask)
	return selection.fetch(ctx)
}

func workItemRequestFields(spec *schemas, requested []requestedField) []requestedField {
	asked := cloneFields(requested)
	fillInDurations(spec, workItemSchema, asked)
	issueBlocks(spec, composedWorkItem(), asked)
	return asked
}

func workItemFields(spec *schemas, expression string, defaults string) ([]requestedField, *Error) {
	written := defaults
	requested, fault := parseDefault(defaults, false)
	if expression != "" {
		written = expression
		requested, fault = parseFields(written, defaults)
	}
	if fault != nil {
		return nil, fault
	}
	if fault := rejectDurationParts(spec, workItemSchema, written, requested); fault != nil {
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
