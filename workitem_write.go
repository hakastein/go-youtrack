package youtrack

import (
	"encoding/json"
	"fmt"
	"time"
)

type WorkItemInput struct {
	// Whole minutes; zero is left for the server to judge.
	Duration time.Duration
	// A calendar day, as 2026-09-16, or its midnight UTC, as a work item reads it back; empty for the day the server
	// takes for today.
	Date string
	// Empty writes no text.
	Text string
	// A type of work of the project, named in any letter case, an exact spelling settling a tie; empty for none.
	Type       string
	Attributes []AttributeWrite
}

// WorkItemUpdate: a nil part is left as the work item holds it. Duration and Date take what WorkItemInput does and
// are never emptied, since every work item holds both.
type WorkItemUpdate struct {
	Duration *time.Duration
	Date     *string
	// An empty text is written as one; ClearText takes the text away.
	Text      *string
	Type      *string
	ClearText bool
	ClearType bool
	// An attribute of the work item the update does not name is left as it stands.
	Attributes []AttributeWrite
}

type workItemCreateInput struct {
	spent      workDuration
	day        *workDate
	text       *string
	workType   *string
	attributes []AttributeWrite
}

type workDuration struct {
	minutes int64
}

type workDate struct {
	text string
	noon int64
}

type workItemCreate struct {
	input      workItemCreateInput
	workType   *resolvedWorkType
	attributes []resolvedAttribute
}

type resolvedWorkType struct {
	id   string
	name string
}

func parseWorkItemCreate(in WorkItemInput) (workItemCreateInput, *Error) {
	spent, fault := parseWorkDuration(in.Duration)
	if fault != nil {
		return workItemCreateInput{}, fault
	}
	if fault := rejectNoUTF8(workItemText, in.Text); fault != nil {
		return workItemCreateInput{}, fault
	}
	written := workItemCreateInput{spent: spent, text: workItemOptional(in.Text), workType: workItemOptional(in.Type),
		attributes: in.Attributes}
	if in.Date != "" {
		against, fault := parseWorkDate(in.Date)
		if fault != nil {
			return workItemCreateInput{}, fault
		}
		written.day = &against
	}
	if fault := checkAttributes(in.Attributes); fault != nil {
		return workItemCreateInput{}, fault
	}
	return written, nil
}

func workItemOptional(text string) *string {
	if text == "" {
		return nil
	}
	return &text
}

const workItemText = "the text of the work item"

func parseWorkDuration(spent time.Duration) (workDuration, *Error) {
	switch {
	case spent < 0:
		message := fmt.Sprintf("the duration %s is negative, and a work item holds a whole number of minutes", spent)
		return workDuration{}, &Error{Code: CodeBadUsage, Message: message}
	case spent%time.Minute != 0:
		message := fmt.Sprintf("the duration %s is no whole number of minutes: YouTrack keeps a work item as the "+
			"minutes it comes to, and a second is no part of what a work item holds", spent)
		return workDuration{}, &Error{Code: CodeBadUsage, Message: message}
	}
	return workDuration{minutes: int64(spent / time.Minute)}, nil
}

func parseWorkDate(text string) (workDate, *Error) {
	if day, err := time.Parse(time.DateOnly, text); err == nil {
		return workDate{text: text, noon: noonUTC(day)}, nil
	}
	moment, err := time.Parse(time.RFC3339, text)
	if err != nil {
		return workDate{}, badWorkDate(text, "is neither a calendar day, as in 2026-09-01, nor midnight UTC of one, "+
			"as in 2026-09-01T00:00:00Z: a work item is written against a day, and YouTrack keeps no moment of it")
	}
	utc := moment.UTC()
	midnight := time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
	if !utc.Equal(midnight) {
		return workDate{}, badWorkDate(text, "names a time of day, and a work item is written against a day: "+
			"YouTrack would file the moment under the calendar day of the time zone of whoever wrote it, which is "+
			"not the caller's to know")
	}
	if _, offset := moment.Zone(); offset != 0 {
		return workDate{}, badWorkDate(text, "is midnight UTC written in an offset of its own, and a day goes in as "+
			"the day it is written in: a calendar day, as in 2026-09-01, or midnight UTC of one with Z or +00:00 on "+
			"it, as a work item reads it back")
	}
	return workDate{text: text, noon: noonUTC(midnight)}, nil
}

func badWorkDate(text, because string) *Error {
	return &Error{Code: CodeBadUsage, Message: fmt.Sprintf("the day %s %s", quote(text), because)}
}

type createWorkItemBody struct {
	Duration   minutesBody     `json:"duration"`
	Type       *workItemIDBody `json:"type,omitempty"`
	Date       *int64          `json:"date,omitempty"`
	Text       *string         `json:"text,omitempty"`
	Attributes []attributeBody `json:"attributes,omitempty"`
}

type workItemIDBody struct {
	ID string `json:"id"`
}

