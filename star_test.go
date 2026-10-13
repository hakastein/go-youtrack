package youtrack_test

import (
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/go-youtrack"
	"github.com/hakastein/go-youtrack/fake"
)

func issueStarred(star bool) string {
	held := "false"
	if star {
		held = "true"
	}
	return `{"$type":"Issue","idReadable":"DEV-1","summary":"First","tags":[],` +
		`"watchers":{"$type":"IssueWatchers","hasStar":` + held + `}}`
}

// answersInTurn answers each request with the next of answers, and the last one past them.
func answersInTurn(answers ...http.HandlerFunc) http.HandlerFunc {
	var calls atomic.Int32
	return func(w http.ResponseWriter, r *http.Request) {
		at := min(int(calls.Add(1))-1, len(answers)-1)
		answers[at](w, r)
	}
}

func issueStarring(t *testing.T, create, update http.HandlerFunc) *fake.Server {
	t.Helper()
	return routes(t, map[string]http.HandlerFunc{
		"GET " + projectPath: fake.JSON(http.StatusOK, issueProject()),
		"GET " + issuePath:   fake.JSON(http.StatusOK, issueToWrite(issueProject(), "[]")),
		"POST " + issuesPath: create,
		"POST " + issuePath:  update,
	})
}

func starred(star bool) youtrack.IssueInput {
	return youtrack.IssueInput{Summary: "First", Star: &star}
}

func TestCreateIssueWritesTheStarInTheWriteThatFilesIt(t *testing.T) {
	t.Parallel()
	server := issueStarring(t, fake.JSON(http.StatusOK, issueStarred(true)), fake.Unexpected(t))

	node, err := issueCreate(t, server, starred(true), "idReadable")

	require.NoError(t, err)
	assert.Equal(t, issueWrittenID(), node)
	assert.Equal(t, []string{projectPath, issuesPath}, server.Paths())
	assert.JSONEq(t, `{"project":{"id":"0-1"},"summary":"First","watchers":{"hasStar":true}}`, server.Last(t).Body)
	assert.Equal(t, "idReadable,summary,watchers(hasStar)", server.Last(t).URL.Query().Get("fields"))
}

func TestCreateIssueTakesOffTheStarTheProfilePutOnIt(t *testing.T) {
	t.Parallel()
	server := issueStarring(t, fake.JSON(http.StatusOK, issueStarred(true)), fake.JSON(http.StatusOK, issueStarred(false)))

	node, err := issueCreate(t, server, starred(false), "idReadable")

	require.NoError(t, err)
	assert.Equal(t, issueWrittenID(), node)
	assert.Equal(t, []string{projectPath, issuesPath, issuePath}, server.Paths())
	assert.JSONEq(t, `{"watchers":{"hasStar":false}}`, server.Last(t).Body)
}

func TestCreateIssueWritesOnceWhenTheNewIssueHasNoStar(t *testing.T) {
	t.Parallel()
	server := issueStarring(t, fake.JSON(http.StatusOK, issueStarred(false)), fake.Unexpected(t))

	_, err := issueCreate(t, server, starred(false), "idReadable")

	require.NoError(t, err)
	assert.Equal(t, []string{projectPath, issuesPath}, server.Paths())
}

func TestCreateIssueFailsAfterTheWriteWhenTheStarStaysOn(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		update http.HandlerFunc
		code   youtrack.Code
	}{
		{name: "the server refuses to take it off", update: fake.JSON(http.StatusForbidden, `{"error":"Forbidden"}`),
			code: youtrack.CodeDenied},
		{name: "the star comes back on", update: fake.JSON(http.StatusOK, issueStarred(true)),
			code: youtrack.CodeUpstreamInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := issueStarring(t, fake.JSON(http.StatusOK, issueStarred(true)), tc.update)

			_, err := issueCreate(t, server, starred(false), "idReadable")

			got := errorOf(t, err)
			assert.Equal(t, tc.code, got.Code)
			assert.True(t, got.AfterWrite)
			assert.Contains(t, got.Details, youtrack.Pair{Key: "issue", Value: youtrack.NewString("DEV-1")})
			assert.Equal(t, []string{projectPath, issuesPath, issuePath}, server.Paths())
		})
	}
}

