package youtrack_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/youtrack"
	"github.com/hakastein/youtrack/fake"
)

func fill(name string, values ...string) youtrack.FieldWrite {
	return youtrack.FieldWrite{Name: name, Values: values}
}

func clear(name string) youtrack.FieldWrite {
	return youtrack.FieldWrite{Name: name, Clear: true}
}

func writtenBody(field string) string {
	return `{"customFields":[` + field + `]}`
}

func readRequest(server *fake.Server) youtrack.Request {
	return requestTo(http.MethodGet, server, issuePath+"?fields="+issueToWriteFields)
}

func writeRequest(server *fake.Server) youtrack.Request {
	return requestTo(http.MethodPost, server, issuePath+"?fields="+issueFields)
}

func writing(t *testing.T, server *fake.Server, writes ...youtrack.FieldWrite) (*youtrack.Issue, error) {
	t.Helper()
	return client(t, server).WriteFields(t.Context(), "DEV-1", writes)
}

func valueErrorOf(t *testing.T, err error) (youtrack.ValueError, []youtrack.InvalidValue) {
	t.Helper()
	var failed *youtrack.ValueError
	require.ErrorAs(t, err, &failed)
	invalid := make([]youtrack.InvalidValue, 0, len(failed.Invalid))
	for _, v := range failed.Invalid {
		assert.NotEmpty(t, v.Reason)
		invalid = append(invalid, youtrack.InvalidValue{Field: v.Field, Value: v.Value})
	}
	kept := *failed
	kept.Invalid = nil
	return kept, invalid
}