func (w workItemCreate) body() []byte {
	written := createWorkItemBody{Duration: minutesBody{Minutes: w.input.spent.minutes}, Text: w.input.text,
		Attributes: attributeBodies(w.attributes)}
	if w.workType != nil {
		written.Type = &workItemIDBody{ID: w.workType.id}
	}
	if w.input.day != nil {
		written.Date = &w.input.day.noon
	}
	body, _ := json.Marshal(written)
	return body
}

func (w workItemCreateInput) verifyFields() []requestedField {
	return []requestedField{
		{name: idKey},
		{name: durationKey},
		{name: dateKey},
		{name: textKey},
		{name: issueOwner.String(), children: []requestedField{{name: idReadableKey}}},
	}
}

func verifyFieldsWithSettings(own []requestedField, workType *resolvedWorkType, attributes []resolvedAttribute) []requestedField {
	if workType != nil {
		own = append(own, requestedField{name: typeKey, children: []requestedField{{name: idKey}, {name: nameKey}}})
	}
	if len(attributes) > 0 {
		own = append(own, requestedField{name: attributesKey})
	}
	return own
}

func verifyWorkItem(a decodedResponse, wrong []mismatch, workType *resolvedWorkType, attributes []resolvedAttribute, identity []Pair) *Error {
	if workType != nil {
		wrong = workType.verify(wrong, a.objects[0][typeKey])
	}
	wrong = attributeMismatches(wrong, attributes, a.objects[0][attributesKey])
	return mismatchFault(a, wrong, identity...)
}

func (w workItemCreate) verifyFields() []requestedField {
	return verifyFieldsWithSettings(w.input.verifyFields(), w.workType, w.attributes)
}

func (w workItemCreateInput) diff(item map[string]any) []mismatch {
	wrong := w.spent.verify(nil, item[durationKey])
	if w.day != nil {
		wrong = w.day.verify(wrong, item[dateKey])
	}
	if w.text != nil {
		wrong = textMismatch(wrong, textKey, *w.text, item[textKey])
	}
	return wrong
}

func (w workItemCreate) verify(a decodedResponse) *Error {
	return verifyWorkItem(a, w.input.diff(a.objects[0]), w.workType, w.attributes, []Pair{
		{Key: issueOwner.String(), Value: owningIssue(a)},
		{Key: idKey, Value: responseID(a, idKey)},
	})
}

func (t resolvedWorkType) verify(wrong []mismatch, value any) []mismatch {
	if kept, isText := memberOf(value, idKey).(string); isText && kept == t.id {
		return wrong
	}
	return append(wrong, mismatch{
		field:    typeKey,
		expected: NewString(t.name),
		actual:   rawValueNode(memberOf(value, nameKey)),
	})
}

func (d workDuration) verify(wrong []mismatch, value any) []mismatch {
	written := NewString(duration(d.minutes))
	held, isObject := value.(map[string]any)
	if isObject {
		if minutes, isWhole := parseInt64(held[minutesKey]); isWhole {
			if minutes == d.minutes {
				return wrong
			}
			return append(wrong, mismatch{field: durationKey, expected: written, actual: NewString(duration(minutes))})
		}
	}
	return append(wrong, mismatch{field: durationKey, expected: written, actual: NewNull()})
}

func (d workDate) verify(wrong []mismatch, value any) []mismatch {
	at, isInstant := parseInt64(value)
	if isInstant && sameDayUTC(at, d.noon) {
		return wrong
	}
	received := NewNull()
	if isInstant {
		received = NewString(formatMoment(at))
	}
	return append(wrong, mismatch{field: dateKey, expected: NewString(d.text), actual: received})
}

func sameDayUTC(a, b int64) bool {
	return time.UnixMilli(a).UTC().Format(time.DateOnly) == time.UnixMilli(b).UTC().Format(time.DateOnly)
}

func owningIssue(a decodedResponse) *Node {
	issue, isObject := a.objects[0][issueOwner.String()].(map[string]any)
	if !isObject {
		return NewNull()
	}
	readable, isText := issue[idReadableKey].(string)
	if !isText {
		return NewNull()
	}
	return NewString(readable)
}

type workItemUpdateInput struct {
	spent      *workDuration
	day        *workDate
	text       *string
	workType   *string
	clearsType bool
	clearsText bool
	attributes []AttributeWrite
}

type workItemUpdate struct {
	input      workItemUpdateInput
	issue      string
	at         childID
	workType   *resolvedWorkType
	attributes []resolvedAttribute
}

