package youtrack

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// ValueType is the type of a custom field's value as the server names it in fieldType.valueType.
type ValueType string

const (
	EnumType       ValueType = "enum"
	StateType      ValueType = "state"
	VersionType    ValueType = "version"
	BuildType      ValueType = "build"
	OwnedFieldType ValueType = "ownedField"
	UserType       ValueType = "user"
	GroupType      ValueType = "group"
	PeriodType     ValueType = "period"
	TextType       ValueType = "text"
	DateType       ValueType = "date"
	DateTimeType   ValueType = "date and time"
	IntegerType    ValueType = "integer"
	FloatType      ValueType = "float"
	StringType     ValueType = "string"
)

// FieldType is the type of a custom field: the type of its value and whether it holds several.
type FieldType struct {
	ValueType ValueType
	Multi     bool
}

func (t FieldType) String() string {
	if t.Multi {
		return string(t.ValueType) + " of many"
	}
	return string(t.ValueType)
}

// Known says the type is one of the twenty pairs of value type and multiplicity the module models.
func (t FieldType) Known() bool {
	_, known := t.kind()
	return known
}

// Class is the $type a write sends for a custom field of this type, as in SingleEnumIssueCustomField.
func (t FieldType) Class() string {
	k, _ := t.kind()
	return k.class
}

// HasBundle says the values the field accepts are a bundle of named values: enum, state, version, build and
// ownedField have one, and Bundle reads it.
func (t FieldType) HasBundle() bool {
	k, _ := t.kind()
	return k.bundle
}

// ValueKey is the member of a value by which it is read and written: name, login, text or minutes. Empty for
// a type whose value is the scalar itself.
func (t FieldType) ValueKey() string {
	k, _ := t.kind()
	return k.member
}

// Named says the server resolves a value of this type by its name or login and fixes the letter case on the
// way, so a written value and a held one are the same without regard to case.
func (t FieldType) Named() bool {
	k, _ := t.kind()
	return k.isNamedValue()
}

// Same says a written value key and a held one name the same value of this type.
func (t FieldType) Same(written, held string) bool {
	k, _ := t.kind()
	return k.sameValue(written, held)
}

// BundleFields is the fields= expression of what the field's project settings hold for the values it accepts:
// bundle(values(name,archived)) for a type with a bundle, bundle(aggregatedUsers(login)) for a user, empty otherwise.
func (t FieldType) BundleFields() string {
	k, _ := t.kind()
	switch {
	case k.bundle:
		return "bundle(values(name,archived))"
	case k.valueType == UserType:
		return "bundle(aggregatedUsers(login))"
	}
	return ""
}

// ValueKeys are the members values are named by across the types: name, login, minutes and text. A fields=
// expression asking for them under value reads the value key of a field of any type.
func ValueKeys() []string {
	var keys []string
	for _, k := range fieldKinds() {
		if k.member != "" && !slices.Contains(keys, k.member) {
			keys = append(keys, k.member)
		}
	}
	return keys
}

// Encoded is a value ready to be written: the JSON under value and the value key it was written as.
type Encoded struct {
	Body any
	Key  string
}

// Encode turns a value key into what a write sends for a field of this type; a value the type cannot hold is an
// *ArgumentError with the reason.
func (t FieldType) Encode(text string) (Encoded, error) {
	k, known := t.kind()
	if !known {
		return Encoded{}, &ArgumentError{Argument: "value", Value: text, Reason: "is written into a field of the type " + unmodelled(t)}
	}
	encoded, reason := k.encode(text)
	if reason != "" {
		return Encoded{}, &ArgumentError{Argument: "value", Value: text, Reason: reason}
	}
	return Encoded{Body: encoded.body, Key: encoded.key}, nil
}

