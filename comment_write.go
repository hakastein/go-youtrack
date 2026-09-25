package youtrack

import (
	"context"
	"encoding/json"
	"net/http"
)

func (s *CommentsService) create(ctx context.Context, owner, text string, opts WriteOptions) (*Node, *Error) {
	at, fault := parseOwner(owner)
	if fault != nil {
		return nil, fault
	}
	written, fault := parseCommentText(text)
	if fault != nil {
		return nil, fault
	}
	requested, fault := parseFields(opts.Fields, CommentFields)
	if fault != nil {
		return nil, fault
	}
	c := s.client
	held := commentTargetOf(at.kind)
	body := written.body()
	return c.write(ctx, held.comment, withFields(requested, written.verifyFields()...), func(ctx context.Context, fields string) (*http.Response, error) {
		return held.api.create(c, ctx, at, body, fields)
	}, written.verify, writeResultNode(requested))
}

func (s *CommentsService) update(ctx context.Context, owner, id, text string, opts WriteOptions) (*Node, *Error) {
	at, fault := parseOwner(owner)
	if fault != nil {
		return nil, fault
	}
	which, fault := parseChildID(commentKey, commentOwnerNoun, id)
	if fault != nil {
		return nil, fault
	}
	rewritten, fault := parseCommentText(text)
	if fault != nil {
		return nil, fault
	}
	requested, fault := parseFields(opts.Fields, CommentFields)
	if fault != nil {
		return nil, fault
	}
	c := s.client
	held := commentTargetOf(at.kind)
	if held.keepsDeleted() {
		if fault := c.checkCommentNotDeleted(ctx, held, at, which); fault != nil {
			return nil, fault
		}
	}
	written := commentUpdate{commentCreate: rewritten, at: which}
	body := written.body()
	return c.write(ctx, held.comment, withFields(requested, written.verifyFields()...), func(ctx context.Context, fields string) (*http.Response, error) {
		return held.api.update(c, ctx, at, written.at, body, fields)
	}, written.verify, writeResultNode(requested))
}

func (c *Client) checkCommentNotDeleted(ctx context.Context, held commentTarget, at owner, comment childID) *Error {
	a, fault := c.request(ctx, held.comment, []requestedField{{name: deletedKey}}, func(ctx context.Context, fields string) (*http.Response, error) {
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
	"a comment without a word: the text would be changed where nothing shows it and the answer would carry " +
	"none. A comment taken back can be deleted for good and changed by nothing at all"

func (s *CommentsService) delete(ctx context.Context, owner, id string) (*Node, *Error) {
	at, fault := parseOwner(owner)
	if fault != nil {
		return nil, fault
	}
	which, fault := parseChildID(commentKey, commentOwnerNoun, id)
	if fault != nil {
		return nil, fault
	}
	c := s.client
	held := commentTargetOf(at.kind)
	if fault := writeEmpty(ctx, func(ctx context.Context) (*http.Response, error) {
		return held.api.remove(c, ctx, at, which)
	}); fault != nil {
		return nil, fault
	}
	return NewMap(Pair{Key: idKey, Value: NewString(which.String())}), nil
}

type commentCreate struct {
	text string
}

func parseCommentText(text string) (commentCreate, *Error) {
	if fault := rejectReplaced("the text of the comment", text, textOfAComment, nil); fault != nil {
		return commentCreate{}, fault
	}
	return commentCreate{text: text}, nil
}

const textOfAComment = "is empty, and a comment is nothing but its text: YouTrack refuses an empty one on an issue " +
	"and keeps it on an article, so neither is written"

type commentBody struct {
	Text string `json:"text"`
}

func (w commentCreate) body() []byte {
	body, _ := json.Marshal(commentBody{Text: w.text})
	return body
}

func (w commentCreate) verifyFields() []requestedField {
	return []requestedField{{name: idKey}, {name: textKey}}
}

func (w commentCreate) verifyText(a decodedResponse, named *Node) *Error {
	wrong := textMismatch(nil, textKey, w.text, a.objects[0][textKey])
	if len(wrong) == 0 {
		return nil
	}
	return mismatchFault(a, knownAs(commentKey, named), wrong)
}

func (w commentCreate) verify(a decodedResponse) *Error {
	return w.verifyText(a, responseID(a, idKey))
}

type commentUpdate struct {
	commentCreate
	at childID
}

func (w commentUpdate) verifyFields() []requestedField {
	return []requestedField{{name: textKey}}
}

func (w commentUpdate) verify(a decodedResponse) *Error {
	return w.verifyText(a, NewString(w.at.String()))
}
