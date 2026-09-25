package youtrack

import (
	"context"
	"net/http"
	"slices"
	"strings"
)

const ArticleShowFields = "idReadable,summary,reporter(login),created,updated,tags(name)," +
	"parentArticle(idReadable,summary),childArticles(idReadable,summary),content"

const ArticleListFields = "idReadable,summary"

const (
	articleSchema   = "Article"
	articlesPlural  = "articles"
	articlesListing = "[]" + articleSchema
)

func ShowArticle(id string, expression string, comments Comments) (Call, *Error) {
	id, fault := parseArticleID(id)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	requested, fault := articleFields(spec, expression, ArticleShowFields, commentsOfAShow)
	if fault != nil {
		return nil, fault
	}
	if fault := comments.check(); fault != nil {
		return nil, fault
	}
	return func(ctx context.Context, c *Client) (*Node, *Error) {
		return c.showArticle(ctx, spec, id, requested, comments)
	}, nil
}

func CreateArticle(code, summary string, content, parent *string, expression string) (Call, *Error) {
	code, fault := parseProjectCode(code)
	if fault != nil {
		return nil, fault
	}
	parts, fault := parseArticleCreate(code, summary, content, parent)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	requested, fault := articleFields(spec, expression, ArticleShowFields, articleCommentTarget().commentsOfAWrite())
	if fault != nil {
		return nil, fault
	}
	return func(ctx context.Context, c *Client) (*Node, *Error) {
		return c.createArticle(ctx, spec, parts, requested)
	}, nil
}

func (c *Client) createArticle(ctx context.Context, spec *schemas, parts articleCreateInput, requested []requestedField) (*Node, *Error) {
	filed, fault := c.resolveCreateParent(ctx, spec, parts)
	if fault != nil {
		return nil, fault
	}
	body := filed.body()
	return c.write(ctx, spec, articleSchema, withFields(requested, filed.verifyFields()...), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiCreateArticle(ctx, body, fields)
	}, filed.verify, writeResultNode(requested))
}

type articleRef struct {
	id       string
	readable readableID
	project  string
	response decodedResponse
}

func (c *Client) resolveCreateParent(ctx context.Context, spec *schemas, parts articleCreateInput) (articleCreate, *Error) {
	filed := articleCreate{project: parts.project, summary: parts.summary, content: parts.content}
	if parts.parentID == nil {
		return filed, nil
	}
	found, fault := c.readArticleToWrite(ctx, spec, *parts.parentID, parentFlag)
	if fault != nil {
		return articleCreate{}, fault
	}
	if !strings.EqualFold(found.project, parts.project) {
		return articleCreate{}, found.crossProjectFault(parts.project, createAcrossProjectsMessage)
	}
	filed.parent = &found
	return filed, nil
}

func articleToWriteFields() []requestedField {
	return []requestedField{
		{name: idKey},
		{name: idReadableKey},
		{name: projectKey, children: []requestedField{{name: shortNameKey}}},
	}
}

func (c *Client) readArticleToWrite(ctx context.Context, spec *schemas, id, what string) (articleRef, *Error) {
	a, fault := c.request(ctx, spec, articleSchema, articleToWriteFields(), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetArticle(ctx, id, fields)
	})
	if fault != nil {
		return articleRef{}, fault
	}
	return readArticleToWrite(a, what)
}

func readArticleToWrite(a decodedResponse, what string) (articleRef, *Error) {
	readable, fault := readableIDAt(a, a.objects[0], articleOwner, what)
	if fault != nil {
		return articleRef{}, fault
	}
	found := a.objects[0]
	id, isText := found[idKey].(string)
	code, isNamed := memberOf(found[projectKey], shortNameKey).(string)
	if !isText || !isNamed {
		message := "the id or the project of the article is not text"
		return articleRef{}, shapeFailure(a.httpResponse, a.body, message)
	}
	return articleRef{id: id, readable: readable, project: code, response: a}, nil
}

const parentFlag = "--" + parentKey

func (p articleRef) crossProjectFault(code, message string) *Error {
	a := p.response
	details := []Pair{
		requestDetail(a.httpResponse.Request.Method, a.httpResponse.Request.URL.Redacted()),
		{Key: projectKey, Value: NewString(code)},
		{Key: parentKey, Value: NewString(p.readable.String())},
		{Key: "parent_project", Value: NewString(p.project)},
	}
	return &Error{Code: CodeBadUsage, Message: message, Details: details}
}

const (
	createAcrossProjectsMessage = "--parent names an article of another project, and YouTrack would file the new " +
		"article in the project of the parent rather than in the one the call names"
	updateAcrossProjectsMessage = "--parent names an article of another project, and an article hangs from a parent " +
		"of its own project alone"
)

