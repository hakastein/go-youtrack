package youtrack_test

import (
	"cmp"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/go-youtrack"
	"github.com/hakastein/go-youtrack/fake"
)

const (
	issuePath         = "/api/issues/DEV-1"
	issueCustomFields = "customFields(name,value(name,login,minutes,text)," +
		"projectCustomField(id,ordinal,field(fieldType(valueType,isMultiValue))))"
)

// translation is the JSON of localizedName, null when empty; ordinal is 0 and binding 1-1 when empty.
type issueHeld struct {
	name        string
	translation string
	valueType   string
	multi       bool
	value       string
	ordinal     string
	binding     string
}

func (f issueHeld) json() string {
	return `{"$type":"IssueCustomField","name":` + strconv.Quote(f.name) + `,"value":` + cmp.Or(f.value, "null") +
		`,"projectCustomField":{"$type":"ProjectCustomField","id":` + strconv.Quote(cmp.Or(f.binding, "1-1")) +
		`,"ordinal":` + cmp.Or(f.ordinal, "0") +
		`,"field":{"$type":"CustomField","localizedName":` + cmp.Or(f.translation, "null") +
		`,"fieldType":{"$type":"FieldType","valueType":` + strconv.Quote(f.valueType) +
		`,"isMultiValue":` + strconv.FormatBool(f.multi) + `}}}}`
}

func issueHeldFields(fields ...issueHeld) json.RawMessage {
	held := make([]string, 0, len(fields))
	for _, f := range fields {
		held = append(held, f.json())
	}
	return json.RawMessage("[" + strings.Join(held, ",") + "]")
}

func issueElement(name string) string {
	return `{"$type":"EnumBundleElement","id":"3-1","name":` + strconv.Quote(name) + `,"localizedName":null}`
}

func issueShown(t *testing.T, server *fake.Server, expression string, comments youtrack.Comments) (*youtrack.Node, error) {
	t.Helper()
	opts := &youtrack.ShowIssueOptions{Fields: expression, Comments: comments}
	return client(t, server).Issues.Show(t.Context(), "DEV-1", opts)
}

func issueShownFields(t *testing.T, block string) (*fake.Server, *youtrack.Node, error) {
	t.Helper()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"Issue","idReadable":"DEV-1","customFields":`+block+`}`))
	node, err := issueShown(t, server, "customFields", youtrack.Comments{})
	return server, node, err
}

func issueFieldsBlock(pairs ...youtrack.Pair) *youtrack.Node {
	return youtrack.NewMap(youtrack.Pair{Key: "customFields", Value: youtrack.NewMap(pairs...)})
}

func TestShowIssueRefusesAnIDOfAnyOtherForm(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   string
	}{
		{name: "an article", id: "DEV-A-1"},
		{name: "an internal id", id: "3-19"},
		{name: "two dots", id: ".."},
		{name: "a code and a dash", id: "DEV-"},
		{name: "a number without a code", id: "-1"},
		{name: "a digit first in the code", id: "1DEV-1"},
		{name: "an underscore first in the code", id: "_DEV-1"},
		{name: "a space before the code", id: " DEV-1"},
		{name: "a sign before the number", id: "DEV-+1"},
		{name: "a path after the number", id: "DEV-1/.."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := client(t, fake.ServeNothing(t)).Issues.Show(t.Context(), tc.id, &youtrack.ShowIssueOptions{Fields: "id"})

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

func TestShowIssueSendsEveryFormOfAnIssueAsWritten(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   string
	}{
		{name: "a code in upper case", id: "DEV-1"},
		{name: "a code in lower case", id: "dev-1"},
		{name: "a number with a leading zero", id: "DEV-01"},
		{name: "digits and an underscore in the code", id: "Dev_32-1"},
		{name: "letters outside ASCII", id: "ДЕВ-1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"Issue","id":"2-1"}`))

			_, err := client(t, server).Issues.Show(t.Context(), tc.id, &youtrack.ShowIssueOptions{Fields: "id"})

			require.NoError(t, err)
			assert.Equal(t, "/api/issues/"+tc.id, server.Request(t, 0).URL.Path)
		})
	}
}

