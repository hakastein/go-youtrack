package youtrack

import (
	"encoding/json"
)

type commentCreate struct {
	text string
}

func parseCommentText(text string) (commentCreate, *Error) {
	if fault := rejectReplaced("--"+textKey, text, textOfAComment, nil); fault != nil {
		return commentCreate{}, fault
	}
	return commentCreate{text: text}, nil
}

const textOfAComment = "is empty, and a comment is the text of it: YouTrack answers an empty one on an issue " +
	"with a refusal of its own and keeps an empty comment on an article, so ytrack writes neither"

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