func UpdateArticle(id string, summary, content, parent *string, cleared []string, expression string) (Call, *Error) {
	id, fault := parseArticleID(id)
	if fault != nil {
		return nil, fault
	}
	if summary == nil && content == nil && parent == nil && len(cleared) == 0 {
		return nil, &Error{Code: CodeBadUsage, Message: nothingToWriteIntoAnArticle}
	}
	parts, fault := parseArticleUpdate(summary, content, parent, cleared)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	requested, fault := articleFields(spec, expression, ArticleShowFields, articleCommentTarget().commentsOfAWrite())
	if fault != nil {
		return nil, fault
	}
	return func(ctx context.Context, c *Client) (*Node, *Error) {
		return c.updateArticle(ctx, spec, id, parts, requested)
	}, nil
}

func (c *Client) updateArticle(ctx context.Context, spec *schemas, id string, parts articleUpdateInput, requested []requestedField) (*Node, *Error) {
	article, fault := c.readArticleToWrite(ctx, spec, id, "an update")
	if fault != nil {
		return nil, fault
	}
	written, fault := c.resolveUpdateParent(ctx, spec, article, parts)
	if fault != nil {
		return nil, fault
	}
	body := written.body()
	return c.write(ctx, spec, articleSchema, withFields(requested, written.verifyFields()...), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiUpdateArticle(ctx, article.readable, body, fields)
	}, written.verify, writeResultNode(requested))
}

func (c *Client) resolveUpdateParent(ctx context.Context, spec *schemas, article articleRef, parts articleUpdateInput) (articleUpdate, *Error) {
	written := articleUpdate{
		summary:       parts.summary,
		content:       parts.content,
		clearsContent: parts.clearsContent,
		clearsParent:  parts.clearsParent,
	}
	if parts.parentID == nil {
		return written, nil
	}
	parent, line, fault := c.readAncestors(ctx, spec, *parts.parentID)
	if fault != nil {
		return articleUpdate{}, fault
	}
	if !strings.EqualFold(parent.project, article.project) {
		return articleUpdate{}, parent.crossProjectFault(article.project, updateAcrossProjectsMessage)
	}
	if fault := parent.checkNoCycle(article, line); fault != nil {
		return articleUpdate{}, fault
	}
	written.parent = &parent
	return written, nil
}

type ancestor struct {
	id       string
	readable string
}

const ancestorsPerRequest = 10

func ancestorFields() []requestedField {
	var parentObject []requestedField
	for range ancestorsPerRequest {
		parentObject = []requestedField{{name: parentArticleKey, children: append(
			[]requestedField{{name: idKey}, {name: idReadableKey}}, parentObject...)}}
	}
	return append(articleToWriteFields(), parentObject...)
}

func (c *Client) readAncestors(ctx context.Context, spec *schemas, id string) (articleRef, []ancestor, *Error) {
	var parent articleRef
	var line []ancestor
	seen := map[string]bool{}
	for at := id; ; {
		a, fault := c.request(ctx, spec, articleSchema, ancestorFields(), func(ctx context.Context, fields string) (*http.Response, error) {
			return c.apiGetArticle(ctx, at, fields)
		})
		if fault != nil {
			return articleRef{}, nil, fault
		}
		object := a.objects[0]
		if line == nil {
			parent, fault = readArticleToWrite(a, parentFlag)
			if fault != nil {
				return articleRef{}, nil, fault
			}
			line = append(line, ancestor{id: parent.id, readable: parent.readable.String()})
			seen[parent.id] = true
		}
		readBefore := len(line)
		for {
			value, asked := object[parentArticleKey]
			if !asked {
				break
			}
			if value == nil {
				return parent, line, nil
			}
			parentObject, isObject := value.(map[string]any)
			if !isObject {
				return articleRef{}, nil, shapeFailure(a.httpResponse, a.body, "the parent of an article is neither an object nor null")
			}
			step, fault := readAncestor(a, parentObject)
			if fault != nil {
				return articleRef{}, nil, fault
			}
			if seen[step.id] {
				return articleRef{}, nil, ancestorCycleFault(a, step)
			}
			seen[step.id] = true
			line = append(line, step)
			object = parentObject
		}
		if len(line) == readBefore {
			return articleRef{}, nil, brokenAncestryFault(a, line[readBefore-1])
		}
		at = line[len(line)-1].id
	}
}

func readAncestor(a decodedResponse, parent map[string]any) (ancestor, *Error) {
	id, isText := parent[idKey].(string)
	readable, isReadable := parent[idReadableKey].(string)
	if !isText || !isReadable {
		return ancestor{}, shapeFailure(a.httpResponse, a.body, "the id or the readable id of an ancestor is not text")
	}
	return ancestor{id: id, readable: readable}, nil
}