// ReadValue reads one element of a custom field's value as the server sends it under value: an object for a
// bundle element, a user, a group, a period or a text, the scalar itself for the other types. present is false
// when the member the type is named by is null. The error names the shape the element should have had.
func (t FieldType) ReadValue(item any) (value Value, present bool, err error) {
	k, known := t.kind()
	if !known {
		return Value{}, false, errors.New("the value is of a field of the type " + unmodelled(t))
	}
	held := item
	if k.member != "" {
		object, isObject := item.(map[string]any)
		if !isObject {
			return Value{}, false, fmt.Errorf("the value holds no %s, which is what a field of its type is named by", k.member)
		}
		inside, ok := object[k.member]
		if !ok {
			return Value{}, false, fmt.Errorf("the value holds no %s, which is what a field of its type is named by", k.member)
		}
		if inside == nil {
			return Value{}, false, nil
		}
		held = inside
		if k.isNamedValue() {
			value.ID, _ = object[idKey].(string)
			value.LocalizedName, _ = object[localizedNameKey].(string)
		}
	}
	text, read := k.keyText(held)
	if !read {
		what := "the value"
		if k.member != "" {
			what = "the " + k.member + " of the value"
		}
		return Value{}, false, errors.New(what + " is not " + k.shape())
	}
	value.Text = text
	return value, true, nil
}

type form int

const (
	asString form = iota
	asText
	asDuration
	asDay
	asMoment
	asInteger
	asFloat
)

type fieldKind struct {
	valueType ValueType
	multi     bool
	class     string
	member    string
	form      form
	bundle    bool
}

const (
	nameKey    = "name"
	loginKey   = "login"
	minutesKey = "minutes"
	textKey    = "text"
)

func fieldKinds() []fieldKind {
	return []fieldKind{
		{valueType: EnumType, multi: false, class: "SingleEnumIssueCustomField", member: nameKey, bundle: true},
		{valueType: EnumType, multi: true, class: "MultiEnumIssueCustomField", member: nameKey, bundle: true},
		{valueType: StateType, multi: false, class: "StateIssueCustomField", member: nameKey, bundle: true},
		{valueType: VersionType, multi: false, class: "SingleVersionIssueCustomField", member: nameKey, bundle: true},
		{valueType: VersionType, multi: true, class: "MultiVersionIssueCustomField", member: nameKey, bundle: true},
		{valueType: BuildType, multi: false, class: "SingleBuildIssueCustomField", member: nameKey, bundle: true},
		{valueType: BuildType, multi: true, class: "MultiBuildIssueCustomField", member: nameKey, bundle: true},
		{valueType: OwnedFieldType, multi: false, class: "SingleOwnedIssueCustomField", member: nameKey, bundle: true},
		{valueType: OwnedFieldType, multi: true, class: "MultiOwnedIssueCustomField", member: nameKey, bundle: true},
		{valueType: UserType, multi: false, class: "SingleUserIssueCustomField", member: loginKey},
		{valueType: UserType, multi: true, class: "MultiUserIssueCustomField", member: loginKey},
		{valueType: GroupType, multi: false, class: "SingleGroupIssueCustomField", member: nameKey},
		{valueType: GroupType, multi: true, class: "MultiGroupIssueCustomField", member: nameKey},
		{valueType: PeriodType, multi: false, class: "PeriodIssueCustomField", member: minutesKey, form: asDuration},
		{valueType: TextType, multi: false, class: "TextIssueCustomField", member: textKey, form: asText},
		{valueType: DateType, multi: false, class: "DateIssueCustomField", form: asDay},
		{valueType: DateTimeType, multi: false, class: "SimpleIssueCustomField", form: asMoment},
		{valueType: IntegerType, multi: false, class: "SimpleIssueCustomField", form: asInteger},
		{valueType: FloatType, multi: false, class: "SimpleIssueCustomField", form: asFloat},
		{valueType: StringType, multi: false, class: "SimpleIssueCustomField", form: asString},
	}
}

func (t FieldType) kind() (fieldKind, bool) {
	for _, k := range fieldKinds() {
		if k.valueType == t.ValueType && k.multi == t.Multi {
			return k, true
		}
	}
	return fieldKind{}, false
}

func valueMembers() []field {
	var members []field
	for _, key := range ValueKeys() {
		members = append(members, field{name: key})
	}
	return append(members, field{name: idKey}, field{name: localizedNameKey})
}

// A named value is one the server resolves by name, fixing its letter case on the way.
func (k fieldKind) isNamedValue() bool {
	return k.member != "" && k.form == asString
}

func (k fieldKind) sameValue(written, received string) bool {
	if k.isNamedValue() {
		return strings.EqualFold(written, received)
	}
	return written == received
}

type encodedValue struct {
	body any
	key  string
}

