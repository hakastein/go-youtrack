package youtrack_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/go-youtrack"
	"github.com/hakastein/go-youtrack/fake"
)

const issueTagged = issuesPath + "?fields=idReadable,summary,tags(id,name)"

func issueTagging(t *testing.T, tags string, write http.HandlerFunc) *fake.Server {
	t.Helper()
	return routes(t, map[string]http.HandlerFunc{
		"GET " + projectPath: fake.JSON(http.StatusOK, issueProject()),
		"GET /api/tags":      fake.JSON(http.StatusOK, tags),
		"POST " + issuesPath: write,
	})
}

func issueTaggedWith(t *testing.T, tags string) http.HandlerFunc {
	t.Helper()
	return fake.JSON(http.StatusOK, `{"$type":"Issue","idReadable":"DEV-1","summary":"First","tags":`+tags+`}`)
}

func issueTags(refs ...youtrack.TagRef) youtrack.IssueInput {
	return youtrack.IssueInput{Summary: "First", Tags: refs}
}

func TestCreateIssueHangsTheTagsInTheWriteThatFilesIt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		tags []youtrack.TagRef
		body string
	}{
		{name: "a name in another letter case", tags: []youtrack.TagRef{{Name: "EARLY"}}, body: `[{"id":"10-1"}]`},
		{name: "the exact spelling among names apart by letter case", tags: []youtrack.TagRef{{Name: "mixed"}},
			body: `[{"id":"10-2"}]`},
		{name: "one name of two owners, of one of them", tags: []youtrack.TagRef{{Name: "Mixed", OwnedBy: "second"}},
			body: `[{"id":"10-3"}]`},
		{name: "two tags", tags: []youtrack.TagRef{{Name: "Early"}, {Name: "Mixed", OwnedBy: "FIRST"}},
			body: `[{"id":"10-1"},{"id":"10-4"}]`},
		{name: "one tag named twice", tags: []youtrack.TagRef{{Name: "Early"}, {Name: "early", OwnedBy: "first"}},
			body: `[{"id":"10-1"}]`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := issueTagging(t, tagsShown(), issueTaggedWith(t, tagsShown()))

			_, err := issueCreate(t, server, issueTags(tc.tags...), "idReadable")

			require.NoError(t, err)
			assert.Equal(t, []string{projectPath, "/api/tags", issuesPath}, server.Paths())
			assert.JSONEq(t, `{"project":{"id":"0-1"},"summary":"First","tags":`+tc.body+`}`, server.Last(t).Body)
		})
	}
}

func TestCreateIssueReadsEveryTagOnceAndAsksForTheTagsItChecks(t *testing.T) {
	t.Parallel()
	server := issueTagging(t, tagsShown(), issueTaggedWith(t, tagsShown()))

	_, err := issueCreate(t, server, issueTags(youtrack.TagRef{Name: "Early"}, youtrack.TagRef{Name: "10-1"}), "idReadable")

	require.NoError(t, err)
	assert.Equal(t, []string{projectPath, "/api/tags", issuesPath}, server.Paths())
	assert.Equal(t, tagCatalogueTarget, server.Targets(t)[1])
	assert.Equal(t, "idReadable,summary,tags(id,name)", server.Last(t).URL.Query().Get("fields"))
}

