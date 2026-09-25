package youtrack

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
)

type workItemCreateInput struct {
	spent      parsedDuration
	day        *workDate
	text       *string
	attributes []namedValue
}

type parsedDuration struct {
	text    string
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

func rejectWorkItemType(named *string) *Error {
	if named == nil || *named != "" {
		return nil
	}
	return invalidValueFault("--"+typeKey, *named, "names no type of work: the types an issue may be written "+
		"against are the settings of its project, printed by ytrack project show <code> under plugins")
}

func rejectWorkItemText(text *string) *Error {
	if text == nil {
		return nil
	}
	return rejectNoUTF8("--"+textKey, *text)
}

func parseWorkItemCreate(spent string, day, text, named *string, attributes []string) (workItemCreateInput, *Error) {
	length, fault := parseDuration(spent)
	if fault != nil {
		return workItemCreateInput{}, fault
	}
	if fault := rejectWorkItemText(text); fault != nil {
		return workItemCreateInput{}, fault
	}
	written := workItemCreateInput{spent: length, text: text}
	if day != nil {
		against, fault := parseWorkDate(*day)
		if fault != nil {
			return workItemCreateInput{}, fault
		}
		written.day = &against
	}
	if fault := rejectWorkItemType(named); fault != nil {
		return workItemCreateInput{}, fault
	}
	if written.attributes, fault = attributeValues(attributes); fault != nil {
		return workItemCreateInput{}, fault
	}
	return written, nil
}

func parseDuration(text string) (parsedDuration, *Error) {
	minutes, read := periodMinutes(text)
	switch {
	case !read:
		return parsedDuration{}, invalidValueFault("duration", text, "is no ISO 8601 period of hours and minutes, as "+
			"in PT1H30M, PT90M or PT0M: ytrack writes a work item as the minutes it comes to, and neither a day "+
			"nor a week is a fixed count of them — YouTrack reads P1D as the working day of the instance — while "+
			"a second and a fraction are no part of what a work item holds")
	case minutes > math.MaxInt32:
		return parsedDuration{}, invalidValueFault("duration", text,
			fmt.Sprintf("is longer than the %d minutes YouTrack keeps a work item for", math.MaxInt32))
	}
	return parsedDuration{text: text, minutes: minutes}, nil
}

func parseWorkDate(text string) (workDate, *Error) {
	if day, err := time.Parse(time.DateOnly, text); err == nil {
		return workDate{text: text, noon: noonUTC(day)}, nil
	}
	moment, err := time.Parse(time.RFC3339, text)
	if err != nil {
		return workDate{}, invalidValueFault(dateKey, text, "is neither a calendar day, as in 2026-09-01, nor "+
			"midnight UTC of one, as in 2026-09-01T00:00:00Z: a work item is written against a day, and "+
			"YouTrack keeps no moment of it")
	}
	utc := moment.UTC()
	midnight := time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
	if !utc.Equal(midnight) {
		return workDate{}, invalidValueFault(dateKey, text, "names a time of day, and a work item is written "+
			"against a day: YouTrack would file the moment under the calendar day of the time zone of whoever "+
			"wrote it, which is not the caller's to know")
	}
	if _, offset := moment.Zone(); offset != 0 {
		return workDate{}, invalidValueFault(dateKey, text, "is midnight UTC written in an offset of its own, "+
			"and a day goes in as the day it is written in: a calendar day, as in 2026-09-01, or midnight UTC "+
			"of one with Z or +00:00 on it, as ytrack prints it")
	}
	return workDate{text: text, noon: noonUTC(midnight)}, nil
}

func invalidValueFault(named, value, because string) *Error {
	return &Error{Code: CodeBadUsage, Message: fmt.Sprintf("%s %s %s", named, quote(value), because)}
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
	if len(wrong) == 0 {
		return nil
	}
	return mismatchFault(a, identity, wrong)
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

func (d parsedDuration) verify(wrong []mismatch, value any) []mismatch {
	held, isObject := value.(map[string]any)
	if isObject {
		if minutes, isWhole := parseInt64(held[minutesKey]); isWhole {
			if minutes == d.minutes {
				return wrong
			}
			return append(wrong, mismatch{
				field:    durationKey,
				expected: NewString(d.text),
				actual:   NewString(duration(minutes)),
			})
		}
	}
	return append(wrong, mismatch{field: durationKey, expected: NewString(d.text), actual: NewNull()})
}

func (d workDate) verify(wrong []mismatch, value any) []mismatch {
	at, isInstant := parseInt64(value)
	if isInstant && sameDayUTC(at, d.noon) {
		return wrong
	}
	received := NewNull()
	if isInstant {
		received = NewString(formatDateTime(at))
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
	spent            *parsedDuration
	day              *workDate
	text             *string
	attributes       []namedValue
	clearsType       bool
	clearsText       bool
	clearsAttributes []string
}

type workItemUpdate struct {
	input      workItemUpdateInput
	issue      string
	at         childID
	workType   *resolvedWorkType
	attributes []resolvedAttribute
}

func clearableWorkItemParts() []clearablePart[workItemUpdateInput] {
	return []clearablePart[workItemUpdateInput]{
		{name: typeKey, empty: func(w *workItemUpdateInput) { w.clearsType = true }},
		{name: textKey, empty: func(w *workItemUpdateInput) { w.clearsText = true }},
	}
}

func keptWorkItemParts() []string {
	return []string{durationKey, dateKey}
}

func parseWorkItemUpdate(spent, day, text, named *string, attributes, cleared []string) (workItemUpdateInput, *Error) {
	if spent == nil && day == nil && text == nil && named == nil && len(attributes) == 0 && len(cleared) == 0 {
		return workItemUpdateInput{}, &Error{Code: CodeBadUsage, Message: nothingToWriteIntoAWorkItem}
	}
	var written workItemUpdateInput
	if fault := written.parseClear(cleared); fault != nil {
		return workItemUpdateInput{}, fault
	}
	if written.clearsType && named != nil {
		return workItemUpdateInput{}, &Error{Code: CodeBadUsage, Message: typeBothWays}
	}
	if written.clearsText && text != nil {
		return workItemUpdateInput{}, &Error{Code: CodeBadUsage, Message: textBothWays}
	}
	if fault := rejectWorkItemText(text); fault != nil {
		return workItemUpdateInput{}, fault
	}
	written.text = text
	if spent != nil {
		length, fault := parseDuration(*spent)
		if fault != nil {
			return workItemUpdateInput{}, fault
		}
		written.spent = &length
	}
	if day != nil {
		against, fault := parseWorkDate(*day)
		if fault != nil {
			return workItemUpdateInput{}, fault
		}
		written.day = &against
	}
	if fault := rejectWorkItemType(named); fault != nil {
		return workItemUpdateInput{}, fault
	}
	var fault *Error
	if written.attributes, fault = attributeValues(attributes); fault != nil {
		return workItemUpdateInput{}, fault
	}
	for _, set := range written.attributes {
		if slices.ContainsFunc(written.clearsAttributes, func(name string) bool { return strings.EqualFold(name, set.name) }) {
			return workItemUpdateInput{}, attributeBothWays(set.name)
		}
	}
	return written, nil
}

func (w *workItemUpdateInput) parseClear(cleared []string) *Error {
	parts := clearableWorkItemParts()
	for _, name := range cleared {
		if at := clearablePartIndex(parts, name); at >= 0 {
			parts[at].empty(w)
			continue
		}
		if at := slices.IndexFunc(keptWorkItemParts(), func(kept string) bool {
			return strings.EqualFold(name, kept)
		}); at >= 0 {
			held := keptWorkItemParts()[at]
			message := fmt.Sprintf("--clear %s names a part every work item holds: YouTrack answers a %s of null "+
				"with Field %s cannot be null, so there is no way to empty one; --%s writes it afresh",
				quote(name), held, held, held)
			return &Error{Code: CodeBadUsage, Message: message}
		}
		if name == "" {
			message := fmt.Sprintf(`--clear "" names nothing to empty: it takes %s or the name of an attribute`, partsOf(parts))
			return &Error{Code: CodeBadUsage, Message: message}
		}
		w.clearsAttributes = append(w.clearsAttributes, name)
	}
	return nil
}

const nothingToWriteIntoAWorkItem = "the call writes nothing into the work item: an update is given --duration, " +
	"--type, --date, --text, --attribute or --clear, and a part it is given none of is left as the work item " +
	"holds it"

const typeBothWays = "--type writes the type of work of the work item and --clear type takes it away, and the " +
	"call gives both"

const textBothWays = "--text writes the text of the work item and --clear text empties it, and the call gives both"

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
