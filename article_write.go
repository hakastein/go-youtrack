package youtrack

import (
	"encoding/json"
	"strings"
)

// ArticleInput is a new article: empty Content files it with no content, and empty Parent at the root of the
// project rather than under an article of it.
type ArticleInput struct {
	Summary string
	Content string
	Parent  string
}

// ArticleUpdate: a nil part stays as the article holds it. ClearContent empties the content and ClearParent moves
// the article to the root of its project; each is refused beside the part it empties.
type ArticleUpdate struct {
	Summary      *string
	Content      *string
	Parent       *string
	ClearContent bool
	ClearParent  bool
}

type articleCreate struct {
	project string
	summary string
	content string
	parent  *articleRef
}

const (
	titleOfAnArticle   = "the title of the article"
	contentOfAnArticle = "the content of the article"
)

func checkArticleInput(in ArticleInput) *Error {
	if fault := rejectReplaced(titleOfAnArticle, in.Summary, emptyTitle, lineBreakRewrites()); fault != nil {
		return fault
	}
	if fault := rejectRewritten(contentOfAnArticle, in.Content); fault != nil {
		return fault
	}
	if in.Parent == "" {
		return nil
	}
	_, fault := parseArticleID(in.Parent)
	return fault
}

func lineBreakRewrites() []charReplacement {
	return []charReplacement{
		{rune: '\n', into: "a space"},
		{rune: '\r', into: "a space"},
	}
}

const (
	emptyTitle   = "is empty, and YouTrack files no article without a title"
	emptyContent = "is empty, and YouTrack keeps empty content as none: an update empties the content outright " +
		"when it is asked to, and leaves content it is not given as the article holds it"
)

type articleUpdate struct {
	summary       *string
	content       *string
	clearsContent bool
	parent        *articleRef
	clearsParent  bool
}

func checkArticleUpdate(in ArticleUpdate) *Error {
	if in.Summary == nil && in.Content == nil && in.Parent == nil && !in.ClearContent && !in.ClearParent {
		return &Error{Code: CodeBadUsage, Message: nothingToWriteIntoAnArticle}
	}
	if in.ClearContent && in.Content != nil {
		return &Error{Code: CodeBadUsage, Message: contentBothWays}
	}
	if in.ClearParent && in.Parent != nil {
		return &Error{Code: CodeBadUsage, Message: parentBothWays}
	}
	if in.Parent != nil {
		if _, fault := parseArticleID(*in.Parent); fault != nil {
			return fault
		}
	}
	if in.Summary != nil {
		if fault := rejectReplaced(titleOfAnArticle, *in.Summary, emptyTitle, lineBreakRewrites()); fault != nil {
			return fault
		}
	}
	if in.Content != nil {
		if fault := rejectReplaced(contentOfAnArticle, *in.Content, emptyContent, nil); fault != nil {
			return fault
		}
	}
	return nil
}

const nothingToWriteIntoAnArticle = "the call writes nothing into the article: an update is given a title, content " +
	"or a parent to write, or content or a parent to take away, and a part it is given none of is left as the " +
	"article holds it"

const contentBothWays = "the call both writes the content of the article and empties it"

const parentBothWays = "the call both puts the article under a parent and takes its parent away"

type updateArticleBody struct {
	Summary       *string         `json:"summary,omitempty"`
	Content       json.RawMessage `json:"content,omitempty"`
	ParentArticle json.RawMessage `json:"parentArticle,omitempty"`
}

func (w articleUpdate) body() []byte {
	var parent *idBody
	if w.parent != nil {
		parent = &idBody{ID: w.parent.id}
	}
	body, _ := json.Marshal(updateArticleBody{Summary: w.summary, Content: optionalJSON(w.clearsContent, w.content),
		ParentArticle: optionalJSON(w.clearsParent, parent)})
	return body
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
		wrong = emptyObjectMismatch(wrong, parentArticleKey, article[parentArticleKey], idReadableKey)
	}
	return mismatchFault(a, wrong, Pair{Key: articleOwner.String(), Value: responseID(a, idReadableKey)})
}

type createArticleBody struct {
	Project       articleProject `json:"project"`
	Summary       string         `json:"summary"`
	Content       string         `json:"content,omitempty"`
	ParentArticle *idBody        `json:"parentArticle,omitempty"`
}

type articleProject struct {
	ShortName string `json:"shortName"`
}

func (w articleCreate) body() []byte {
	filed := createArticleBody{
		Project: articleProject{ShortName: w.project},
		Summary: w.summary,
		Content: w.content,
	}
	if w.parent != nil {
		filed.ParentArticle = &idBody{ID: w.parent.id}
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
	if w.content != "" {
		wrong = textMismatch(wrong, contentKey, w.content, article[contentKey])
	}
	wrong = projectMismatch(wrong, w.project, article[projectKey])
	if w.parent != nil {
		wrong = parentMismatch(wrong, w.parent.readable.String(), article[parentArticleKey])
	}
	return mismatchFault(a, wrong, Pair{Key: articleOwner.String(), Value: responseID(a, idReadableKey)})
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

func memberOf(value any, name string) any {
	object, isObject := value.(map[string]any)
	if !isObject {
		return nil
	}
	return object[name]
}
