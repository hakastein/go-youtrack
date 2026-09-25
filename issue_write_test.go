package youtrack_test

import (
	"cmp"
	"context"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/youtrack"
	"github.com/hakastein/youtrack/fake"
)

const (
	issueMetadataFields = "id,shortName,customFields(id,canBeEmpty,defaultValues(name)," +
		"condition($type,showForNullValue,field(id),values(name))," +
		"field(name,localizedName,fieldType(valueType,isMultiValue)))"
	issueToWriteFields = "idReadable,customFields($type,name,projectCustomField(id)),project(" + issueMetadataFields + ")"
)

type issueField struct {
	metaField
	defaults  []string
	condition string
}

func (f issueField) metadata() string {
	return `{"$type":"ProjectCustomField","id":` + strconv.Quote(f.id) +
		`,"canBeEmpty":` + strconv.FormatBool(!f.required) +
		`,"defaultValues":` + issueNames(f.defaults...) +
		`,"condition":` + cmp.Or(f.condition, "null") +
		`,"field":` + f.naming() + `}`
}

func issueNames(names ...string) string {
	elements := make([]string, 0, len(names))
	for _, name := range names {
		elements = append(elements, issueElement(name))
	}
	return "[" + strings.Join(elements, ",") + "]"
}

func issueProject(fields ...metaField) string {
	shown := make([]issueField, 0, len(fields))
	for _, f := range fields {
		shown = append(shown, issueField{metaField: f})
	}
	return issueConditional(shown...)
}

func issueConditional(fields ...issueField) string {
	metadata := make([]string, 0, len(fields))
	for _, f := range fields {
		metadata = append(metadata, f.metadata())
	}
	return projectOf("[" + strings.Join(metadata, ",") + "]")
}

func issueWritten(t *testing.T, members map[string]any) string {
	t.Helper()
	issue := map[string]any{"$type": "Issue", "idReadable": "DEV-1"}
	maps.Copy(issue, members)
	answer, err := json.Marshal(issue)
	require.NoError(t, err)
	return string(answer)
}

type issueClass struct {
	name    string
	class   string
	binding string
}

func issueClasses(held ...issueClass) string {
	fields := make([]string, 0, len(held))
	for _, f := range held {
		fields = append(fields, `{"$type":`+strconv.Quote(f.class)+`,"name":`+strconv.Quote(f.name)+
			`,"projectCustomField":{"$type":"ProjectCustomField","id":`+strconv.Quote(f.binding)+`}}`)
	}
	return "[" + strings.Join(fields, ",") + "]"
}

func issueToWrite(project, classes string) string {
	return `{"$type":"Issue","idReadable":"DEV-1","customFields":` + classes + `,"project":` + project + `}`
}

func issueWriting(t *testing.T, project, classes string, write http.HandlerFunc) *fake.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+projectPath, fake.JSON(http.StatusOK, project))
	mux.HandleFunc("GET "+issuePath, fake.JSON(http.StatusOK, issueToWrite(project, classes)))
	mux.HandleFunc("POST "+issuesPath, write)
	mux.HandleFunc("POST "+issuePath, write)
	return fake.Serve(t, mux.ServeHTTP)
}

func issueCreate(t *testing.T, server *fake.Server, in youtrack.IssueInput, fields string) (*youtrack.Node, error) {
	t.Helper()
	return client(t, server).Issues.Create(t.Context(), "DEV", &in, answeredWith(fields))
}

func issueUpdate(t *testing.T, server *fake.Server, in youtrack.IssueUpdate, fields string) (*youtrack.Node, error) {
	t.Helper()
	return client(t, server).Issues.Update(t.Context(), "DEV-1", &in, answeredWith(fields))
}

func issueFill(name string, values ...string) youtrack.FieldWrite {
	return youtrack.FieldWrite{Name: name, Values: values}
}

func issueClear(name string) youtrack.FieldWrite {
	return youtrack.FieldWrite{Name: name, Clear: true}
}

func issueWrittenID() *youtrack.Node {
	return youtrack.NewMap(youtrack.Pair{Key: "idReadable", Value: youtrack.NewString("DEV-1")})
}

