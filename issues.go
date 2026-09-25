package youtrack

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"unicode/utf8"
)

// customFields(Name) stands for the value of that field alone, and a bare customFields for every field of the issue.
const (
	IssueShowFields = "idReadable,summary,reporter(login),created,updated,resolved,tags(name),customFields," +
		"links(issues(idReadable,summary)),description"
	IssueListFields = "idReadable,summary,customFields(State,Type),created"
)

const (
	issueSchema   = "Issue"
	idReadableKey = "idReadable"
	issuesPlural  = "issues"
	countSchema   = "IssueCountResponse"
	countKey      = "count"
)

const stillCounting = -1

// ShowIssueOptions: Fields is a fields= expression, empty for IssueShowFields and +x for them and x. Comments come
// under comments oldest first, and the zero value asks for none.
type ShowIssueOptions struct {
	Fields   string
	Comments Comments
}

// ListIssuesOptions: Fields is a fields= expression, empty for IssueListFields and +x for them and x. Warn is handed
// the parts of the search YouTrack looks for as free text, before the search is sent; nil asks YouTrack for none.
type ListIssuesOptions struct {
	Fields string
	Page   Page
	Warn   func(*Warning)
}

// Show resolves a custom field named under customFields by its name, then by its translation, among the custom
// fields of the instance.
func (s *IssuesService) Show(ctx context.Context, id string, opts *ShowIssueOptions) (*Node, error) {
	return result(s.show(ctx, id, optionsOf(opts)))
}

// List sends query to YouTrack as written. total is null when YouTrack has not counted the search after two asks.
func (s *IssuesService) List(ctx context.Context, query string, opts *ListIssuesOptions) (*Node, error) {
	return result(s.list(ctx, query, optionsOf(opts)))
}

// Delete answers with the readable id the server gave just before the deletion, which goes to that id.
func (s *IssuesService) Delete(ctx context.Context, id string) (*Node, error) {
	return result(s.delete(ctx, id))
}

func (s *IssuesService) show(ctx context.Context, id string, opts ShowIssueOptions) (*Node, *Error) {
	id, fault := parseIssueID(id)
	if fault != nil {
		return nil, fault
	}
	c := s.client
	requested, fault := issueFields(c.spec, opts.Fields, IssueShowFields, commentsOfAShow)
	if fault != nil {
		return nil, fault
	}
	if fault := opts.Comments.check(); fault != nil {
		return nil, fault
	}
	return c.showIssue(ctx, id, requested, opts.Comments)
}

func (s *IssuesService) list(ctx context.Context, query string, opts ListIssuesOptions) (*Node, *Error) {
	if fault := rejectUnreadableQuery(query); fault != nil {
		return nil, fault
	}
	page, fault := opts.Page.parse()
	if fault != nil {
		return nil, fault
	}
	c := s.client
	requested, fault := issueFields(c.spec, opts.Fields, IssueListFields, issueCommentTarget().commentsOfAList())
	if fault != nil {
		return nil, fault
	}
	return c.listIssues(ctx, query, requested, page, opts.Warn)
}

func (s *IssuesService) delete(ctx context.Context, id string) (*Node, *Error) {
	id, fault := parseIssueID(id)
	if fault != nil {
		return nil, fault
	}
	c := s.client
	return c.deleteOwner(ctx, issueOwner, issueSchema, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetIssue(ctx, id, fields, nil)
	}, c.apiDeleteIssue)
}

func issueFields(spec *schemas, expression string, defaults, comments string) ([]requestedField, *Error) {
	written, requested, fault := fieldsOrDefault(expression, defaults, true)
	if fault != nil {
		return nil, fault
	}
	expandBareCustomFields(spec, requested)
	if fault := issueCommentTarget().reject(spec, written, requested, comments); fault != nil {
		return nil, fault
	}
	return requested, rejectIssueBlocks(spec, composedIssue(), written, requested)
}

func rejectUnreadableQuery(query string) *Error {
	if utf8.ValidString(query) {
		return nil
	}
	message := "the query is no valid UTF-8, and YouTrack reads a search as text"
	return &Error{Code: CodeBadUsage, Message: message}
}

