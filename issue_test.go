package youtrack_test

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/youtrack"
	"github.com/hakastein/youtrack/fake"
)

const issueRecordFields = "id,idReadable,summary,description,project(id,shortName,name)," +
	"customFields(name,value(name,login,minutes,text,id,localizedName)," +
	"projectCustomField(id,ordinal,field(fieldType(valueType,isMultiValue),localizedName)))," +
	"links(direction,linkType(name,sourceToTarget,targetToSource),issues(id,idReadable))"

func issueRecord(t *testing.T, members map[string]any) string {
	t.Helper()
	issue := map[string]any{
		"$type":        "Issue",
		"id":           "2-1",
		"idReadable":   "DEV-1",
		"summary":      "First",
		"description":  nil,
		"project":      map[string]any{"$type": "Project", "id": "0-1", "shortName": "DEV", "name": "Development"},
		"customFields": json.RawMessage(`[]`),
		"links":        []any{},
	}
	maps.Copy(issue, members)
	answer, err := json.Marshal(issue)
	require.NoError(t, err)
	return string(answer)
}

func issueRecordHolding(t *testing.T, fields ...issueHeld) string {
	t.Helper()
	return issueRecord(t, map[string]any{"customFields": issueHeldFields(fields...)})
}

func issueGot(t *testing.T, server *fake.Server) (*youtrack.Issue, error) {
	t.Helper()
	return client(t, server).Issues.Get(t.Context(), "DEV-1")
}

func issueOfDEV(fields ...youtrack.Field) *youtrack.Issue {
	return &youtrack.Issue{
		ID: "2-1", IDReadable: "DEV-1", Summary: "First",
		Project: youtrack.Project{ID: "0-1", ShortName: "DEV", Name: "Development"},
		Fields:  append([]youtrack.Field{}, fields...),
		Links:   []youtrack.Link{},
	}
}