func TestWriteFieldsSendsAValueUnderTheClassAndTheKeyOfItsType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		valueType string
		multi     bool
		values    []string
		sent      string
		held      string
	}{
		{name: "an enum", valueType: "enum", values: []string{"First"},
			sent: `{"$type":"SingleEnumIssueCustomField","name":"Field","value":{"name":"First"}}`, held: element("First")},
		{name: "enums", valueType: "enum", multi: true, values: []string{"First", "Second"},
			sent: `{"$type":"MultiEnumIssueCustomField","name":"Field","value":[{"name":"First"},{"name":"Second"}]}`,
			held: `[` + element("First") + `,` + element("Second") + `]`},
		{name: "a state", valueType: "state", values: []string{"First"},
			sent: `{"$type":"StateIssueCustomField","name":"Field","value":{"name":"First"}}`, held: element("First")},
		{name: "a version", valueType: "version", values: []string{"First"},
			sent: `{"$type":"SingleVersionIssueCustomField","name":"Field","value":{"name":"First"}}`, held: element("First")},
		{name: "versions", valueType: "version", multi: true, values: []string{"First"},
			sent: `{"$type":"MultiVersionIssueCustomField","name":"Field","value":[{"name":"First"}]}`, held: `[` + element("First") + `]`},
		{name: "a build", valueType: "build", values: []string{"First"},
			sent: `{"$type":"SingleBuildIssueCustomField","name":"Field","value":{"name":"First"}}`, held: element("First")},
		{name: "builds", valueType: "build", multi: true, values: []string{"First"},
			sent: `{"$type":"MultiBuildIssueCustomField","name":"Field","value":[{"name":"First"}]}`, held: `[` + element("First") + `]`},
		{name: "an owned value", valueType: "ownedField", values: []string{"First"},
			sent: `{"$type":"SingleOwnedIssueCustomField","name":"Field","value":{"name":"First"}}`, held: element("First")},
		{name: "owned values", valueType: "ownedField", multi: true, values: []string{"First"},
			sent: `{"$type":"MultiOwnedIssueCustomField","name":"Field","value":[{"name":"First"}]}`, held: `[` + element("First") + `]`},
		{name: "a user", valueType: "user", values: []string{"first"},
			sent: `{"$type":"SingleUserIssueCustomField","name":"Field","value":{"login":"first"}}`, held: `{"$type":"User","login":"first"}`},
		{name: "users", valueType: "user", multi: true, values: []string{"first", "second"},
			sent: `{"$type":"MultiUserIssueCustomField","name":"Field","value":[{"login":"first"},{"login":"second"}]}`,
			held: `[{"$type":"User","login":"first"},{"$type":"User","login":"second"}]`},
		{name: "a group", valueType: "group", values: []string{"First"},
			sent: `{"$type":"SingleGroupIssueCustomField","name":"Field","value":{"name":"First"}}`, held: `{"$type":"UserGroup","name":"First"}`},
		{name: "groups", valueType: "group", multi: true, values: []string{"First"},
			sent: `{"$type":"MultiGroupIssueCustomField","name":"Field","value":[{"name":"First"}]}`, held: `[{"$type":"UserGroup","name":"First"}]`},
		{name: "a name holding an equals sign", valueType: "enum", values: []string{"First=Second"},
			sent: `{"$type":"SingleEnumIssueCustomField","name":"Field","value":{"name":"First=Second"}}`, held: element("First=Second")},
		{name: "a period of hours and minutes", valueType: "period", values: []string{"PT1H30M"},
			sent: `{"$type":"PeriodIssueCustomField","name":"Field","value":{"minutes":90}}`, held: `{"$type":"PeriodValue","minutes":90}`},
		{name: "a period of no time at all", valueType: "period", values: []string{"PT0M"},
			sent: `{"$type":"PeriodIssueCustomField","name":"Field","value":{"minutes":0}}`, held: `{"$type":"PeriodValue","minutes":0}`},
		{name: "a period of the most minutes the field holds", valueType: "period", values: []string{"PT2147483647M"},
			sent: `{"$type":"PeriodIssueCustomField","name":"Field","value":{"minutes":2147483647}}`, held: `{"$type":"PeriodValue","minutes":2147483647}`},
		{name: "a period of hours coming to the most minutes the field holds", valueType: "period", values: []string{"PT35791394H7M"},
			sent: `{"$type":"PeriodIssueCustomField","name":"Field","value":{"minutes":2147483647}}`, held: `{"$type":"PeriodValue","minutes":2147483647}`},
		{name: "a text holding a CRLF", valueType: "text", values: []string{"First\r\nSecond"},
			sent: `{"$type":"TextIssueCustomField","name":"Field","value":{"text":"First\r\nSecond"}}`, held: `{"$type":"TextFieldValue","text":"First\r\nSecond"}`},
		{name: "a day, at noon UTC", valueType: "date", values: []string{"2026-09-16"},
			sent: `{"$type":"DateIssueCustomField","name":"Field","value":1789560000000}`, held: `1789560000000`},
		{name: "a moment in an offset of its own", valueType: "date and time", values: []string{"2026-08-31T03:00:00.123+03:00"},
			sent: `{"$type":"SimpleIssueCustomField","name":"Field","value":1788134400123}`, held: `1788134400123`},
		{name: "a whole number written with leading zeroes", valueType: "integer", values: []string{"007"},
			sent: `{"$type":"SimpleIssueCustomField","name":"Field","value":7}`, held: `7`},
		{name: "a number written with an exponent", valueType: "float", values: []string{"1e3"},
			sent: `{"$type":"SimpleIssueCustomField","name":"Field","value":1000}`, held: `1000`},
		{name: "a string", valueType: "string", values: []string{"First"},
			sent: `{"$type":"SimpleIssueCustomField","name":"Field","value":"First"}`, held: `"First"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			project := projectJSON(metaField{id: "1-1", name: "Field", valueType: tc.valueType, multi: tc.multi})
			answer := issueHolding(t, heldValue{name: "Field", valueType: tc.valueType, multi: tc.multi, value: tc.held})
			server := servingWrite(t, project, "[]", fake.JSON(http.StatusOK, answer))

			_, err := writing(t, server, fill("Field", tc.values...))

			require.NoError(t, err)
			assert.JSONEq(t, writtenBody(tc.sent), server.Last(t).Body)
			assert.Equal(t, writeRequest(server), lastRequest(t, server))
		})
	}
}

func TestWriteFieldsRefusesAValueItsFieldCannotHold(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		valueType string
		given     string
	}{
		{name: "a period of days", valueType: "period", given: "P1D"},
		{name: "a period of a fraction of an hour", valueType: "period", given: "PT1.5H"},
		{name: "a period of seconds", valueType: "period", given: "PT30S"},
		{name: "a period in lower case", valueType: "period", given: "pt1h"},
		{name: "a period of no part at all", valueType: "period", given: "PT"},
		{name: "a period of hours with no count before the mark", valueType: "period", given: "PTH"},
		{name: "a period of minutes with no count before the mark", valueType: "period", given: "PTM"},
		{name: "a period of nothing at all", valueType: "period", given: ""},
		{name: "a period a minute past what the field holds", valueType: "period", given: "PT2147483648M"},
		{name: "a period of hours and minutes a minute past what the field holds", valueType: "period", given: "PT35791394H8M"},
		{name: "a period of hours past what the field holds", valueType: "period", given: "PT35791395H"},
		{name: "a period of more minutes than twenty digits count", valueType: "period", given: "PT99999999999999999999M"},
		{name: "a period of more hours than twenty digits count", valueType: "period", given: "PT99999999999999999999H"},
		{name: "a period of a minute more than a 64-bit count holds", valueType: "period", given: "PT18446744073709551617M"},
		{name: "a date of nothing at all", valueType: "date", given: ""},
		{name: "a date written the way a human writes one", valueType: "date", given: "16.09.2026"},
		{name: "a date carrying a moment of the day", valueType: "date", given: "2026-09-16T00:00:00Z"},
		{name: "a moment with no offset from UTC", valueType: "date and time", given: "2026-08-31T00:00:00"},
		{name: "a moment finer than a millisecond", valueType: "date and time", given: "2026-08-31T00:00:00.0001Z"},
		{name: "a whole number that is a fraction", valueType: "integer", given: "2.5"},
		{name: "a whole number past what the field holds", valueType: "integer", given: "2147483648"},
		{name: "a whole number below what the field holds", valueType: "integer", given: "-2147483649"},
		{name: "a whole number written in hexadecimal", valueType: "integer", given: "0x10"},
		{name: "a number that is not one", valueType: "float", given: "NaN"},
		{name: "a number past every number", valueType: "float", given: "Inf"},
		{name: "a number past the largest a float holds", valueType: "float", given: "1e999"},
		{name: "a number written in hexadecimal", valueType: "float", given: "0x1p-2"},
		{name: "a number written with a comma", valueType: "float", given: "1,5"},
		{name: "a string that begins with a space", valueType: "string", given: " First"},
		{name: "a string that ends with a tab", valueType: "string", given: "First\t"},
		{name: "a string that begins with a file separator", valueType: "string", given: "\x1cFirst"},
		{name: "a string that ends with a unit separator", valueType: "string", given: "First\x1f"},
		{name: "a string holding a NEL", valueType: "string", given: "a\u0085b"},
		{name: "a string holding a line separator", valueType: "string", given: "a b"},
		{name: "a string holding a paragraph separator", valueType: "string", given: "a b"},
		{name: "a string that is no UTF-8", valueType: "string", given: "a\xffb"},
		{name: "a string of nothing at all", valueType: "string", given: ""},
		{name: "a text that is no UTF-8", valueType: "text", given: "a\xffb"},
		{name: "a text of nothing at all", valueType: "text", given: ""},
		{name: "a name of nothing at all", valueType: "enum", given: ""},
		{name: "a login of nothing at all", valueType: "user", given: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			project := projectJSON(metaField{id: "1-1", name: "Field", valueType: tc.valueType})
			server := servingWrite(t, project, "[]", fake.Unexpected(t))

			_, err := writing(t, server, fill("Field", tc.given))

			kept, invalid := valueErrorOf(t, err)
			assert.Equal(t, youtrack.ValueError{Request: readRequest(server), Project: "DEV"}, kept)
			assert.Equal(t, []youtrack.InvalidValue{{Field: "Field", Value: tc.given}}, invalid)
			assert.Equal(t, []string{http.MethodGet}, server.Methods())
		})
	}
}

func TestWriteFieldsNamesEveryValueItCannotSendAtOnce(t *testing.T) {
	t.Parallel()
	project := projectJSON(
		metaField{id: "1-1", name: "First", valueType: "integer"},
		metaField{id: "1-2", name: "Second", valueType: "period"},
		metaField{id: "1-3", name: "Third", valueType: "string"},
	)
	server := servingWrite(t, project, "[]", fake.Unexpected(t))

	_, err := writing(t, server, fill("Third", " x"), fill("Second", "P1D"), fill("First", "2.5"))

	kept, invalid := valueErrorOf(t, err)
	assert.Equal(t, youtrack.ValueError{Request: readRequest(server), Project: "DEV"}, kept)
	assert.Equal(t, []youtrack.InvalidValue{{Field: "First", Value: "2.5"}, {Field: "Second", Value: "P1D"}, {Field: "Third", Value: " x"}}, invalid)
}

func TestWriteFieldsRefusesMoreThanAFieldTakesInOneWrite(t *testing.T) {
	t.Parallel()
	project := projectJSON(metaField{id: "1-1", name: "Single", valueType: "enum"})
	tests := []struct {
		name    string
		writes  []youtrack.FieldWrite
		invalid youtrack.InvalidValue
	}{
		{name: "two values of a field that holds one", writes: []youtrack.FieldWrite{fill("Single", "First", "Second")},
			invalid: youtrack.InvalidValue{Field: "Single", Value: "Second"}},
		{name: "two values of a field that holds one, over two writes", writes: []youtrack.FieldWrite{fill("Single", "First"), fill("single", "Second")},
			invalid: youtrack.InvalidValue{Field: "Single", Value: "Second"}},
		{name: "a value into a field it empties", writes: []youtrack.FieldWrite{fill("Single", "First"), clear("single")},
			invalid: youtrack.InvalidValue{Field: "Single", Value: "First"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := servingWrite(t, project, "[]", fake.Unexpected(t))

			_, err := writing(t, server, tc.writes...)

			kept, invalid := valueErrorOf(t, err)
			assert.Equal(t, youtrack.ValueError{Request: readRequest(server), Project: "DEV"}, kept)
			assert.Equal(t, []youtrack.InvalidValue{tc.invalid}, invalid)
		})
	}
}

func TestWriteFieldsResolvesAFieldByEitherNameAndSendsTheNameOfTheProject(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		fields []metaField
		asked  string
		sent   string
	}{
		{name: "its name in another letter case", fields: []metaField{{id: "1-1", name: "Field", valueType: "string"}}, asked: "FIELD", sent: "Field"},
		{name: "its translation in another letter case", fields: []metaField{{id: "1-1", name: "Field", localized: "Поле", valueType: "string"}}, asked: "поле", sent: "Field"},
		{name: "a name one field has and another is translated as", fields: []metaField{
			{id: "1-1", name: "Other", localized: "Shared", valueType: "string"},
			{id: "1-2", name: "Shared", valueType: "string"},
		}, asked: "shared", sent: "Shared"},
		{name: "a name that ends with a space", fields: []metaField{{id: "1-1", name: "Field ", valueType: "string"}}, asked: "Field ", sent: "Field "},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			answer := issueHolding(t, heldValue{name: tc.sent, valueType: "string", value: `"First"`})
			server := servingWrite(t, projectJSON(tc.fields...), "[]", fake.JSON(http.StatusOK, answer))

			_, err := writing(t, server, fill(tc.asked, "First"))

			require.NoError(t, err)
			assert.JSONEq(t, writtenBody(`{"$type":"SimpleIssueCustomField","name":"`+tc.sent+`","value":"First"}`), server.Last(t).Body)
		})
	}
}

func TestWriteFieldsRefusesANameNoSingleFieldAnswersTo(t *testing.T) {
	t.Parallel()
	project := projectJSON(
		metaField{id: "1-1", name: "First", localized: "Shared", valueType: "enum"},
		metaField{id: "1-2", name: "Second", localized: "Shared", valueType: "enum"},
		metaField{id: "1-3", name: "Third", valueType: "enum"},
	)
	tests := []struct {
		name      string
		writes    []youtrack.FieldWrite
		unknown   []string
		ambiguous []youtrack.AmbiguousName
	}{
		{name: "a name of no field", writes: []youtrack.FieldWrite{fill("Thrid", "x")}, unknown: []string{"Thrid"}},
		{name: "a name with a space before it", writes: []youtrack.FieldWrite{fill(" Third", "x")}, unknown: []string{" Third"}},
		{name: "two names of no field, each once", writes: []youtrack.FieldWrite{fill("Thrid", "x"), fill("Nothing", "y"), fill("Thrid", "z")},
			unknown: []string{"Thrid", "Nothing"}},
		{name: "a name of no field written and emptied", writes: []youtrack.FieldWrite{fill("Thrid", "x"), clear("Thrid")}, unknown: []string{"Thrid"}},
		{name: "a name of two fields, written twice", writes: []youtrack.FieldWrite{fill("shared", "x"), fill("shared", "y")},
			ambiguous: []youtrack.AmbiguousName{{Name: "shared", Candidates: []string{"First", "Second"}}}},
		{name: "a name of no field beside a name of two", writes: []youtrack.FieldWrite{fill("shared", "x"), fill("Nothing", "y")},
			unknown: []string{"Nothing"}, ambiguous: []youtrack.AmbiguousName{{Name: "shared", Candidates: []string{"First", "Second"}}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := servingWrite(t, project, "[]", fake.Unexpected(t))

			_, err := writing(t, server, tc.writes...)

			var failed *youtrack.FieldNameError
			require.ErrorAs(t, err, &failed)
			want := youtrack.FieldNameError{Request: readRequest(server), Project: "DEV", Unknown: tc.unknown, Ambiguous: tc.ambiguous,
				Known: []string{"First", "Second", "Third"}}
			assert.Equal(t, want, *failed)
			assert.Equal(t, []string{http.MethodGet}, server.Methods())
		})
	}
}

func TestWriteFieldsRefusesAnIssueOfAnotherShape(t *testing.T) {
	t.Parallel()
	field := func(members string) string {
		return `{"$type":"ProjectCustomField","id":"1-1",` + members + `,"field":{"$type":"CustomField","name":"Field",` +
			`"localizedName":null,"fieldType":{"$type":"FieldType","valueType":"enum","isMultiValue":false}}}`
	}
	projectOf := func(field string) string {
		return `{"$type":"Project","id":"0-1","shortName":"DEV","customFields":[` + field + `]}`
	}
	tests := []struct {
		name  string
		issue string
	}{
		{name: "a readable id that is no text", issue: `{"$type":"Issue","idReadable":5,"customFields":[],"project":` + oneFieldProject() + `}`},
		{name: "a readable id of another form", issue: `{"$type":"Issue","idReadable":"2-1","customFields":[],"project":` + oneFieldProject() + `}`},
		{name: "a project that is no object", issue: `{"$type":"Issue","idReadable":"DEV-1","customFields":[],"project":null}`},
		{name: "the short name of the project", issue: issueToWriteJSON(`{"$type":"Project","id":"0-1","shortName":7,"customFields":[]}`, "[]")},
		{name: "the custom fields of the project", issue: issueToWriteJSON(`{"$type":"Project","id":"0-1","shortName":"DEV","customFields":null}`, "[]")},
		{name: "whether the field may stand empty", issue: issueToWriteJSON(projectOf(field(`"ordinal":0,"canBeEmpty":null`)), "[]")},
		{name: "a type the module does not model", issue: issueToWriteJSON(projectJSON(metaField{id: "1-1", name: "Field", valueType: "quantum"}), "[]")},
		{name: "the classes of the issue that are no array", issue: issueToWriteJSON(oneFieldProject(), `null`)},
		{name: "a class that is no text", issue: issueToWriteJSON(oneFieldProject(), `[{"$type":5,"name":"Field","projectCustomField":{"id":"1-1"}}]`)},
		{name: "a class with no binding", issue: issueToWriteJSON(oneFieldProject(), `[{"$type":"SingleEnumIssueCustomField","name":"Field","projectCustomField":null}]`)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mux := http.NewServeMux()
			mux.HandleFunc("GET "+issuePath, fake.JSON(http.StatusOK, tc.issue))
			server := fake.Serve(t, mux.ServeHTTP)

			_, err := writing(t, server, fill("Field", "First"))

			assert.Equal(t, invalidAnswer(readRequest(server), tc.issue), responseErrorOf(t, err))
		})
	}
}

func TestWriteFieldsRefusesAProjectWithNoFields(t *testing.T) {
	t.Parallel()
	server := servingWrite(t, projectJSON(), "[]", fake.Unexpected(t))

	_, err := writing(t, server, fill("Field", "First"))

	var failed *youtrack.PermissionError
	require.ErrorAs(t, err, &failed)
	assert.Equal(t, youtrack.PermissionError{Request: readRequest(server), Project: "DEV", Permission: "jetbrains.jetpass.project-read"}, *failed)
}

func TestWriteFieldsEmptiesAFieldTheWayItsTypeHoldsNothing(t *testing.T) {
	t.Parallel()
	project := projectJSON(
		metaField{id: "1-1", name: "Single", valueType: "user"},
		metaField{id: "1-2", name: "Multi", valueType: "enum", multi: true},
	)
	tests := []struct {
		name    string
		cleared string
		held    []heldValue
		sent    string
	}{
		{name: "a field that holds one value", cleared: "Single", held: []heldValue{{name: "Single", valueType: "user"}},
			sent: `{"$type":"SingleUserIssueCustomField","name":"Single","value":null}`},
		{name: "a field that holds several", cleared: "Multi", held: []heldValue{{name: "Multi", valueType: "enum", multi: true, value: "[]"}},
			sent: `{"$type":"MultiEnumIssueCustomField","name":"Multi","value":[]}`},
		{name: "a field the answer does not hold at all", cleared: "Single",
			sent: `{"$type":"SingleUserIssueCustomField","name":"Single","value":null}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := servingWrite(t, project, "[]", fake.JSON(http.StatusOK, issueHolding(t, tc.held...)))

			_, err := writing(t, server, clear(tc.cleared))

			require.NoError(t, err)
			assert.JSONEq(t, writtenBody(tc.sent), server.Last(t).Body)
		})
	}
}