func TestCreateIssueNamesNoTagWhenTheStarStaysOnAfterTheWrite(t *testing.T) {
	t.Parallel()
	filed := `{"$type":"Issue","idReadable":"DEV-1","summary":"First","tags":[` + tagOf("10-1", "Early", "first") +
		`],"watchers":{"$type":"IssueWatchers","hasStar":true}}`
	server := routes(t, map[string]http.HandlerFunc{
		"GET " + projectPath: fake.JSON(http.StatusOK, issueProject()),
		"GET /api/tags":      fake.JSON(http.StatusOK, tagsShown()),
		"GET " + profilePath: fake.JSON(http.StatusOK, starProfile("10-9")),
		"POST " + issuesPath: fake.JSON(http.StatusOK, filed),
		"POST " + issuePath:  fake.JSON(http.StatusForbidden, `{"error":"Forbidden"}`),
	})
	off := false
	in := youtrack.IssueInput{Summary: "First", Tags: []youtrack.TagRef{{Name: "Early"}}, Star: &off}

	_, err := issueCreate(t, server, in, "idReadable")

	got := errorOf(t, err)
	assert.Equal(t, youtrack.CodeDenied, got.Code)
	assert.True(t, got.AfterWrite)
	assert.False(t, hasPair(got.Details, "tags"))
}

func hasPair(details []youtrack.Pair, key string) bool {
	for _, pair := range details {
		if pair.Key == key {
			return true
		}
	}
	return false
}

func TestCreateIssueRefusesAnAnswerWithoutTheStarItWrote(t *testing.T) {
	t.Parallel()
	server := issueStarring(t, fake.JSON(http.StatusOK, issueStarred(false)), fake.Unexpected(t))

	_, err := issueCreate(t, server, starred(true), "idReadable")

	got := errorOf(t, err)
	assert.Equal(t, youtrack.CodeUpstreamInvalid, got.Code)
	assert.True(t, got.AfterWrite)
}

func TestUpdateIssueWritesTheStar(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		star bool
	}{
		{name: "on", star: true},
		{name: "off", star: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := issueStarring(t, fake.Unexpected(t), fake.JSON(http.StatusOK, issueStarred(tc.star)))

			_, err := issueUpdate(t, server, youtrack.IssueUpdate{Star: &tc.star}, "idReadable")

			require.NoError(t, err)
			want := `{"watchers":{"hasStar":false}}`
			if tc.star {
				want = `{"watchers":{"hasStar":true}}`
			}
			assert.JSONEq(t, want, server.Last(t).Body)
		})
	}
}

func TestUpdateIssueRefusesAnAnswerWithAnotherStar(t *testing.T) {
	t.Parallel()
	off := false
	server := issueStarring(t, fake.Unexpected(t), fake.JSON(http.StatusOK, issueStarred(true)))

	_, err := issueUpdate(t, server, youtrack.IssueUpdate{Star: &off}, "idReadable")

	got := errorOf(t, err)
	assert.Equal(t, youtrack.CodeUpstreamInvalid, got.Code)
	assert.True(t, got.AfterWrite)
}

func starTagging(t *testing.T, profile http.HandlerFunc) *fake.Server {
	t.Helper()
	return routes(t, map[string]http.HandlerFunc{
		"GET " + projectPath:                 fake.JSON(http.StatusOK, issueProject()),
		"GET /api/issues/{id}":               fake.JSON(http.StatusOK, tagOwner("Issue", "DEV-7")),
		"GET /api/tags":                      fake.JSON(http.StatusOK, tagsShown()),
		"GET " + profilePath:                 profile,
		"POST " + issuesPath:                 issueTaggedWith(t, tagCatalogue(tagOf("10-1", "Early", "first"))),
		"POST /api/issues/{id}/tags":         fake.JSON(http.StatusOK, tagOf("10-1", "Early", "first")),
		"DELETE /api/issues/{id}/tags/{tag}": fake.JSON(http.StatusOK, ""),
	})
}