func (c *Client) listIssues(ctx context.Context, query string, requested []requestedField, page Page, warn func(*Warning)) (*Node, *Error) {
	if fault := c.warnOfFreeText(ctx, query, warn); fault != nil {
		return nil, fault
	}
	asked, named, fault := c.issueRequest(ctx, requested)
	if fault != nil {
		return nil, fault
	}
	selection := list{
		client:    c,
		plural:    issuesPlural,
		schema:    "[]" + issueSchema,
		requested: requested,
		page:      page,
		fetchPage: func(ctx context.Context, fields string, w window) (*http.Response, error) {
			return c.apiGetIssues(ctx, query, fields, named, w)
		},
		sentFields: asked,
		countTotal: func(ctx context.Context) (count, *Error) { return c.countIssuesWithRetry(ctx, query) },
	}
	return selection.fetch(ctx)
}

func (c *Client) warnOfFreeText(ctx context.Context, query string, warn func(*Warning)) *Error {
	if warn == nil {
		return nil
	}
	marked, fault := c.searchMarkup(ctx, query)
	if fault != nil {
		return fault
	}
	if warning := freeTextWarning(query, marked); warning != nil {
		warn(warning)
	}
	return nil
}

func (c *Client) countIssuesWithRetry(ctx context.Context, query string) (count, *Error) {
	found, fault := c.countIssues(ctx, query)
	if fault != nil || found.known {
		return found, fault
	}
	return c.countIssues(ctx, query)
}

func (c *Client) countIssues(ctx context.Context, query string) (count, *Error) {
	body := searchBody(query)
	decoded, fault := c.request(ctx, countSchema, []requestedField{{name: countKey}}, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiCountIssues(ctx, body, fields)
	})
	if fault != nil {
		return count{}, fault
	}
	found, whole := parseInt64(decoded.objects[0][countKey])
	switch {
	case !whole || found < stillCounting:
		message := "the count of the issues the search finds is neither a whole number of them nor -1 for a " +
			"count that is not ready"
		return count{}, shapeFailure(decoded.httpResponse, decoded.body, message)
	case found == stillCounting:
		return count{}, nil
	}
	return counted(int(found)), nil
}

func rejectCustomFieldNames(spec *schemas, at, expression string, requested []requestedField) *Error {
	var fault *Error
	eachCustomFields(spec, at, requested, func(parents []string, field *requestedField) {
		if field.children == nil || fault != nil {
			return
		}
		if at != issueSchema || len(parents) > 0 {
			fault = rejectBareCustomFields(at, expression, fieldPath(parents, field.name))
			return
		}
		for _, name := range field.children {
			if name.children == nil {
				continue
			}
			message := fmt.Sprintf("fields %s: %s names a custom field of the issue, which is printed as the "+
				"one value it holds, so no name stands under it",
				quote(expression), fieldPath([]string{field.name}, formatName(name)))
			fault = &Error{Code: CodeBadUsage, Message: message}
			return
		}
	})
	return fault
}

func rejectBareCustomFields(at, expression, path string) *Error {
	message := fmt.Sprintf("fields %s: %s holds the custom fields of another issue, which are printed whole, so "+
		"no name stands under it; only the custom fields of the issue asked for are named one by one",
		quote(expression), path)
	if at != issueSchema {
		message = fmt.Sprintf("fields %s: %s holds the custom fields of the issue, which are printed whole, so "+
			"no name stands under it; a custom field is named one by one only in the fields of an issue read by its id",
			quote(expression), path)
	}
	return &Error{Code: CodeBadUsage, Message: message}
}

func rejectQuotedNames(spec *schemas, at, expression string, requested []requestedField) *Error {
	own := ownCustomFields(spec, at, requested)
	var reject func(fields []requestedField, parents []string, named bool) *Error
	reject = func(fields []requestedField, parents []string, named bool) *Error {
		for i := range fields {
			field := &fields[i]
			written := formatName(*field)
			if field.quoted && !named {
				message := fmt.Sprintf("fields %s: %s is a name in double quotes, which is how a custom field "+
					"of the issue is named, and such a name stands only under customFields of the issue asked for",
					quote(expression), fieldPath(parents, written))
				return &Error{Code: CodeBadUsage, Message: message}
			}
			if fault := reject(field.children, append(slices.Clip(parents), written), field == own); fault != nil {
				return fault
			}
		}
		return nil
	}
	return reject(requested, nil, false)
}

