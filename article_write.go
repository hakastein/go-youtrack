package youtrack

import (
	"encoding/json"
	"fmt"
	"strings"
)

type articleCreateInput struct {
	project  string
	summary  string
	content  *string
	parentID *string
}

type articleCreate struct {
	project string
	summary string
	content *string
	parent  *articleRef
}

func parseArticleCreate(code, summary string, content, parent *string) (articleCreateInput, *Error) {
	if fault := rejectReplaced("--"+summaryKey, summary, summaryOfAnArticle, articleTitleRewrites()); fault != nil {
		return articleCreateInput{}, fault
	}
	if content != nil {
		if fault := rejectReplaced("--"+contentKey, *content, contentOfANewArticle, nil); fault != nil {
			return articleCreateInput{}, fault
		}
	}
	written := articleCreateInput{project: code, summary: summary, content: content}
	if parent != nil {
		id, fault := parseArticleID(*parent)
		if fault != nil {
			return articleCreateInput{}, fault
		}
		written.parentID = &id
	}
	return written, nil
}

func articleTitleRewrites() []charReplacement {
	return []charReplacement{
		{rune: '\n', into: "a space"},
		{rune: '\r', into: "a space"},
	}
}

const (
	summaryOfAnArticle   = "is empty, and YouTrack files no article without a title"
	contentOfANewArticle = "is empty, and YouTrack keeps empty content as none: leave the flag out to file the " +
		"article with no content at all"
	contentOfAnArticle = "is empty, and YouTrack keeps empty content as none: --clear content empties it " +
		"outright, and content the call does not write is left as the article holds it"
)

type articleUpdateInput struct {
	summary       *string
	content       *string
	clearsContent bool
	parentID      *string
	clearsParent  bool
}

type articleUpdate struct {
	summary       *string
	content       *string
	clearsContent bool
	parent        *articleRef
	clearsParent  bool
}

func clearableArticleParts() []clearablePart[articleUpdateInput] {
	return []clearablePart[articleUpdateInput]{
		{name: contentKey, empty: func(w *articleUpdateInput) { w.clearsContent = true }},
		{name: parentKey, empty: func(w *articleUpdateInput) { w.clearsParent = true }},
	}
}

func parseArticleUpdate(summary, content, parent *string, cleared []string) (articleUpdateInput, *Error) {
	written := articleUpdateInput{summary: summary, content: content}
	if fault := written.parseClear(cleared); fault != nil {
		return articleUpdateInput{}, fault
	}
	if written.clearsContent && content != nil {
		return articleUpdateInput{}, &Error{Code: CodeBadUsage, Message: contentBothWays}
	}
	if written.clearsParent && parent != nil {
		return articleUpdateInput{}, &Error{Code: CodeBadUsage, Message: parentBothWays}
	}
	if parent != nil {
		id, fault := parseArticleID(*parent)
		if fault != nil {
			return articleUpdateInput{}, fault
		}
		written.parentID = &id
	}
	if summary != nil {
		if fault := rejectReplaced("--"+summaryKey, *summary, summaryOfAnArticle, articleTitleRewrites()); fault != nil {
			return articleUpdateInput{}, fault
		}
	}
	if content != nil {
		if fault := rejectReplaced("--"+contentKey, *content, contentOfAnArticle, nil); fault != nil {
			return articleUpdateInput{}, fault
		}
	}
	return written, nil
}

func (w *articleUpdateInput) parseClear(cleared []string) *Error {
	parts := clearableArticleParts()
	for _, name := range cleared {
		at := clearablePartIndex(parts, name)
		if at < 0 {
			message := fmt.Sprintf("--clear %s names no part of an article a call may empty: it takes %s",
				quote(name), partsOf(parts))
			return &Error{Code: CodeBadUsage, Message: message}
		}
		parts[at].empty(w)
	}
	return nil
}

const nothingToWriteIntoAnArticle = "the call writes nothing into the article: an update is given --summary, " +
	"--content, --parent, --clear content or --clear parent, and a part it is given none of is left as the " +
	"article holds it"

const contentBothWays = "--content writes the text of the article and --clear content empties it, and the call " +
	"gives both"

const parentBothWays = "--parent writes the article this one hangs from and --clear parent takes it away, and " +
	"the call gives both"

type updateArticleBody struct {
	Summary       *string         `json:"summary,omitempty"`
	Content       json.RawMessage `json:"content,omitempty"`
	ParentArticle json.RawMessage `json:"parentArticle,omitempty"`
}

func (w articleUpdate) body() []byte {
	body, _ := json.Marshal(updateArticleBody{Summary: w.summary, Content: w.text(), ParentArticle: w.parentJSON()})
	return body
}