func TestWriteFieldsRefusesToEmptyAFieldTheProjectRequires(t *testing.T) {
	t.Parallel()
	project := projectJSON(
		metaField{id: "1-1", name: "Required", valueType: "enum", required: true},
		metaField{id: "1-2", name: "Free", valueType: "enum"},
		metaField{id: "1-3", name: "Also", valueType: "state", required: true},
	)
	server := servingWrite(t, project, "[]", fake.Unexpected(t))

	_, err := writing(t, server, clear("Free"), clear("also"), clear("Required"))

	var failed *youtrack.RequiredFieldError
	require.ErrorAs(t, err, &failed)
	assert.Equal(t, youtrack.RequiredFieldError{Request: readRequest(server), Project: "DEV", Fields: []string{"Required", "Also"}}, *failed)
}

func TestWriteFieldsWritesAFieldUnderTheClassTheIssueHoldsItIn(t *testing.T) {
	t.Parallel()
	project := projectJSON(
		metaField{id: "1-1", name: "Held", valueType: "state"},
		metaField{id: "1-2", name: "Unheld", valueType: "enum"},
	)
	classes := classesJSON(heldClass{name: "Held", class: "StateMachineIssueCustomField", binding: "1-1"})
	answer := issueHolding(t,
		heldValue{name: "Held", valueType: "state", value: element("First"), binding: "1-1"},
		heldValue{name: "Unheld", valueType: "enum", value: element("Second"), binding: "1-2"})
	server := servingWrite(t, project, classes, fake.JSON(http.StatusOK, answer))

	_, err := writing(t, server, fill("Held", "First"), fill("Unheld", "Second"))

	require.NoError(t, err)
	assert.JSONEq(t, `{"customFields":[`+
		`{"$type":"StateMachineIssueCustomField","name":"Held","value":{"name":"First"}},`+
		`{"$type":"SingleEnumIssueCustomField","name":"Unheld","value":{"name":"Second"}}]}`, server.Last(t).Body)
}

