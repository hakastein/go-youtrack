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
	articleSchema    = "Article"
	articlesPlural   = "articles"
	contentKey       = "content"
	parentArticleKey = "parentArticle"
	parentKey        = "parent"
)

// ShowArticleOptions: Fields is a fields= expression, empty for ArticleShowFields and +x for them and x. Comments
// come under comments oldest first, and the zero value asks for none.
type ShowArticleOptions struct {
	Fields   string
	Comments Comments
}

// ListArticlesOptions: Fields is a fields= expression, empty for ArticleListFields and +x for them and x.
type ListArticlesOptions struct {
	Fields string
	Page   Page
}

// Show refuses comments named anywhere in Fields: an article brings its comments by Comments alone.
func (s *ArticlesService) Show(ctx context.Context, id string, opts *ShowArticleOptions) (*Node, error) {
	return result(s.show(ctx, id, optionsOf(opts)))
}

// List sends query to YouTrack as written, in the search language of articles; YouTrack finds every article for a
// query it cannot parse and nothing for an attribute of issues, neither with an error.
func (s *ArticlesService) List(ctx context.Context, query string, opts *ListArticlesOptions) (*Node, error) {
	return result(s.list(ctx, query, optionsOf(opts)))
}

func (s *ArticlesService) Children(ctx context.Context, parent string, opts *ListArticlesOptions) (*Node, error) {
	return result(s.children(ctx, parent, optionsOf(opts)))
}

// Create reads the parent first and refuses one of another project: YouTrack would file the article in the project
// of the parent rather than in project.
func (s *ArticlesService) Create(ctx context.Context, project string, in *ArticleInput, opts *WriteOptions) (*Node, error) {
	return result(s.create(ctx, project, optionsOf(in), optionsOf(opts)))
}

// Update reads the article first and writes it by the readable id the read gave. A new parent is read up to the
// root of the project and refused when it is of another project, the article itself or an article under it.
func (s *ArticlesService) Update(ctx context.Context, id string, in *ArticleUpdate, opts *WriteOptions) (*Node, error) {
	return result(s.update(ctx, id, optionsOf(in), optionsOf(opts)))
}

// Delete deletes the article with every article under it and answers with the readable id the server gave just
// before the deletion, which goes to that id.
func (s *ArticlesService) Delete(ctx context.Context, id string) (*Node, error) {
	return result(s.delete(ctx, id))
}

func (s *ArticlesService) show(ctx context.Context, id string, opts ShowArticleOptions) (*Node, *Error) {
	c := s.client
	id, fault := parseArticleID(id)
	if fault != nil {
		return nil, fault
	}
	requested, fault := c.parseFields(articleSchema, opts.Fields, ArticleShowFields)
	if fault != nil {
		return nil, fault
	}
	if fault := opts.Comments.check(); fault != nil {
		return nil, fault
	}
	target := articleCommentTarget()
	decoded, fault := c.request(ctx, articleSchema, opts.Comments.merged(target, requested), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetArticle(ctx, id, fields)
	})
	if fault != nil {
		return nil, fault
	}
	article := decoded.objects[0]
	own, fault := opts.Comments.pair(target, decoded, article)
	if fault != nil {
		return nil, fault
	}
	return objectNode(decoded, requested, article, own)
}

func (s *ArticlesService) list(ctx context.Context, query string, opts ListArticlesOptions) (*Node, *Error) {
	c := s.client
	if fault := rejectRewritten(queryNoun, query); fault != nil {
		return nil, fault
	}
	page, fault := opts.Page.parse()
	if fault != nil {
		return nil, fault
	}
	requested, fault := c.parseFields(articleSchema, opts.Fields, ArticleListFields)
	if fault != nil {
		return nil, fault
	}
	return c.listPage(ctx, articlesPlural, "[]"+articleSchema, requested, page, func(ctx context.Context, fields string, w window) (*http.Response, error) {
		return c.apiGetArticles(ctx, query, fields, w)
	})
}