func (w articleUpdate) parentJSON() json.RawMessage {
	switch {
	case w.clearsParent:
		return json.RawMessage("null")
	case w.parent == nil:
		return nil
	}
	encoded, _ := json.Marshal(articleIDBody{ID: w.parent.id})
	return encoded
}

func (w articleUpdate) text() json.RawMessage {
	switch {
	case w.clearsContent:
		return json.RawMessage("null")
	case w.content == nil:
		return nil
	}
	encoded, _ := json.Marshal(*w.content)
	return encoded
}

func (w articleUpdate) verifyFields() []requestedField {
	own := []requestedField{{name: idReadableKey}}
	if w.summary != nil {
		own = append(own, requestedField{name: summaryKey})
	}
	if w.content != nil || w.clearsContent {
		own = append(own, requestedField{name: contentKey})
	}
	if w.parent != nil || w.clearsParent {
		own = append(own, requestedField{name: parentArticleKey, children: []requestedField{{name: idReadableKey}}})
	}
	return own
}

func (w articleUpdate) verify(a decodedResponse) *Error {
	article := a.objects[0]
	var wrong []mismatch
	if w.summary != nil {
		wrong = textMismatch(wrong, summaryKey, *w.summary, article[summaryKey])
	}
	switch {
	case w.content != nil:
		wrong = textMismatch(wrong, contentKey, *w.content, article[contentKey])
	case w.clearsContent:
		wrong = emptyMismatch(wrong, contentKey, article[contentKey])
	}
	switch {
	case w.parent != nil:
		wrong = parentMismatch(wrong, w.parent.readable.String(), article[parentArticleKey])
	case w.clearsParent:
		wrong = noParentMismatch(wrong, article[parentArticleKey])
	}
	if len(wrong) == 0 {
		return nil
	}
	return mismatchFault(a, knownAs(articleOwner.String(), responseID(a, idReadableKey)), wrong)
}

type createArticleBody struct {
	Project       articleProject `json:"project"`
	Summary       string         `json:"summary"`
	Content       *string        `json:"content,omitempty"`
	ParentArticle *articleIDBody `json:"parentArticle,omitempty"`
}

type articleProject struct {
	ShortName string `json:"shortName"`
}

type articleIDBody struct {
	ID string `json:"id"`
}

func (w articleCreate) body() []byte {
	filed := createArticleBody{
		Project: articleProject{ShortName: w.project},
		Summary: w.summary,
		Content: w.content,
	}
	if w.parent != nil {
		filed.ParentArticle = &articleIDBody{ID: w.parent.id}
	}
	body, _ := json.Marshal(filed)
	return body
}

func (w articleCreate) verifyFields() []requestedField {
	own := []requestedField{
		{name: idReadableKey},
		{name: summaryKey},
		{name: contentKey},
		{name: projectKey, children: []requestedField{{name: shortNameKey}}},
	}
	if w.parent != nil {
		own = append(own, requestedField{name: parentArticleKey, children: []requestedField{{name: idReadableKey}}})
	}
	return own
}

func (w articleCreate) verify(a decodedResponse) *Error {
	article := a.objects[0]
	wrong := textMismatch(nil, summaryKey, w.summary, article[summaryKey])
	if w.content != nil {
		wrong = textMismatch(wrong, contentKey, *w.content, article[contentKey])
	}
	wrong = projectMismatch(wrong, w.project, article[projectKey])
	if w.parent != nil {
		wrong = parentMismatch(wrong, w.parent.readable.String(), article[parentArticleKey])
	}
	if len(wrong) == 0 {
		return nil
	}
	return mismatchFault(a, knownAs(articleOwner.String(), responseID(a, idReadableKey)), wrong)
}

func projectMismatch(wrong []mismatch, code string, value any) []mismatch {
	received := memberOf(value, shortNameKey)
	if kept, isText := received.(string); isText && strings.EqualFold(kept, code) {
		return wrong
	}
	return append(wrong, mismatch{field: projectKey, expected: NewString(code), actual: rawValueNode(received)})
}

func parentMismatch(wrong []mismatch, readable string, value any) []mismatch {
	received := memberOf(value, idReadableKey)
	if kept, isText := received.(string); isText && kept == readable {
		return wrong
	}
	return append(wrong, mismatch{field: parentArticleKey, expected: NewString(readable), actual: rawValueNode(received)})
}

func noParentMismatch(wrong []mismatch, value any) []mismatch {
	if value == nil {
		return wrong
	}
	received := rawValueNode(memberOf(value, idReadableKey))
	return append(wrong, mismatch{field: parentArticleKey, expected: NewNull(), actual: received})
}

func memberOf(value any, name string) any {
	object, isObject := value.(map[string]any)
	if !isObject {
		return nil
	}
	return object[name]
}
