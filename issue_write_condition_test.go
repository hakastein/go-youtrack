package youtrack_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/go-youtrack"
	"github.com/hakastein/go-youtrack/fake"
)

func issueCondition(watched string, forNothing bool, values ...string) string {
	return `{"$type":"FieldBasedCondition","showForNullValue":` + strconv.FormatBool(forNothing) +
		`,"field":{"$type":"ProjectCustomField","id":` + strconv.Quote(watched) + `},"values":` + issueNames(values...) + `}`
}

func issueShownField(condition string) issueField {
	return issueField{metaField: enumField("1-2", "Shown"), condition: condition}
}

func issueRequiredShown(condition string) issueField {
	shown := issueShownField(condition)
	shown.required = true
	return shown
}

func issueWatched(valueType string, defaults ...string) issueField {
	return issueField{metaField: metaField{id: "1-1", name: "Watched", valueType: valueType}, defaults: defaults}
}

func TestCreateIssueNamesEveryRequiredFieldItLeavesEmpty(t *testing.T) {
	t.Parallel()
	required := func(id, name string, multi bool) issueField {
		return issueField{metaField: metaField{id: id, name: name, valueType: "enum", multi: multi, required: true}}
	}
	manyWatched := issueWatched("enum")
	manyWatched.multi = true
	goneWatched := issueWatched("state")
	goneWatched.id = "1-9"
	tests := []struct {
		name    string
		fields  []issueField
		filled  []youtrack.FieldWrite
		missing []string
	}{
		{
			name: "every field that holds one value or several",
			fields: []issueField{
				required("1-2", "First", false),
				required("1-3", "Second", true),
				{metaField: enumField("1-4", "Optional")},
				{metaField: metaField{id: "1-5", name: "Filled by the project", valueType: "enum", required: true}, defaults: []string{"Early"}},
			},
			missing: []string{"First", "Second"},
		},
		{
			name:    "a field the call names another",
			fields:  []issueField{required("1-2", "First", false), required("1-3", "Second", false)},
			filled:  []youtrack.FieldWrite{issueFill("Second", "x")},
			missing: []string{"First"},
		},
		{
			name:    "a field a value of the project uncovers",
			fields:  []issueField{issueWatched("state", "late"), issueRequiredShown(issueCondition("1-1", false, "Late"))},
			missing: []string{"Shown"},
		},
		{
			name:    "a field shown for nothing in the field it watches",
			fields:  []issueField{issueWatched("state"), issueRequiredShown(issueCondition("1-1", true, "Late"))},
			missing: []string{"Shown"},
		},
		{
			name:    "a field that watches a field holding several values",
			fields:  []issueField{manyWatched, issueRequiredShown(issueCondition("1-1", false, "Late"))},
			missing: []string{"Shown"},
		},
		{
			name:    "a field that watches a field the project no longer has",
			fields:  []issueField{goneWatched, issueRequiredShown(issueCondition("1-1", false, "Late"))},
			missing: []string{"Shown"},
		},
		{
			name:    "a field the call uncovers",
			fields:  []issueField{issueWatched("state", "Early"), issueRequiredShown(issueCondition("1-1", false, "Late"))},
			filled:  []youtrack.FieldWrite{issueFill("Watched", "Late")},
			missing: []string{"Shown"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := issueWriting(t, issueConditional(tc.fields...), "[]", fake.Unexpected(t))

			_, err := issueCreate(t, server, issueFilling(tc.filled...), "idReadable")

			want := youtrack.Error{Code: youtrack.CodeMissingRequired, Details: []youtrack.Pair{
				issueMetadataRequest(server), issueProjectDetail(), {Key: "missing", Value: texts(tc.missing...)},
			}}
			assert.Equal(t, want, errorOf(t, err))
			assert.Equal(t, []string{projectPath}, server.Paths())
		})
	}
}

func TestCreateIssueFilesAnIssueWithoutAFieldNobodyAsksFor(t *testing.T) {
	t.Parallel()
	requiredShown := issueField{metaField: metaField{id: "1-2", name: "Shown", valueType: "enum", required: true}}
	filledShown := requiredShown
	filledShown.defaults = []string{"First"}
	tests := []struct {
		name   string
		fields []issueField
		filled []youtrack.FieldWrite
		held   []issueHeld
	}{
		{name: "a field that may stand empty", fields: []issueField{issueShownField("")}},
		{name: "a required field the project fills itself", fields: []issueField{filledShown}},
		{
			name:   "a required field the call fills",
			fields: []issueField{requiredShown},
			filled: []youtrack.FieldWrite{issueFill("Shown", "First")},
			held:   []issueHeld{{name: "Shown", valueType: "enum", value: issueElement("First")}},
		},
		{
			name:   "a required field a condition hides",
			fields: []issueField{issueWatched("state", "Early"), issueRequiredShown(issueCondition("1-1", false, "Late"))},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			answer := issueWritten(t, map[string]any{"summary": "First", "customFields": issueHeldFields(tc.held...)})
			server := issueWriting(t, issueConditional(tc.fields...), "[]", fake.JSON(http.StatusOK, answer))

			node, err := issueCreate(t, server, issueFilling(tc.filled...), "idReadable")

			require.NoError(t, err)
			assert.Equal(t, issueWrittenID(), node)
		})
	}
}