func parseWorkItemUpdate(in WorkItemUpdate) (workItemUpdateInput, *Error) {
	if in.Duration == nil && in.Date == nil && in.Text == nil && in.Type == nil && !in.ClearText && !in.ClearType &&
		len(in.Attributes) == 0 {
		return workItemUpdateInput{}, &Error{Code: CodeBadUsage, Message: nothingToWriteIntoAWorkItem}
	}
	if in.ClearType && in.Type != nil {
		return workItemUpdateInput{}, &Error{Code: CodeBadUsage, Message: typeBothWays}
	}
	if in.ClearText && in.Text != nil {
		return workItemUpdateInput{}, &Error{Code: CodeBadUsage, Message: textBothWays}
	}
	written := workItemUpdateInput{clearsType: in.ClearType, clearsText: in.ClearText, attributes: in.Attributes}
	if in.Text != nil {
		if fault := rejectNoUTF8(workItemText, *in.Text); fault != nil {
			return workItemUpdateInput{}, fault
		}
		text := *in.Text
		written.text = &text
	}
	if in.Duration != nil {
		spent, fault := parseWorkDuration(*in.Duration)
		if fault != nil {
			return workItemUpdateInput{}, fault
		}
		written.spent = &spent
	}
	if in.Date != nil {
		against, fault := parseWorkDate(*in.Date)
		if fault != nil {
			return workItemUpdateInput{}, fault
		}
		written.day = &against
	}
	if in.Type != nil {
		if *in.Type == "" {
			return workItemUpdateInput{}, &Error{Code: CodeBadUsage, Message: unnamedWorkItemType}
		}
		named := *in.Type
		written.workType = &named
	}
	if fault := checkAttributes(in.Attributes); fault != nil {
		return workItemUpdateInput{}, fault
	}
	return written, nil
}

const nothingToWriteIntoAWorkItem = "the update writes nothing into the work item: it names no part to write and " +
	"none to empty, and a part it names none of is left as the work item holds it"

const typeBothWays = "the update both writes the type of work of the work item and takes it away"

const textBothWays = "the update both writes the text of the work item and empties it"

const unnamedWorkItemType = "the type of work to write has no name: the types a work item may be written against " +
	"are the time tracking settings of its project, and a type is taken away by emptying it"

type updateWorkItemBody struct {
	Duration   *minutesBody    `json:"duration,omitempty"`
	Type       json.RawMessage `json:"type,omitempty"`
	Date       *int64          `json:"date,omitempty"`
	Text       json.RawMessage `json:"text,omitempty"`
	Attributes []attributeBody `json:"attributes,omitempty"`
}

func (w workItemUpdate) body() []byte {
	changed := updateWorkItemBody{Type: w.typeJSON(), Text: w.input.textJSON(), Attributes: attributeBodies(w.attributes)}
	if w.input.spent != nil {
		changed.Duration = &minutesBody{Minutes: w.input.spent.minutes}
	}
	if w.input.day != nil {
		changed.Date = &w.input.day.noon
	}
	body, _ := json.Marshal(changed)
	return body
}

func (w workItemUpdate) typeJSON() json.RawMessage {
	switch {
	case w.input.clearsType:
		return json.RawMessage("null")
	case w.workType == nil:
		return nil
	}
	encoded, _ := json.Marshal(workItemIDBody{ID: w.workType.id})
	return encoded
}

func (w workItemUpdateInput) textJSON() json.RawMessage {
	switch {
	case w.clearsText:
		return json.RawMessage("null")
	case w.text == nil:
		return nil
	}
	encoded, _ := json.Marshal(*w.text)
	return encoded
}

func (w workItemUpdateInput) verifyFields() []requestedField {
	var own []requestedField
	if w.spent != nil {
		own = append(own, requestedField{name: durationKey})
	}
	if w.day != nil {
		own = append(own, requestedField{name: dateKey})
	}
	if w.text != nil || w.clearsText {
		own = append(own, requestedField{name: textKey})
	}
	if w.clearsType {
		own = append(own, requestedField{name: typeKey, children: []requestedField{{name: nameKey}}})
	}
	return own
}

func (w workItemUpdate) verifyFields() []requestedField {
	return verifyFieldsWithSettings(w.input.verifyFields(), w.workType, w.attributes)
}

func (w workItemUpdate) verify(a decodedResponse) *Error {
	return verifyWorkItem(a, w.input.diff(a.objects[0]), w.workType, w.attributes, []Pair{
		{Key: issueOwner.String(), Value: NewString(w.issue)},
		{Key: idKey, Value: NewString(w.at.String())},
	})
}

func (w workItemUpdateInput) diff(item map[string]any) []mismatch {
	var wrong []mismatch
	if w.spent != nil {
		wrong = w.spent.verify(wrong, item[durationKey])
	}
	if w.day != nil {
		wrong = w.day.verify(wrong, item[dateKey])
	}
	switch {
	case w.text != nil:
		wrong = textMismatch(wrong, textKey, *w.text, item[textKey])
	case w.clearsText:
		wrong = emptyMismatch(wrong, textKey, item[textKey])
	}
	if w.clearsType {
		wrong = emptyMismatch(wrong, typeKey, memberOf(item[typeKey], nameKey))
	}
	return wrong
}