func TestGetReadsTheIssueAndItsFieldsByTheKeyOfTheirTypes(t *testing.T) {
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
			value: `[{"$type":"EnumBundleElement","id":"3-5","name":"First","localizedName":null},` +
				`{"$type":"EnumBundleElement","id":"3-6","name":"Second","localizedName":"Второе"}]`,
			values: []youtrack.Value{{ID: "3-5", Text: "First"}, {ID: "3-6", Text: "Second", LocalizedName: "Второе"}}},
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
		{name: "users", valueType: "user", multi: true,
			value:  `[{"$type":"User","id":"1-5","login":"first"},{"$type":"User","id":"1-6","login":"second"}]`,
			values: []youtrack.Value{{ID: "1-5", Text: "first"}, {ID: "1-6", Text: "second"}}},
		{name: "a group", valueType: "group", value: `{"$type":"UserGroup","id":"4-1","name":"First"}`,
			values: []youtrack.Value{{ID: "4-1", Text: "First"}}},
		{name: "groups", valueType: "group", multi: true, value: `[{"$type":"UserGroup","id":"4-1","name":"First"}]`,
			values: []youtrack.Value{{ID: "4-1", Text: "First"}}},
		{name: "a period", valueType: "period", value: `{"$type":"PeriodValue","id":"PT1H30M","minutes":90,"presentation":"1h 30m"}`,
			values: []youtrack.Value{{Text: "PT1H30M"}}},
		{name: "a period the server counts in working days", valueType: "period",
			value: `{"$type":"PeriodValue","minutes":1635,"id":"P3DT3H15M"}`, values: []youtrack.Value{{Text: "PT27H15M"}}},
		{name: "a date", valueType: "date", value: `1789560000000`, values: []youtrack.Value{{Text: "2026-09-16"}}},
		{name: "a date and time with milliseconds", valueType: "date and time", value: `1788134400123`,
			values: []youtrack.Value{{Text: "2026-08-31T00:00:00.123Z"}}},
		{name: "an integer", valueType: "integer", value: `1`, values: []youtrack.Value{{Text: "1"}}},
		{name: "a float", valueType: "float", value: `1.50`, values: []youtrack.Value{{Text: "1.5"}}},
		{name: "a string", valueType: "string", value: `"First"`, values: []youtrack.Value{{Text: "First"}}},
		{name: "a text", valueType: "text", value: `{"$type":"TextFieldValue","id":"6-1","text":"\n  First\nSecond","markdownText":"<div>First</div>"}`,
			values: []youtrack.Value{{Text: "\n  First\nSecond"}}},
		{name: "a field holding nothing", valueType: "enum", value: `null`},
		{name: "a field holding no value of several", valueType: "enum", multi: true, value: `[]`},
		{name: "a text field holding no text", valueType: "text", value: `{"$type":"TextFieldValue","id":"6-1","text":null}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			held := issueHeld{name: "Field", translation: `"Поле"`, valueType: tc.valueType, multi: tc.multi, value: tc.value}
			server := fake.Serve(t, fake.JSON(http.StatusOK, issueRecordHolding(t, held)))

			issue, err := issueGot(t, server)

			require.NoError(t, err)
			field := youtrack.Field{Name: "Field", LocalizedName: "Поле", Type: fieldType(tc.valueType, tc.multi), Values: tc.values}
			assert.Equal(t, issueOfDEV(field), issue)
			assert.Equal(t, requestTo(http.MethodGet, server, issuePath+"?fields="+issueRecordFields), lastRequest(t, server))
		})
	}
}

func TestGetOrdersTheFieldsAsTheProjectDoes(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, issueRecordHolding(t,
		issueHeld{name: "Tenth", valueType: "string", value: `"b"`, ordinal: "3", binding: "1-10"},
		issueHeld{name: "Second", valueType: "string", value: `"c"`, ordinal: "2", binding: "1-2"},
		issueHeld{name: "Ninth", valueType: "string", value: `"a"`, ordinal: "3", binding: "1-9"},
	)))

	issue, err := issueGot(t, server)

	require.NoError(t, err)
	names := make([]string, 0, len(issue.Fields))
	for _, f := range issue.Fields {
		names = append(names, f.Name)
	}
	assert.Equal(t, []string{"Second", "Ninth", "Tenth"}, names)
}

func TestGetRefusesCustomFieldsOfAnotherShape(t *testing.T) {
	t.Parallel()
	for _, tc := range issueBrokenFields() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := issueRecord(t, map[string]any{"customFields": json.RawMessage(tc.block)})
			server := fake.Serve(t, fake.JSON(http.StatusOK, body))

			_, err := issueGot(t, server)

			assert.Equal(t, unreadable(lastRequest(t, server), body), errorOf(t, err))
		})
	}
}

func TestGetReadsTheDescriptionAndTheLinks(t *testing.T) {
	t.Parallel()
	subtask := `{"$type":"IssueLinkType","name":"Subtask","sourceToTarget":"parent for","targetToSource":"subtask of"}`
	links := json.RawMessage(`[
		{"$type":"IssueLink","direction":"INWARD","linkType":` + subtask + `,"issues":[{"$type":"Issue","id":"2-7","idReadable":"DEV-7"}]},
		{"$type":"IssueLink","direction":"OUTWARD","linkType":` + subtask + `,"issues":[]},
		{"$type":"IssueLink","direction":"BOTH","linkType":{"$type":"IssueLinkType","name":"Relates","sourceToTarget":"relates to","targetToSource":null},
		 "issues":[{"$type":"Issue","id":"2-8","idReadable":"DEV-8"},{"$type":"Issue","id":"2-9","idReadable":"DOCS-9"}]}
	]`)
	server := fake.Serve(t, fake.JSON(http.StatusOK, issueRecord(t, map[string]any{"description": "First\nSecond", "links": links})))

	issue, err := issueGot(t, server)

	require.NoError(t, err)
	parent := youtrack.LinkType{Name: "Subtask", SourceToTarget: "parent for", TargetToSource: "subtask of"}
	want := issueOfDEV()
	want.Description = "First\nSecond"
	want.Links = []youtrack.Link{
		{Direction: youtrack.Inward, Type: parent, Issues: []youtrack.IssueRef{{ID: "2-7", IDReadable: "DEV-7"}}},
		{Direction: youtrack.Outward, Type: parent, Issues: []youtrack.IssueRef{}},
		{Direction: youtrack.Both, Type: youtrack.LinkType{Name: "Relates", SourceToTarget: "relates to"},
			Issues: []youtrack.IssueRef{{ID: "2-8", IDReadable: "DEV-8"}, {ID: "2-9", IDReadable: "DOCS-9"}}},
	}
	assert.Equal(t, want, issue)
}

func TestGetReadsAnIssueWithoutLinks(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, issueRecord(t, map[string]any{"links": nil})))

	issue, err := issueGot(t, server)

	require.NoError(t, err)
	want := issueOfDEV()
	want.Links = nil
	assert.Equal(t, want, issue)
}

func TestGetRefusesAnIssueOfAnotherShape(t *testing.T) {
	t.Parallel()
	link := func(members string) json.RawMessage {
		return json.RawMessage(`[{"$type":"IssueLink",` + members + `}]`)
	}
	const relates = `"linkType":{"name":"Relates","sourceToTarget":"a","targetToSource":"b"}`
	tests := []struct {
		name    string
		members map[string]any
	}{
		{name: "a readable id that is a number", members: map[string]any{"idReadable": 5}},
		{name: "a summary that is null", members: map[string]any{"summary": nil}},
		{name: "a description that is a number", members: map[string]any{"description": 5}},
		{name: "a project that is null", members: map[string]any{"project": nil}},
		{name: "a project whose short name is no text",
			members: map[string]any{"project": map[string]any{"id": "0-1", "shortName": 7, "name": "Development"}}},
		{name: "no custom fields at all", members: map[string]any{"customFields": nil}},
		{name: "links that are one slot rather than a list",
			members: map[string]any{"links": json.RawMessage(`{"direction":"BOTH",` + relates + `,"issues":[]}`)}},
		{name: "a link slot that is null", members: map[string]any{"links": json.RawMessage(`[null]`)}},
		{name: "a direction that is no text", members: map[string]any{"links": link(`"direction":5,` + relates + `,"issues":[]`)}},
		{name: "a link type named by no text",
			members: map[string]any{"links": link(`"direction":"BOTH","linkType":{"name":5,"sourceToTarget":"a","targetToSource":"b"},"issues":[]`)}},
		{name: "a link whose issues are null", members: map[string]any{"links": link(`"direction":"BOTH",` + relates + `,"issues":null`)}},
		{name: "a linked issue whose readable id is no text",
			members: map[string]any{"links": link(`"direction":"BOTH",` + relates + `,"issues":[{"id":"2-7","idReadable":7}]`)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := issueRecord(t, tc.members)
			server := fake.Serve(t, fake.JSON(http.StatusOK, body))

			_, err := issueGot(t, server)

			assert.Equal(t, unreadable(lastRequest(t, server), body), errorOf(t, err))
		})
	}
}

func TestGetFindsAFieldByEitherOfItsNames(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, issueRecordHolding(t,
		issueHeld{name: "Shared", valueType: "string", value: `"own"`, binding: "1-1"},
		issueHeld{name: "Named", translation: `"Shared"`, valueType: "string", value: `"translated"`, binding: "1-2"},
		issueHeld{name: "Other", translation: `"Другое"`, valueType: "string", value: `"other"`, binding: "1-3"},
	)))
	issue, err := issueGot(t, server)
	require.NoError(t, err)
	tests := []struct {
		name  string
		asked string
		texts []string
		found bool
	}{
		{name: "a name", asked: "Named", texts: []string{"translated"}, found: true},
		{name: "a name in another letter case", asked: "NAMED", texts: []string{"translated"}, found: true},
		{name: "a translation", asked: "другое", texts: []string{"other"}, found: true},
		{name: "a name that is also the translation of another field", asked: "shared", texts: []string{"own"}, found: true},
		{name: "a name of no field", asked: "Nothing", texts: []string{}},
		{name: "an empty name", asked: "", texts: []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f, found := issue.Field(tc.asked)

			assert.Equal(t, tc.found, found)
			assert.Equal(t, tc.texts, f.Texts())
		})
	}
}

func TestGetRefusesAnIDOfAnyOtherForm(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   string
	}{
		{name: "an empty id", id: ""},
		{name: "a project code alone", id: "DEV"},
		{name: "an id of an article", id: "DEV-A-1"},
		{name: "an internal id", id: "2-1"},
		{name: "an id with no number", id: "DEV-"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := client(t, fake.ServeNothing(t)).Issues.Get(t.Context(), tc.id)

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

func TestGetSendsTheReadableIdAsWritten(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, issueRecord(t, nil)))

	_, err := client(t, server).Issues.Get(t.Context(), "dev-01")

	require.NoError(t, err)
	assert.Equal(t, []string{"/api/issues/dev-01"}, server.Paths())
}

func TestWriteFieldsRefusesACallItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		id     string
		writes []youtrack.FieldWrite
	}{
		{name: "an id of an article", id: "DEV-A-1", writes: []youtrack.FieldWrite{issueFill("Field", "x")}},
		{name: "no writes at all", id: "DEV-1"},
		{name: "a field of no name", id: "DEV-1", writes: []youtrack.FieldWrite{issueFill("", "x")}},
		{name: "a field given no value", id: "DEV-1", writes: []youtrack.FieldWrite{{Name: "Field"}}},
		{name: "the summary written as a field", id: "DEV-1", writes: []youtrack.FieldWrite{issueFill("Summary", "x")}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := client(t, fake.ServeNothing(t)).Issues.WriteFields(t.Context(), tc.id, tc.writes)

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

func TestWriteFieldsAnswersWithTheIssueAsTheServerHoldsIt(t *testing.T) {
	t.Parallel()
	project := issueProject(metaField{id: "1-1", name: "State", valueType: "state"}, metaField{id: "1-2", name: "Resolved", valueType: "date"})
	answer := issueRecord(t, map[string]any{"summary": "Renamed by a workflow", "customFields": issueHeldFields(
		issueHeld{name: "Resolved", valueType: "date", value: `1789560000000`, binding: "1-2", ordinal: "1"},
		issueHeld{name: "State", valueType: "state", binding: "1-1",
			value: `{"$type":"StateBundleElement","id":"3-9","name":"Done","localizedName":null}`},
	)})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/issues/dev-1", fake.JSON(http.StatusOK, issueToWrite(project, "[]")))
	mux.HandleFunc("POST "+issuePath, fake.JSON(http.StatusOK, answer))
	server := fake.Serve(t, mux.ServeHTTP)

	issue, err := client(t, server).Issues.WriteFields(t.Context(), "dev-1", []youtrack.FieldWrite{issueFill("State", "Done")})

	require.NoError(t, err)
	want := issueOfDEV(
		youtrack.Field{Name: "State", Type: fieldType("state", false), Values: []youtrack.Value{{ID: "3-9", Text: "Done"}}},
		youtrack.Field{Name: "Resolved", Type: fieldType("date", false), Values: []youtrack.Value{{Text: "2026-09-16"}}},
	)
	want.Summary = "Renamed by a workflow"
	assert.Equal(t, want, issue)
	assert.Equal(t, []string{"GET /api/issues/dev-1", "POST " + issuePath}, server.Routes())
	assert.Equal(t, requestTo(http.MethodPost, server, issuePath+"?fields="+issueRecordFields), lastRequest(t, server))
	assert.JSONEq(t, `{"customFields":[{"$type":"StateIssueCustomField","name":"State","value":{"name":"Done"}}]}`,
		server.Last(t).Body)
}

func issueWritingOne(ctx context.Context, c *youtrack.Client) error {
	_, err := c.Issues.WriteFields(ctx, "DEV-1", []youtrack.FieldWrite{issueFill("Field", "First")})
	return err
}

func issueOneField() string {
	return issueProject(enumField("1-1", "Field"))
}

func issueCancellingOnArrival(cancel context.CancelFunc) http.HandlerFunc {
	return func(_ http.ResponseWriter, r *http.Request) {
		cancel()
		<-r.Context().Done()
	}
}

func TestWriteFieldsTellsAWriteThatNeverLeftFromOneTheServerMayHaveActedOn(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		serve   func(t *testing.T, cancel context.CancelFunc) *fake.Server
		code    youtrack.Code
		methods []string
	}{
		{
			name: "the answer never came",
			serve: func(t *testing.T, _ context.CancelFunc) *fake.Server {
				return issueWriting(t, issueOneField(), "[]", breakOff)
			},
			code:    youtrack.CodeWriteUncertain,
			methods: []string{http.MethodGet, http.MethodPost},
		},
		{
			name: "the write never left",
			serve: func(t *testing.T, _ context.CancelFunc) *fake.Server {
				var server *fake.Server
				server = fake.ServeAlone(t, func(w http.ResponseWriter, r *http.Request) {
					server.StopListening(t)
					fake.JSON(http.StatusOK, issueToWrite(issueOneField(), "[]"))(w, r)
				})
				return server
			},
			code:    youtrack.CodeUpstreamFailed,
			methods: []string{http.MethodGet},
		},
		{
			name: "the call was cancelled after the write was sent",
			serve: func(t *testing.T, cancel context.CancelFunc) *fake.Server {
				return issueWriting(t, issueOneField(), "[]", issueCancellingOnArrival(cancel))
			},
			code:    youtrack.CodeWriteUncertain,
			methods: []string{http.MethodGet, http.MethodPost},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			server := tc.serve(t, cancel)

			err := issueWritingOne(ctx, client(t, server))

			sent := requestTo(http.MethodPost, server, issuePath+"?fields="+issueRecordFields)
			assert.Equal(t, youtrack.Error{Code: tc.code, Details: []youtrack.Pair{sent}}, errorOf(t, err))
			assert.Equal(t, tc.methods, server.Methods())
		})
	}
}

func TestWriteFieldsTellsACancelledWriteByTheCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	server := issueWriting(t, issueOneField(), "[]", issueCancellingOnArrival(cancel))

	err := issueWritingOne(ctx, client(t, server))

	assert.ErrorIs(t, err, context.Canceled)
	assert.ErrorIs(t, err, youtrack.ErrWriteUncertain)
}

func TestWriteFieldsReadsTheAnswerToTheWriteByWhoAnsweredIt(t *testing.T) {
	t.Parallel()
	said := func(key, value string) youtrack.Pair {
		return youtrack.Pair{Key: key, Value: youtrack.NewString(value)}
	}
	tests := []struct {
		name       string
		status     int
		body       string
		code       youtrack.Code
		afterWrite bool
		words      []youtrack.Pair
	}{
		{name: "YouTrack failed the write", status: http.StatusInternalServerError,
			body: `{"error":"server_error","error_description":"NPE"}`, code: youtrack.CodeUpstreamFailed,
			words: []youtrack.Pair{said("upstream_error", "server_error"), said("upstream_message", "NPE")}},
		{name: "something else answered the write with 502", status: http.StatusBadGateway,
			body: "<html>Bad Gateway</html>", code: youtrack.CodeWriteUncertain,
			words: []youtrack.Pair{said("upstream_body", "<html>Bad Gateway</html>")}},
		{name: "something else answered the write with JSON that names no error", status: http.StatusServiceUnavailable,
			body: `{"status":"down"}`, code: youtrack.CodeWriteUncertain,
			words: []youtrack.Pair{said("upstream_body", `{"status":"down"}`)}},
		{name: "YouTrack refused the write", status: http.StatusBadRequest,
			body: `{"error":"bad_request","error_description":"Bad Request"}`, code: youtrack.CodeRejected,
			words: []youtrack.Pair{said("upstream_error", "bad_request"), said("upstream_message", "Bad Request")}},
		{name: "YouTrack took the write and answered 201", status: http.StatusCreated, body: `{}`,
			code: youtrack.CodeUpstreamFailed, afterWrite: true, words: []youtrack.Pair{said("upstream_body", `{}`)}},
		{name: "an answer that is no JSON under 200", status: http.StatusOK, body: "<html/>",
			code: youtrack.CodeUpstreamInvalid, afterWrite: true, words: []youtrack.Pair{said("upstream_body", "<html/>")}},
		{name: "an answer that is no JSON under 404", status: http.StatusNotFound, body: "<html/>",
			code: youtrack.CodeUpstreamInvalid, words: []youtrack.Pair{said("upstream_body", "<html/>")}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := issueWriting(t, issueOneField(), "[]", fake.JSON(tc.status, tc.body))

			err := issueWritingOne(t.Context(), client(t, server))

			details := append([]youtrack.Pair{lastRequest(t, server), {Key: "upstream_status", Value: number(tc.status)}}, tc.words...)
			assert.Equal(t, youtrack.Error{Code: tc.code, AfterWrite: tc.afterWrite, Details: details}, errorOf(t, err))
		})
	}
}