func TestShowIssueRefusesAnExpressionBeforeTheNetwork(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
	}{
		{name: "comments in place of the default", expression: "comments(text)"},
		{name: "comments added to the default", expression: "+comments"},
		{name: "comments under the issues of a link", expression: "links(issues(comments(id)))"},
		{name: "the id of a link slot", expression: "links(id)"},
		{name: "the end the issue stands at", expression: "links(direction)"},
		{name: "the type of a link", expression: "links(linkType(name))"},
		{name: "the id of the parent slot", expression: "parent(id)"},
		{name: "the trimmed issues of the subtasks slot", expression: "subtasks(trimmedIssues(idReadable))"},
		{name: "a slot under the issues of a link", expression: "links(issues(parent(id)))"},
		{name: "a name asked of a custom field", expression: "customFields(First(name))"},
		{name: "the members of the custom field block", expression: "customFields(name,value(name))"},
		{name: "a custom field named under the issues of a link", expression: "links(issues(customFields(First)))"},
		{name: "a name in quotes at the root", expression: `"First"`},
		{name: "a name in quotes under a link", expression: `links(issues("First"))`},
		{name: "a name in quotes of nothing", expression: `customFields("")`},
		{name: "a quote left open", expression: `customFields("First`},
		{name: "a backslash before anything but a quote or a backslash", expression: `customFields("a\b")`},
		{name: "the content of a file under the attachments", expression: "attachments(base64Content)"},
		{name: "the content of a file beside a name of the default", expression: "+attachments(name,base64Content)"},
		{name: "the content of a file as a custom field", expression: "customFields(base64Content)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := issueShown(t, fake.ServeNothing(t), tc.expression, youtrack.AllComments())

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

func TestShowIssueTakesAQuotedBase64ContentForACustomField(t *testing.T) {
	t.Parallel()
	catalogue := issueCatalogue(issueCatalogued("base64Content", "null"))
	server := issueCataloguing(t, catalogue, fake.JSON(http.StatusOK, issueWithFields(issueEnum("base64Content", "Value"))))

	node, err := issueShown(t, server, `customFields("base64Content")`, youtrack.Comments{})

	require.NoError(t, err)
	assert.Equal(t, issueFieldsBlock(issueNamed("base64Content", "Value")), node)
}

func TestListIssuesRefusesACallBeforeTheNetwork(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		query      string
		expression string
	}{
		{name: "a search that is no UTF-8", query: "\xff", expression: "idReadable"},
		{name: "a search holding a byte that is no UTF-8", query: "field: \xc3\x28", expression: "idReadable"},
		{name: "comments in place of the default", expression: "comments(text)"},
		{name: "comments added to the default", expression: "+comments"},
		{name: "a custom field named under the issues of a link", expression: "links(issues(customFields(First)))"},
		{name: "a part of a link other than its issues", expression: "+links(direction)"},
		{name: "the content of a file in a record", expression: "+attachments(base64Content)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			issues := client(t, fake.ServeNothing(t)).Issues

			_, err := issues.List(t.Context(), tc.query, &youtrack.ListIssuesOptions{Fields: tc.expression})

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

func TestShowIssuePrintsTheFieldsInTheOrderAsked(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"Issue","idReadable":"DEV-1","summary":"First"}`))

	node, err := issueShown(t, server, "summary,idReadable", youtrack.Comments{})

	require.NoError(t, err)
	assert.Equal(t, youtrack.NewMap(
		youtrack.Pair{Key: "summary", Value: youtrack.NewString("First")},
		youtrack.Pair{Key: "idReadable", Value: youtrack.NewString("DEV-1")},
	), node)
}

func TestShowIssuePrintsTextForAHumanToReadAsText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
		answer     string
		printed    *youtrack.Node
	}{
		{
			name:       "the description",
			expression: "description",
			answer:     `{"$type":"Issue","description":"First\nSecond"}`,
			printed:    youtrack.NewMap(youtrack.Pair{Key: "description", Value: youtrack.NewText("First\nSecond")}),
		},
		{
			name:       "a description that is not there",
			expression: "description",
			answer:     `{"$type":"Issue","description":null}`,
			printed:    youtrack.NewMap(youtrack.Pair{Key: "description", Value: youtrack.NewNull()}),
		},
		{
			name:       "the title",
			expression: "summary",
			answer:     `{"$type":"Issue","summary":"First\nSecond"}`,
			printed:    youtrack.NewMap(youtrack.Pair{Key: "summary", Value: youtrack.NewString("First\nSecond")}),
		},
		{
			name:       "the description of the project, under a nested key",
			expression: "project(description)",
			answer:     `{"$type":"Issue","project":{"$type":"Project","description":"First"}}`,
			printed: youtrack.NewMap(youtrack.Pair{Key: "project", Value: youtrack.NewMap(
				youtrack.Pair{Key: "description", Value: youtrack.NewText("First")})}),
		},
		{
			name:       "the text of a comment, inside a record of a list",
			expression: "pinnedComments(text)",
			answer:     `{"$type":"Issue","pinnedComments":[{"$type":"IssueComment","id":"1-1","text":"First"}]}`,
			printed: youtrack.NewMap(youtrack.Pair{Key: "pinnedComments", Value: youtrack.NewList(youtrack.NewMap(
				youtrack.Pair{Key: "text", Value: youtrack.NewText("First")}))}),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tc.answer))

			node, err := issueShown(t, server, tc.expression, youtrack.Comments{})

			require.NoError(t, err)
			assert.Equal(t, tc.printed, node)
		})
	}
}

func TestListIssuesPrintsTheTextOfARecordAsAString(t *testing.T) {
	t.Parallel()
	notes := issueHeld{name: "Notes", valueType: "text", value: `{"$type":"TextFieldValue","text":"First\nSecond"}`}
	server := fake.Serve(t, fake.Searching(t, fake.JSON(http.StatusOK,
		`[{"$type":"Issue","description":"First\nSecond","customFields":[`+notes.json()+`]}]`)))

	node, _, err := issueSearch(t, server, "field: value", "description,customFields", 50)

	require.NoError(t, err)
	assert.Equal(t, wholePage("issues", youtrack.NewMap(
		youtrack.Pair{Key: "description", Value: youtrack.NewString("First\nSecond")},
		youtrack.Pair{Key: "customFields", Value: youtrack.NewMap(youtrack.DataPair("Notes", youtrack.NewString("First\nSecond")))},
	)), node)
}

func TestShowIssuePrintsAMomentInUTC(t *testing.T) {
	t.Parallel()
	_, offset := time.Now().Zone()
	require.NotZero(t, offset, "the process runs in UTC, where a moment in its zone reads as one in UTC")
	tests := []struct {
		name     string
		received string
		printed  string
	}{
		{name: "the epoch itself", received: "0", printed: "1970-01-01T00:00:00Z"},
		{name: "a whole second", received: "1788134400000", printed: "2026-08-31T00:00:00Z"},
		{name: "a tenth of a second", received: "1788134400100", printed: "2026-08-31T00:00:00.1Z"},
		{name: "every millisecond", received: "1788134400123", printed: "2026-08-31T00:00:00.123Z"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"Issue","created":`+tc.received+`}`))

			node, err := issueShown(t, server, "created", youtrack.Comments{})

			require.NoError(t, err)
			assert.Equal(t, youtrack.NewMap(youtrack.Pair{Key: "created", Value: youtrack.NewString(tc.printed)}), node)
		})
	}
}