func issueMismatch(request youtrack.Pair, mismatches ...*youtrack.Node) youtrack.Error {
	return youtrack.Error{Code: youtrack.CodeUpstreamInvalid, AfterWrite: true, Details: []youtrack.Pair{
		request,
		{Key: "issue", Value: youtrack.NewString("DEV-1")},
		{Key: "mismatch", Value: youtrack.NewList(mismatches...)},
	}}
}

type issueInvalid struct {
	field string
	value string
}

// The reason of an entry under invalid is prose, and only the field and the value name what was refused.
func issueWriteError(t *testing.T, err error) (youtrack.Error, []issueInvalid) {
	t.Helper()
	kept := errorOf(t, err)
	kept.Details = slices.Clone(kept.Details)
	at := slices.IndexFunc(kept.Details, func(pair youtrack.Pair) bool { return pair.Key == "invalid" })
	require.GreaterOrEqual(t, at, 0, "the error names nothing under invalid: %v", kept.Details)
	var entries []issueInvalid
	for _, entry := range kept.Details[at].Value.Items() {
		field, named := entry.Lookup("field")
		value, given := entry.Lookup("value")
		reason, said := entry.Lookup("reason")
		require.True(t, named && given && said, "an entry under invalid lacks a member: %v", entry)
		assert.NotEmpty(t, reason.Value())
		entries = append(entries, issueInvalid{field: field.Value(), value: value.Value()})
	}
	kept.Details[at].Value = nil
	return kept, entries
}

func issueMetadataRequest(server *fake.Server) youtrack.Pair {
	return requestTo(http.MethodGet, server, projectPath+"?fields="+issueMetadataFields)
}

func issueReadRequest(server *fake.Server) youtrack.Pair {
	return requestTo(http.MethodGet, server, issuePath+"?fields="+issueToWriteFields)
}

func issueProjectDetail() youtrack.Pair {
	return youtrack.Pair{Key: "project", Value: youtrack.NewString("DEV")}
}

func TestCreateIssueRefusesACallBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		project    string
		in         *youtrack.IssueInput
		expression string
	}{
		{name: "no issue at all"},
		{name: "a project code of another form", project: "DEV-1", in: &youtrack.IssueInput{Summary: "First"}},
		{name: "an empty title", in: &youtrack.IssueInput{}},
		{name: "a line feed in the title", in: &youtrack.IssueInput{Summary: "a\nb"}},
		{name: "a carriage return in the title", in: &youtrack.IssueInput{Summary: "a\rb"}},
		{name: "a NEL in the title", in: &youtrack.IssueInput{Summary: "a\u0085b"}},
		{name: "a line separator in the title", in: &youtrack.IssueInput{Summary: "a\u2028b"}},
		{name: "a paragraph separator in the title", in: &youtrack.IssueInput{Summary: "a\u2029b"}},
		{name: "a title that is no UTF-8", in: &youtrack.IssueInput{Summary: "a\xffb"}},
		{name: "a carriage return in the description", in: &youtrack.IssueInput{Summary: "First", Description: "a\rb"}},
		{name: "a description that is no UTF-8", in: &youtrack.IssueInput{Summary: "First", Description: "a\xffb"}},
		{name: "a field of no name", in: &youtrack.IssueInput{Summary: "First", Fields: []youtrack.FieldWrite{issueFill("", "First")}}},
		{name: "a field given no value", in: &youtrack.IssueInput{Summary: "First", Fields: []youtrack.FieldWrite{{Name: "Field"}}}},
		{name: "a field emptied on an issue not filed yet",
			in: &youtrack.IssueInput{Summary: "First", Fields: []youtrack.FieldWrite{issueClear("Field")}}},
		{name: "the title written as a field", in: &youtrack.IssueInput{Summary: "First",
			Fields: []youtrack.FieldWrite{issueFill("summary", "First")}}},
		{name: "the description written as a field in another letter case", in: &youtrack.IssueInput{Summary: "First",
			Fields: []youtrack.FieldWrite{issueFill("DESCRIPTION", "First")}}},
		{name: "the comments of the issue", in: &youtrack.IssueInput{Summary: "First"}, expression: "+comments(text)"},
		{name: "a name under the custom fields of a linked issue", in: &youtrack.IssueInput{Summary: "First"},
			expression: "links(issues(customFields(name)))"},
		{name: "a name under a custom field", in: &youtrack.IssueInput{Summary: "First"}, expression: "customFields(Field(name))"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			issues := client(t, fake.ServeNothing(t)).Issues

			_, err := issues.Create(t.Context(), cmp.Or(tc.project, "DEV"), tc.in, answeredWith(tc.expression))

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

func TestUpdateIssueRefusesACallBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		id         string
		in         *youtrack.IssueUpdate
		expression string
	}{
		{name: "no update at all"},
		{name: "nothing to write", in: &youtrack.IssueUpdate{Fields: []youtrack.FieldWrite{}}},
		{name: "an id of an article", id: "DEV-A-1", in: &youtrack.IssueUpdate{Summary: new("First")}},
		{name: "an empty title", in: &youtrack.IssueUpdate{Summary: new("")}},
		{name: "a line feed in the title", in: &youtrack.IssueUpdate{Summary: new("a\nb")}},
		{name: "a carriage return in the description", in: &youtrack.IssueUpdate{Description: new("a\rb")}},
		{name: "an empty description", in: &youtrack.IssueUpdate{Description: new("")}},
		{name: "a description written and emptied at once", in: &youtrack.IssueUpdate{Description: new("First"), ClearDescription: true}},
		{name: "the title emptied", in: &youtrack.IssueUpdate{Fields: []youtrack.FieldWrite{issueClear("summary")}}},
		{name: "the title emptied in another letter case", in: &youtrack.IssueUpdate{Fields: []youtrack.FieldWrite{issueClear("SUMMARY")}}},
		{name: "the description emptied as a field", in: &youtrack.IssueUpdate{Fields: []youtrack.FieldWrite{issueClear("description")}}},
		{name: "a name of nothing emptied", in: &youtrack.IssueUpdate{Fields: []youtrack.FieldWrite{issueClear("")}}},
		{name: "a field given no value", in: &youtrack.IssueUpdate{Fields: []youtrack.FieldWrite{{Name: "Field"}}}},
		{name: "the comments of the issue", in: &youtrack.IssueUpdate{Summary: new("First")}, expression: "+comments(text)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			issues := client(t, fake.ServeNothing(t)).Issues

			_, err := issues.Update(t.Context(), cmp.Or(tc.id, "DEV-1"), tc.in, answeredWith(tc.expression))

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

func TestCreateIssueSendsTheTitleAndTheDescriptionAsWritten(t *testing.T) {
	t.Parallel()
	const title, description = "\t[First] ", "Second  \n---\n~~~\n😀"
	answer := issueWritten(t, map[string]any{"summary": title, "description": description})
	server := issueWriting(t, issueProject(), "[]", fake.JSON(http.StatusOK, answer))

	_, err := issueCreate(t, server, youtrack.IssueInput{Summary: title, Description: description}, "idReadable")

	require.NoError(t, err)
	assert.Equal(t, []string{projectPath, issuesPath}, server.Paths())
	assert.JSONEq(t, `{"project":{"id":"0-1"},"summary":"\t[First] ","description":"Second  \n---\n~~~\n😀"}`,
		server.Last(t).Body)
}

func TestCreateIssueFilesAnEmptyDescriptionAsNone(t *testing.T) {
	t.Parallel()
	server := issueWriting(t, issueProject(), "[]", fake.JSON(http.StatusOK, issueWritten(t, map[string]any{"summary": "First"})))

	_, err := issueCreate(t, server, youtrack.IssueInput{Summary: "First"}, "idReadable")

	require.NoError(t, err)
	assert.JSONEq(t, `{"project":{"id":"0-1"},"summary":"First"}`, server.Last(t).Body)
}

func TestUpdateIssueSendsOnlyThePartsItWrites(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		in     youtrack.IssueUpdate
		answer map[string]any
		body   string
	}{
		{name: "the title", in: youtrack.IssueUpdate{Summary: new("First")}, answer: map[string]any{"summary": "First"},
			body: `{"summary":"First"}`},
		{name: "the description", in: youtrack.IssueUpdate{Description: new("Second")},
			answer: map[string]any{"description": "Second"}, body: `{"description":"Second"}`},
		{name: "the description emptied", in: youtrack.IssueUpdate{ClearDescription: true},
			answer: map[string]any{"description": nil}, body: `{"description":null}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := issueWriting(t, issueProject(), "[]", fake.JSON(http.StatusOK, issueWritten(t, tc.answer)))

			_, err := issueUpdate(t, server, tc.in, "idReadable")

			require.NoError(t, err)
			assert.Equal(t, []string{issuePath, issuePath}, server.Paths())
			assert.JSONEq(t, tc.body, server.Last(t).Body)
		})
	}
}

type issueWrite func(ctx context.Context, issues *youtrack.IssuesService) (*youtrack.Node, error)

func issueCreating(in youtrack.IssueInput) issueWrite {
	return func(ctx context.Context, issues *youtrack.IssuesService) (*youtrack.Node, error) {
		return issues.Create(ctx, "DEV", &in, answeredWith("idReadable"))
	}
}

func issueUpdating(in youtrack.IssueUpdate) issueWrite {
	return func(ctx context.Context, issues *youtrack.IssuesService) (*youtrack.Node, error) {
		return issues.Update(ctx, "DEV-1", &in, answeredWith("idReadable"))
	}
}

func TestIssueWriteAsksForWhatItChecks(t *testing.T) {
	t.Parallel()
	project := issueProject(enumField("1-1", "Field"))
	filled := issueHeldFields(issueHeld{name: "Field", valueType: "enum", value: issueElement("First")})
	tests := []struct {
		name   string
		write  issueWrite
		answer map[string]any
		fields string
	}{
		{
			name:   "a title",
			write:  issueCreating(youtrack.IssueInput{Summary: "First"}),
			answer: map[string]any{"summary": "First"},
			fields: "idReadable,summary",
		},
		{
			name:   "a title and a description",
			write:  issueCreating(youtrack.IssueInput{Summary: "First", Description: "Second"}),
			answer: map[string]any{"summary": "First", "description": "Second"},
			fields: "idReadable,summary,description",
		},
		{
			name:   "a title and a custom field",
			write:  issueCreating(youtrack.IssueInput{Summary: "First", Fields: []youtrack.FieldWrite{issueFill("Field", "First")}}),
			answer: map[string]any{"summary": "First", "customFields": filled},
			fields: "idReadable,summary," + issueCustomFields,
		},
		{
			name:   "an emptied description",
			write:  issueUpdating(youtrack.IssueUpdate{ClearDescription: true}),
			answer: map[string]any{"description": nil},
			fields: "idReadable,description",
		},
		{
			name:   "an emptied custom field",
			write:  issueUpdating(youtrack.IssueUpdate{Fields: []youtrack.FieldWrite{issueClear("Field")}}),
			answer: map[string]any{"customFields": issueHeldFields()},
			fields: "idReadable," + issueCustomFields,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := issueWriting(t, project, "[]", fake.JSON(http.StatusOK, issueWritten(t, tc.answer)))

			_, err := tc.write(t.Context(), client(t, server).Issues)

			require.NoError(t, err)
			assert.Equal(t, tc.fields, server.Last(t).URL.Query().Get("fields"))
		})
	}
}

func TestIssueWritePrintsWhatTheCallerAskedFor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
		printed    *youtrack.Node
	}{
		{name: "less than the write checks", expression: "idReadable", printed: issueWrittenID()},
		{
			name:       "a part the write checks",
			expression: "idReadable,description",
			printed: youtrack.NewMap(
				youtrack.Pair{Key: "idReadable", Value: youtrack.NewString("DEV-1")},
				youtrack.Pair{Key: "description", Value: youtrack.NewText("Second")}),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			answer := issueWritten(t, map[string]any{"summary": "First", "description": "Second"})
			server := issueWriting(t, issueProject(), "[]", fake.JSON(http.StatusOK, answer))

			node, err := issueCreate(t, server, youtrack.IssueInput{Summary: "First", Description: "Second"}, tc.expression)

			require.NoError(t, err)
			assert.Equal(t, tc.printed, node)
		})
	}
}

func TestIssueWriteRefusesAnAnswerThatDisagreesWithTheText(t *testing.T) {
	t.Parallel()
	const created = issuesPath + "?fields=idReadable,summary,description"
	tests := []struct {
		name     string
		write    issueWrite
		answer   map[string]any
		target   string
		mismatch []*youtrack.Node
	}{
		{
			name:     "a title in another letter case",
			write:    issueCreating(youtrack.IssueInput{Summary: "Upper"}),
			answer:   map[string]any{"summary": "upper"},
			target:   issuesPath + "?fields=idReadable,summary",
			mismatch: []*youtrack.Node{mismatch("summary", youtrack.NewString("Upper"), youtrack.NewString("upper"))},
		},
		{
			name:     "a title with a space the server doubled",
			write:    issueUpdating(youtrack.IssueUpdate{Summary: new("a b")}),
			answer:   map[string]any{"summary": "a  b"},
			target:   issuePath + "?fields=idReadable,summary",
			mismatch: []*youtrack.Node{mismatch("summary", youtrack.NewString("a b"), youtrack.NewString("a  b"))},
		},
		{
			name:     "a description in another letter case",
			write:    issueCreating(youtrack.IssueInput{Summary: "First", Description: "Upper"}),
			answer:   map[string]any{"summary": "First", "description": "upper"},
			target:   created,
			mismatch: []*youtrack.Node{mismatch("description", youtrack.NewString("Upper"), youtrack.NewString("upper"))},
		},
		{
			name:     "a description the server kept none of",
			write:    issueCreating(youtrack.IssueInput{Summary: "First", Description: "Second"}),
			answer:   map[string]any{"summary": "First", "description": nil},
			target:   created,
			mismatch: []*youtrack.Node{mismatch("description", youtrack.NewString("Second"), youtrack.NewNull())},
		},
		{
			name:   "a title and a description both",
			write:  issueCreating(youtrack.IssueInput{Summary: "First", Description: "Second"}),
			answer: map[string]any{"summary": "Third", "description": "Fourth"},
			target: created,
			mismatch: []*youtrack.Node{
				mismatch("summary", youtrack.NewString("First"), youtrack.NewString("Third")),
				mismatch("description", youtrack.NewString("Second"), youtrack.NewString("Fourth")),
			},
		},
		{
			name:     "an emptied description that came back",
			write:    issueUpdating(youtrack.IssueUpdate{ClearDescription: true}),
			answer:   map[string]any{"description": "Kept"},
			target:   issuePath + "?fields=idReadable,description",
			mismatch: []*youtrack.Node{mismatch("description", youtrack.NewNull(), youtrack.NewString("Kept"))},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := issueWriting(t, issueProject(), "[]", fake.JSON(http.StatusOK, issueWritten(t, tc.answer)))

			_, err := tc.write(t.Context(), client(t, server).Issues)

			want := issueMismatch(requestTo(http.MethodPost, server, tc.target), tc.mismatch...)
			assert.Equal(t, want, errorOf(t, err))
		})
	}
}

func TestIssueWriteRefusesAnAnswerItCannotPrintAfterTheWrite(t *testing.T) {
	t.Parallel()
	answer := issueWritten(t, map[string]any{"summary": "First", "customFields": issueHeldFields(
		issueHeld{name: "Field", valueType: "string", value: "42"})})
	server := issueWriting(t, issueProject(), "[]", fake.JSON(http.StatusOK, answer))

	_, err := issueCreate(t, server, youtrack.IssueInput{Summary: "First"}, "idReadable,customFields")

	want := unreadable(requestTo(http.MethodPost, server, issuesPath+"?fields=idReadable,"+issueCustomFields+",summary"), answer)
	want.AfterWrite = true
	assert.Equal(t, want, errorOf(t, err))
}

func TestIssueWritePrintsACustomFieldTheExpressionNamesAndChecksThemAll(t *testing.T) {
	t.Parallel()
	answer := issueWritten(t, map[string]any{"summary": "First", "customFields": issueHeldFields(
		issueHeld{name: "Named", valueType: "string", value: `"First"`},
		issueHeld{name: "Unnamed", valueType: "string", value: `"Second"`})})
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+projectPath, fake.JSON(http.StatusOK, issueProject()))
	mux.HandleFunc("GET "+issueCataloguePath, fake.JSON(http.StatusOK,
		issueCatalogue(issueCatalogued("Named", `"Localized"`), issueCatalogued("Unnamed", "null"))))
	mux.HandleFunc("POST "+issuesPath, fake.JSON(http.StatusOK, answer))
	server := fake.Serve(t, mux.ServeHTTP)

	node, err := issueCreate(t, server, youtrack.IssueInput{Summary: "First"}, `idReadable,customFields("localized")`)

	require.NoError(t, err)
	assert.Equal(t, youtrack.NewMap(
		youtrack.Pair{Key: "idReadable", Value: youtrack.NewString("DEV-1")},
		youtrack.Pair{Key: "customFields", Value: youtrack.NewMap(issueNamed("Named", "First"))},
	), node)
	assert.Equal(t, url.Values{"fields": {"idReadable," + issueCustomFields + ",summary"}}, server.Last(t).URL.Query())
}

func issueBrokenOff(status int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"error"`)
	}
}

func TestIssueWriteIsUncertainOfATruncatedAnswerOnlyWhereItMayHaveWritten(t *testing.T) {
	t.Parallel()
	broken := []youtrack.Pair{{Key: "upstream_body", Value: youtrack.NewString(`{"error"`)}}
	tests := []struct {
		name    string
		status  int
		code    youtrack.Code
		details []youtrack.Pair
	}{
		{name: "a refusal of the request", status: http.StatusBadRequest, code: youtrack.CodeUpstreamFailed},
		{name: "a refusal of the token", status: http.StatusForbidden, code: youtrack.CodeUpstreamFailed},
		{name: "an issue the server does not find", status: http.StatusNotFound, code: youtrack.CodeUpstreamFailed},
		{name: "a success", status: http.StatusOK, code: youtrack.CodeWriteUncertain, details: broken},
		{name: "a failure of the server", status: http.StatusBadGateway, code: youtrack.CodeWriteUncertain, details: broken},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := issueWriting(t, issueProject(), "[]", issueBrokenOff(tc.status))

			_, err := issueUpdate(t, server, youtrack.IssueUpdate{Summary: new("First")}, "idReadable")

			want := youtrack.Error{Code: tc.code, Details: append([]youtrack.Pair{
				requestTo(http.MethodPost, server, issuePath+"?fields=idReadable,summary"),
				{Key: "upstream_status", Value: number(tc.status)},
			}, tc.details...)}
			assert.Equal(t, want, errorOf(t, err))
		})
	}
}