func TestWriteFieldsAddressesTheWriteByTheReadableIdTheServerGave(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/issues/dev-01", fake.JSON(http.StatusOK, issueToWriteJSON(oneFieldProject(), "[]")))
	mux.HandleFunc("POST "+issuePath, fake.JSON(http.StatusOK, issueHolding(t, heldValue{name: "Field", valueType: "enum", value: element("First")})))
	server := fake.Serve(t, mux.ServeHTTP)

	_, err := client(t, server).WriteFields(t.Context(), "dev-01", []youtrack.FieldWrite{fill("Field", "First")})

	require.NoError(t, err)
	assert.Equal(t, []string{"GET /api/issues/dev-01", "POST " + issuePath}, server.Routes())
}

func servingAWrittenField(t *testing.T, valueType string, multi bool, held string) *fake.Server {
	t.Helper()
	project := projectJSON(metaField{id: "1-1", name: "Field", valueType: valueType, multi: multi})
	answer := issueHolding(t, heldValue{name: "Field", valueType: valueType, multi: multi, value: held})
	return servingWrite(t, project, "[]", fake.JSON(http.StatusOK, answer))
}

func mismatchErrorOf(t *testing.T, err error) youtrack.MismatchError {
	t.Helper()
	var failed *youtrack.MismatchError
	require.ErrorAs(t, err, &failed)
	return *failed
}