func (k fieldKind) encode(text string) (encodedValue, string) {
	if text == "" {
		return encodedValue{}, k.emptyValueReason()
	}
	switch k.form {
	case asDuration:
		return encodePeriod(text)
	case asDay:
		return encodeDate(text)
	case asMoment:
		return encodeDateTime(text)
	case asInteger:
		return encodeInteger(text)
	case asFloat:
		return encodeFloat(text)
	case asText:
		return encodeText(text)
	}
	if k.member == "" {
		return encodeString(text)
	}
	return encodedValue{body: map[string]string{k.member: text}, key: text}, ""
}

func (k fieldKind) emptyValueReason() string {
	const leftAlone = "; a field is emptied with Clear, and a field the call does not name is left as it stands"
	switch {
	case k.valueType == StringType || k.valueType == TextType:
		return fmt.Sprintf("YouTrack keeps a %s field it is given nothing for as holding nothing at all", k.valueType) + leftAlone
	case k.isNamedValue():
		return fmt.Sprintf("a value of a %s field is a name, and no value is named by nothing", k.valueType) + leftAlone
	}
	return fmt.Sprintf("no value of a %s field is empty", k.valueType) + leftAlone
}

func encodePeriod(text string) (encodedValue, string) {
	minutes, read := periodMinutes(text)
	switch {
	case !read:
		return encodedValue{}, "a period is written in hours and minutes, as in PT1H30M, PT90M or PT0M: a day of " +
			"YouTrack is the working day of the instance, and a second is no part of what a period field holds"
	case minutes > math.MaxInt32:
		return encodedValue{}, fmt.Sprintf("a period field holds at most %d minutes", math.MaxInt32)
	}
	return encodedValue{body: map[string]int64{minutesKey: minutes}, key: duration(minutes)}, ""
}

func periodMinutes(text string) (int64, bool) {
	rest, isPeriod := strings.CutPrefix(text, "PT")
	if !isPeriod {
		return 0, false
	}
	hours, rest, hoursGiven, read := countBefore(rest, 'H')
	if !read {
		return 0, false
	}
	minutes, rest, minutesGiven, read := countBefore(rest, 'M')
	if !read || rest != "" || (!hoursGiven && !minutesGiven) {
		return 0, false
	}
	return min(hours*60, pastMaxInt32) + minutes, true
}

const pastMaxInt32 = math.MaxInt32 + 1

func countBefore(text string, mark byte) (count int64, rest string, given, read bool) {
	before, after, marked := strings.Cut(text, string(mark))
	if !marked {
		return 0, text, false, true
	}
	if before == "" {
		return 0, "", false, false
	}
	for _, digit := range []byte(before) {
		if digit < '0' || digit > '9' {
			return 0, "", false, false
		}
		count = min(count*10+int64(digit-'0'), pastMaxInt32)
	}
	return count, after, true, true
}

func duration(minutes int64) string {
	written := ""
	if hours := minutes / 60; hours != 0 {
		written += strconv.FormatInt(hours, 10) + "H"
	}
	if rest := minutes % 60; rest != 0 {
		written += strconv.FormatInt(rest, 10) + "M"
	}
	if written == "" {
		written = "0M"
	}
	return "PT" + written
}

func encodeDate(text string) (encodedValue, string) {
	day, err := time.Parse(time.DateOnly, text)
	if err != nil {
		return encodedValue{}, "a date field holds a day, written as in 2026-09-16"
	}
	return encodedValue{body: noonUTC(day), key: day.Format(time.DateOnly)}, ""
}

// Noon UTC falls on the same calendar day in every zone from UTC-12 to UTC+11:59.
func noonUTC(day time.Time) int64 {
	return time.Date(day.Year(), day.Month(), day.Day(), 12, 0, 0, 0, time.UTC).UnixMilli()
}

func encodeDateTime(text string) (encodedValue, string) {
	moment, err := time.Parse(time.RFC3339, text)
	switch {
	case err != nil:
		return encodedValue{}, "a date and time field holds a moment, written as in 2026-08-31T03:00:00.123+03:00, " +
			"with the offset from UTC on it"
	case moment.Nanosecond()%int(time.Millisecond) != 0:
		return encodedValue{}, "YouTrack keeps a moment to the millisecond, and this one is written finer than that"
	}
	milliseconds := moment.UnixMilli()
	return encodedValue{body: milliseconds, key: formatMoment(milliseconds)}, ""
}