func ancestorCycleFault(a decodedResponse, twice ancestor) *Error {
	details := []Pair{
		requestDetail(a.httpResponse.Request.Method, a.httpResponse.Request.URL.Redacted()),
		{Key: articleOwner.String(), Value: NewString(twice.readable)},
	}
	message := "the article under article stands twice in the line of parents the server answered with, and no " +
		"article hangs from itself"
	return &Error{Code: CodeUpstreamInvalid, Message: message, Details: details}
}

func brokenAncestryFault(a decodedResponse, at ancestor) *Error {
	details := []Pair{
		requestDetail(a.httpResponse.Request.Method, a.httpResponse.Request.URL.Redacted()),
		{Key: articleOwner.String(), Value: NewString(at.readable)},
	}
	message := "the server answered no parent for the article under article and no root above it either, and a " +
		"line read on from there would be read from the same place again"
	return &Error{Code: CodeUpstreamInvalid, Message: message, Details: details}
}

func (p articleRef) checkNoCycle(article articleRef, line []ancestor) *Error {
	at := slices.IndexFunc(line, func(step ancestor) bool { return step.id == article.id })
	if at < 0 {
		return nil
	}
	chain := make([]*Node, 0, at+1)
	for _, step := range line[:at+1] {
		chain = append(chain, NewString(step.readable))
	}
	a := p.response
	details := []Pair{
		requestDetail(a.httpResponse.Request.Method, a.httpResponse.Request.URL.Redacted()),
		{Key: articleOwner.String(), Value: NewString(article.readable.String())},
		{Key: parentKey, Value: NewString(p.readable.String())},
		{Key: "chain", Value: NewList(chain...)},
	}
	message := "--parent names the article itself or one written under it, and chain runs from the parent up to " +
		"the article: an article hangs from no line of its own"
	return &Error{Code: CodeBadUsage, Message: message, Details: details}
}

func DeleteArticle(id string) (Call, *Error) {
	id, fault := parseArticleID(id)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	return func(ctx context.Context, c *Client) (*Node, *Error) {
		return c.deleteOwner(ctx, spec, articleOwner, articleSchema, func(ctx context.Context, fields string) (*http.Response, error) {
			return c.apiGetArticle(ctx, id, fields)
		}, c.apiDeleteArticle)
	}, nil
}

func ListArticles(query string, expression string, page Page) (Call, *Error) {
	if fault := rejectUnreadableQuery(query); fault != nil {
		return nil, fault
	}
	page, fault := page.parse()
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	requested, fault := articleFields(spec, expression, ArticleListFields, articleCommentTarget().commentsOfAList())
	if fault != nil {
		return nil, fault
	}
	return func(ctx context.Context, c *Client) (*Node, *Error) {
		return c.listArticles(ctx, spec, query, requested, page)
	}, nil
}

func (c *Client) listArticles(ctx context.Context, spec *schemas, query string, requested []requestedField, page Page) (*Node, *Error) {
	return c.listPage(ctx, spec, articlesPlural, articlesListing, requested, page, func(ctx context.Context, fields string, w window) (*http.Response, error) {
		return c.apiGetArticles(ctx, query, fields, w)
	})
}

func ListChildArticles(parent string, expression string, page Page) (Call, *Error) {
	parent, fault := parseArticleID(parent)
	if fault != nil {
		return nil, fault
	}
	page, fault = page.parse()
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	requested, fault := articleFields(spec, expression, ArticleListFields, articleCommentTarget().commentsOfAList())
	if fault != nil {
		return nil, fault
	}
	return func(ctx context.Context, c *Client) (*Node, *Error) {
		return c.listPage(ctx, spec, articlesPlural, articlesListing, requested, page, func(ctx context.Context, fields string, w window) (*http.Response, error) {
			return c.apiGetArticleChildArticles(ctx, parent, fields, w)
		})
	}, nil
}

func articleFields(spec *schemas, expression string, defaults, because string) ([]requestedField, *Error) {
	written := defaults
	requested, fault := parseDefault(defaults, false)
	if expression != "" {
		written = expression
		requested, fault = parseFields(written, defaults)
	}
	if fault != nil {
		return nil, fault
	}
	if fault := articleCommentTarget().reject(spec, written, requested, because); fault != nil {
		return nil, fault
	}
	return requested, nil
}

func (c *Client) showArticle(ctx context.Context, spec *schemas, id string, requested []requestedField, comments Comments) (*Node, *Error) {
	held := articleCommentTarget()
	decoded, fault := c.request(ctx, spec, articleSchema, comments.merged(held, requested), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetArticle(ctx, id, fields)
	})
	if fault != nil {
		return nil, fault
	}
	article := decoded.objects[0]
	own, fault := comments.pair(held, decoded, article)
	if fault != nil {
		return nil, fault
	}
	return objectNode(decoded, requested, article, own)
}