func TestUpdateIssueRefusesAnIssueOfAnotherShape(t *testing.T) {
	t.Parallel()
	project := issueProject(enumField("1-1", "Field"))
	tests := []struct {
		name string
		read string
	}{
		{name: "a readable id it cannot write by", read: `{"$type":"Issue","idReadable":"DEV-1/..","customFields":[],"project":` + project + `}`},
		{name: "a readable id that is no text", read: `{"$type":"Issue","idReadable":5,"customFields":[],"project":` + project + `}`},
		{name: "a readable id of another form", read: `{"$type":"Issue","idReadable":"2-1","customFields":[],"project":` + project + `}`},
		{name: "no project", read: `{"$type":"Issue","idReadable":"DEV-1","customFields":[],"project":null}`},
		{name: "no custom fields", read: `{"$type":"Issue","idReadable":"DEV-1","customFields":null,"project":` + project + `}`},
		{
			name: "the class of a field that is not text",
			read: issueToWrite(project, `[{"$type":7,"name":"Field","projectCustomField":{"$type":"ProjectCustomField","id":"1-1"}}]`),
		},
		{
			name: "the name of a field that is not text",
			read: issueToWrite(project, `[{"$type":"SingleEnumIssueCustomField","name":7,`+
				`"projectCustomField":{"$type":"ProjectCustomField","id":"1-1"}}]`),
		},
		{
			name: "the binding of a field named by no text",
			read: issueToWrite(project, `[{"$type":"SingleEnumIssueCustomField","name":"Field",`+
				`"projectCustomField":{"$type":"ProjectCustomField","id":7}}]`),
		},
		{
			name: "a field with no binding",
			read: issueToWrite(project, `[{"$type":"SingleEnumIssueCustomField","name":"Field","projectCustomField":null}]`),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mux := http.NewServeMux()
			mux.HandleFunc("GET "+issuePath, fake.JSON(http.StatusOK, tc.read))
			server := fake.Serve(t, mux.ServeHTTP)

			_, err := issueUpdate(t, server, youtrack.IssueUpdate{Fields: []youtrack.FieldWrite{issueFill("Field", "First")}}, "")

			assert.Equal(t, unreadable(issueReadRequest(server), tc.read), errorOf(t, err))
			assert.Equal(t, []string{issuePath}, server.Paths())
		})
	}
}