func TestWriteFieldsRefusesAnAnswerThatDisagreesWithAField(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		valueType string
		multi     bool
		values    []string
		held      string
		expected  []string
		actual    []string
	}{
		{name: "a name the server resolved to another", valueType: "enum", values: []string{"First"}, held: element("Second"),
			expected: []string{"First"}, actual: []string{"Second"}},
		{name: "a string in another letter case", valueType: "string", values: []string{"Upper"}, held: `"upper"`,
			expected: []string{"Upper"}, actual: []string{"upper"}},
		{name: "a text in another letter case", valueType: "text", values: []string{"Upper"}, held: `{"$type":"TextFieldValue","text":"upper"}`,
			expected: []string{"Upper"}, actual: []string{"upper"}},
		{name: "a set holding a value more", valueType: "enum", multi: true, values: []string{"First", "Second"},
			held:     `[` + element("First") + `,` + element("Second") + `,` + element("Third") + `]`,
			expected: []string{"First", "Second"}, actual: []string{"First", "Second", "Third"}},
		{name: "a set holding a value fewer", valueType: "enum", multi: true, values: []string{"First", "Second"}, held: `[` + element("First") + `]`,
			expected: []string{"First", "Second"}, actual: []string{"First"}},
		{name: "a set held empty", valueType: "enum", multi: true, values: []string{"First"}, held: `[]`,
			expected: []string{"First"}, actual: []string{}},
		{name: "a value held as nothing", valueType: "enum", values: []string{"First"}, held: `null`,
			expected: []string{"First"}, actual: []string{}},
		{name: "a day the server keeps as another", valueType: "date", values: []string{"2026-09-16"}, held: `1789646400000`,
			expected: []string{"2026-09-16"}, actual: []string{"2026-09-17"}},
		{name: "a moment the server keeps a millisecond off", valueType: "date and time", values: []string{"2026-08-31T03:00:00.123+03:00"}, held: `1788134400124`,
			expected: []string{"2026-08-31T00:00:00.123Z"}, actual: []string{"2026-08-31T00:00:00.124Z"}},
		{name: "a number the server keeps as another", valueType: "float", values: []string{"1.5"}, held: `1.75`,
			expected: []string{"1.5"}, actual: []string{"1.75"}},
		{name: "a period the server rounded to the hour", valueType: "period", values: []string{"PT1H30M"}, held: `{"$type":"PeriodValue","minutes":60}`,
			expected: []string{"PT1H30M"}, actual: []string{"PT1H"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := servingAWrittenField(t, tc.valueType, tc.multi, tc.held)

			_, err := writing(t, server, fill("Field", tc.values...))

			want := youtrack.MismatchError{Request: writeRequest(server), Issue: "DEV-1", Mismatches: []youtrack.Mismatch{
				{Field: "Field", Type: fieldType(tc.valueType, tc.multi), Expected: tc.expected, Actual: tc.actual},
			}}
			assert.Equal(t, want, mismatchErrorOf(t, err))
		})
	}
}

