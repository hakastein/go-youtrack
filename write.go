package youtrack

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"
)

// WriteOptions: Fields is the fields= expression of what a write answers with, the entity written or, for
// Links.Add, each linked issue; empty for the defaults of the operation and +x for them and x.
type WriteOptions struct {
	Fields string
}

const (
	summaryKey          = "summary"
	descriptionKey      = "description"
	contentKey          = "content"
	projectKey          = "project"
	shortNameKey        = "shortName"
	parentArticleKey    = "parentArticle"
	parentKey           = "parent"
	projectSchema       = "Project"
	fieldBasedCondition = "FieldBasedCondition"
)

func writeResultNode(requested []requestedField) func(decodedResponse) (*Node, *Error) {
	return func(a decodedResponse) (*Node, *Error) {
		return objectNode(a, requested, a.objects[0], nil)
	}
}

func (c *Client) deleteOwner(ctx context.Context, kind ownerKind, schema string,
	read func(ctx context.Context, fields string) (*http.Response, error),
	destroy func(ctx context.Context, at readableID) (*http.Response, error),
) (*Node, *Error) {
	requested := []requestedField{{name: idReadableKey}}
	decoded, fault := c.request(ctx, schema, requested, read)
	if fault != nil {
		return nil, fault
	}
	readable, fault := readableIDAt(decoded, decoded.objects[0], kind, "a deletion")
	if fault != nil {
		return nil, fault
	}
	if fault := writeEmpty(ctx, func(ctx context.Context) (*http.Response, error) {
		return destroy(ctx, readable)
	}); fault != nil {
		return nil, fault
	}
	return objectNode(decoded, requested, decoded.objects[0], nil)
}

func rejectReplaced(flag, text, empty string, replacements []charReplacement) *Error {
	if text == "" {
		return &Error{Code: CodeBadUsage, Message: flag + " " + empty}
	}
	if fault := rejectNoUTF8(flag, text); fault != nil {
		return fault
	}
	for _, rewritten := range replacements {
		if strings.ContainsRune(text, rewritten.rune) {
			return &Error{Code: CodeBadUsage, Message: rewrittenAs(flag, rewritten)}
		}
	}
	return nil
}

func rejectNoUTF8(flag, text string) *Error {
	if utf8.ValidString(text) {
		return nil
	}
	return &Error{Code: CodeBadUsage, Message: noUTF8(flag)}
}

func noUTF8(what string) string {
	return fmt.Sprintf("%s is no valid UTF-8, and every byte of it that is none would reach YouTrack as %s",
		what, quote(string(utf8.RuneError)))
}

func rewrittenAs(what string, rewritten charReplacement) string {
	return fmt.Sprintf("%s holds U+%04X, which YouTrack stores as %s", what, rewritten.rune, rewritten.into)
}

type mismatch struct {
	field    string
	expected *Node
	actual   *Node
}

func textMismatch(wrong []mismatch, field, sent string, value any) []mismatch {
	if received, isText := value.(string); isText && received == sent {
		return wrong
	}
	return append(wrong, mismatch{field: field, expected: NewString(sent), actual: rawValueNode(value)})
}

func emptyMismatch(wrong []mismatch, field string, value any) []mismatch {
	if value == nil {
		return wrong
	}
	return append(wrong, mismatch{field: field, expected: NewNull(), actual: rawValueNode(value)})
}

func mismatchFault(a decodedResponse, identity []Pair, wrong []mismatch) *Error {
	entries := make([]*Node, 0, len(wrong))
	for _, m := range wrong {
		entries = append(entries, NewMap(
			Pair{Key: "field", Value: NewString(m.field)},
			Pair{Key: "expected", Value: m.expected},
			Pair{Key: "actual", Value: m.actual}))
	}
	details := append([]Pair{requestDetail(a.httpResponse.Request.Method, a.httpResponse.Request.URL.Redacted())},
		append(identity, Pair{Key: "mismatch", Value: NewList(entries...)})...)
	message := "the write went through and the values under mismatch came back as something other than what was written"
	return &Error{Code: CodeUpstreamInvalid, Message: message, Details: details}
}

func knownAs(named string, id *Node) []Pair {
	return []Pair{{Key: named, Value: id}}
}

func responseID(a decodedResponse, name string) *Node {
	id, isText := a.objects[0][name].(string)
	if !isText {
		return NewNull()
	}
	return NewString(id)
}
