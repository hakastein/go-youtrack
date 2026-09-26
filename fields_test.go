package youtrack_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/go-youtrack"
	"github.com/hakastein/go-youtrack/fake"
)

const (
	projectExpressed = `{"$type":"Project","shortName":"DEV","name":"First","description":null,` +
		`"leader":{"$type":"User","login":"leader","fullName":"Leader"},` +
		`"team":{"$type":"ProjectTeam","name":"Team","users":[{"$type":"User","login":"leader","fullName":"Leader"}]},` +
		`"plugins":{"timeTrackingSettings":{"enabled":true,"workItemTypes":[]}}}`
	projectAsked = "shortName,name,archived,leader(login)"
)

func projectShown(t *testing.T, server *fake.Server, expression string) (*youtrack.Node, error) {
	t.Helper()
	return client(t, server).Projects.Show(t.Context(), "DEV", &youtrack.ShowProjectOptions{Fields: expression})
}

func projectUnchecked(server *fake.Server, code youtrack.Code, expression, key string, entries ...*youtrack.Node) youtrack.Error {
	return youtrack.Error{Code: code, Details: []youtrack.Pair{
		requestTo(http.MethodGet, server, "/api/admin/projects/DEV?fields="+expression),
		{Key: "fields", Value: youtrack.NewString(expression)},
		{Key: key, Value: youtrack.NewList(entries...)},
	}}
}

func projectMissing(field string, schema *youtrack.Node) *youtrack.Node {
	return youtrack.NewMap(youtrack.Pair{Key: "field", Value: youtrack.NewString(field)}, youtrack.Pair{Key: "type", Value: schema})
}

func TestShowProjectRefusesAnExpressionItCannotRead(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
	}{
		{name: "no fields", expression: ""},
		{name: "spaces and tabs alone", expression: " \t"},
		{name: "a plus alone", expression: "+"},
		{name: "a comma at the end", expression: "a,"},
		{name: "a comma at the start", expression: ",a"},
		{name: "two commas", expression: "a,,b"},
		{name: "empty parentheses", expression: "a()"},
		{name: "an unclosed parenthesis", expression: "a(b"},
		{name: "an unopened parenthesis", expression: "a)"},
		{name: "a space between names", expression: "a b"},
		{name: "a plus inside", expression: "+a+b"},
		{name: "a name outside ASCII", expression: "поле"},
		{name: "tabs before a closing parenthesis", expression: "a,\t\t)"},
		{name: "a name in quotes where no custom field is named", expression: `"name"`},
		{name: "a word read as a bool", expression: "on"},
		{name: "a word read as a bool, letter case aside", expression: "leader(login,No)"},
		{name: "a word read as null", expression: "null"},
		{name: "a leading digit", expression: "1abc"},
		{name: "the content of a file", expression: "issues(attachments(base64Content))"},
		{name: "the content of a file at a place that holds no file at all", expression: "base64Content"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := projectShown(t, fake.ServeNothing(t), tc.expression)

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

func TestShowProjectSendsEachFieldOnceInOneForm(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
		sent       string
	}{
		{name: "spaces and tabs around names and punctuation", expression: " \tname\t, leader ( login ) ", sent: "name,leader(login)"},
		{name: "a name given again keeps its first place", expression: "name,shortName,name", sent: "name,shortName"},
		{name: "a name given bare before its fields", expression: "leader,leader(login)", sent: "leader(login)"},
		{
			name:       "fields merged at depth",
			expression: "team(users(login)),name,team(users(fullName),name)",
			sent:       "team(users(login,fullName),name),name",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, projectExpressed))

			_, err := projectShown(t, server, tc.expression)

			require.NoError(t, err)
			assert.Equal(t, []string{tc.sent}, server.Fields())
		})
	}
}