func (s *ArticlesService) children(ctx context.Context, parent string, opts ListArticlesOptions) (*Node, *Error) {
	c := s.client
	parent, fault := parseArticleID(parent)
	if fault != nil {
		return nil, fault
	}
	page, fault := opts.Page.parse()
	if fault != nil {
		return nil, fault
	}
	requested, fault := c.parseFields(articleSchema, opts.Fields, ArticleListFields)
	if fault != nil {
		return nil, fault
	}
	return c.listPage(ctx, articlesPlural, "[]"+articleSchema, requested, page, func(ctx context.Context, fields string, w window) (*http.Response, error) {
		return c.apiGetArticleChildArticles(ctx, parent, fields, w)
	})
}

func (s *ArticlesService) create(ctx context.Context, project string, in ArticleInput, opts WriteOptions) (*Node, *Error) {
	c := s.client
	code, fault := parseProjectCode(project)
	if fault != nil {
		return nil, fault
	}
	if fault := checkArticleInput(in); fault != nil {
		return nil, fault
	}
	requested, fault := c.parseFields(articleSchema, opts.Fields, ArticleShowFields)
	if fault != nil {
		return nil, fault
	}
	filed, fault := c.resolveCreateParent(ctx, code, in)
	if fault != nil {
		return nil, fault
	}
	body := filed.body()
	return writeAs(ctx, c, articleSchema, withFields(requested, filed.verifyFields()...), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiCreateArticle(ctx, body, fields)
	}, filed.verify, writeResultNode(requested))
}

type articleRef struct {
	id       string
	readable readableID
	project  string
	response decodedResponse
}

func (c *Client) resolveCreateParent(ctx context.Context, code string, in ArticleInput) (articleCreate, *Error) {
	filed := articleCreate{project: code, summary: in.Summary, content: in.Content}
	if in.Parent == "" {
		return filed, nil
	}
	found, fault := c.readArticleToWrite(ctx, in.Parent, parentOfAWrite)
	if fault != nil {
		return articleCreate{}, fault
	}
	if !strings.EqualFold(found.project, code) {
		return articleCreate{}, found.crossProjectFault(code, createAcrossProjectsMessage)
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

func (c *Client) readArticleToWrite(ctx context.Context, id, what string) (articleRef, *Error) {
	a, fault := c.request(ctx, articleSchema, articleToWriteFields(), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetArticle(ctx, id, fields)
	})
	if fault != nil {
		return articleRef{}, fault
	}
	return readArticleToWrite(a, what)
}

func readArticleToWrite(a decodedResponse, what string) (articleRef, *Error) {
	readable, fault := readableIDOf(a, articleOwner, what)
	if fault != nil {
		return articleRef{}, fault
	}
	found := a.objects[0]
	id, isText := found[idKey].(string)
	code, isNamed := memberOf(found[projectKey], shortNameKey).(string)
	if !isText || !isNamed {
		message := "the id or the project of the article is not text"
		return articleRef{}, a.invalid(message)
	}
	return articleRef{id: id, readable: readable, project: code, response: a}, nil
}

const parentOfAWrite = "the parent a write names"

func (p articleRef) crossProjectFault(code, message string) *Error {
	return p.response.fault(CodeBadUsage, message,
		Pair{Key: projectKey, Value: NewString(code)},
		Pair{Key: parentKey, Value: NewString(p.readable.String())},
		Pair{Key: "parent_project", Value: NewString(p.project)})
}

const (
	createAcrossProjectsMessage = "the parent the call names is an article of another project, and YouTrack would " +
		"file the new article in the project of the parent rather than in the one the call names"
	updateAcrossProjectsMessage = "the parent the call names is an article of another project, and an article " +
		"hangs from a parent of its own project alone"
)

func (s *ArticlesService) update(ctx context.Context, id string, in ArticleUpdate, opts WriteOptions) (*Node, *Error) {
	c := s.client
	id, fault := parseArticleID(id)
	if fault != nil {
		return nil, fault
	}
	if fault := checkArticleUpdate(in); fault != nil {
		return nil, fault
	}
	requested, fault := c.parseFields(articleSchema, opts.Fields, ArticleShowFields)
	if fault != nil {
		return nil, fault
	}
	article, fault := c.readArticleToWrite(ctx, id, "an update")
	if fault != nil {
		return nil, fault
	}
	written, fault := c.resolveUpdateParent(ctx, article, in)
	if fault != nil {
		return nil, fault
	}
	body := written.body()
	return writeAs(ctx, c, articleSchema, withFields(requested, written.verifyFields()...), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiUpdateArticle(ctx, article.readable, body, fields)
	}, written.verify, writeResultNode(requested))
}