func TestShowIssuePrintsOnlyATimeAsAMoment(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"Issue","created":0,"resolved":null,"numberInProject":1,`+
		`"attachments":[{"$type":"IssueAttachment","created":0,"size":75}]}`))

	node, err := issueShown(t, server, "created,resolved,numberInProject,attachments(created,size)", youtrack.Comments{})

	require.NoError(t, err)
	assert.Equal(t, youtrack.NewMap(
		youtrack.Pair{Key: "created", Value: youtrack.NewString("1970-01-01T00:00:00Z")},
		youtrack.Pair{Key: "resolved", Value: youtrack.NewNull()},
		youtrack.Pair{Key: "numberInProject", Value: youtrack.NewNumber("1")},
		youtrack.Pair{Key: "attachments", Value: youtrack.NewList(youtrack.NewMap(
			youtrack.Pair{Key: "created", Value: youtrack.NewString("1970-01-01T00:00:00Z")},
			youtrack.Pair{Key: "size", Value: youtrack.NewNumber("75")},
		))},
	), node)
}

func TestShowIssueRefusesAMomentThatIsNoWholeNumberOfMilliseconds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
	}{
		{name: "a number inside a string", body: `{"$type":"Issue","created":"1788134400000"}`},
		{name: "a fraction of a millisecond", body: `{"$type":"Issue","created":1.5}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tc.body))

			_, err := issueShown(t, server, "created", youtrack.Comments{})

			want := unreadable(requestTo(http.MethodGet, server, issuePath+"?fields=created"), tc.body)
			assert.Equal(t, want, errorOf(t, err))
		})
	}
}

