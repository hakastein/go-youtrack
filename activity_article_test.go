package youtrack_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/hakastein/go-youtrack"
	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const articleActivityPath = "/api/articles/DEV-A-1/activities"

func articleActivityServer(t *testing.T, activities string) *fake.Server {
	t.Helper()
	return routes(t, map[string]http.HandlerFunc{"GET " + articleActivityPath: fake.JSON(http.StatusOK, activities)})
}

func articleActivityList(t *testing.T, server *fake.Server, fields string, categories ...youtrack.ActivityCategory) (*youtrack.Node, error) {
	t.Helper()
	opts := &youtrack.ListActivitiesOptions{Fields: fields, Categories: categories}
	return client(t, server).Activities.List(t.Context(), youtrack.ArticleID("DEV-A-1"), opts)
}

func TestListActivitiesOfAnArticleAsksForTheCategoriesOfAnArticle(t *testing.T) {
	t.Parallel()
	every := make([]string, 0, len(youtrack.ArticleActivityCategories()))
	for _, category := range youtrack.ArticleActivityCategories() {
		every = append(every, string(category))
	}
	tests := []struct {
		name       string
		categories []youtrack.ActivityCategory
		sent       string
	}{
		{name: "no category, which asks for every one", sent: strings.Join(every, ",")},
		{
			name:       "one category in another letter case",
			categories: []youtrack.ActivityCategory{youtrack.ArticleActivityCategory("articlesummarycategory")},
			sent:       "ArticleSummaryCategory",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := articleActivityServer(t, `[]`)

			_, err := articleActivityList(t, server, "category", tc.categories...)

			require.NoError(t, err)
			assert.Equal(t, articleActivityPath, server.Last(t).URL.Path)
			assert.Equal(t, []string{tc.sent}, server.Last(t).URL.Query()["categories"])
		})
	}
}

// The server answers a category of the other kind with an empty history, so the refusal is the module's own.
func TestListActivitiesRefusesACategoryOfTheOtherKindOfOwner(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		owner    youtrack.Owner
		category youtrack.ActivityCategory
		want     youtrack.Code
	}{
		{name: "a category of an issue asked of an article", owner: youtrack.ArticleID("DEV-A-1"),
			category: youtrack.SummaryCategory, want: youtrack.CodeUnknownName},
		{name: "a category of an article asked of an issue", owner: youtrack.IssueID("DEV-1"),
			category: youtrack.ArticleSummaryCategory, want: youtrack.CodeUnknownName},
		{name: "a category of an issue holding the name of one of an article", owner: youtrack.ArticleID("DEV-A-1"),
			category: youtrack.IssueActivityCategory("ArticleSummaryCategory"), want: youtrack.CodeUnknownName},
		{name: "no category at all", owner: youtrack.ArticleID("DEV-A-1"), category: nil, want: youtrack.CodeBadUsage},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts := &youtrack.ListActivitiesOptions{Fields: "timestamp", Categories: []youtrack.ActivityCategory{tc.category}}
			_, err := client(t, fake.ServeNothing(t)).Activities.List(t.Context(), tc.owner, opts)

			assert.Equal(t, tc.want, errorOf(t, err).Code)
		})
	}
}

// Article…ActivityItem is missing from the specification: its members are read by what ActivityItem declares.
func TestListActivitiesOfAnArticleReadsATypeTheSpecificationLeavesOut(t *testing.T) {
	t.Parallel()
	project := func(id, name string) *youtrack.Node {
		return youtrack.NewMap(youtrack.Pair{Key: "id", Value: youtrack.NewString(id)},
			youtrack.Pair{Key: "name", Value: youtrack.NewString(name)})
	}
	tests := []struct {
		name     string
		activity string
		added    *youtrack.Node
		removed  *youtrack.Node
	}{
		{
			name: "a content, a text at either end",
			activity: activityJSON("ArticleTextMarkupActivityItem", "ArticleDescriptionCategory", activityEarly,
				`"field":{"$type":"PredefinedFilterField","id":"content"},"added":"Late","removed":"Early"`),
			added:   texts("Late"),
			removed: texts("Early"),
		},
		{
			name: "a project, one on its own at either end",
			activity: activityJSON("ArticleProjectActivityItem", "ArticleProjectCategory", activityEarly,
				`"field":null,"added":{"$type":"Project","id":"0-2","name":"Docs"},`+
					`"removed":{"$type":"Project","id":"0-0","name":"Demo"}`),
			added:   youtrack.NewList(project("0-2", "Docs")),
			removed: youtrack.NewList(project("0-0", "Demo")),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := articleActivityServer(t, activityArray(tc.activity))

			got, err := articleActivityList(t, server, "timestamp,field,added(id,name),removed(id,name)")

			require.NoError(t, err)
			want := youtrack.NewMap(
				youtrack.Pair{Key: "timestamp", Value: youtrack.NewString("1970-01-01T00:00:01Z")},
				youtrack.Pair{Key: "field", Value: youtrack.NewNull()},
				youtrack.Pair{Key: "added", Value: tc.added},
				youtrack.Pair{Key: "removed", Value: tc.removed})
			assert.Equal(t, wholePage("activities", want), got)
		})
	}
}