func expandBareCustomFields(spec *schemas, requested []requestedField) {
	eachCustomFields(spec, issueSchema, requested, func(_ []string, field *requestedField) {
		if field.bare {
			field.children = nil
		}
	})
}

type blockSchema struct{ schema string }

func composedIssue() blockSchema    { return blockSchema{issueSchema} }
func composedWorkItem() blockSchema { return blockSchema{workItemSchema} }

func composedSchemas() []blockSchema {
	return []blockSchema{composedIssue(), composedWorkItem()}
}

func hasIssueBlocks(schema string) bool {
	return slices.ContainsFunc(composedSchemas(), func(at blockSchema) bool { return at.schema == schema })
}

func issueBlocks(spec *schemas, at blockSchema, asked []requestedField) {
	matchTranslatedNames := hasDefaultNames(spec, asked)
	eachCustomFields(spec, at.schema, asked, func(parents []string, field *requestedField) {
		field.children = customFieldsAsked(matchTranslatedNames && len(parents) == 0)
	})
	eachIssueLink(spec, at.schema, asked, func(_ []string, field *requestedField) { field.children = linkRequestFields(field.children) })
	eachAttributes(spec, at.schema, asked, func(_ []string, field *requestedField) { field.children = attributesAsked() })
}

func rejectIssueBlocks(spec *schemas, at blockSchema, expression string, requested []requestedField) *Error {
	if fault := rejectQuotedNames(spec, at.schema, expression, requested); fault != nil {
		return fault
	}
	if fault := rejectCustomFieldNames(spec, at.schema, expression, requested); fault != nil {
		return fault
	}
	if fault := rejectLinkParts(spec, at.schema, expression, requested); fault != nil {
		return fault
	}
	return rejectAttributeNames(spec, at.schema, expression, requested)
}

func eachCustomFields(spec *schemas, at string, requested []requestedField, visit func(parents []string, field *requestedField)) {
	fieldsNamed(spec, at, issueSchema, customFieldsKey, requested, nil, visit)
}

func ownCustomFields(spec *schemas, at string, requested []requestedField) *requestedField {
	var own *requestedField
	eachCustomFields(spec, at, requested, func(parents []string, field *requestedField) {
		if len(parents) == 0 {
			own = field
		}
	})
	return own
}

func namedCustomFields(spec *schemas, requested []requestedField) *requestedField {
	own := ownCustomFields(spec, issueSchema, requested)
	if own == nil || own.children == nil {
		return nil
	}
	return own
}

func (c *Client) showIssue(ctx context.Context, id string, requested []requestedField, comments Comments) (*Node, *Error) {
	asked, named, fault := c.issueRequest(ctx, requested)
	if fault != nil {
		return nil, fault
	}
	held := issueCommentTarget()
	decoded, fault := c.request(ctx, issueSchema, comments.merged(held, asked), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetIssue(ctx, id, fields, named)
	})
	if fault != nil {
		return nil, fault
	}
	issue := decoded.objects[0]
	own, fault := comments.pair(held, decoded, issue)
	if fault != nil {
		return nil, fault
	}
	return objectNode(decoded, requested, issue, own)
}

func (c *Client) issueRequest(ctx context.Context, requested []requestedField) ([]requestedField, []string, *Error) {
	if fault := c.resolveCustomFields(ctx, requested); fault != nil {
		return nil, nil, fault
	}
	asked := cloneFields(requested)
	issueBlocks(c.spec, composedIssue(), asked)
	return asked, customFieldsFilter(c.spec, requested), nil
}

func customFieldsFilter(spec *schemas, requested []requestedField) []string {
	named := namedCustomFields(spec, requested)
	if named == nil || filterReachesOtherBlocks(spec, requested) {
		return nil
	}
	names := make([]string, 0, len(named.children))
	for _, name := range named.children {
		names = append(names, name.name)
	}
	return names
}

func filterReachesOtherBlocks(spec *schemas, requested []requestedField) bool {
	blocks := 0
	eachCustomFields(spec, issueSchema, requested, func(_ []string, _ *requestedField) { blocks++ })
	return blocks > 1
}