func (c *Client) resolveUpdateParent(ctx context.Context, article articleRef, in ArticleUpdate) (articleUpdate, *Error) {
	written := articleUpdate{
		summary:       in.Summary,
		content:       in.Content,
		clearsContent: in.ClearContent,
		clearsParent:  in.ClearParent,
	}
	if in.Parent == nil {
		return written, nil
	}
	parent, line, fault := c.readAncestors(ctx, *in.Parent)
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

func (c *Client) readAncestors(ctx context.Context, id string) (articleRef, []ancestor, *Error) {
	var parent articleRef
	var line []ancestor
	for at := id; ; at = line[len(line)-1].id {
		a, fault := c.request(ctx, articleSchema, ancestorFields(), func(ctx context.Context, fields string) (*http.Response, error) {
			return c.apiGetArticle(ctx, at, fields)
		})
		if fault != nil {
			return articleRef{}, nil, fault
		}
		if line == nil {
			if parent, fault = readArticleToWrite(a, parentOfAWrite); fault != nil {
				return articleRef{}, nil, fault
			}
			line = []ancestor{{id: parent.id, readable: parent.readable.String()}}
		}
		var root bool
		if line, root, fault = ancestorsIn(a, line); fault != nil {
			return articleRef{}, nil, fault
		}
		if root {
			return parent, line, nil
		}
	}
}

func ancestorsIn(a decodedResponse, line []ancestor) ([]ancestor, bool, *Error) {
	object, readBefore := a.objects[0], len(line)
	for {
		value, asked := object[parentArticleKey]
		switch {
		case !asked && len(line) == readBefore:
			return nil, false, ancestorFault(a, brokenAncestry, line[readBefore-1])
		case !asked:
			return line, false, nil
		case value == nil:
			return line, true, nil
		}
		parentObject, isObject := value.(map[string]any)
		if !isObject {
			return nil, false, a.invalid("the parent of an article is neither an object nor null")
		}
		id, isText := parentObject[idKey].(string)
		readable, isReadable := parentObject[idReadableKey].(string)
		if !isText || !isReadable {
			return nil, false, a.invalid("the id or the readable id of an ancestor is not text")
		}
		step := ancestor{id: id, readable: readable}
		if slices.ContainsFunc(line, func(seen ancestor) bool { return seen.id == id }) {
			return nil, false, ancestorFault(a, ancestorCycle, step)
		}
		line = append(line, step)
		object = parentObject
	}
}

func ancestorFault(a decodedResponse, message string, at ancestor) *Error {
	return a.fault(CodeUpstreamInvalid, message, Pair{Key: articleKey, Value: NewString(at.readable)})
}

const (
	ancestorCycle = "the article under article stands twice in the line of parents the server answered with, and no " +
		"article hangs from itself"
	brokenAncestry = "the server answered no parent for the article under article and no root above it either, and a " +
		"line read on from there would be read from the same place again"
)

func (p articleRef) checkNoCycle(article articleRef, line []ancestor) *Error {
	at := slices.IndexFunc(line, func(step ancestor) bool { return step.id == article.id })
	if at < 0 {
		return nil
	}
	chain := make([]*Node, 0, at+1)
	for _, step := range line[:at+1] {
		chain = append(chain, NewString(step.readable))
	}
	message := "the parent the call names is the article itself or one written under it, and chain runs from the " +
		"parent up to the article: an article hangs from no line of its own"
	return p.response.fault(CodeBadUsage, message,
		Pair{Key: articleKey, Value: NewString(article.readable.String())},
		Pair{Key: parentKey, Value: NewString(p.readable.String())},
		Pair{Key: "chain", Value: NewList(chain...)})
}

func (s *ArticlesService) delete(ctx context.Context, id string) (*Node, *Error) {
	c := s.client
	id, fault := parseArticleID(id)
	if fault != nil {
		return nil, fault
	}
	return c.deleteOwner(ctx, articleOwner, articleSchema, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetArticle(ctx, id, fields)
	}, c.apiDeleteArticle)
}