func TestShowIssueResolvesAnAddressOfTheInstanceFromTheAddressOfTheClient(t *testing.T) {
	t.Parallel()
	linked := func(key string, link *youtrack.Node) *youtrack.Node {
		return youtrack.NewList(youtrack.NewMap(youtrack.Pair{Key: key, Value: link}))
	}
	tests := []struct {
		name       string
		expression string
		body       string
		want       func(origin string) *youtrack.Node
	}{
		{
			name:       "the link and the preview of an attachment",
			expression: "attachments(url,thumbnailURL)",
			body: `{"$type":"Issue","attachments":[{"$type":"IssueAttachment",` +
				`"url":"/api/files/12-2?sign=s&updated=1","thumbnailURL":"/api/files/12-3?sign=t"}]}`,
			want: func(origin string) *youtrack.Node {
				return youtrack.NewMap(youtrack.Pair{Key: "attachments", Value: youtrack.NewList(youtrack.NewMap(
					youtrack.Pair{Key: "url", Value: youtrack.NewString(origin + "/api/files/12-2?sign=s&updated=1")},
					youtrack.Pair{Key: "thumbnailURL", Value: youtrack.NewString(origin + "/api/files/12-3?sign=t")}))})
			},
		},
		{
			name:       "no preview",
			expression: "attachments(thumbnailURL)",
			body:       `{"$type":"Issue","attachments":[{"$type":"IssueAttachment","thumbnailURL":null}]}`,
			want: func(string) *youtrack.Node {
				return youtrack.NewMap(youtrack.Pair{Key: "attachments", Value: linked("thumbnailURL", youtrack.NewNull())})
			},
		},
		{
			name:       "an attachment the server named nothing",
			expression: "attachments(url)",
			body:       `{"$type":"Issue","attachments":[{"url":"/api/files/12-2?sign=s"}]}`,
			want: func(origin string) *youtrack.Node {
				return youtrack.NewMap(youtrack.Pair{Key: "attachments",
					Value: linked("url", youtrack.NewString(origin+"/api/files/12-2?sign=s"))})
			},
		},
		{
			name:       "a link of another schema",
			expression: "externalIssue(url)",
			body:       `{"$type":"Issue","externalIssue":{"$type":"ExternalIssue","url":"/browse/X-1"}}`,
			want: func(string) *youtrack.Node {
				return youtrack.NewMap(youtrack.Pair{Key: "externalIssue",
					Value: youtrack.NewMap(youtrack.Pair{Key: "url", Value: youtrack.NewString("/browse/X-1")})})
			},
		},
		{
			name:       "the avatar of a user",
			expression: "reporter(avatarUrl)",
			body:       `{"$type":"Issue","reporter":{"$type":"User","avatarUrl":"/hub/api/rest/avatar/u?s=48"}}`,
			want: func(origin string) *youtrack.Node {
				return youtrack.NewMap(youtrack.Pair{Key: "reporter", Value: youtrack.NewMap(
					youtrack.Pair{Key: "avatarUrl", Value: youtrack.NewString(origin + "/hub/api/rest/avatar/u?s=48")})})
			},
		},
		{
			name:       "the avatar of a subtype of a user",
			expression: "reporter(avatarUrl)",
			body:       `{"$type":"Issue","reporter":{"$type":"Me","avatarUrl":"/hub/api/rest/avatar/u?s=48"}}`,
			want: func(origin string) *youtrack.Node {
				return youtrack.NewMap(youtrack.Pair{Key: "reporter", Value: youtrack.NewMap(
					youtrack.Pair{Key: "avatarUrl", Value: youtrack.NewString(origin + "/hub/api/rest/avatar/u?s=48")})})
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tc.body))

			node, err := issueShown(t, server, tc.expression, youtrack.Comments{})

			require.NoError(t, err)
			assert.Equal(t, tc.want(server.Origin), node)
		})
	}
}