func TestWriteFieldsTakesAnAnswerThatHoldsWhatWasWritten(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		valueType string
		multi     bool
		values    []string
		held      string
		texts     []string
	}{
		{name: "a name in another letter case", valueType: "enum", values: []string{"first"}, held: element("First"), texts: []string{"First"}},
		{name: "a login in another letter case", valueType: "user", values: []string{"FIRST"}, held: `{"$type":"User","login":"first"}`, texts: []string{"first"}},
		{name: "a set in another order and letter case", valueType: "enum", multi: true, values: []string{"First", "second", "first"},
			held: `[` + element("Second") + `,` + element("First") + `]`, texts: []string{"Second", "First"}},
		{name: "a day the server keeps at midnight UTC", valueType: "date", values: []string{"2026-09-16"}, held: `1789516800000`, texts: []string{"2026-09-16"}},
		{name: "a moment the server keeps in UTC", valueType: "date and time", values: []string{"2026-08-31T03:00:00.123+03:00"}, held: `1788134400123`,
			texts: []string{"2026-08-31T00:00:00.123Z"}},
		{name: "a number the server keeps to the digits a float holds", valueType: "float", values: []string{"123456789.123456789"}, held: `123456789.12345679`,
			texts: []string{"1.2345678912345679e+08"}},
		{name: "a period written in minutes", valueType: "period", values: []string{"PT90M"}, held: `{"$type":"PeriodValue","minutes":90}`, texts: []string{"PT1H30M"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := servingAWrittenField(t, tc.valueType, tc.multi, tc.held)

			issue, err := writing(t, server, fill("Field", tc.values...))

			require.NoError(t, err)
			f, found := issue.Field("Field")
			require.True(t, found)
			assert.Equal(t, tc.texts, f.Texts())
			assert.Equal(t, "DEV-1", issue.IDReadable)
		})
	}
}

