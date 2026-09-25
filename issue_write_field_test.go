package youtrack_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/youtrack"
	"github.com/hakastein/youtrack/fake"
)

func issueCreatedBody(field string) string {
	return `{"project":{"id":"0-1"},"summary":"First","customFields":[` + field + `]}`
}

func issueFilling(fields ...youtrack.FieldWrite) youtrack.IssueInput {
	return youtrack.IssueInput{Summary: "First", Fields: fields}
}

func TestIssueWriteSendsAValueUnderTheClassAndTheKeyOfItsType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		valueType string
		multi     bool
		filled    []youtrack.FieldWrite
		sent      string
		held      string
	}{
		{name: "an enum", valueType: "enum", filled: []youtrack.FieldWrite{issueFill("Field", "First")},
			sent: `{"$type":"SingleEnumIssueCustomField","name":"Field","value":{"name":"First"}}`,
			held: issueElement("First")},
		{name: "enums", valueType: "enum", multi: true, filled: []youtrack.FieldWrite{issueFill("Field", "First", "Second")},
			sent: `{"$type":"MultiEnumIssueCustomField","name":"Field","value":[{"name":"First"},{"name":"Second"}]}`,
			held: `[` + issueElement("First") + `,` + issueElement("Second") + `]`},
		{name: "enums over two writes of one name", valueType: "enum", multi: true,
			filled: []youtrack.FieldWrite{issueFill("Field", "First"), issueFill("Field", "Second")},
			sent:   `{"$type":"MultiEnumIssueCustomField","name":"Field","value":[{"name":"First"},{"name":"Second"}]}`,
			held:   `[` + issueElement("First") + `,` + issueElement("Second") + `]`},
		{name: "a state", valueType: "state", filled: []youtrack.FieldWrite{issueFill("Field", "First")},
			sent: `{"$type":"StateIssueCustomField","name":"Field","value":{"name":"First"}}`,
			held: issueElement("First")},
		{name: "a version", valueType: "version", filled: []youtrack.FieldWrite{issueFill("Field", "First")},
			sent: `{"$type":"SingleVersionIssueCustomField","name":"Field","value":{"name":"First"}}`,
			held: issueElement("First")},
		{name: "versions", valueType: "version", multi: true, filled: []youtrack.FieldWrite{issueFill("Field", "First")},
			sent: `{"$type":"MultiVersionIssueCustomField","name":"Field","value":[{"name":"First"}]}`,
			held: `[` + issueElement("First") + `]`},
		{name: "a build", valueType: "build", filled: []youtrack.FieldWrite{issueFill("Field", "First")},
			sent: `{"$type":"SingleBuildIssueCustomField","name":"Field","value":{"name":"First"}}`,
			held: issueElement("First")},
		{name: "builds", valueType: "build", multi: true, filled: []youtrack.FieldWrite{issueFill("Field", "First")},
			sent: `{"$type":"MultiBuildIssueCustomField","name":"Field","value":[{"name":"First"}]}`,
			held: `[` + issueElement("First") + `]`},
		{name: "an owned value", valueType: "ownedField", filled: []youtrack.FieldWrite{issueFill("Field", "First")},
			sent: `{"$type":"SingleOwnedIssueCustomField","name":"Field","value":{"name":"First"}}`,
			held: issueElement("First")},
		{name: "owned values", valueType: "ownedField", multi: true, filled: []youtrack.FieldWrite{issueFill("Field", "First")},
			sent: `{"$type":"MultiOwnedIssueCustomField","name":"Field","value":[{"name":"First"}]}`,
			held: `[` + issueElement("First") + `]`},
		{name: "a user", valueType: "user", filled: []youtrack.FieldWrite{issueFill("Field", "first")},
			sent: `{"$type":"SingleUserIssueCustomField","name":"Field","value":{"login":"first"}}`,
			held: `{"$type":"User","login":"first"}`},
		{name: "users", valueType: "user", multi: true, filled: []youtrack.FieldWrite{issueFill("Field", "first", "second")},
			sent: `{"$type":"MultiUserIssueCustomField","name":"Field","value":[{"login":"first"},{"login":"second"}]}`,
			held: `[{"$type":"User","login":"first"},{"$type":"User","login":"second"}]`},
		{name: "a user named me", valueType: "user", filled: []youtrack.FieldWrite{issueFill("Field", "me")},
			sent: `{"$type":"SingleUserIssueCustomField","name":"Field","value":{"login":"me"}}`,
			held: `{"$type":"User","login":"me"}`},
		{name: "a group", valueType: "group", filled: []youtrack.FieldWrite{issueFill("Field", "First")},
			sent: `{"$type":"SingleGroupIssueCustomField","name":"Field","value":{"name":"First"}}`,
			held: `{"$type":"UserGroup","name":"First"}`},
		{name: "groups", valueType: "group", multi: true, filled: []youtrack.FieldWrite{issueFill("Field", "First")},
			sent: `{"$type":"MultiGroupIssueCustomField","name":"Field","value":[{"name":"First"}]}`,
			held: `[{"$type":"UserGroup","name":"First"}]`},
		{name: "a name holding an equals sign", valueType: "enum", filled: []youtrack.FieldWrite{issueFill("Field", "First=Second")},
			sent: `{"$type":"SingleEnumIssueCustomField","name":"Field","value":{"name":"First=Second"}}`,
			held: issueElement("First=Second")},
		{name: "a period of hours and minutes", valueType: "period", filled: []youtrack.FieldWrite{issueFill("Field", "PT1H30M")},
			sent: `{"$type":"PeriodIssueCustomField","name":"Field","value":{"minutes":90}}`,
			held: `{"$type":"PeriodValue","minutes":90}`},
		{name: "a period of no time at all", valueType: "period", filled: []youtrack.FieldWrite{issueFill("Field", "PT0M")},
			sent: `{"$type":"PeriodIssueCustomField","name":"Field","value":{"minutes":0}}`,
			held: `{"$type":"PeriodValue","minutes":0}`},
		{name: "a period of the most minutes the field holds", valueType: "period",
			filled: []youtrack.FieldWrite{issueFill("Field", "PT2147483647M")},
			sent:   `{"$type":"PeriodIssueCustomField","name":"Field","value":{"minutes":2147483647}}`,
			held:   `{"$type":"PeriodValue","minutes":2147483647}`},
		{name: "a period of hours coming to the most minutes the field holds", valueType: "period",
			filled: []youtrack.FieldWrite{issueFill("Field", "PT35791394H7M")},
			sent:   `{"$type":"PeriodIssueCustomField","name":"Field","value":{"minutes":2147483647}}`,
			held:   `{"$type":"PeriodValue","minutes":2147483647}`},
		{name: "a text holding a CRLF", valueType: "text", filled: []youtrack.FieldWrite{issueFill("Field", "First\r\nSecond")},
			sent: `{"$type":"TextIssueCustomField","name":"Field","value":{"text":"First\r\nSecond"}}`,
			held: `{"$type":"TextFieldValue","text":"First\r\nSecond"}`},
		{name: "a day, at noon UTC", valueType: "date", filled: []youtrack.FieldWrite{issueFill("Field", "2026-09-16")},
			sent: `{"$type":"DateIssueCustomField","name":"Field","value":1789560000000}`,
			held: `1789560000000`},
		{name: "a moment in an offset of its own", valueType: "date and time",
			filled: []youtrack.FieldWrite{issueFill("Field", "2026-08-31T03:00:00.123+03:00")},
			sent:   `{"$type":"SimpleIssueCustomField","name":"Field","value":1788134400123}`,
			held:   `1788134400123`},
		{name: "a whole number written with leading zeroes", valueType: "integer", filled: []youtrack.FieldWrite{issueFill("Field", "007")},
			sent: `{"$type":"SimpleIssueCustomField","name":"Field","value":7}`,
			held: `7`},
		{name: "a number written with an exponent", valueType: "float", filled: []youtrack.FieldWrite{issueFill("Field", "1e3")},
			sent: `{"$type":"SimpleIssueCustomField","name":"Field","value":1000}`,
			held: `1000`},
		{name: "a string", valueType: "string", filled: []youtrack.FieldWrite{issueFill("Field", "First")},
			sent: `{"$type":"SimpleIssueCustomField","name":"Field","value":"First"}`,
			held: `"First"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			project := issueProject(metaField{id: "1-1", name: "Field", valueType: tc.valueType, multi: tc.multi})
			held := issueHeldFields(issueHeld{name: "Field", valueType: tc.valueType, multi: tc.multi, value: tc.held})
			answer := issueWritten(t, map[string]any{"summary": "First", "customFields": held})
			server := issueWriting(t, project, "[]", fake.JSON(http.StatusOK, answer))

			_, err := issueCreate(t, server, issueFilling(tc.filled...), "idReadable")

			require.NoError(t, err)
			assert.JSONEq(t, issueCreatedBody(tc.sent), server.Last(t).Body)
		})
	}
}

func TestIssueWriteRefusesAValueItsFieldCannotHold(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		valueType string
		given     string
		shown     string
	}{
		{name: "a period of days", valueType: "period", given: "P1D", shown: "P1D"},
		{name: "a period of a fraction of an hour", valueType: "period", given: "PT1.5H", shown: "PT1.5H"},
		{name: "a period of seconds", valueType: "period", given: "PT30S", shown: "PT30S"},
		{name: "a period in lower case", valueType: "period", given: "pt1h", shown: "pt1h"},
		{name: "a period of no part at all", valueType: "period", given: "PT", shown: "PT"},
		{name: "a period of hours with no count before the mark", valueType: "period", given: "PTH", shown: "PTH"},
		{name: "a period of minutes with no count before the mark", valueType: "period", given: "PTM", shown: "PTM"},
		{name: "a period of nothing at all", valueType: "period", given: "", shown: ""},
		{name: "a period a minute past what the field holds", valueType: "period", given: "PT2147483648M",
			shown: "PT2147483648M"},
		{name: "a period of hours and minutes a minute past what the field holds", valueType: "period",
			given: "PT35791394H8M", shown: "PT35791394H8M"},
		{name: "a period of hours past what the field holds", valueType: "period", given: "PT35791395H",
			shown: "PT35791395H"},
		{name: "a period of more minutes than twenty digits count", valueType: "period",
			given: "PT99999999999999999999M", shown: "PT99999999999999999999M"},
		{name: "a period of more hours than twenty digits count", valueType: "period",
			given: "PT99999999999999999999H", shown: "PT99999999999999999999H"},
		{name: "a period of more hours than twenty digits count and minutes", valueType: "period",
			given: "PT99999999999999999999H1M", shown: "PT99999999999999999999H1M"},
		{name: "a period of a minute more than a 64-bit count holds", valueType: "period",
			given: "PT18446744073709551617M", shown: "PT18446744073709551617M"},
		{name: "a period of an hour more than a 64-bit count holds", valueType: "period",
			given: "PT18446744073709551617H", shown: "PT18446744073709551617H"},
		{name: "a date of nothing at all", valueType: "date", given: "", shown: ""},
		{name: "a date written the way a human writes one", valueType: "date", given: "16.09.2026", shown: "16.09.2026"},
		{name: "a date carrying a moment of the day", valueType: "date", given: "2026-09-16T00:00:00Z",
			shown: "2026-09-16T00:00:00Z"},
		{name: "a moment with no offset from UTC", valueType: "date and time", given: "2026-08-31T00:00:00",
			shown: "2026-08-31T00:00:00"},
		{name: "a moment finer than a millisecond", valueType: "date and time", given: "2026-08-31T00:00:00.0001Z",
			shown: "2026-08-31T00:00:00.0001Z"},
		{name: "a whole number that is a fraction", valueType: "integer", given: "2.5", shown: "2.5"},
		{name: "a whole number past what the field holds", valueType: "integer", given: "2147483648", shown: "2147483648"},
		{name: "a whole number below what the field holds", valueType: "integer", given: "-2147483649",
			shown: "-2147483649"},
		{name: "a whole number written in hexadecimal", valueType: "integer", given: "0x10", shown: "0x10"},
		{name: "a number that is not one", valueType: "float", given: "NaN", shown: "NaN"},
		{name: "a number past every number", valueType: "float", given: "Inf", shown: "Inf"},
		{name: "a number past the largest a float holds", valueType: "float", given: "1e999", shown: "1e999"},
		{name: "a number written in hexadecimal", valueType: "float", given: "0x1p-2", shown: "0x1p-2"},
		{name: "a number written with a comma", valueType: "float", given: "1,5", shown: "1,5"},
		{name: "a string that begins with a space", valueType: "string", given: " First", shown: " First"},
		{name: "a string that ends with a tab", valueType: "string", given: "First\t", shown: "First\t"},
		{name: "a string that begins with a file separator", valueType: "string", given: "\x1cFirst", shown: "\x1cFirst"},
		{name: "a string that ends with a group separator", valueType: "string", given: "First\x1d", shown: "First\x1d"},
		{name: "a string that begins with a record separator", valueType: "string", given: "\x1eFirst",
			shown: "\x1eFirst"},
		{name: "a string that ends with a unit separator", valueType: "string", given: "First\x1f", shown: "First\x1f"},
		{name: "a string holding a NEL", valueType: "string", given: "a\u0085b", shown: "a\u0085b"},
		{name: "a string holding a line separator", valueType: "string", given: "a\u2028b", shown: "a\u2028b"},
		{name: "a string holding a paragraph separator", valueType: "string", given: "a\u2029b", shown: "a\u2029b"},
		{name: "a string that is no UTF-8", valueType: "string", given: "a\xffb", shown: "a\xffb"},
		{name: "a string of nothing at all", valueType: "string", given: "", shown: ""},
		{name: "a text that is no UTF-8", valueType: "text", given: "a\xffb", shown: "a\xffb"},
		{name: "a text of nothing at all", valueType: "text", given: "", shown: ""},
		{name: "a name of nothing at all", valueType: "enum", given: "", shown: ""},
		{name: "a login of nothing at all", valueType: "user", given: "", shown: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			project := issueProject(metaField{id: "1-1", name: "Field", valueType: tc.valueType})
			server := issueWriting(t, project, "[]", fake.Unexpected(t))

			_, err := issueCreate(t, server, issueFilling(issueFill("Field", tc.given)), "")

			kept, invalid := issueWriteError(t, err)
			want := youtrack.Error{Code: youtrack.CodeBadUsage, Details: []youtrack.Pair{
				issueMetadataRequest(server), issueProjectDetail(), {Key: "invalid"},
			}}
			assert.Equal(t, want, kept)
			assert.Equal(t, []issueInvalid{{field: "Field", value: tc.shown}}, invalid)
			assert.Equal(t, []string{projectPath}, server.Paths())
		})
	}
}

func TestIssueWriteNamesEveryValueItCannotSendAtOnce(t *testing.T) {
	t.Parallel()
	project := issueProject(
		metaField{id: "1-1", name: "First", valueType: "integer"},
		metaField{id: "1-2", name: "Second", valueType: "period"},
		metaField{id: "1-3", name: "Third", valueType: "string"},
	)
	server := issueWriting(t, project, "[]", fake.Unexpected(t))

	_, err := issueCreate(t, server, issueFilling(issueFill("Third", " x"), issueFill("Second", "P1D"), issueFill("First", "2.5")), "")

	kept, invalid := issueWriteError(t, err)
	assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage, Details: []youtrack.Pair{
		issueMetadataRequest(server), issueProjectDetail(), {Key: "invalid"},
	}}, kept)
	assert.Equal(t, []issueInvalid{
		{field: "First", value: "2.5"},
		{field: "Second", value: "P1D"},
		{field: "Third", value: " x"},
	}, invalid)
}

func TestIssueWriteRefusesMoreThanAFieldTakesInOneWrite(t *testing.T) {
	t.Parallel()
	project := issueProject(enumField("1-1", "Single"))
	tests := []struct {
		name    string
		write   issueWrite
		request func(server *fake.Server) youtrack.Pair
		read    string
		invalid issueInvalid
	}{
		{
			name:    "two values of a field that holds one",
			write:   issueCreating(issueFilling(issueFill("Single", "First", "Second"))),
			request: issueMetadataRequest,
			read:    projectPath,
			invalid: issueInvalid{field: "Single", value: "Second"},
		},
		{
			name:    "two values of a field that holds one, over two writes",
			write:   issueCreating(issueFilling(issueFill("Single", "First"), issueFill("single", "Second"))),
			request: issueMetadataRequest,
			read:    projectPath,
			invalid: issueInvalid{field: "Single", value: "Second"},
		},
		{
			name:    "a value into a field it empties",
			write:   issueUpdating(youtrack.IssueUpdate{Fields: []youtrack.FieldWrite{issueFill("Single", "First"), issueClear("single")}}),
			request: issueReadRequest,
			read:    issuePath,
			invalid: issueInvalid{field: "Single", value: "First"},
		},
		{
			name: "a value and the emptying in one write",
			write: issueUpdating(youtrack.IssueUpdate{Fields: []youtrack.FieldWrite{
				{Name: "Single", Values: []string{"First"}, Clear: true},
			}}),
			request: issueReadRequest,
			read:    issuePath,
			invalid: issueInvalid{field: "Single", value: "First"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := issueWriting(t, project, "[]", fake.Unexpected(t))

			_, err := tc.write(t.Context(), client(t, server).Issues)

			kept, invalid := issueWriteError(t, err)
			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage, Details: []youtrack.Pair{
				tc.request(server), issueProjectDetail(), {Key: "invalid"},
			}}, kept)
			assert.Equal(t, []issueInvalid{tc.invalid}, invalid)
			assert.Equal(t, []string{tc.read}, server.Paths())
		})
	}
}

func TestIssueWriteResolvesAFieldByItsNameAndSendsTheNameOfTheProject(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		fields []metaField
		filled string
		sent   string
	}{
		{
			name:   "its name in another letter case",
			fields: []metaField{{id: "1-1", name: "Field", valueType: "string"}},
			filled: "FIELD",
			sent:   "Field",
		},
		{
			name:   "its localized name in another letter case",
			fields: []metaField{{id: "1-1", name: "Field", translation: `"Localized"`, valueType: "string"}},
			filled: "localized",
			sent:   "Field",
		},
		{
			name: "a name one field has and another is localized as",
			fields: []metaField{
				{id: "1-1", name: "Other", translation: `"Shared"`, valueType: "string"},
				{id: "1-2", name: "Shared", valueType: "string"},
			},
			filled: "shared",
			sent:   "Shared",
		},
		{
			name:   "a name that ends with a space",
			fields: []metaField{{id: "1-1", name: "Field ", valueType: "string"}},
			filled: "Field ",
			sent:   "Field ",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			held := issueHeldFields(issueHeld{name: tc.sent, valueType: "string", value: `"First"`})
			answer := issueWritten(t, map[string]any{"summary": "First", "customFields": held})
			server := issueWriting(t, issueProject(tc.fields...), "[]", fake.JSON(http.StatusOK, answer))

			_, err := issueCreate(t, server, issueFilling(issueFill(tc.filled, "First")), "idReadable")

			require.NoError(t, err)
			assert.JSONEq(t, issueCreatedBody(`{"$type":"SimpleIssueCustomField","name":"`+tc.sent+`","value":"First"}`),
				server.Last(t).Body)
		})
	}
}

func TestIssueWriteRefusesANameNoSingleFieldAnswersTo(t *testing.T) {
	t.Parallel()
	project := issueProject(
		metaField{id: "1-1", name: "First", translation: `"Shared"`, valueType: "enum"},
		metaField{id: "1-2", name: "Second", translation: `"Shared"`, valueType: "enum"},
		metaField{id: "1-3", name: "Third", valueType: "enum"},
	)
	tests := []struct {
		name    string
		write   issueWrite
		request func(server *fake.Server) youtrack.Pair
		read    string
		key     string
		named   []*youtrack.Node
	}{
		{
			name:    "a name close to one field",
			write:   issueCreating(issueFilling(issueFill("Thrid", "x"))),
			request: issueMetadataRequest,
			read:    projectPath,
			key:     "unknown",
			named:   []*youtrack.Node{withNearest("field", "Thrid", "Third")},
		},
		{
			name:    "a name close to no field",
			write:   issueCreating(issueFilling(issueFill("Nothing", "x"))),
			request: issueMetadataRequest,
			read:    projectPath,
			key:     "unknown",
			named:   []*youtrack.Node{withNearest("field", "Nothing", "First", "Second", "Third")},
		},
		{
			name:    "a name with a space before it",
			write:   issueCreating(issueFilling(issueFill(" Third", "x"))),
			request: issueMetadataRequest,
			read:    projectPath,
			key:     "unknown",
			named:   []*youtrack.Node{withNearest("field", " Third", "Third")},
		},
		{
			name:    "two names of no field, each once",
			write:   issueCreating(issueFilling(issueFill("Thrid", "x"), issueFill("Nothing", "y"), issueFill("Thrid", "z"))),
			request: issueMetadataRequest,
			read:    projectPath,
			key:     "unknown",
			named: []*youtrack.Node{
				withNearest("field", "Thrid", "Third"),
				withNearest("field", "Nothing", "First", "Second", "Third"),
			},
		},
		{
			name:    "a name of no field written and emptied",
			write:   issueUpdating(youtrack.IssueUpdate{Fields: []youtrack.FieldWrite{issueFill("Thrid", "x"), issueClear("Thrid")}}),
			request: issueReadRequest,
			read:    issuePath,
			key:     "unknown",
			named:   []*youtrack.Node{withNearest("field", "Thrid", "Third")},
		},
		{
			name:    "a name of two fields, written twice",
			write:   issueCreating(issueFilling(issueFill("shared", "x"), issueFill("shared", "y"))),
			request: issueMetadataRequest,
			read:    projectPath,
			key:     "ambiguous",
			named:   []*youtrack.Node{issueCandidates("shared", "First", "Second")},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := issueWriting(t, project, "[]", fake.Unexpected(t))

			_, err := tc.write(t.Context(), client(t, server).Issues)

			want := youtrack.Error{Code: youtrack.CodeUnknownName, Details: []youtrack.Pair{
				tc.request(server), issueProjectDetail(), {Key: tc.key, Value: youtrack.NewList(tc.named...)},
			}}
			assert.Equal(t, want, errorOf(t, err))
			assert.Equal(t, []string{tc.read}, server.Paths())
		})
	}
}

func TestIssueWriteRefusesMetadataOfAnotherShape(t *testing.T) {
	t.Parallel()
	field := func(members string) string {
		return `{"$type":"ProjectCustomField","id":"1-1",` + members + `,"field":` + enumField("1-1", "Field").naming() + `}`
	}
	tests := []struct {
		name    string
		project string
	}{
		{name: "the short name of the project", project: `{"$type":"Project","id":"0-1","shortName":7,"customFields":[]}`},
		{name: "the custom fields of the project", project: `{"$type":"Project","id":"0-1","shortName":"DEV","customFields":null}`},
		{name: "whether the field may stand empty",
			project: projectOf(`[` + field(`"canBeEmpty":null,"defaultValues":[],"condition":null`) + `]`)},
		{name: "a value the project fills the field with unasked",
			project: projectOf(`[` + field(`"canBeEmpty":true,"defaultValues":[{"$type":"EnumBundleElement","name":7}],"condition":null`) + `]`)},
		{name: "whether the condition of the field shows it for nothing",
			project: projectOf(`[` + field(`"canBeEmpty":true,"defaultValues":[],"condition":{"$type":"FieldBasedCondition",`+
				`"showForNullValue":null,"field":null,"values":[]}`) + `]`)},
		{name: "the field the condition of the field watches",
			project: projectOf(`[` + field(`"canBeEmpty":true,"defaultValues":[],"condition":{"$type":"FieldBasedCondition",`+
				`"showForNullValue":false,"field":{"$type":"ProjectCustomField","id":7},"values":[]}`) + `]`)},
		{name: "a type the module does not model", project: issueProject(metaField{id: "1-1", name: "Field", valueType: "quantum"})},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := issueWriting(t, tc.project, "[]", fake.Unexpected(t))

			_, err := issueCreate(t, server, issueFilling(issueFill("Field", "First")), "")

			assert.Equal(t, unreadable(issueMetadataRequest(server), tc.project), errorOf(t, err))
			assert.Equal(t, []string{projectPath}, server.Paths())
		})
	}
}

func TestUpdateIssueEmptiesAFieldTheWayItsTypeHoldsNothing(t *testing.T) {
	t.Parallel()
	project := issueProject(
		metaField{id: "1-1", name: "Single", valueType: "user"},
		metaField{id: "1-2", name: "Multi", valueType: "enum", multi: true},
	)
	tests := []struct {
		name    string
		cleared string
		held    []issueHeld
		sent    string
	}{
		{
			name:    "a field that holds one value",
			cleared: "Single",
			held:    []issueHeld{{name: "Single", valueType: "user"}},
			sent:    `{"$type":"SingleUserIssueCustomField","name":"Single","value":null}`,
		},
		{
			name:    "a field that holds several",
			cleared: "Multi",
			held:    []issueHeld{{name: "Multi", valueType: "enum", multi: true, value: "[]"}},
			sent:    `{"$type":"MultiEnumIssueCustomField","name":"Multi","value":[]}`,
		},
		{
			name:    "a field the answer does not hold at all",
			cleared: "Single",
			sent:    `{"$type":"SingleUserIssueCustomField","name":"Single","value":null}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			answer := issueWritten(t, map[string]any{"customFields": issueHeldFields(tc.held...)})
			server := issueWriting(t, project, "[]", fake.JSON(http.StatusOK, answer))

			_, err := issueUpdate(t, server, youtrack.IssueUpdate{Fields: []youtrack.FieldWrite{issueClear(tc.cleared)}}, "idReadable")

			require.NoError(t, err)
			assert.JSONEq(t, `{"customFields":[`+tc.sent+`]}`, server.Last(t).Body)
		})
	}
}

