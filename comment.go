package youtrack

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"slices"
)

const (
	commentsKey      = "comments"
	deletedKey       = "deleted"
	commentKey       = "comment"
	commentOwnerNoun = "the issue or the article"
)

const (
	CommentFields = "id,author(login),created,updated,text"
	// A comment of an article carries no deleted, so a list of them goes without it.
	CommentListFields = printedCommentFields + ",deleted"
)

const printedCommentFields = "id,author(login),created,text"

// ListCommentsOptions: Fields is a fields= expression, empty for the defaults of the owner and +x for them and x.
type ListCommentsOptions struct {
	Fields string
	Page   Page
}

// Comments is how many of the latest comments a read of an issue or an article brings, oldest first by created and
// without the ones taken back by their authors; the zero value brings none and asks the server for none.
type Comments struct {
	last int
	all  bool
}

// LastComments is refused by the read it is passed to when n is negative.
func LastComments(n int) Comments {
	return Comments{last: n}
}

func AllComments() Comments {
	return Comments{all: true}
}

func (c Comments) check() *Error {
	if c.last >= 0 {
		return nil
	}
	message := fmt.Sprintf("the read asks for the latest %d comments, and a count of them is 0 for none or more", c.last)
	return &Error{Code: CodeBadUsage, Message: message}
}

func (c Comments) asked() bool {
	return c.all || c.last > 0
}

// List runs oldest first, and owner is the readable id of an issue or an article. An issue lists the comments taken
// back by their authors too, with text null, so its defaults carry deleted; a comment of an article has none.
func (s *CommentsService) List(ctx context.Context, owner string, opts *ListCommentsOptions) (*Node, error) {
	return result(s.list(ctx, owner, optionsOf(opts)))
}

// Create refuses an empty text before the request: YouTrack refuses one on an issue and keeps it on an article.
func (s *CommentsService) Create(ctx context.Context, owner, text string, opts *WriteOptions) (*Node, error) {
	return result(s.create(ctx, owner, text, optionsOf(opts)))
}

// A comment of an issue is read before the write, and one its author deleted is refused: YouTrack would take the
// write silently.
func (s *CommentsService) Update(ctx context.Context, owner, id, text string, opts *WriteOptions) (*Node, error) {
	return result(s.update(ctx, owner, id, text, optionsOf(opts)))
}

// The answer holds the id alone: the comment is not read before the deletion.
func (s *CommentsService) Delete(ctx context.Context, owner, id string) (*Node, error) {
	return result(s.delete(ctx, owner, id))
}

func (s *CommentsService) list(ctx context.Context, owner string, opts ListCommentsOptions) (*Node, *Error) {
	at, fault := parseOwner(owner)
	if fault != nil {
		return nil, fault
	}
	page, fault := opts.Page.parse()
	if fault != nil {
		return nil, fault
	}
	held := commentTargetOf(at.kind)
	requested, fault := s.client.parseFields(held.comment, opts.Fields, held.listFields)
	if fault != nil {
		return nil, fault
	}
	c := s.client
	return c.listPage(ctx, commentsKey, "[]"+held.comment, requested, page, func(ctx context.Context, fields string, w window) (*http.Response, error) {
		return held.list(c, ctx, at, fields, w)
	})
}

type commentTarget struct {
	schema       string
	comment      string
	kind         ownerKind
	listFields   string
	keepsDeleted bool
	list         func(c *Client, ctx context.Context, at owner, fields string, w window) (*http.Response, error)
	create       func(c *Client, ctx context.Context, at owner, body []byte, fields string) (*http.Response, error)
	get          func(c *Client, ctx context.Context, at owner, comment childID, fields string) (*http.Response, error)
	update       func(c *Client, ctx context.Context, at owner, comment childID, body []byte, fields string) (*http.Response, error)
	remove       func(c *Client, ctx context.Context, at owner, comment childID) (*http.Response, error)
}