func TestTheStarOfTheUserIsNoTagOfAnIssue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		write func(c *youtrack.Client, t *testing.T) error
		paths []string
	}{
		{
			name: "a new issue",
			write: func(c *youtrack.Client, t *testing.T) error {
				in := issueTags(youtrack.TagRef{Name: "Early"})
				_, err := c.Issues.Create(t.Context(), "DEV", &in, answeredWith("idReadable"))
				return err
			},
			paths: []string{projectPath, "/api/tags", profilePath},
		},
		{
			name: "a tagging",
			write: func(c *youtrack.Client, t *testing.T) error {
				_, err := c.Tags.Add(t.Context(), youtrack.IssueID("DEV-7"), "Early", nil)
				return err
			},
			paths: []string{"/api/issues/DEV-7", "/api/tags", profilePath},
		},
		{
			name: "a removal",
			write: func(c *youtrack.Client, t *testing.T) error {
				_, err := c.Tags.Remove(t.Context(), youtrack.IssueID("DEV-7"), "Early", nil)
				return err
			},
			paths: []string{"/api/issues/DEV-7", "/api/tags", profilePath},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := starTagging(t, fake.JSON(http.StatusOK, starProfile("10-1")))

			err := tc.write(client(t, server), t)

			assert.Equal(t, youtrack.CodeBadUsage, errorOf(t, err).Code)
			assert.Equal(t, tc.paths, server.Paths())
		})
	}
}

func ageCache(t *testing.T, root string, age time.Duration) {
	t.Helper()
	then := time.Now().Add(-age)
	require.NoError(t, filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		return os.Chtimes(path, then, then)
	}))
}

func tagEarly(t *testing.T, c *youtrack.Client) error {
	t.Helper()
	_, err := c.Tags.Add(t.Context(), youtrack.IssueID("DEV-7"), "Early", nil)
	return err
}

func TestTheStarOfTheUserIsReadOnceADay(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		age   time.Duration
		reads int
	}{
		{name: "a fresh cache", age: time.Hour, reads: 1},
		{name: "a cache past a day", age: 25 * time.Hour, reads: 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server, root := starTagging(t, fake.JSON(http.StatusOK, starProfile("10-9"))), t.TempDir()
			require.NoError(t, tagEarly(t, cached(t, server, root)))
			ageCache(t, root, tc.age)

			require.NoError(t, tagEarly(t, cached(t, server, root)))

			reads := 0
			for _, path := range server.Paths() {
				if path == profilePath {
					reads++
				}
			}
			assert.Equal(t, tc.reads, reads)
		})
	}
}

func TestACachedStarRefusesOnlyWhatTheProfileStillSays(t *testing.T) {
	t.Parallel()
	server, root := starTagging(t, answersInTurn(
		fake.JSON(http.StatusOK, starProfile("10-1")), fake.JSON(http.StatusOK, starProfile("10-9")))), t.TempDir()
	cache := cached(t, server, root)
	require.Equal(t, youtrack.CodeBadUsage, errorOf(t, tagEarly(t, cache)).Code)

	require.NoError(t, tagEarly(t, cached(t, server, root)))

	assert.Equal(t, []string{"/api/issues/DEV-7", "/api/tags", profilePath,
		"/api/issues/DEV-7", "/api/tags", profilePath, "/api/issues/DEV-7/tags"}, server.Paths())
}

func TestTheStarOfOneTokenIsNotTheStarOfAnother(t *testing.T) {
	t.Parallel()
	server, root := starTagging(t, answersInTurn(
		fake.JSON(http.StatusOK, starProfile("10-9")), fake.JSON(http.StatusOK, starProfile("10-1")))), t.TempDir()
	require.NoError(t, tagEarly(t, cached(t, server, root)))
	another, err := youtrack.NewClient(server.URL, fake.Token+"-of-another-user", youtrack.WithMetadataCache(root))
	require.NoError(t, err)

	err = tagEarly(t, another)

	assert.Equal(t, youtrack.CodeBadUsage, errorOf(t, err).Code)
}

func TestCreateIssueRefusesATagNamedTwiceBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.Unexpected(t))

	_, err := issueCreate(t, server, issueTags(youtrack.TagRef{Name: "Early", OwnedBy: "first"},
		youtrack.TagRef{Name: "Early", OwnedBy: "FIRST"}), "idReadable")

	assert.Equal(t, youtrack.CodeBadUsage, errorOf(t, err).Code)
	assert.Empty(t, server.Paths())
}