func TestCreateIssueRefusesAValueAConditionHides(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		watched issueField
		shownAt string
		filled  []youtrack.FieldWrite
	}{
		{
			name:    "the project fills the field it watches with a value it does not show at",
			watched: issueWatched("state", "Early"),
			shownAt: issueCondition("1-1", false, "Late", "Later"),
		},
		{
			name:    "nothing stands in the field it watches",
			watched: issueWatched("state"),
			shownAt: issueCondition("1-1", false, "Late"),
		},
		{
			name:    "it shows for nothing as well, and the field it watches holds another value",
			watched: issueWatched("state", "Early"),
			shownAt: issueCondition("1-1", true, "Late"),
		},
		{
			name:    "it names no value and does not show for nothing either",
			watched: issueWatched("state", "Early"),
			shownAt: issueCondition("1-1", false),
		},
		{
			name:    "the call writes a value it does not show at over one it does",
			watched: issueWatched("state", "Late"),
			shownAt: issueCondition("1-1", false, "Late"),
			filled:  []youtrack.FieldWrite{issueFill("Watched", "Early")},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := issueWriting(t, issueConditional(tc.watched, issueShownField(tc.shownAt)), "[]", fake.Unexpected(t))

			_, err := issueCreate(t, server, issueFilling(append(tc.filled, issueFill("Shown", "First"))...), "idReadable")

			kept, invalid := issueWriteError(t, err)
			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage, Details: []youtrack.Pair{
				issueMetadataRequest(server), issueProjectDetail(), {Key: "invalid"},
			}}, kept)
			assert.Equal(t, []issueInvalid{{field: "Shown", value: "First"}}, invalid)
			assert.Equal(t, []string{projectPath}, server.Paths())
		})
	}
}

func TestCreateIssueSendsAValueAConditionShows(t *testing.T) {
	t.Parallel()
	const sentShown = `{"$type":"SingleEnumIssueCustomField","name":"Shown","value":{"name":"First"}}`
	shown := issueHeld{name: "Shown", valueType: "enum", value: issueElement("First")}
	manyWatched := issueWatched("enum", "Early")
	manyWatched.multi = true
	goneWatched := issueWatched("state", "Early")
	goneWatched.id = "1-9"
	tests := []struct {
		name    string
		watched issueField
		shownAt string
		filled  []youtrack.FieldWrite
		held    []issueHeld
		sent    string
	}{
		{
			name:    "the call writes a value it shows at into the field it watches, in another letter case",
			watched: issueWatched("state", "Early"),
			shownAt: issueCondition("1-1", false, "Late"),
			filled:  []youtrack.FieldWrite{issueFill("Watched", "late")},
			held:    []issueHeld{{name: "Watched", valueType: "state", value: issueElement("Late")}, shown},
			sent:    `{"$type":"StateIssueCustomField","name":"Watched","value":{"name":"late"}},` + sentShown,
		},
		{
			name:    "the project fills the field it watches with a value it shows at",
			watched: issueWatched("state", "Late"),
			shownAt: issueCondition("1-1", false, "Late"),
			held:    []issueHeld{shown},
			sent:    sentShown,
		},
		{
			name:    "nothing stands in the field it watches and it shows for nothing",
			watched: issueWatched("state"),
			shownAt: issueCondition("1-1", true, "Late"),
			held:    []issueHeld{shown},
			sent:    sentShown,
		},
		{
			name:    "the field it watches holds several values",
			watched: manyWatched,
			shownAt: issueCondition("1-1", false, "Late"),
			held:    []issueHeld{shown},
			sent:    sentShown,
		},
		{
			name:    "the project no longer has the field it watches",
			watched: goneWatched,
			shownAt: issueCondition("1-1", false, "Late"),
			held:    []issueHeld{shown},
			sent:    sentShown,
		},
		{
			name:    "it watches no field",
			watched: issueWatched("state", "Early"),
			shownAt: `{"$type":"FieldBasedCondition","showForNullValue":false,"field":null,"values":[]}`,
			held:    []issueHeld{shown},
			sent:    sentShown,
		},
		{
			name:    "it is of a kind the module does not evaluate",
			watched: issueWatched("state", "Early"),
			shownAt: `{"$type":"CustomFieldCondition","id":"2-1"}`,
			held:    []issueHeld{shown},
			sent:    sentShown,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			project := issueConditional(tc.watched, issueShownField(tc.shownAt))
			answer := issueWritten(t, map[string]any{"summary": "First", "customFields": issueHeldFields(tc.held...)})
			server := issueWriting(t, project, "[]", fake.JSON(http.StatusOK, answer))

			_, err := issueCreate(t, server, issueFilling(append(tc.filled, issueFill("Shown", "First"))...), "idReadable")

			require.NoError(t, err)
			assert.JSONEq(t, issueCreatedBody(tc.sent), server.Last(t).Body)
		})
	}
}

func TestUpdateIssueRefusesToEmptyAFieldTheProjectRequires(t *testing.T) {
	t.Parallel()
	project := issueProject(
		metaField{id: "1-1", name: "First", valueType: "enum", required: true},
		metaField{id: "1-2", name: "Second", valueType: "enum", multi: true, required: true},
		enumField("1-3", "Optional"),
	)
	server := issueWriting(t, project, "[]", fake.Unexpected(t))

	_, err := issueUpdate(t, server, youtrack.IssueUpdate{Fields: []youtrack.FieldWrite{
		issueClear("Optional"), issueClear("second"), issueClear("First"),
	}}, "idReadable")

	want := youtrack.Error{Code: youtrack.CodeMissingRequired, Details: []youtrack.Pair{
		issueReadRequest(server), issueProjectDetail(), {Key: "missing", Value: texts("First", "Second")},
	}}
	assert.Equal(t, want, errorOf(t, err))
	assert.Equal(t, []string{issuePath}, server.Paths())
}