func TestCreateIssueRefusesTagNamesItCannotResolve(t *testing.T) {
	t.Parallel()
	mixed := youtrack.Pair{Key: "ambiguous", Value: youtrack.NewList(youtrack.NewMap(
		youtrack.Pair{Key: "tag", Value: youtrack.NewString("MIXED")},
		youtrack.Pair{Key: "candidates", Value: youtrack.NewList(
			tagCandidate("Mixed", "first"), tagCandidate("Mixed", "second"), tagCandidate("mixed", "first"))}))}
	tests := []struct {
		name    string
		tags    []youtrack.TagRef
		details []youtrack.Pair
	}{
		{
			name:    "a name near a tag",
			tags:    []youtrack.TagRef{{Name: "Early"}, {Name: "Erly"}},
			details: []youtrack.Pair{{Key: "unknown", Value: youtrack.NewList(withNearest("tag", "Erly", "Early"))}},
		},
		{
			name:    "a name of several tags and no exact spelling",
			tags:    []youtrack.TagRef{{Name: "MIXED"}},
			details: []youtrack.Pair{mixed},
		},
		{
			name: "a login no tag of that name belongs to",
			tags: []youtrack.TagRef{{Name: "early", OwnedBy: "second"}},
			details: []youtrack.Pair{{Key: "unknown", Value: youtrack.NewList(youtrack.NewMap(
				youtrack.Pair{Key: "tag", Value: youtrack.NewString("early")},
				youtrack.Pair{Key: "owned_by", Value: youtrack.NewString("second")},
				youtrack.Pair{Key: "candidates", Value: youtrack.NewList(tagCandidate("Early", "first"))}))}},
		},
		{
			name: "an unknown name and an ambiguous one",
			tags: []youtrack.TagRef{{Name: "MIXED"}, {Name: "Erly"}, {Name: "zzzzzz"}},
			details: []youtrack.Pair{
				{Key: "unknown", Value: youtrack.NewList(
					withNearest("tag", "Erly", "Early"),
					withNearest("tag", "zzzzzz", "10-1", "Early", "Mixed", "Mixed", "mixed"))},
				mixed,
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := issueTagging(t, tagsShown(), fake.Unexpected(t))

			_, err := issueCreate(t, server, issueTags(tc.tags...), "idReadable")

			want := youtrack.Error{Code: youtrack.CodeUnknownName,
				Details: append([]youtrack.Pair{requestTo(http.MethodGet, server, tagCatalogueTarget)}, tc.details...)}
			assert.Equal(t, want, errorOf(t, err))
			assert.Equal(t, []string{projectPath, "/api/tags"}, server.Paths())
		})
	}
}

func TestCreateIssueRefusesATagItCannotHangByID(t *testing.T) {
	t.Parallel()
	catalogue := tagCatalogue(tagOf("10-x", "Early", "first"))
	server := issueTagging(t, catalogue, fake.Unexpected(t))

	_, err := issueCreate(t, server, issueTags(youtrack.TagRef{Name: "Early"}), "idReadable")

	assert.Equal(t, unreadable(requestTo(http.MethodGet, server, tagCatalogueTarget), catalogue), errorOf(t, err))
	assert.Equal(t, []string{projectPath, "/api/tags"}, server.Paths())
}

func TestCreateIssueTakesTagsTheServerHungBesidesTheOnesWritten(t *testing.T) {
	t.Parallel()
	server := issueTagging(t, tagsShown(), issueTaggedWith(t, tagCatalogue(
		tagOf("10-5", "10-1", "first"), tagOf("10-1", "Early", "first"))))

	node, err := issueCreate(t, server, issueTags(youtrack.TagRef{Name: "Early"}), "idReadable")

	require.NoError(t, err)
	assert.Equal(t, issueWrittenID(), node)
}

func TestCreateIssueRefusesAnAnswerWithoutATagItHung(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		tags   string
		actual *youtrack.Node
	}{
		{name: "no tag at all", tags: "[]", actual: texts()},
		{name: "one of the two", tags: tagCatalogue(tagOf("10-3", "Mixed", "second")), actual: texts("Mixed")},
		{name: "another tag of the same name", tags: tagCatalogue(tagOf("10-4", "Mixed", "first")), actual: texts("Mixed")},
		{name: "tags that are null", tags: "null", actual: youtrack.NewNull()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := issueTagging(t, tagsShown(), issueTaggedWith(t, tc.tags))

			_, err := issueCreate(t, server, issueTags(youtrack.TagRef{Name: "early"},
				youtrack.TagRef{Name: "Mixed", OwnedBy: "second"}), "idReadable")

			want := issueMismatch(requestTo(http.MethodPost, server, issueTagged),
				mismatch("tags", texts("Early", "Mixed"), tc.actual))
			assert.Equal(t, want, errorOf(t, err))
		})
	}
}