func TestWriteFieldsTakesAnAnswerWhereAnEmptiedFieldHoldsNothing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		held []heldValue
	}{
		{name: "the field held with no value", held: []heldValue{{name: "Field", valueType: "enum"}}},
		{name: "the field not held at all"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := servingWrite(t, oneFieldProject(), "[]", fake.JSON(http.StatusOK, issueHolding(t, tc.held...)))

			_, err := writing(t, server, clear("Field"))

			require.NoError(t, err)
		})
	}
}

func TestWriteFieldsRefusesAnAnswerWhereAnEmptiedFieldHoldsAValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		multi  bool
		held   string
		actual []string
	}{
		{name: "a field that holds one value", held: element("First"), actual: []string{"First"}},
		{name: "a field that holds several", multi: true, held: `[` + element("First") + `]`, actual: []string{"First"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := servingAWrittenField(t, "enum", tc.multi, tc.held)

			_, err := writing(t, server, clear("Field"))

			want := youtrack.MismatchError{Request: writeRequest(server), Issue: "DEV-1", Mismatches: []youtrack.Mismatch{
				{Field: "Field", Type: fieldType("enum", tc.multi), Actual: tc.actual},
			}}
			assert.Equal(t, want, mismatchErrorOf(t, err))
		})
	}
}

func TestWriteFieldsRefusesACallItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		id     string
		writes []youtrack.FieldWrite
		want   youtrack.ArgumentError
	}{
		{name: "an id of an article", id: "DEV-A-1", writes: []youtrack.FieldWrite{fill("Field", "x")}, want: youtrack.ArgumentError{Argument: "id", Value: "DEV-A-1"}},
		{name: "no writes at all", id: "DEV-1", want: youtrack.ArgumentError{Argument: "writes"}},
		{name: "a field of no name", id: "DEV-1", writes: []youtrack.FieldWrite{fill("", "x")}, want: youtrack.ArgumentError{Argument: "field"}},
		{name: "a field given values and Clear both", id: "DEV-1", writes: []youtrack.FieldWrite{{Name: "Field", Values: []string{"x"}, Clear: true}},
			want: youtrack.ArgumentError{Argument: "field", Value: "Field"}},
		{name: "a field given no value", id: "DEV-1", writes: []youtrack.FieldWrite{{Name: "Field"}}, want: youtrack.ArgumentError{Argument: "field", Value: "Field"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			_, err := client(t, server).WriteFields(t.Context(), tc.id, tc.writes)

			assert.Equal(t, tc.want, argumentErrorOf(t, err))
		})
	}
}

func TestWriteFieldsAnswersWithTheIssueAsTheServerHoldsIt(t *testing.T) {
	t.Parallel()
	project := projectJSON(metaField{id: "1-1", name: "State", valueType: "state"}, metaField{id: "1-2", name: "Resolved", valueType: "date"})
	answer := issueJSON(t, map[string]any{"summary": "Renamed by a workflow", "customFields": customFieldsJSON(
		heldValue{name: "State", valueType: "state", value: `{"$type":"StateBundleElement","id":"3-9","name":"Done"}`, binding: "1-1"},
		heldValue{name: "Resolved", valueType: "date", value: `1789560000000`, binding: "1-2", ordinal: "1"},
	)})
	server := servingWrite(t, project, "[]", fake.JSON(http.StatusOK, answer))

	issue, err := writing(t, server, fill("State", "Done"))

	require.NoError(t, err)
	assert.Equal(t, "Renamed by a workflow", issue.Summary)
	assert.Equal(t, []youtrack.Field{
		{Name: "State", Type: fieldType("state", false), Values: []youtrack.Value{{ID: "3-9", Text: "Done"}}},
		{Name: "Resolved", Type: fieldType("date", false), Values: []youtrack.Value{{Text: "2026-09-16"}}},
	}, issue.Fields)
}