func issueDeleting(t *testing.T, read string, deletion http.HandlerFunc) *fake.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/issues/dev-7", fake.JSON(http.StatusOK, read))
	mux.HandleFunc("DELETE /api/issues/DEV-7", deletion)
	return fake.Serve(t, mux.ServeHTTP)
}

func issueDeleted(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func TestDeleteIssueDeletesByTheIDTheReadAnswers(t *testing.T) {
	t.Parallel()
	server := issueDeleting(t, `{"$type":"Issue","idReadable":"DEV-7"}`, issueDeleted)

	node, err := client(t, server).Issues.Delete(t.Context(), "dev-7")

	require.NoError(t, err)
	assert.Equal(t, youtrack.NewMap(youtrack.Pair{Key: "idReadable", Value: youtrack.NewString("DEV-7")}), node)
	assert.Equal(t, []string{"/api/issues/dev-7?fields=idReadable", "/api/issues/DEV-7?"}, server.Targets(t))
	assert.Equal(t, []string{http.MethodGet, http.MethodDelete}, server.Methods())
}

func TestDeleteIssueRefusesAnIDOfAnyOtherForm(t *testing.T) {
	t.Parallel()

	_, err := client(t, fake.ServeNothing(t)).Issues.Delete(t.Context(), "DEV-A-7")

	assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
}

func TestDeleteIssueRefusesAReadableIDItCannotDeleteBy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		readable string
	}{
		{name: "two dots", readable: `".."`},
		{name: "a path after the id", readable: `"DEV-7/.."`},
		{name: "the id of an article", readable: `"DEV-A-7"`},
		{name: "a number", readable: "7"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			read := `{"$type":"Issue","idReadable":` + tc.readable + `}`
			server := issueDeleting(t, read, issueDeleted)

			_, err := client(t, server).Issues.Delete(t.Context(), "dev-7")

			assert.Equal(t, unreadable(requestTo(http.MethodGet, server, "/api/issues/dev-7?fields=idReadable"), read), errorOf(t, err))
			assert.Equal(t, []string{"/api/issues/dev-7"}, server.Paths())
		})
	}
}

func TestDeleteIssueRefusesADeletionAnsweredWithABody(t *testing.T) {
	t.Parallel()
	server := issueDeleting(t, `{"$type":"Issue","idReadable":"DEV-7"}`, fake.JSON(http.StatusOK, `{"x":1}`))

	_, err := client(t, server).Issues.Delete(t.Context(), "dev-7")

	want := youtrack.Error{Code: youtrack.CodeUpstreamInvalid, AfterWrite: true, Details: []youtrack.Pair{
		requestTo(http.MethodDelete, server, "/api/issues/DEV-7"),
		{Key: "upstream_status", Value: number(http.StatusOK)},
		{Key: "upstream_body", Value: youtrack.NewString(`{"x":1}`)},
	}}
	assert.Equal(t, want, errorOf(t, err))
}
