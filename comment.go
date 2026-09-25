package youtrack

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
)

const (
	commentsKey      = "comments"
	deletedKey       = "deleted"
	commentKey       = "comment"
	commentOwnerNoun = "the issue or the article"
)

const CommentFields = "id,author(login),created,updated,text"

const everyComment = "all"

type Comments struct {
	last int
	all  bool
}

func AllComments() Comments {
	return Comments{all: true}
}

func ParseComments(text string) (Comments, error) {
	if text == everyComment {
		return AllComments(), nil
	}
	last, err := strconv.Atoi(text)
	switch {
	case errors.Is(err, strconv.ErrRange):
		return Comments{}, errors.New("it is a larger number than there could ever be comments")
	case err != nil:
		return Comments{}, fmt.Errorf("it is neither %s nor a whole number of comments", everyComment)
	case last < 0:
		return Comments{}, errors.New("a number of comments is not negative")
	}
	return Comments{last: last}, nil
}

func (c Comments) String() string {
	if c.all {
		return everyComment
	}
	return strconv.Itoa(c.last)
}

func (c Comments) asked() bool {
	return c.all || c.last > 0
}

type commentTarget struct {
	schema  string
	comment string
	owner   ownerKind
	api     commentAPI
}

type commentAPI struct {
	list       func(c *Client, ctx context.Context, at owner, fields string, w window) (*http.Response, error)
	create     func(c *Client, ctx context.Context, at owner, body []byte, fields string) (*http.Response, error)
	getComment func(c *Client, ctx context.Context, at owner, comment childID, fields string) (*http.Response, error)
	update     func(c *Client, ctx context.Context, at owner, comment childID, body []byte, fields string) (*http.Response, error)
	remove     func(c *Client, ctx context.Context, at owner, comment childID) (*http.Response, error)
}

func issueCommentTarget() commentTarget {
	return commentTarget{schema: issueSchema, comment: "IssueComment", owner: issueOwner, api: commentAPI{
		list:       (*Client).apiGetIssueComments,
		create:     (*Client).apiCreateIssueComment,
		getComment: (*Client).apiGetIssueComment,
		update:     (*Client).apiUpdateIssueComment,
		remove:     (*Client).apiDeleteIssueComment,
	}}
}

func articleCommentTarget() commentTarget {
	return commentTarget{schema: articleSchema, comment: "ArticleComment", owner: articleOwner, api: commentAPI{
		list:   (*Client).apiGetArticleComments,
		create: (*Client).apiCreateArticleComment,
		update: (*Client).apiUpdateArticleComment,
		remove: (*Client).apiDeleteArticleComment,
	}}
}

func (h commentTarget) keepsDeleted() bool {
	return h.api.getComment != nil
}

func commentTargetOf(kind ownerKind) commentTarget {
	if kind == articleOwner {
		return articleCommentTarget()
	}
	return issueCommentTarget()
}

const commentsOfAShow = "is filled by --comments, which settles how many comments are printed and what each of " +
	"them holds"

func (h commentTarget) commentsOfAList() string {
	return fmt.Sprintf("holds the comments of an %s, which are printed a record at a time by ytrack comment "+
		"list, and with the %s itself by ytrack %s show --comments", h.owner, h.owner, h.owner)
}

func (h commentTarget) commentsOfAWrite() string {
	return fmt.Sprintf("holds the comments of an %s, which no write changes; they are printed by ytrack %s "+
		"show --comments", h.owner, h.owner)
}

func (h commentTarget) reject(spec *schemas, expression string, requested []requestedField, because string) *Error {
	path, written := firstFieldNamed(spec, h.schema, h.schema, commentsKey, requested, nil)
	if !written {
		return nil
	}
	message := fmt.Sprintf("fields %s: %s %s", quote(expression), path, because)
	return &Error{Code: CodeBadUsage, Message: message}
}

func commentOutputFields() []requestedField {
	return []requestedField{
		{name: idKey},
		{name: "author", children: []requestedField{{name: "login"}}},
		{name: "created"},
		{name: textKey},
	}
}

func CreateComment(id, text string, expression string) (Call, *Error) {
	at, fault := parseOwner(id)
	if fault != nil {
		return nil, fault
	}
	written, fault := parseCommentText(text)
	if fault != nil {
		return nil, fault
	}
	requested, fault := commentFields(expression)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	return func(ctx context.Context, c *Client) (*Node, *Error) {
		return c.createComment(ctx, spec, at, written, requested)
	}, nil
}

func (c *Client) createComment(ctx context.Context, spec *schemas, at owner, written commentCreate, requested []requestedField) (*Node, *Error) {
	held := commentTargetOf(at.kind)
	body := written.body()
	return c.write(ctx, spec, held.comment, withFields(requested, written.verifyFields()...), func(ctx context.Context, fields string) (*http.Response, error) {
		return held.api.create(c, ctx, at, body, fields)
	}, written.verify, writeResultNode(requested))
}

func UpdateComment(id, comment, text string, expression string) (Call, *Error) {
	at, fault := parseOwner(id)
	if fault != nil {
		return nil, fault
	}
	which, fault := parseChildID(commentKey, commentOwnerNoun, comment)
	if fault != nil {
		return nil, fault
	}
	written, fault := parseCommentText(text)
	if fault != nil {
		return nil, fault
	}
	requested, fault := commentFields(expression)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	rewritten := commentUpdate{commentCreate: written, at: which}
	return func(ctx context.Context, c *Client) (*Node, *Error) {
		return c.updateComment(ctx, spec, at, rewritten, requested)
	}, nil
}