func TestShowProjectPrintsWhatArrivedAsItArrived(t *testing.T) {
	t.Parallel()
	const pastFloat64Precision = "9007199254740993"
	server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"Project","name":"First","archived":true,`+
		`"startingNumber":`+pastFloat64Precision+`,"issues":[],"leader":null}`))

	node, err := projectShown(t, server, "$type,name,archived,startingNumber,issues(idReadable),leader(login)")

	require.NoError(t, err)
	assert.Equal(t, youtrack.NewMap(
		youtrack.Pair{Key: "$type", Value: youtrack.NewString("Project")},
		youtrack.Pair{Key: "name", Value: youtrack.NewString("First")},
		youtrack.Pair{Key: "archived", Value: youtrack.NewBool(true)},
		youtrack.Pair{Key: "startingNumber", Value: youtrack.NewNumber(pastFloat64Precision)},
		youtrack.Pair{Key: "issues", Value: youtrack.NewList()},
		youtrack.Pair{Key: "leader", Value: youtrack.NewNull()},
	), node)
}

func TestShowProjectRefusesAFieldMissingFromTheAnswer(t *testing.T) {
	t.Parallel()
	typed := youtrack.NewString
	tests := []struct {
		name       string
		expression string
		body       string
		missing    []*youtrack.Node
	}{
		{
			name:       "a field the named type declares",
			expression: projectAsked,
			body:       `{"leader":{"login":"leader","$type":"User"},"shortName":"DEV","archived":false,"$type":"Project"}`,
			missing:    []*youtrack.Node{projectMissing("name", typed("Project"))},
		},
		{
			name:       "a field of an object that names no type",
			expression: projectAsked,
			body:       `{"leader":{"login":"leader","$type":"User"},"shortName":"DEV","archived":false}`,
			missing:    []*youtrack.Node{projectMissing("name", youtrack.NewNull())},
		},
		{
			name:       "a field of an object of a type the specification does not have",
			expression: projectAsked,
			body:       `{"leader":{"login":"leader","$type":"User"},"shortName":"DEV","archived":false,"$type":"Unknown"}`,
			missing:    []*youtrack.Node{projectMissing("name", typed("Unknown"))},
		},
		{
			name:       "a nested field the named type declares",
			expression: projectAsked,
			body:       `{"leader":{"$type":"User"},"shortName":"DEV","name":"First","archived":false,"$type":"Project"}`,
			missing:    []*youtrack.Node{projectMissing("leader(login)", typed("User"))},
		},
		{
			name:       "fields asked of a string where the schema declares an object",
			expression: projectAsked,
			body:       `{"leader":"leader","shortName":"DEV","name":"First","archived":false,"$type":"Project"}`,
			missing:    []*youtrack.Node{projectMissing("leader(login)", youtrack.NewNull())},
		},
		{
			name:       "fields asked of a number where the schema declares an object",
			expression: projectAsked,
			body:       `{"leader":7,"shortName":"DEV","name":"First","archived":false,"$type":"Project"}`,
			missing:    []*youtrack.Node{projectMissing("leader(login)", youtrack.NewNull())},
		},
		{
			name:       "a field of an item of a list",
			expression: "issues(idReadable)",
			body:       `{"issues":[{"idReadable":"DEV-1","$type":"Issue"},{"$type":"Issue"}],"$type":"Project"}`,
			missing:    []*youtrack.Node{projectMissing("issues(idReadable)", typed("Issue"))},
		},
		{
			name:       "fields asked of a string in a list of objects",
			expression: "issues(idReadable)",
			body:       `{"issues":["DEV-1"],"$type":"Project"}`,
			missing:    []*youtrack.Node{projectMissing("issues(idReadable)", youtrack.NewNull())},
		},
		{
			name:       "a field absent from every item of a list, once",
			expression: "issues(idReadable,summary)",
			body:       `{"issues":[{"summary":"First","$type":"Issue"},{"summary":"Second","$type":"Issue"}],"$type":"Project"}`,
			missing:    []*youtrack.Node{projectMissing("issues(idReadable)", typed("Issue"))},
		},
		{
			name:       "a field beside a name no schema declares",
			expression: "shortName,bogus,name",
			body:       `{"shortName":"DEV","$type":"Project"}`,
			missing:    []*youtrack.Node{projectMissing("name", typed("Project"))},
		},
		{
			name:       "a field under a field of no schema, by the type named there",
			expression: "customFields(field(name))",
			body: `{"customFields":[{"field":{"name":"First","$type":"CustomField"},"$type":"StateProjectCustomField"},` +
				`{"$type":"EnumProjectCustomField"}],"$type":"Project"}`,
			missing: []*youtrack.Node{projectMissing("customFields(field)", typed("EnumProjectCustomField"))},
		},
		{
			name:       "every field of an object of a schema that may not stand at the root",
			expression: projectAsked,
			body:       `{"login":"leader","$type":"User"}`,
			missing: []*youtrack.Node{
				projectMissing("shortName", typed("User")), projectMissing("name", typed("User")),
				projectMissing("archived", typed("User")), projectMissing("leader", typed("User")),
			},
		},
		{
			name:       "a field of an object of a schema that may not stand under its field",
			expression: projectAsked,
			body:       `{"leader":{"shortName":"X","$type":"Project"},"shortName":"DEV","name":"First","archived":false,"$type":"Project"}`,
			missing:    []*youtrack.Node{projectMissing("leader(login)", typed("Project"))},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tc.body))

			_, err := projectShown(t, server, tc.expression)

			want := projectUnchecked(server, youtrack.CodeUpstreamInvalid, tc.expression, "missing", tc.missing...)
			assert.Equal(t, want, errorOf(t, err))
		})
	}
}

func TestShowProjectRefusesANameNoSchemaOfItsNodeDeclares(t *testing.T) {
	t.Parallel()
	projectNames := []string{"$type", "archived", "createdBy", "customFields", "description", "fromEmail", "iconUrl",
		"id", "issues", "leader", "name", "replyToEmail", "shortName", "startingNumber", "team", "template"}
	userNames := []string{"$type", "avatarUrl", "banned", "email", "fullName", "guest", "id", "isAnonymized", "login",
		"name", "online", "profiles", "ringId", "savedQueries", "tags"}
	tests := []struct {
		name       string
		expression string
		body       string
		unknown    *youtrack.Node
	}{
		{
			name:       "the nearest name first",
			expression: "tam",
			body:       `{"$type":"Project"}`,
			unknown:    withNearest("field", "tam", "team", "name"),
		},
		{
			name:       "letter case aside",
			expression: "leader(LOGIN)",
			body:       `{"leader":{"login":"leader","$type":"User"},"$type":"Project"}`,
			unknown:    withNearest("field", "leader(LOGIN)", "login"),
		},
		{
			name:       "a name absent from every item of a list, once",
			expression: "issues(summery)",
			body:       `{"issues":[{"$type":"Issue"},{"$type":"Issue"}],"$type":"Project"}`,
			unknown:    withNearest("field", "issues(summery)", "summary"),
		},
		{
			name:       "under a field of no schema, by the types named there",
			expression: "customFields(bundel)",
			body:       `{"customFields":[{"$type":"EnumProjectCustomField"},{"$type":"PeriodProjectCustomField"}],"$type":"Project"}`,
			unknown:    withNearest("field", "customFields(bundel)", "bundle"),
		},
		{
			name:       "at the root, a name only another schema of the hierarchy of the answer declares",
			expression: "owner",
			body:       `{"$type":"Project"}`,
			unknown:    withNearest("field", "owner", projectNames...),
		},
		{
			name:       "a name only the schema of an object that may not stand at its place declares",
			expression: "leader(shortName)",
			body:       `{"leader":{"$type":"Project"},"$type":"Project"}`,
			unknown:    withNearest("field", "leader(shortName)", userNames...),
		},
		{
			name:       "under a field the schema above does not declare, by the type named there",
			expression: "team(users(logn))",
			body:       `{"team":{"users":[{"login":"leader","$type":"User"}],"$type":"ProjectTeam"},"$type":"Project"}`,
			unknown:    withNearest("field", "team(users(logn))", "login"),
		},
		{
			name:       "a name asked of a scalar",
			expression: "shortName(bogus)",
			body:       `{"shortName":"DEV","$type":"Project"}`,
			unknown:    withNearest("field", "shortName(bogus)"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tc.body))

			_, err := projectShown(t, server, tc.expression)

			want := projectUnchecked(server, youtrack.CodeUnknownName, tc.expression, "unknown", tc.unknown)
			assert.Equal(t, want, errorOf(t, err))
		})
	}
}

func TestShowProjectLeavesOutAFieldTheNamedTypeDoesNotDeclare(t *testing.T) {
	t.Parallel()
	listed := func(key string, items ...*youtrack.Node) *youtrack.Node {
		return youtrack.NewMap(youtrack.Pair{Key: key, Value: youtrack.NewList(items...)})
	}
	nameOf := func(name string) *youtrack.Node {
		return youtrack.NewMap(youtrack.Pair{Key: "name", Value: youtrack.NewString(name)})
	}
	field := func(name string) youtrack.Pair { return youtrack.Pair{Key: "field", Value: nameOf(name)} }
	bundle := youtrack.Pair{Key: "bundle", Value: nameOf("Bundle")}
	tests := []struct {
		name       string
		expression string
		body       string
		printed    *youtrack.Node
	}{
		{
			name:       "names asked of an empty list",
			expression: "issues(bogus)",
			body:       `{"issues":[],"$type":"Project"}`,
			printed:    listed("issues"),
		},
		{
			name:       "a name asked of a null",
			expression: "createdBy(bogus)",
			body:       `{"createdBy":null,"$type":"Project"}`,
			printed:    youtrack.NewMap(youtrack.Pair{Key: "createdBy", Value: youtrack.NewNull()}),
		},
		{
			name:       "a field of another schema under a field of no schema",
			expression: "customFields(field(name),bundle(name))",
			body: `{"customFields":[{"field":{"name":"First","$type":"CustomField"},"bundle":{"name":"Bundle","$type":"EnumBundle"},` +
				`"$type":"EnumProjectCustomField"},{"field":{"name":"Second","$type":"CustomField"},"$type":"PeriodProjectCustomField"}],` +
				`"$type":"Project"}`,
			printed: listed("customFields", youtrack.NewMap(field("First"), bundle), youtrack.NewMap(field("Second"))),
		},
		{
			name:       "a field of no schema whose one type lacks a field its siblings declare",
			expression: "customFields(field(name),bundle(name))",
			body: `{"customFields":[{"field":{"name":"First","$type":"CustomField"},"$type":"PeriodProjectCustomField"},` +
				`{"field":{"name":"Second","$type":"CustomField"},"$type":"PeriodProjectCustomField"}],"$type":"Project"}`,
			printed: listed("customFields", youtrack.NewMap(field("First")), youtrack.NewMap(field("Second"))),
		},
		{
			name:       "a field of no schema whose first type lacks a field a type after it declares",
			expression: "customFields(field(name),bundle(name))",
			body: `{"customFields":[{"field":{"name":"First","$type":"CustomField"},"$type":"PeriodProjectCustomField"},` +
				`{"field":{"name":"Second","$type":"CustomField"},"bundle":{"name":"Bundle","$type":"EnumBundle"},` +
				`"$type":"EnumProjectCustomField"}],"$type":"Project"}`,
			printed: listed("customFields", youtrack.NewMap(field("First")), youtrack.NewMap(field("Second"), bundle)),
		},
		{
			name:       "a field of no schema that another schema of the hierarchy named there declares",
			expression: "customFields(owner)",
			body:       `{"customFields":[{"$type":"Project"}],"$type":"Project"}`,
			printed:    listed("customFields", youtrack.NewMap()),
		},
		{
			name:       "under a field no schema declares, a field of a type named after the object that lacks it",
			expression: "team(users(login))",
			body: `{"team":{"users":[{"name":"Group","$type":"UserGroup"},{"login":"leader","$type":"User"}],` +
				`"$type":"ProjectTeam"},"$type":"Project"}`,
			printed: youtrack.NewMap(youtrack.Pair{Key: "team", Value: listed("users", youtrack.NewMap(),
				youtrack.NewMap(youtrack.Pair{Key: "login", Value: youtrack.NewString("leader")}))}),
		},
		{
			name:       "a field of an object of any schema where a schema declares a value of no schema",
			expression: "issues(customFields(value(login)))",
			body: `{"issues":[{"customFields":[{"value":{"minutes":90,"$type":"DurationValue"},` +
				`"$type":"SimpleIssueCustomField"}],"$type":"Issue"}],"$type":"Project"}`,
			printed: listed("issues", listed("customFields", youtrack.NewMap(youtrack.Pair{Key: "value", Value: youtrack.NewMap()}))),
		},
		{
			name:       "fields asked of a scalar where a schema declares a value of no schema",
			expression: "issues(customFields(value(name)))",
			body: `{"issues":[{"customFields":[{"value":7,"$type":"SimpleIssueCustomField"},` +
				`{"value":{"name":"Open","$type":"StateBundleElement"},"$type":"StateIssueCustomField"},` +
				`{"value":{"minutes":90,"$type":"PeriodValue"},"$type":"PeriodIssueCustomField"}],"$type":"Issue"}],"$type":"Project"}`,
			printed: listed("issues", listed("customFields",
				youtrack.NewMap(youtrack.Pair{Key: "value", Value: youtrack.NewNumber("7")}),
				youtrack.NewMap(youtrack.Pair{Key: "value", Value: nameOf("Open")}),
				youtrack.NewMap(youtrack.Pair{Key: "value", Value: youtrack.NewMap()}),
			)),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tc.body))

			node, err := projectShown(t, server, tc.expression)

			require.NoError(t, err)
			assert.Equal(t, tc.printed, node)
		})
	}
}

type fieldsCall func(ctx context.Context, c *youtrack.Client, fields *string) (*youtrack.Node, error)

func optionsFor[T any](fields *string, with func(string) T) *T {
	if fields == nil {
		return nil
	}
	options := with(*fields)
	return &options
}

func writeOptions(fields *string) *youtrack.WriteOptions {
	return optionsFor(fields, func(f string) youtrack.WriteOptions { return youtrack.WriteOptions{Fields: f} })
}

func fieldsCalls() map[string]fieldsCall {
	summary := "Title"
	return map[string]fieldsCall{
		"Issues.Show": func(ctx context.Context, c *youtrack.Client, fields *string) (*youtrack.Node, error) {
			return c.Issues.Show(ctx, "DEV-1", optionsFor(fields, func(f string) youtrack.ShowIssueOptions { return youtrack.ShowIssueOptions{Fields: f} }))
		},
		"Issues.List": func(ctx context.Context, c *youtrack.Client, fields *string) (*youtrack.Node, error) {
			return c.Issues.List(ctx, "State: Open", optionsFor(fields, func(f string) youtrack.ListIssuesOptions { return youtrack.ListIssuesOptions{Fields: f} }))
		},
		"Issues.Create": func(ctx context.Context, c *youtrack.Client, fields *string) (*youtrack.Node, error) {
			return c.Issues.Create(ctx, "DEV", &youtrack.IssueInput{Summary: summary}, writeOptions(fields))
		},
		"Issues.Update": func(ctx context.Context, c *youtrack.Client, fields *string) (*youtrack.Node, error) {
			return c.Issues.Update(ctx, "DEV-1", &youtrack.IssueUpdate{Summary: &summary}, writeOptions(fields))
		},
		"Articles.Show": func(ctx context.Context, c *youtrack.Client, fields *string) (*youtrack.Node, error) {
			return c.Articles.Show(ctx, "DEV-A-1", optionsFor(fields, func(f string) youtrack.ShowArticleOptions { return youtrack.ShowArticleOptions{Fields: f} }))
		},
		"Articles.List": func(ctx context.Context, c *youtrack.Client, fields *string) (*youtrack.Node, error) {
			return c.Articles.List(ctx, "project: DEV", optionsFor(fields, func(f string) youtrack.ListArticlesOptions { return youtrack.ListArticlesOptions{Fields: f} }))
		},
		"Articles.Children": func(ctx context.Context, c *youtrack.Client, fields *string) (*youtrack.Node, error) {
			return c.Articles.Children(ctx, "DEV-A-1", optionsFor(fields, func(f string) youtrack.ListArticlesOptions { return youtrack.ListArticlesOptions{Fields: f} }))
		},
		"Articles.Create": func(ctx context.Context, c *youtrack.Client, fields *string) (*youtrack.Node, error) {
			return c.Articles.Create(ctx, "DEV", &youtrack.ArticleInput{Summary: summary}, writeOptions(fields))
		},
		"Articles.Update": func(ctx context.Context, c *youtrack.Client, fields *string) (*youtrack.Node, error) {
			return c.Articles.Update(ctx, "DEV-A-1", &youtrack.ArticleUpdate{Summary: &summary}, writeOptions(fields))
		},
		"Comments.List": func(ctx context.Context, c *youtrack.Client, fields *string) (*youtrack.Node, error) {
			return c.Comments.List(ctx, "DEV-1", optionsFor(fields, func(f string) youtrack.ListCommentsOptions { return youtrack.ListCommentsOptions{Fields: f} }))
		},
		"Comments.Create": func(ctx context.Context, c *youtrack.Client, fields *string) (*youtrack.Node, error) {
			return c.Comments.Create(ctx, "DEV-1", "Text", writeOptions(fields))
		},
		"Comments.Update": func(ctx context.Context, c *youtrack.Client, fields *string) (*youtrack.Node, error) {
			return c.Comments.Update(ctx, "DEV-1", "4-1", "Text", writeOptions(fields))
		},
		"Attachments.List": func(ctx context.Context, c *youtrack.Client, fields *string) (*youtrack.Node, error) {
			return c.Attachments.List(ctx, "DEV-1", optionsFor(fields, func(f string) youtrack.ListAttachmentsOptions { return youtrack.ListAttachmentsOptions{Fields: f} }))
		},
		"Attachments.Create": func(ctx context.Context, c *youtrack.Client, fields *string) (*youtrack.Node, error) {
			file := youtrack.File{Name: "first.txt", Content: strings.NewReader("First")}
			return c.Attachments.Create(ctx, "DEV-1", file, writeOptions(fields))
		},
		"Links.List": func(ctx context.Context, c *youtrack.Client, fields *string) (*youtrack.Node, error) {
			return c.Links.List(ctx, "DEV-1", optionsFor(fields, func(f string) youtrack.ListLinksOptions { return youtrack.ListLinksOptions{Fields: f} }))
		},
		"Links.Add": func(ctx context.Context, c *youtrack.Client, fields *string) (*youtrack.Node, error) {
			return c.Links.Add(ctx, "DEV-1", "relates to", "DEV-2", writeOptions(fields))
		},
		"Tags.List": func(ctx context.Context, c *youtrack.Client, fields *string) (*youtrack.Node, error) {
			return c.Tags.List(ctx, optionsFor(fields, func(f string) youtrack.ListTagsOptions { return youtrack.ListTagsOptions{Fields: f} }))
		},
		"Tags.Create": func(ctx context.Context, c *youtrack.Client, fields *string) (*youtrack.Node, error) {
			return c.Tags.Create(ctx, "Early", youtrack.TagSharing{}, writeOptions(fields))
		},
		"WorkItems.List": func(ctx context.Context, c *youtrack.Client, fields *string) (*youtrack.Node, error) {
			return c.WorkItems.List(ctx, "DEV-1", optionsFor(fields, func(f string) youtrack.ListWorkItemsOptions { return youtrack.ListWorkItemsOptions{Fields: f} }))
		},
		"WorkItems.Create": func(ctx context.Context, c *youtrack.Client, fields *string) (*youtrack.Node, error) {
			return c.WorkItems.Create(ctx, "DEV-1", &youtrack.WorkItemInput{Duration: time.Hour}, writeOptions(fields))
		},
		"WorkItems.Update": func(ctx context.Context, c *youtrack.Client, fields *string) (*youtrack.Node, error) {
			return c.WorkItems.Update(ctx, "DEV-1", "7-1", &youtrack.WorkItemUpdate{Text: &summary}, writeOptions(fields))
		},
		"Activities.List": func(ctx context.Context, c *youtrack.Client, fields *string) (*youtrack.Node, error) {
			return c.Activities.List(ctx, "DEV-1", optionsFor(fields, func(f string) youtrack.ListActivitiesOptions { return youtrack.ListActivitiesOptions{Fields: f} }))
		},
		"Projects.Show": func(ctx context.Context, c *youtrack.Client, fields *string) (*youtrack.Node, error) {
			return c.Projects.Show(ctx, "DEV", optionsFor(fields, func(f string) youtrack.ShowProjectOptions { return youtrack.ShowProjectOptions{Fields: f} }))
		},
		"Projects.List": func(ctx context.Context, c *youtrack.Client, fields *string) (*youtrack.Node, error) {
			return c.Projects.List(ctx, optionsFor(fields, func(f string) youtrack.ListProjectsOptions { return youtrack.ListProjectsOptions{Fields: f} }))
		},
		"Fields.List": func(ctx context.Context, c *youtrack.Client, fields *string) (*youtrack.Node, error) {
			return c.Fields.List(ctx, "DEV", optionsFor(fields, func(f string) youtrack.ListFieldsOptions { return youtrack.ListFieldsOptions{Fields: f} }))
		},
		"Fields.Show": func(ctx context.Context, c *youtrack.Client, fields *string) (*youtrack.Node, error) {
			return c.Fields.Show(ctx, "DEV", "State", optionsFor(fields, func(f string) youtrack.ShowFieldOptions { return youtrack.ShowFieldOptions{Fields: f} }))
		},
		"Users.Show": func(ctx context.Context, c *youtrack.Client, fields *string) (*youtrack.Node, error) {
			return c.Users.Show(ctx, "first", optionsFor(fields, func(f string) youtrack.ShowUserOptions { return youtrack.ShowUserOptions{Fields: f} }))
		},
		"Users.List": func(ctx context.Context, c *youtrack.Client, fields *string) (*youtrack.Node, error) {
			return c.Users.List(ctx, "fir", optionsFor(fields, func(f string) youtrack.ListUsersOptions { return youtrack.ListUsersOptions{Fields: f} }))
		},
	}
}

func TestEveryDocumentOperationRefusesToReadWithoutFields(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		fields *string
	}{
		{name: "no options"},
		{name: "no fields", fields: new("")},
	}
	for operation, call := range fieldsCalls() {
		for _, tc := range tests {
			t.Run(operation+"/"+tc.name, func(t *testing.T) {
				t.Parallel()

				_, err := call(t.Context(), client(t, fake.ServeNothing(t)), tc.fields)

				assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
			})
		}
	}
}
