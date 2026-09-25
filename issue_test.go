package youtrack_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/youtrack"
	"github.com/hakastein/youtrack/fake"
)

func issueHolding(t *testing.T, values ...heldValue) string {
	t.Helper()
	return issueJSON(t, map[string]any{"customFields": customFieldsJSON(values...)})
}

func fieldType(valueType string, multi bool) youtrack.FieldType {
	return youtrack.FieldType{ValueType: youtrack.ValueType(valueType), Multi: multi}
}

func TestIssueReadsTheIssueAndItsFieldsByTheKeyOfTheirTypes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		valueType string
		multi     bool
		value     string
		values    []youtrack.Value
	}{
		{name: "an enum", valueType: "enum",
			value:  `{"$type":"EnumBundleElement","id":"3-1","name":"First","localizedName":"Localized","presentation":"Presented"}`,
			values: []youtrack.Value{{ID: "3-1", Text: "First", LocalizedName: "Localized"}}},
		{name: "enums", valueType: "enum", multi: true,
			value:  `[` + element("First") + `,` + element("Second") + `]`,
			values: []youtrack.Value{{ID: "3-5", Text: "First"}, {ID: "3-6", Text: "Second"}}},
		{name: "a state", valueType: "state",
			value:  `{"$type":"StateBundleElement","id":"3-2","name":"In Progress","localizedName":"В работе","isResolved":false}`,
			values: []youtrack.Value{{ID: "3-2", Text: "In Progress", LocalizedName: "В работе"}}},
		{name: "a state whose translation is null", valueType: "state",
			value:  `{"$type":"StateBundleElement","id":"3-2","name":"Open","localizedName":null}`,
			values: []youtrack.Value{{ID: "3-2", Text: "Open"}}},
		{name: "a version", valueType: "version", value: `{"$type":"VersionBundleElement","id":"3-3","name":"First","released":true}`,
			values: []youtrack.Value{{ID: "3-3", Text: "First"}}},
		{name: "versions", valueType: "version", multi: true, value: `[{"$type":"VersionBundleElement","id":"3-3","name":"First"}]`,
			values: []youtrack.Value{{ID: "3-3", Text: "First"}}},
		{name: "a build", valueType: "build", value: `{"$type":"BuildBundleElement","id":"3-4","name":"First"}`,
			values: []youtrack.Value{{ID: "3-4", Text: "First"}}},
		{name: "builds", valueType: "build", multi: true, value: `[{"$type":"BuildBundleElement","id":"3-4","name":"First"}]`,
			values: []youtrack.Value{{ID: "3-4", Text: "First"}}},
		{name: "an owned value", valueType: "ownedField",
			value:  `{"$type":"OwnedBundleElement","id":"3-7","name":"First","owner":{"$type":"User","login":"second"}}`,
			values: []youtrack.Value{{ID: "3-7", Text: "First"}}},
		{name: "owned values", valueType: "ownedField", multi: true, value: `[{"$type":"OwnedBundleElement","id":"3-7","name":"First"}]`,
			values: []youtrack.Value{{ID: "3-7", Text: "First"}}},
		{name: "a user", valueType: "user", value: `{"$type":"User","id":"1-5","login":"first","fullName":"Named"}`,
			values: []youtrack.Value{{ID: "1-5", Text: "first"}}},
		{name: "users", valueType: "user", multi: true, value: `[{"$type":"User","id":"1-5","login":"first"},{"$type":"User","id":"1-6","login":"second"}]`,
			values: []youtrack.Value{{ID: "1-5", Text: "first"}, {ID: "1-6", Text: "second"}}},
		{name: "a value the server names without an id", valueType: "enum", value: `{"$type":"EnumBundleElement","name":"First"}`,
			values: []youtrack.Value{{Text: "First"}}},
		{name: "a group", valueType: "group", value: `{"$type":"UserGroup","id":"4-1","name":"First"}`,
			values: []youtrack.Value{{ID: "4-1", Text: "First"}}},
		{name: "groups", valueType: "group", multi: true, value: `[{"$type":"UserGroup","id":"4-1","name":"First"}]`,
			values: []youtrack.Value{{ID: "4-1", Text: "First"}}},
		{name: "a period", valueType: "period", value: `{"$type":"PeriodValue","id":"PT1H30M","minutes":90,"presentation":"1h 30m"}`,
			values: []youtrack.Value{{Text: "PT1H30M"}}},
		{name: "a period the server counts in working days", valueType: "period", value: `{"$type":"PeriodValue","minutes":1635,"id":"P3DT3H15M"}`,
			values: []youtrack.Value{{Text: "PT27H15M"}}},
		{name: "a period of whole hours", valueType: "period", value: `{"$type":"PeriodValue","minutes":60}`,
			values: []youtrack.Value{{Text: "PT1H"}}},
		{name: "a period of no time at all", valueType: "period", value: `{"$type":"PeriodValue","minutes":0}`,
			values: []youtrack.Value{{Text: "PT0M"}}},
		{name: "a date", valueType: "date", value: `1789560000000`, values: []youtrack.Value{{Text: "2026-09-16"}}},
		{name: "a date and time", valueType: "date and time", value: `1788134400000`, values: []youtrack.Value{{Text: "2026-08-31T00:00:00Z"}}},
		{name: "a date and time with milliseconds", valueType: "date and time", value: `1788134400123`, values: []youtrack.Value{{Text: "2026-08-31T00:00:00.123Z"}}},
		{name: "an integer", valueType: "integer", value: `1`, values: []youtrack.Value{{Text: "1"}}},
		{name: "a float", valueType: "float", value: `1.50`, values: []youtrack.Value{{Text: "1.5"}}},
		{name: "a string", valueType: "string", value: `"First"`, values: []youtrack.Value{{Text: "First"}}},
		{name: "a text", valueType: "text", value: `{"$type":"TextFieldValue","id":"6-1","text":"\n  First\nSecond","markdownText":"<div>First</div>"}`,
			values: []youtrack.Value{{Text: "\n  First\nSecond"}}},
		{name: "a field holding nothing", valueType: "enum", value: `null`},
		{name: "a field holding no value of several", valueType: "enum", multi: true, value: `[]`},
		{name: "a text field holding no text", valueType: "text", value: `{"$type":"TextFieldValue","text":null}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			held := heldValue{name: "Field", localized: "Поле", valueType: tc.valueType, multi: tc.multi, value: tc.value}
			server := fake.Serve(t, fake.JSON(http.StatusOK, issueHolding(t, held)))

			issue, err := client(t, server).Issue(t.Context(), "DEV-1", "")

			require.NoError(t, err)
			want := &youtrack.Issue{
				ID: "2-1", IDReadable: "DEV-1", Summary: "First",
				Project: youtrack.Project{ID: "0-1", ShortName: "DEV", Name: "Development"},
				Fields:  []youtrack.Field{{Name: "Field", LocalizedName: "Поле", Type: fieldType(tc.valueType, tc.multi), Values: tc.values}},
				Links:   []youtrack.Link{},
				Tree:    issue.Tree,
			}
			assert.Equal(t, want, issue)
			assert.Equal(t, requestTo(http.MethodGet, server, issuePath+"?fields="+issueFields), lastRequest(t, server))
		})
	}
}

func TestIssueOrdersTheFieldsAsTheProjectDoes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		held  []heldValue
		names []string
	}{
		{
			name: "by their place in the project",
			held: []heldValue{
				{name: "Second", valueType: "string", value: `"b"`, ordinal: "2", binding: "1-1"},
				{name: "First", valueType: "string", value: `"a"`, ordinal: "1", binding: "1-2"},
				{name: "Third", valueType: "string", value: `"c"`, ordinal: "8", binding: "1-3"},
			},
			names: []string{"First", "Second", "Third"},
		},
		{
			name: "of one place, by the numbers of their bindings",
			held: []heldValue{
				{name: "Tenth", valueType: "string", value: `"b"`, ordinal: "3", binding: "1-10"},
				{name: "Ninth", valueType: "string", value: `"a"`, ordinal: "3", binding: "1-9"},
			},
			names: []string{"Ninth", "Tenth"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, issueHolding(t, tc.held...)))

			issue, err := client(t, server).Issue(t.Context(), "DEV-1", "")

			require.NoError(t, err)
			names := make([]string, 0, len(issue.Fields))
			for _, f := range issue.Fields {
				names = append(names, f.Name)
			}
			assert.Equal(t, tc.names, names)
		})
	}
}

func TestIssueRefusesCustomFieldsOfAnotherShape(t *testing.T) {
	t.Parallel()
	field := func(name, binding string) string {
		return `[{"$type":"IssueCustomField","name":` + name + `,"value":null,"projectCustomField":` + binding + `}]`
	}
	binding := func(id, valueType string) string {
		return `{"$type":"ProjectCustomField","id":` + id + `,"ordinal":1,"field":{"$type":"CustomField","localizedName":null,` +
			`"fieldType":{"$type":"FieldType","valueType":` + valueType + `,"isMultiValue":false}}}`
	}
	held := func(valueType string, multi bool, value string) string {
		return string(customFieldsJSON(heldValue{name: "Field", valueType: valueType, multi: multi, value: value}))
	}
	tests := []struct {
		name  string
		block string
	}{
		{name: "the block is no array", block: `null`},
		{name: "a field is no object", block: `[null]`},
		{name: "a name is no text", block: field(`5`, binding(`"1-1"`, `"enum"`))},
		{name: "the field of the project is no object", block: field(`"Field"`, `[`+binding(`"1-1"`, `"enum"`)+`]`)},
		{name: "the binding to the project is named by no text", block: field(`"Field"`, binding(`5`, `"enum"`))},
		{name: "the type of the field is no text", block: field(`"Field"`, binding(`"1-1"`, `5`))},
		{name: "a translation that is a number", block: field(`"Field"`, `{"$type":"ProjectCustomField","id":"1-1","ordinal":1,"field":{"$type":"CustomField","localizedName":5,"fieldType":{"$type":"FieldType","valueType":"enum","isMultiValue":false}}}`)},
		{name: "a type the module does not model", block: held("quantum", false, element("First"))},
		{name: "one value by the type and a list in the answer", block: held("enum", false, `[`+element("First")+`]`)},
		{name: "several values by the type and one in the answer", block: held("enum", true, element("First"))},
		{name: "a value carrying nothing its type names it by", block: held("user", false, `{"$type":"PeriodValue","minutes":90}`)},
		{name: "a value that is no object where a name is", block: held("enum", false, `"First"`)},
		{name: "a name that is a number", block: held("enum", false, `{"$type":"EnumBundleElement","name":5}`)},
		{name: "a whole number that is text", block: held("integer", false, `"42"`)},
		{name: "a number that is text", block: held("float", false, `"1.5"`)},
		{name: "a string that is a number", block: held("string", false, `42`)},
		{name: "a day that is a fraction", block: held("date", false, `1.5`)},
		{name: "minutes that are text", block: held("period", false, `{"$type":"PeriodValue","minutes":"90"}`)},
		{
			name: "no place among the fields of the project",
			block: `[{"$type":"IssueCustomField","name":"Field","value":null,"projectCustomField":{"$type":"ProjectCustomField",` +
				`"id":"1-1","ordinal":null,"field":{"$type":"CustomField","localizedName":null,"fieldType":{"$type":"FieldType",` +
				`"valueType":"enum","isMultiValue":false}}}}]`,
		},
		{
			name: "two fields of one name",
			block: string(customFieldsJSON(
				heldValue{name: "Field", valueType: "enum", binding: "1-1"},
				heldValue{name: "Field", valueType: "state", binding: "1-2"})),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := issueJSON(t, map[string]any{"customFields": json.RawMessage(tc.block)})
			server := fake.Serve(t, fake.JSON(http.StatusOK, body))

			_, err := client(t, server).Issue(t.Context(), "DEV-1", "")

			assert.Equal(t, invalidAnswer(lastRequest(t, server), body), responseErrorOf(t, err))
		})
	}
}

func TestIssueReadsTheDescriptionAndTheLinks(t *testing.T) {
	t.Parallel()
	links := json.RawMessage(`[
		{"$type":"IssueLink","direction":"INWARD","linkType":{"$type":"IssueLinkType","name":"Subtask","sourceToTarget":"parent for","targetToSource":"subtask of"},
		 "issues":[{"$type":"Issue","id":"2-7","idReadable":"DEV-7"}]},
		{"$type":"IssueLink","direction":"OUTWARD","linkType":{"$type":"IssueLinkType","name":"Subtask","sourceToTarget":"parent for","targetToSource":"subtask of"},"issues":[]},
		{"$type":"IssueLink","direction":"BOTH","linkType":{"$type":"IssueLinkType","name":"Relates","sourceToTarget":"relates to","targetToSource":null},
		 "issues":[{"$type":"Issue","id":"2-8","idReadable":"DEV-8"},{"$type":"Issue","id":"2-9","idReadable":"DOCS-9"}]}
	]`)
	server := fake.Serve(t, fake.JSON(http.StatusOK, issueJSON(t, map[string]any{
		"description":  "First\nSecond",
		"customFields": json.RawMessage(`[]`),
		"links":        links,
	})))

	issue, err := client(t, server).Issue(t.Context(), "DEV-1", "")

	require.NoError(t, err)
	assert.Equal(t, "First\nSecond", issue.Description)
	subtask := youtrack.LinkType{Name: "Subtask", SourceToTarget: "parent for", TargetToSource: "subtask of"}
	assert.Equal(t, []youtrack.Link{
		{Direction: youtrack.Inward, Type: subtask, Issues: []youtrack.IssueRef{{ID: "2-7", IDReadable: "DEV-7"}}},
		{Direction: youtrack.Outward, Type: subtask, Issues: []youtrack.IssueRef{}},
		{Direction: youtrack.Both, Type: youtrack.LinkType{Name: "Relates", SourceToTarget: "relates to"},
			Issues: []youtrack.IssueRef{{ID: "2-8", IDReadable: "DEV-8"}, {ID: "2-9", IDReadable: "DOCS-9"}}},
	}, issue.Links)
}

func TestIssueReadsAnIssueWithoutADescriptionOrLinks(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, issueJSON(t, map[string]any{
		"description": nil, "customFields": json.RawMessage(`[]`), "links": nil,
	})))

	issue, err := client(t, server).Issue(t.Context(), "DEV-1", "")

	require.NoError(t, err)
	assert.Equal(t, "", issue.Description)
	assert.Nil(t, issue.Links)
}

func TestIssueRefusesAnIssueOfAnotherShape(t *testing.T) {
	t.Parallel()
	link := func(members string) json.RawMessage {
		return json.RawMessage(`[{"$type":"IssueLink",` + members + `}]`)
	}
	tests := []struct {
		name    string
		members map[string]any
	}{
		{name: "a readable id that is a number", members: map[string]any{"idReadable": 5}},
		{name: "a summary that is null", members: map[string]any{"summary": nil}},
		{name: "a description that is a number", members: map[string]any{"description": 5}},
		{name: "a project that is no object", members: map[string]any{"project": "DEV"}},
		{name: "a project without a short name", members: map[string]any{"project": map[string]any{"id": "0-1", "name": "Development"}}},
		{name: "no custom fields at all", members: map[string]any{"customFields": nil}},
		{name: "links that are an object", members: map[string]any{"links": map[string]any{}}},
		{name: "a link slot that is null", members: map[string]any{"links": json.RawMessage(`[null]`)}},
		{name: "a link without a direction", members: map[string]any{"links": link(`"linkType":{"name":"Subtask","sourceToTarget":"a","targetToSource":"b"},"issues":[]`)}},
		{name: "a link type without a name", members: map[string]any{"links": link(`"direction":"BOTH","linkType":{"sourceToTarget":"a","targetToSource":"b"},"issues":[]`)}},
		{name: "a link whose issues are null", members: map[string]any{"links": link(`"direction":"BOTH","linkType":{"name":"Relates","sourceToTarget":"a","targetToSource":"b"},"issues":null`)}},
		{name: "a linked issue without a readable id", members: map[string]any{"links": link(`"direction":"BOTH","linkType":{"name":"Relates","sourceToTarget":"a","targetToSource":"b"},"issues":[{"id":"2-7"}]`)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := issueJSON(t, tc.members)
			server := fake.Serve(t, fake.JSON(http.StatusOK, body))

			_, err := client(t, server).Issue(t.Context(), "DEV-1", "")

			assert.Equal(t, invalidAnswer(lastRequest(t, server), body), responseErrorOf(t, err))
		})
	}
}

func TestIssueFindsAFieldByEitherOfItsNames(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, issueHolding(t,
		heldValue{name: "Shared", valueType: "string", value: `"own"`, binding: "1-1"},
		heldValue{name: "Named", localized: "Shared", valueType: "string", value: `"translated"`, binding: "1-2"},
		heldValue{name: "Other", localized: "Другое", valueType: "string", value: `"other"`, binding: "1-3"},
	)))
	issue, err := client(t, server).Issue(t.Context(), "DEV-1", "")
	require.NoError(t, err)
	tests := []struct {
		name  string
		asked string
		text  string
		found bool
	}{
		{name: "a name", asked: "Named", text: "translated", found: true},
		{name: "a name in another letter case", asked: "NAMED", text: "translated", found: true},
		{name: "a translation", asked: "другое", text: "other", found: true},
		{name: "a name that is also the translation of another field", asked: "shared", text: "own", found: true},
		{name: "a name of no field", asked: "Nothing"},
		{name: "an empty name", asked: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f, found := issue.Field(tc.asked)

			assert.Equal(t, tc.found, found)
			if tc.found {
				assert.Equal(t, []string{tc.text}, f.Texts())
			}
		})
	}
}

func TestIssueAsksForItsOwnFieldsAndTheCallersOnTop(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
		sent       string
	}{
		{name: "nothing beyond its own", expression: "", sent: issueFields},
		{name: "a member of the issue", expression: "created", sent: issueFields + ",created"},
		{name: "members with spaces around them", expression: " created , commentsCount ", sent: issueFields + ",created,commentsCount"},
		{name: "a member it already asks for", expression: "summary,description,project(name),links(direction)", sent: issueFields},
		{name: "a name under a value", expression: "customFields(value(presentation))",
			sent: "id,idReadable,summary,description,project(id,shortName,name),customFields(name,value(name,login,minutes,text,id,localizedName,presentation)," +
				"projectCustomField(id,ordinal,field(localizedName,fieldType(valueType,isMultiValue))))," +
				"links(direction,linkType(name,sourceToTarget,targetToSource),issues(id,idReadable))"},
		{name: "the custom fields whole", expression: "customFields", sent: issueFields},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, issueHolding(t)))

			_, err := client(t, server).Issue(t.Context(), "DEV-1", tc.expression)

			require.NoError(t, err)
			assert.Equal(t, tc.sent, server.Last(t).URL.Query().Get("fields"))
		})
	}
}

func TestIssueCarriesWhatTheCallerAskedForInTheTree(t *testing.T) {
	t.Parallel()
	body := issueJSON(t, map[string]any{"customFields": json.RawMessage(`[]`), "description": "First\nSecond", "commentsCount": 3})
	server := fake.Serve(t, fake.JSON(http.StatusOK, body))

	issue, err := client(t, server).Issue(t.Context(), "DEV-1", "description,commentsCount")

	require.NoError(t, err)
	assert.Equal(t, "First\nSecond", issue.Tree["description"])
	assert.Equal(t, json.Number("3"), issue.Tree["commentsCount"])
	assert.Empty(t, issue.Fields)
}

func TestIssueRefusesACallItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		id         string
		expression string
		argument   string
		value      string
	}{
		{name: "an empty id", id: "", argument: "id"},
		{name: "a project code alone", id: "DEV", argument: "id", value: "DEV"},
		{name: "an id of an article", id: "DEV-A-1", argument: "id", value: "DEV-A-1"},
		{name: "an internal id", id: "2-1", argument: "id", value: "2-1"},
		{name: "an id with no number", id: "DEV-", argument: "id", value: "DEV-"},
		{name: "fields that close nothing", id: "DEV-1", expression: "summary(", argument: "fields", value: "summary("},
		{name: "fields that open nothing", id: "DEV-1", expression: "summary)", argument: "fields", value: "summary)"},
		{name: "fields with a comma too many", id: "DEV-1", expression: "summary,,created", argument: "fields", value: "summary,,created"},
		{name: "fields with a space inside a name", id: "DEV-1", expression: "sum mary", argument: "fields", value: "sum mary"},
		{name: "fields of a comma alone", id: "DEV-1", expression: ",", argument: "fields", value: ","},
		{name: "fields with a cyrillic name", id: "DEV-1", expression: "поле", argument: "fields", value: "поле"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			_, err := client(t, server).Issue(t.Context(), tc.id, tc.expression)

			assert.Equal(t, youtrack.ArgumentError{Argument: tc.argument, Value: tc.value}, argumentErrorOf(t, err))
		})
	}
}

func TestIssueSendsTheReadableIdAsWritten(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, issueHolding(t)))

	_, err := client(t, server).Issue(t.Context(), "dev-01", "")

	require.NoError(t, err)
	assert.Equal(t, []string{"/api/issues/dev-01"}, server.Paths())
}