func TestUpdateIssueWritesAFieldUnderTheClassTheIssueHoldsItIn(t *testing.T) {
	t.Parallel()
	project := issueProject(
		metaField{id: "1-1", name: "Held", valueType: "state"},
		metaField{id: "1-2", name: "Unheld", valueType: "enum"},
	)
	classes := issueClasses(issueClass{name: "Held", class: "StateMachineIssueCustomField", binding: "1-1"})
	answer := issueWritten(t, map[string]any{"customFields": issueHeldFields(
		issueHeld{name: "Held", valueType: "state", value: issueElement("First")},
		issueHeld{name: "Unheld", valueType: "enum", value: issueElement("Second")},
	)})
	server := issueWriting(t, project, classes, fake.JSON(http.StatusOK, answer))

	_, err := issueUpdate(t, server,
		youtrack.IssueUpdate{Fields: []youtrack.FieldWrite{issueFill("Held", "First"), issueFill("Unheld", "Second")}}, "idReadable")

	require.NoError(t, err)
	assert.JSONEq(t, `{"customFields":[`+
		`{"$type":"StateMachineIssueCustomField","name":"Held","value":{"name":"First"}},`+
		`{"$type":"SingleEnumIssueCustomField","name":"Unheld","value":{"name":"Second"}}]}`, server.Last(t).Body)
}
