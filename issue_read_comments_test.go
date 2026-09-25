package youtrack_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/youtrack"
	"github.com/hakastein/youtrack/fake"
)

const (
	issueCommentFields = "comments(id,author(login),created,text,deleted)"
	issueCommentsFrom  = 1788134400000
)

func issueComment(id string, second int, text string) string {
	return `{"$type":"IssueComment","id":` + strconv.Quote(id) + `,"author":{"$type":"User","login":"author"},` +
		`"created":` + strconv.Itoa(issueCommentsFrom+second*1000) + `,"text":` + strconv.Quote(text) + `,"deleted":false}`
}

func issueDeletedComment(id string, second int) string {
	return `{"$type":"IssueComment","id":` + strconv.Quote(id) + `,"author":{"$type":"User","login":"author"},` +
		`"created":` + strconv.Itoa(issueCommentsFrom+second*1000) + `,"text":null,"deleted":true}`
}

func issuePrintedComment(id, created, text string) *youtrack.Node {
	return youtrack.NewMap(
		youtrack.Pair{Key: "id", Value: youtrack.NewString(id)},
		youtrack.Pair{Key: "author", Value: youtrack.NewMap(youtrack.Pair{Key: "login", Value: youtrack.NewString("author")})},
		youtrack.Pair{Key: "created", Value: youtrack.NewString(created)},
		youtrack.Pair{Key: "text", Value: youtrack.NewText(text)},
	)
}

func TestShowIssuePrintsTheCommentsOldestFirst(t *testing.T) {
	t.Parallel()
	early := issuePrintedComment("7-2", "2026-08-31T00:00:00Z", "Early")
	middle := issuePrintedComment("7-3", "2026-08-31T00:00:01Z", "Middle")
	late := issuePrintedComment("7-1", "2026-08-31T00:00:03Z", "Late")
	body := `{"$type":"Issue","idReadable":"DEV-1","comments":[` +
		issueComment("7-3", 1, "Middle") + `,` +
		issueComment("7-1", 3, "Late") + `,` +
		issueDeletedComment("7-4", 2) + `,` +
		issueComment("7-2", 0, "Early") + `]}`
	tests := []struct {
		name     string
		comments youtrack.Comments
		printed  []*youtrack.Node
	}{
		{name: "every one of them", comments: youtrack.AllComments(), printed: []*youtrack.Node{early, middle, late}},
		{name: "the last two", comments: youtrack.LastComments(2), printed: []*youtrack.Node{middle, late}},
		{name: "more than the issue has", comments: youtrack.LastComments(10), printed: []*youtrack.Node{early, middle, late}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, body))

			node, err := issueShown(t, server, "idReadable", tc.comments)

			require.NoError(t, err)
			assert.Equal(t, youtrack.NewMap(
				youtrack.Pair{Key: "idReadable", Value: youtrack.NewString("DEV-1")},
				youtrack.Pair{Key: "comments", Value: youtrack.NewList(tc.printed...)},
			), node)
		})
	}
}

func TestShowIssueAsksForCommentsOnlyWhereItPrintsThem(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		comments youtrack.Comments
		sent     string
		printed  *youtrack.Node
	}{
		{
			name:     "every comment",
			comments: youtrack.AllComments(),
			sent:     "idReadable," + issueCommentFields,
			printed: youtrack.NewMap(
				youtrack.Pair{Key: "idReadable", Value: youtrack.NewString("DEV-1")},
				youtrack.Pair{Key: "comments", Value: youtrack.NewList()},
			),
		},
		{
			name:     "none of them",
			comments: youtrack.Comments{},
			sent:     "idReadable",
			printed:  youtrack.NewMap(youtrack.Pair{Key: "idReadable", Value: youtrack.NewString("DEV-1")}),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"Issue","idReadable":"DEV-1","comments":[]}`))

			node, err := issueShown(t, server, "idReadable", tc.comments)

			require.NoError(t, err)
			assert.Equal(t, tc.printed, node)
			assert.Equal(t, []string{tc.sent}, server.Fields())
		})
	}
}

func TestShowIssueRefusesCommentsOfAnotherShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		comments youtrack.Comments
		received string
	}{
		{name: "no array at all", comments: youtrack.AllComments(), received: `null`},
		{name: "a null in place of a comment", comments: youtrack.AllComments(), received: `[null]`},
		{
			name:     "deleted as a word",
			comments: youtrack.AllComments(),
			received: `[{"$type":"IssueComment","id":"7-1","author":{"$type":"User","login":"author"},"created":0,` +
				`"text":"","deleted":"true"}]`,
		},
		{
			name:     "the moment written as a string",
			comments: youtrack.AllComments(),
			received: `[{"$type":"IssueComment","id":"7-1","author":{"$type":"User","login":"author"},"created":"0",` +
				`"text":"","deleted":false}]`,
		},
		{
			name:     "the moment written as a string on a comment the count leaves out",
			comments: youtrack.LastComments(1),
			received: `[{"$type":"IssueComment","id":"7-1","author":{"$type":"User","login":"author"},"created":"0",` +
				`"text":"","deleted":false},` + issueComment("7-2", 1, "Late") + `]`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `{"$type":"Issue","idReadable":"DEV-1","comments":` + tc.received + `}`
			server := fake.Serve(t, fake.JSON(http.StatusOK, body))

			_, err := issueShown(t, server, "idReadable", tc.comments)

			target := issuePath + "?fields=idReadable," + issueCommentFields
			assert.Equal(t, unreadable(requestTo(http.MethodGet, server, target), body), errorOf(t, err))
		})
	}
}