func issueCommentTarget() commentTarget {
	return commentTarget{schema: issueSchema, comment: "IssueComment", kind: issueOwner, listFields: CommentListFields,
		keepsDeleted: true, list: (*Client).apiGetIssueComments, create: (*Client).apiCreateIssueComment,
		get: (*Client).apiGetIssueComment, update: (*Client).apiUpdateIssueComment, remove: (*Client).apiDeleteIssueComment}
}

func articleCommentTarget() commentTarget {
	return commentTarget{schema: articleSchema, comment: "ArticleComment", kind: articleOwner, listFields: printedCommentFields,
		list: (*Client).apiGetArticleComments, create: (*Client).apiCreateArticleComment,
		update: (*Client).apiUpdateArticleComment, remove: (*Client).apiDeleteArticleComment}
}

func commentTargetOf(kind ownerKind) commentTarget {
	if kind == articleOwner {
		return articleCommentTarget()
	}
	return issueCommentTarget()
}

func (h commentTarget) reject(spec *schemas, root, expression string, requested []requestedField) *Error {
	path, written := firstFieldNamed(spec, root, h.schema, commentsKey, requested)
	if !written {
		return nil
	}
	message := fmt.Sprintf("fields %s: %s holds the comments of an %s, which come a record at a time in a list of its "+
		"comments, and with the %s itself in a read of it that asks for them", quote(expression), path, h.kind, h.kind)
	return &Error{Code: CodeBadUsage, Message: message}
}

func ownFields(expression string) []requestedField {
	fields, fault := readExpression(expression, "", false)
	if fault != nil {
		panic(fault)
	}
	return fields
}

func (c Comments) merged(h commentTarget, asked []requestedField) []requestedField {
	if !c.asked() {
		return asked
	}
	return withFields(asked, requestedField{name: commentsKey, children: ownFields(h.listFields)})
}

func (c Comments) pair(h commentTarget, a decodedResponse, holder map[string]any) ([]Pair, *Error) {
	if !c.asked() {
		return nil, nil
	}
	printed, fault := c.of(h, a, holder)
	if fault != nil {
		return nil, fault
	}
	return []Pair{{Key: commentsKey, Value: printed}}, nil
}

type datedComment struct {
	created int64
	comment map[string]any
}

func (c Comments) of(h commentTarget, a decodedResponse, holder map[string]any) (*Node, *Error) {
	received, isList := holder[commentsKey].([]any)
	if !isList {
		return nil, a.invalid(fmt.Sprintf("the comments of the %s arrived as something other than an array", h.kind))
	}
	kept := make([]datedComment, 0, len(received))
	for _, item := range received {
		comment, isObject := item.(map[string]any)
		if !isObject {
			return nil, a.invalid(fmt.Sprintf("a comment of the %s arrived as something other than an object", h.kind))
		}
		gone, fault := h.deleted(a, comment)
		if fault != nil {
			return nil, fault
		}
		if gone {
			continue
		}
		written, isInstant := parseInt64(comment["created"])
		if !isInstant {
			return nil, a.invalid(notAnInstant("created"))
		}
		kept = append(kept, datedComment{created: written, comment: comment})
	}
	slices.SortStableFunc(kept, func(a, b datedComment) int { return cmp.Compare(a.created, b.created) })
	if !c.all {
		kept = kept[max(len(kept)-c.last, 0):]
	}
	objects := make([]map[string]any, 0, len(kept))
	for _, written := range kept {
		objects = append(objects, written.comment)
	}
	printed, fault := newConverter(a, wholeRecord).objectsAt(h.comment, ownFields(printedCommentFields), objects)
	if fault != nil {
		return nil, fault
	}
	return NewList(printed...), nil
}

func (h commentTarget) deleted(a decodedResponse, comment map[string]any) (bool, *Error) {
	if !h.keepsDeleted {
		return false, nil
	}
	gone, isFlag := comment[deletedKey].(bool)
	if !isFlag {
		return false, a.invalid("whether a comment is deleted arrived as neither true nor false")
	}
	return gone, nil
}
