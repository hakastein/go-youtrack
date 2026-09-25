package youtrack

import (
	"context"
	"encoding/json"
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
	projectSchema       = "Project"
	fieldBasedCondition = "FieldBasedCondition"
)

type idBody struct {
	ID string `json:"id"`
}

func optionalJSON[T any](clears bool, value *T) json.RawMessage {
	switch {
	case clears:
		return json.RawMessage("null")
	case value == nil:
		return nil
	}
	encoded, _ := json.Marshal(*value)
	return encoded
}

func writeResultNode(requested []requestedField) func(decodedResponse) (*Node, *Error) {
	return func(a decodedResponse) (*Node, *Error) {
		return objectNode(a, requested, a.objects[0], nil)
	}
}

func (c *Client) deleteOwner(ctx context.Context, kind ownerKind, schema string,
	read func(ctx context.Context, fields string) (*http.Response, error),
	destroy func(ctx context.Context, at readableID) (*http.Response, error),
) (*Node, *Error) {
	return c.deleteAsRead(ctx, schema, []requestedField{{name: idReadableKey}}, read, func(a decodedResponse) (readableID, *Error) {
		return readableIDOf(a, kind, "a deletion")
	}, destroy)
}

func (c *Client) deleteAsRead(ctx context.Context, schema string, requested []requestedField,
	read func(ctx context.Context, fields string) (*http.Response, error),
	address func(decodedResponse) (readableID, *Error),
	destroy func(ctx context.Context, at readableID) (*http.Response, error),
) (*Node, *Error) {
	decoded, fault := c.request(ctx, schema, requested, read)
	if fault != nil {
		return nil, fault
	}
	at, fault := address(decoded)
	if fault != nil {
		return nil, fault
	}
	if fault := writeEmpty(ctx, func(ctx context.Context) (*http.Response, error) {
		return destroy(ctx, at)
	}); fault != nil {
		return nil, fault
	}
	return objectNode(decoded, requested, decoded.objects[0], nil)
}

func childOwner(noun string, asked childID, kind ownerKind) func(decodedResponse) (readableID, *Error) {
	return func(a decodedResponse) (readableID, *Error) {
		child := a.objects[0]
		if received, isText := child[idKey].(string); !isText || received != asked.id {
			return readableID{}, a.invalid(fmt.Sprintf("the %s asked for under id %s arrived under another id", noun, quote(asked.String())))
		}
		holder, isObject := child[kind.String()].(map[string]any)
		if !isObject {
			return readableID{}, a.invalid(fmt.Sprintf("the %s the %s hangs from arrived as something other than an object", kind, noun))
		}
		return readableIDAt(a, holder, kind, "a deletion")
	}
}

func rejectReplaced(what, text, empty string, replacements []charReplacement) *Error {
	if text == "" {
		return &Error{Code: CodeBadUsage, Message: what + " " + empty}
	}
	return rejectRewritten(what, text, replacements...)
}

func rejectRewritten(what, text string, replacements ...charReplacement) *Error {
	if reason := rewrittenReason(what, text, replacements...); reason != "" {
		return &Error{Code: CodeBadUsage, Message: reason}
	}
	return nil
}

func rewrittenReason(what, text string, replacements ...charReplacement) string {
	if !utf8.ValidString(text) {
		return fmt.Sprintf("%s is no valid UTF-8, and every byte of it that is none would reach YouTrack as %s",
			what, quote(string(utf8.RuneError)))
	}
	for _, rewritten := range replacements {
		if strings.ContainsRune(text, rewritten.rune) {
			return fmt.Sprintf("%s holds U+%04X, which YouTrack stores as %s", what, rewritten.rune, rewritten.into)
		}
	}
	return ""
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

func emptyObjectMismatch(wrong []mismatch, field string, value any, shownBy string) []mismatch {
	if value == nil {
		return wrong
	}
	return append(wrong, mismatch{field: field, expected: NewNull(), actual: rawValueNode(memberOf(value, shownBy))})
}

func mismatchFault(a decodedResponse, wrong []mismatch, identity ...Pair) *Error {
	if len(wrong) == 0 {
		return nil
	}
	entries := make([]*Node, 0, len(wrong))
	for _, m := range wrong {
		entries = append(entries, NewMap(
			Pair{Key: fieldKey, Value: NewString(m.field)},
			Pair{Key: "expected", Value: m.expected},
			Pair{Key: "actual", Value: m.actual}))
	}
	message := "the write went through and the values under mismatch came back as something other than what was written"
	return a.fault(CodeUpstreamInvalid, message, append(identity, Pair{Key: "mismatch", Value: NewList(entries...)})...)
}

func responseID(a decodedResponse, name string) *Node {
	id, isText := a.objects[0][name].(string)
	if !isText {
		return NewNull()
	}
	return NewString(id)
}