func (c *Client) updateComment(ctx context.Context, spec *schemas, at owner, written commentUpdate, requested []requestedField) (*Node, *Error) {
	held := commentTargetOf(at.kind)
	if held.keepsDeleted() {
		if fault := c.checkCommentNotDeleted(ctx, spec, held, at, written.at); fault != nil {
			return nil, fault
		}
	}
	body := written.body()
	return c.write(ctx, spec, held.comment, withFields(requested, written.verifyFields()...), func(ctx context.Context, fields string) (*http.Response, error) {
		return held.api.update(c, ctx, at, written.at, body, fields)
	}, written.verify, writeResultNode(requested))
}

func (c *Client) checkCommentNotDeleted(ctx context.Context, spec *schemas, held commentTarget, at owner, comment childID) *Error {
	a, fault := c.request(ctx, spec, held.comment, []requestedField{{name: deletedKey}}, func(ctx context.Context, fields string) (*http.Response, error) {
		return held.api.getComment(c, ctx, at, comment, fields)
	})
	if fault != nil {
		return fault
	}
	gone, fault := held.deleted(a, a.objects[0])
	if fault != nil {
		return fault
	}
	if !gone {
		return nil
	}
	details := []Pair{
		requestDetail(a.httpResponse.Request.Method, a.httpResponse.Request.URL.Redacted()),
		{Key: commentKey, Value: NewString(comment.String())},
	}
	return &Error{Code: CodeBadUsage, Message: deletedCommentMessage, Details: details}
}

const deletedCommentMessage = "the comment was taken back by whoever wrote it, and YouTrack takes a write into such " +
	"a comment without a word: the text would be changed where nothing prints it and the answer would carry " +
	"none. A comment taken back is removed for good by ytrack comment delete and changed by nothing at all"

func DeleteComment(id, comment string) (Call, *Error) {
	at, fault := parseOwner(id)
	if fault != nil {
		return nil, fault
	}
	which, fault := parseChildID(commentKey, commentOwnerNoun, comment)
	if fault != nil {
		return nil, fault
	}
	return func(ctx context.Context, c *Client) (*Node, *Error) {
		return c.deleteComment(ctx, at, which)
	}, nil
}

func (c *Client) deleteComment(ctx context.Context, at owner, comment childID) (*Node, *Error) {
	held := commentTargetOf(at.kind)
	if fault := writeEmpty(ctx, func(ctx context.Context) (*http.Response, error) {
		return held.api.remove(c, ctx, at, comment)
	}); fault != nil {
		return nil, fault
	}
	return NewMap(Pair{Key: idKey, Value: NewString(comment.String())}), nil
}

func commentFields(expression string) ([]requestedField, *Error) {
	if expression == "" {
		return parseDefault(CommentFields, false)
	}
	return parseFields(expression, CommentFields)
}

func (h commentTarget) listed() []requestedField {
	held := commentOutputFields()
	if h.keepsDeleted() {
		held = append(held, requestedField{name: deletedKey})
	}
	return held
}

func CommentListFields() string {
	return formatFields(issueCommentTarget().listed())
}

func ListComments(id string, expression string, page Page) (Call, *Error) {
	at, fault := parseOwner(id)
	if fault != nil {
		return nil, fault
	}
	page, fault = page.parse()
	if fault != nil {
		return nil, fault
	}
	held := commentTargetOf(at.kind)
	defaults := formatFields(held.listed())
	requested, fault := parseDefault(defaults, false)
	if expression != "" {
		requested, fault = parseFields(expression, defaults)
	}
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	return func(ctx context.Context, c *Client) (*Node, *Error) {
		return c.listComments(ctx, spec, held, at, requested, page)
	}, nil
}

func (c *Client) listComments(ctx context.Context, spec *schemas, held commentTarget, at owner, requested []requestedField, page Page) (*Node, *Error) {
	return c.listPage(ctx, spec, commentsKey, "[]"+held.comment, requested, page, func(ctx context.Context, fields string, w window) (*http.Response, error) {
		return held.api.list(c, ctx, at, fields, w)
	})
}

func (c Comments) merged(h commentTarget, asked []requestedField) []requestedField {
	if !c.asked() {
		return asked
	}
	return withFields(asked, requestedField{name: commentsKey, children: h.listed()})
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
		return nil, shapeFailure(a.httpResponse, a.body, fmt.Sprintf("the comments of the %s arrived as something other than an array", h.owner))
	}
	kept := make([]datedComment, 0, len(received))
	for _, item := range received {
		comment, isObject := item.(map[string]any)
		if !isObject {
			return nil, shapeFailure(a.httpResponse, a.body, fmt.Sprintf("a comment of the %s arrived as something other than an object", h.owner))
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
			return nil, shapeFailure(a.httpResponse, a.body, notAnInstant("created"))
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
	printed, fault := newConverter(a, blockLayout).objectsAt(h.comment, commentOutputFields(), objects)
	if fault != nil {
		return nil, fault
	}
	return NewList(printed...), nil
}

func (h commentTarget) deleted(a decodedResponse, comment map[string]any) (bool, *Error) {
	if !h.keepsDeleted() {
		return false, nil
	}
	gone, isFlag := comment[deletedKey].(bool)
	if !isFlag {
		return false, shapeFailure(a.httpResponse, a.body, "whether a comment is deleted arrived as neither true nor false")
	}
	return gone, nil
}