func formatMoment(milliseconds int64) string {
	return time.UnixMilli(milliseconds).UTC().Format(time.RFC3339Nano)
}

func encodeInteger(text string) (encodedValue, string) {
	count, err := strconv.ParseInt(text, 10, 32)
	if err != nil {
		return encodedValue{}, fmt.Sprintf("an integer field holds a whole number between %d and %d", math.MinInt32, math.MaxInt32)
	}
	return encodedValue{body: count, key: strconv.FormatInt(count, 10)}, ""
}

func encodeFloat(text string) (encodedValue, string) {
	number, isNumber := jsonNumber(text)
	if !isNumber {
		return encodedValue{}, "a float field holds a number written the way JSON writes one, as in 1.5, -0.25 or 1e3"
	}
	held, err := number.Float64()
	if err != nil || math.IsInf(held, 0) || math.IsNaN(held) {
		return encodedValue{}, "a float field holds a finite number, and this one is past the largest one there is"
	}
	return encodedValue{body: held, key: shortestDecimal(held)}, ""
}

func shortestDecimal(number float64) string {
	return strconv.FormatFloat(number, 'g', -1, 64)
}

func jsonNumber(text string) (json.Number, bool) {
	value, isJSON := decode([]byte(text))
	number, isNumber := value.(json.Number)
	return number, isJSON && isNumber && number.String() == text
}

func encodeString(text string) (encodedValue, string) {
	if !utf8.ValidString(text) {
		return encodedValue{}, noUTF8
	}
	for _, rewritten := range stringFieldRewrites() {
		if strings.ContainsRune(text, rewritten.rune) {
			return encodedValue{}, fmt.Sprintf("the value holds U+%04X, which YouTrack stores as %s", rewritten.rune, rewritten.into)
		}
	}
	if strings.TrimFunc(text, trimmedByYouTrack) != text {
		return encodedValue{}, "YouTrack trims the spaces off a string, so it would keep less than what was written"
	}
	return encodedValue{body: text, key: text}, ""
}

const noUTF8 = "the value is no valid UTF-8, and every byte of it that is none would reach YouTrack as �"

type charReplacement struct {
	rune rune
	into string
}

func stringFieldRewrites() []charReplacement {
	return []charReplacement{
		{rune: 0x85, into: "nothing at all"},
		{rune: 0x2028, into: "a space"},
		{rune: 0x2029, into: "a space"},
	}
}

func trimmedByYouTrack(r rune) bool {
	return unicode.IsSpace(r) || r >= '\x1c' && r <= '\x1f'
}

func encodeText(text string) (encodedValue, string) {
	if !utf8.ValidString(text) {
		return encodedValue{}, noUTF8
	}
	return encodedValue{body: map[string]string{textKey: text}, key: text}, ""
}

// keyText reads the value key of a held value as text: a name or login as is, a period as PT1H30M, a day as
// 2026-09-16, a moment in UTC, a number in its shortest decimal form.
func (k fieldKind) keyText(held any) (string, bool) {
	switch k.form {
	case asDuration:
		minutes, isWhole := parseInt64(held)
		return duration(minutes), isWhole
	case asDay, asMoment:
		count, isWhole := parseInt64(held)
		if !isWhole {
			return "", false
		}
		if k.form == asDay {
			return time.UnixMilli(count).UTC().Format(time.DateOnly), true
		}
		return formatMoment(count), true
	case asInteger:
		count, isWhole := parseInt64(held)
		return strconv.FormatInt(count, 10), isWhole
	case asFloat:
		number, isNumber := held.(json.Number)
		if !isNumber {
			return "", false
		}
		count, err := number.Float64()
		return shortestDecimal(count), err == nil
	}
	text, isText := held.(string)
	return text, isText
}

const localizedNameKey = "localizedName"

func (k fieldKind) shape() string {
	switch k.form {
	case asDuration:
		return "a whole number of minutes"
	case asDay, asMoment:
		return "a whole number of milliseconds since the epoch"
	case asInteger, asFloat:
		return "a number"
	}
	return "text"
}