func TestShowIssuePrintsACustomFieldByTheValueKeyOfItsType(t *testing.T) {
	t.Parallel()
	nothing := issueFieldsBlock()
	printed := func(value *youtrack.Node) *youtrack.Node {
		return issueFieldsBlock(youtrack.DataPair("Field", value))
	}
	tests := []struct {
		name      string
		valueType string
		multi     bool
		value     string
		printed   *youtrack.Node
	}{
		{name: "an enum", valueType: "enum",
			value:   `{"$type":"EnumBundleElement","name":"First","localizedName":"Localized","presentation":"Presented"}`,
			printed: printed(youtrack.NewString("First"))},
		{name: "enums", valueType: "enum", multi: true,
			value:   `[` + issueElement("First") + `,` + issueElement("Second") + `]`,
			printed: printed(texts("First", "Second"))},
		{name: "a state", valueType: "state",
			value:   `{"$type":"StateBundleElement","name":"First","isResolved":false,"localizedName":"Localized"}`,
			printed: printed(youtrack.NewString("First"))},
		{name: "a version", valueType: "version", value: `{"$type":"VersionBundleElement","name":"First","released":true}`,
			printed: printed(youtrack.NewString("First"))},
		{name: "versions", valueType: "version", multi: true, value: `[{"$type":"VersionBundleElement","name":"First"}]`,
			printed: printed(texts("First"))},
		{name: "a build", valueType: "build", value: `{"$type":"BuildBundleElement","name":"First"}`,
			printed: printed(youtrack.NewString("First"))},
		{name: "builds", valueType: "build", multi: true, value: `[{"$type":"BuildBundleElement","name":"First"}]`,
			printed: printed(texts("First"))},
		{name: "an owned value", valueType: "ownedField",
			value:   `{"$type":"OwnedBundleElement","name":"First","owner":{"$type":"User","login":"second"}}`,
			printed: printed(youtrack.NewString("First"))},
		{name: "owned values", valueType: "ownedField", multi: true, value: `[{"$type":"OwnedBundleElement","name":"First"}]`,
			printed: printed(texts("First"))},
		{name: "a user", valueType: "user", value: `{"$type":"User","login":"first","name":"Named","fullName":"Named"}`,
			printed: printed(youtrack.NewString("first"))},
		{name: "users", valueType: "user", multi: true, value: `[{"$type":"User","login":"first","name":"Named"}]`,
			printed: printed(texts("first"))},
		{name: "a group", valueType: "group", value: `{"$type":"UserGroup","name":"First"}`,
			printed: printed(youtrack.NewString("First"))},
		{name: "groups", valueType: "group", multi: true, value: `[{"$type":"UserGroup","name":"First"}]`,
			printed: printed(texts("First"))},
		{name: "a period", valueType: "period", value: `{"$type":"PeriodValue","minutes":90,"presentation":"Presented"}`,
			printed: printed(youtrack.NewString("PT1H30M"))},
		{name: "a period the server counts in working days", valueType: "period",
			value:   `{"$type":"PeriodValue","minutes":1635,"id":"P3DT3H15M"}`,
			printed: printed(youtrack.NewString("PT27H15M"))},
		{name: "a period of whole hours", valueType: "period", value: `{"$type":"PeriodValue","minutes":60}`,
			printed: printed(youtrack.NewString("PT1H"))},
		{name: "a period of no time at all", valueType: "period", value: `{"$type":"PeriodValue","minutes":0}`,
			printed: printed(youtrack.NewString("PT0M"))},
		{name: "a date", valueType: "date", value: `1789560000000`,
			printed: printed(youtrack.NewString("2026-09-16"))},
		{name: "a date and time", valueType: "date and time", value: `1788134400000`,
			printed: printed(youtrack.NewString("2026-08-31T00:00:00Z"))},
		{name: "an integer", valueType: "integer", value: `1`,
			printed: printed(youtrack.NewNumber("1"))},
		{name: "a float", valueType: "float", value: `1.5`,
			printed: printed(youtrack.NewNumber("1.5"))},
		{name: "a string", valueType: "string", value: `"First"`,
			printed: printed(youtrack.NewString("First"))},
		{name: "a text", valueType: "text",
			value:   `{"$type":"TextFieldValue","text":"\n  First\nSecond","markdownText":"<div>First</div>"}`,
			printed: printed(youtrack.NewText("\n  First\nSecond"))},
		{name: "a field holding nothing", valueType: "enum", value: `null`, printed: nothing},
		{name: "a field holding no value of several", valueType: "enum", multi: true, value: `[]`, printed: nothing},
		{name: "a text field holding no text", valueType: "text", value: `{"$type":"TextFieldValue","text":null}`,
			printed: nothing},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			block := issueHeldFields(issueHeld{name: "Field", valueType: tc.valueType, multi: tc.multi, value: tc.value})

			_, node, err := issueShownFields(t, string(block))

			require.NoError(t, err)
			assert.Equal(t, tc.printed, node)
		})
	}
}

func TestShowIssuePrintsCustomFieldsInTheOrderOfTheProject(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		held    []issueHeld
		printed *youtrack.Node
	}{
		{
			name: "by their place in the project",
			held: []issueHeld{
				{name: "Second", valueType: "string", value: `"b"`, ordinal: "2", binding: "1-1"},
				{name: "First", valueType: "string", value: `"a"`, ordinal: "1", binding: "1-2"},
				{name: "Third", valueType: "string", value: `"c"`, ordinal: "8", binding: "1-3"},
			},
			printed: issueFieldsBlock(
				youtrack.DataPair("First", youtrack.NewString("a")),
				youtrack.DataPair("Second", youtrack.NewString("b")),
				youtrack.DataPair("Third", youtrack.NewString("c"))),
		},
		{
			name: "of one place, by the numbers of their bindings",
			held: []issueHeld{
				{name: "Tenth", valueType: "string", value: `"b"`, ordinal: "3", binding: "1-10"},
				{name: "Ninth", valueType: "string", value: `"a"`, ordinal: "3", binding: "1-9"},
			},
			printed: issueFieldsBlock(
				youtrack.DataPair("Ninth", youtrack.NewString("a")),
				youtrack.DataPair("Tenth", youtrack.NewString("b"))),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, node, err := issueShownFields(t, string(issueHeldFields(tc.held...)))

			require.NoError(t, err)
			assert.Equal(t, tc.printed, node)
		})
	}
}

type issueBrokenBlock struct {
	name  string
	block string
}

func issueBrokenFields() []issueBrokenBlock {
	field := func(name, binding string) string {
		return `[{"$type":"IssueCustomField","name":` + name + `,"value":null,"projectCustomField":` + binding + `}]`
	}
	binding := func(id, valueType string) string {
		return `{"$type":"ProjectCustomField","id":` + id + `,"ordinal":1,"field":{"$type":"CustomField","localizedName":null,` +
			`"fieldType":{"$type":"FieldType","valueType":` + valueType + `,"isMultiValue":false}}}`
	}
	held := func(valueType string, multi bool, value string) string {
		return string(issueHeldFields(issueHeld{name: "Field", valueType: valueType, multi: multi, value: value}))
	}
	return []issueBrokenBlock{
		{name: "the block is no array", block: `null`},
		{name: "a field is no object", block: `[null]`},
		{name: "a name is no text", block: field(`5`, binding(`"1-1"`, `"enum"`))},
		{name: "the field of the project is no object", block: field(`"Field"`, `[`+binding(`"1-1"`, `"enum"`)+`]`)},
		{name: "the binding to the project is named by no text", block: field(`"Field"`, binding(`5`, `"enum"`))},
		{name: "the type of the field is no text", block: field(`"Field"`, binding(`"1-1"`, `5`))},
		{name: "a translation that is a number", block: field(`"Field"`, `{"$type":"ProjectCustomField","id":"1-1",`+
			`"ordinal":1,"field":{"$type":"CustomField","localizedName":5,"fieldType":{"$type":"FieldType",`+
			`"valueType":"enum","isMultiValue":false}}}`)},
		{name: "a type the module does not model", block: held("quantum", false, issueElement("First"))},
		{name: "one value by the type and a list in the answer", block: held("enum", false, `[`+issueElement("First")+`]`)},
		{name: "several values by the type and one in the answer", block: held("enum", true, issueElement("First"))},
		{name: "a value carrying nothing its type names it by",
			block: held("user", false, `{"$type":"PeriodValue","id":"PT1H30M","minutes":90}`)},
		{name: "a value that is no object where a name is", block: held("enum", false, `"First"`)},
		{name: "a name that is a number",
			block: held("enum", false, `{"$type":"EnumBundleElement","id":"3-1","name":5,"localizedName":null}`)},
		{name: "a value whose id is a number",
			block: held("enum", false, `{"$type":"EnumBundleElement","id":5,"name":"First","localizedName":null}`)},
		{name: "a value whose translation is a number",
			block: held("state", false, `{"$type":"StateBundleElement","id":"3-1","name":"First","localizedName":5}`)},
		{name: "a whole number that is text", block: held("integer", false, `"42"`)},
		{name: "a number that is text", block: held("float", false, `"1.5"`)},
		{name: "a string that is a number", block: held("string", false, `42`)},
		{name: "a day that is a fraction", block: held("date", false, `1.5`)},
		{name: "minutes that are text", block: held("period", false, `{"$type":"PeriodValue","id":"PT1H30M","minutes":"90"}`)},
		{
			name: "no place among the fields of the project",
			block: `[{"$type":"IssueCustomField","name":"Field","value":null,"projectCustomField":{"$type":"ProjectCustomField",` +
				`"id":"1-1","ordinal":null,"field":{"$type":"CustomField","localizedName":null,"fieldType":{"$type":"FieldType",` +
				`"valueType":"enum","isMultiValue":false}}}}]`,
		},
		{
			name: "two fields of one name",
			block: string(issueHeldFields(
				issueHeld{name: "Field", valueType: "enum", binding: "1-1"},
				issueHeld{name: "Field", valueType: "state", binding: "1-2"})),
		},
	}
}

func TestShowIssueRefusesCustomFieldsOfAnotherShape(t *testing.T) {
	t.Parallel()
	for _, tc := range issueBrokenFields() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			server, _, err := issueShownFields(t, tc.block)

			body := `{"$type":"Issue","idReadable":"DEV-1","customFields":` + tc.block + `}`
			want := unreadable(requestTo(http.MethodGet, server, issuePath+"?fields="+issueCustomFields), body)
			assert.Equal(t, want, errorOf(t, err))
		})
	}
}
