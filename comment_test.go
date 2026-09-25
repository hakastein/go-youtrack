package youtrack_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/hakastein/youtrack"
	"github.com/hakastein/youtrack/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type commentAnswers struct {
	read    string
	written string
	listed  string
}

func commentServer(t *testing.T, answers commentAnswers) *fake.Server {
	t.Helper()
	routes := http.NewServeMux()
	routes.HandleFunc("GET /api/{owners}/{owner}/comments", fake.JSON(http.StatusOK, answers.listed))
	routes.HandleFunc("GET /api/{owners}/{owner}/comments/{comment}", fake.JSON(http.StatusOK, answers.read))
	routes.HandleFunc("POST /", fake.JSON(http.StatusOK, answers.written))
	routes.HandleFunc("DELETE /", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	return fake.Serve(t, routes.ServeHTTP)
}

func commentWritten(t *testing.T, id, text string) string {
	t.Helper()
	written, err := json.Marshal(map[string]any{"$type": "IssueComment", "id": id, "text": text})
	require.NoError(t, err)
	return string(written)
}

const commentStanding = `{"$type":"IssueComment","deleted":false}`

const commentKeptByteForByte = "  First\r\nSecond\rThird   \n---\n~~~\n\u0085\xe2\x80\xa8\xef\xbb\xbf\U0001F600\n  "

type commentWrite func(ctx context.Context, comments *youtrack.CommentsService, text string) (*youtrack.Node, error)

func commentCreatedOnAnIssue(ctx context.Context, comments *youtrack.CommentsService, text string) (*youtrack.Node, error) {
	return comments.Create(ctx, "DEV-7", text, &youtrack.WriteOptions{Fields: "id"})
}

func commentUpdatedOnAnIssue(ctx context.Context, comments *youtrack.CommentsService, text string) (*youtrack.Node, error) {
	return comments.Update(ctx, "DEV-7", "7-12", text, &youtrack.WriteOptions{Fields: "id"})
}

func TestCommentsRefuseAnOwnerThatIsNoReadableID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		call func(ctx context.Context, comments *youtrack.CommentsService) error
	}{
		{
			name: "a list by an internal id",
			call: func(ctx context.Context, comments *youtrack.CommentsService) error {
				_, err := comments.List(ctx, "3-19", nil)
				return err
			},
		},
		{
			name: "a list by two dots",
			call: func(ctx context.Context, comments *youtrack.CommentsService) error {
				_, err := comments.List(ctx, "..", nil)
				return err
			},
		},
		{
			name: "a creation",
			call: func(ctx context.Context, comments *youtrack.CommentsService) error {
				_, err := comments.Create(ctx, "3-19", "Text", nil)
				return err
			},
		},
		{
			name: "a rewrite",
			call: func(ctx context.Context, comments *youtrack.CommentsService) error {
				_, err := comments.Update(ctx, "3-19", "7-12", "Text", nil)
				return err
			},
		},
		{
			name: "a deletion",
			call: func(ctx context.Context, comments *youtrack.CommentsService) error {
				_, err := comments.Delete(ctx, "3-19", "7-12")
				return err
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.call(t.Context(), client(t, fake.ServeNothing(t)).Comments)
			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

func TestCommentWritesRefuseATextTheyWillNotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		write commentWrite
		text  string
	}{
		{name: "an empty text of a new comment", write: commentCreatedOnAnIssue, text: ""},
		{name: "a new comment that is no UTF-8", write: commentCreatedOnAnIssue, text: "First\xffSecond"},
		{name: "an empty text of a comment rewritten", write: commentUpdatedOnAnIssue, text: ""},
		{name: "a comment rewritten with no UTF-8", write: commentUpdatedOnAnIssue, text: "First\xffSecond"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := tc.write(t.Context(), client(t, fake.ServeNothing(t)).Comments, tc.text)
			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

func TestCommentWritesRefuseACommentIDThatIsNoInternalID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   string
	}{
		{name: "two dots", id: ".."},
		{name: "a number alone", id: "7"},
		{name: "no number before the dash", id: "-1"},
		{name: "no number after the dash", id: "7-"},
		{name: "a path after the id", id: "7-1/.."},
		{name: "the readable id of an issue", id: "DEV-1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			comments := client(t, fake.ServeNothing(t)).Comments

			_, rewritten := comments.Update(t.Context(), "DEV-1", tc.id, "Text", nil)
			_, deleted := comments.Delete(t.Context(), "DEV-1", tc.id)

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, rewritten))
			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, deleted))
		})
	}
}

func TestCommentsOfAShowRefuseANegativeCount(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		show func(ctx context.Context, c *youtrack.Client) error
	}{
		{
			name: "of an issue",
			show: func(ctx context.Context, c *youtrack.Client) error {
				_, err := c.Issues.Show(ctx, "DEV-1", &youtrack.ShowIssueOptions{Comments: youtrack.LastComments(-1)})
				return err
			},
		},
		{
			name: "of an article",
			show: func(ctx context.Context, c *youtrack.Client) error {
				_, err := c.Articles.Show(ctx, "DEV-A-1", &youtrack.ShowArticleOptions{Comments: youtrack.LastComments(-1)})
				return err
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.show(t.Context(), client(t, fake.ServeNothing(t)))

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

func TestCommentCallsAddressTheCommentsOfTheOwnerTheyNamed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		call   func(ctx context.Context, comments *youtrack.CommentsService) error
		routes []string
	}{
		{
			name: "a creation on an issue",
			call: func(ctx context.Context, comments *youtrack.CommentsService) error {
				_, err := comments.Create(ctx, "DEV-7", "Text", &youtrack.WriteOptions{Fields: "id"})
				return err
			},
			routes: []string{"POST /api/issues/DEV-7/comments"},
		},
		{
			name: "a creation on an article",
			call: func(ctx context.Context, comments *youtrack.CommentsService) error {
				_, err := comments.Create(ctx, "DEV-A-3", "Text", &youtrack.WriteOptions{Fields: "id"})
				return err
			},
			routes: []string{"POST /api/articles/DEV-A-3/comments"},
		},
		{
			name: "a rewrite on an issue, which reads the comment first",
			call: func(ctx context.Context, comments *youtrack.CommentsService) error {
				_, err := comments.Update(ctx, "DEV-7", "7-12", "Text", &youtrack.WriteOptions{Fields: "id"})
				return err
			},
			routes: []string{"GET /api/issues/DEV-7/comments/7-12", "POST /api/issues/DEV-7/comments/7-12"},
		},
		{
			name: "a rewrite on an article",
			call: func(ctx context.Context, comments *youtrack.CommentsService) error {
				_, err := comments.Update(ctx, "DEV-A-3", "8-5", "Text", &youtrack.WriteOptions{Fields: "id"})
				return err
			},
			routes: []string{"POST /api/articles/DEV-A-3/comments/8-5"},
		},
		{
			name: "a removal from an issue",
			call: func(ctx context.Context, comments *youtrack.CommentsService) error {
				_, err := comments.Delete(ctx, "DEV-7", "7-12")
				return err
			},
			routes: []string{"DELETE /api/issues/DEV-7/comments/7-12"},
		},
		{
			name: "a removal from an article",
			call: func(ctx context.Context, comments *youtrack.CommentsService) error {
				_, err := comments.Delete(ctx, "DEV-A-3", "8-5")
				return err
			},
			routes: []string{"DELETE /api/articles/DEV-A-3/comments/8-5"},
		},
		{
			name: "a list of an issue",
			call: func(ctx context.Context, comments *youtrack.CommentsService) error {
				_, err := comments.List(ctx, "DEV-7", &youtrack.ListCommentsOptions{Fields: "id", Page: youtrack.Page{Limit: 1}})
				return err
			},
			routes: []string{"GET /api/issues/DEV-7/comments"},
		},
		{
			name: "a list of an article",
			call: func(ctx context.Context, comments *youtrack.CommentsService) error {
				_, err := comments.List(ctx, "DEV-A-3", &youtrack.ListCommentsOptions{Fields: "id", Page: youtrack.Page{Limit: 1}})
				return err
			},
			routes: []string{"GET /api/articles/DEV-A-3/comments"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := commentServer(t, commentAnswers{
				read:    commentStanding,
				written: `{"id":"7-12","text":"Text"}`,
				listed:  `[]`,
			})

			err := tc.call(t.Context(), client(t, server).Comments)

			require.NoError(t, err)
			assert.Equal(t, tc.routes, server.Routes())
		})
	}
}

func TestDeleteCommentSendsAnInternalIDAsTheSegmentUnderItsOwner(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   string
	}{
		{name: "two numbers", id: "7-1"},
		{name: "leading zeros in both numbers", id: "07-01"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, ``))

			node, err := client(t, server).Comments.Delete(t.Context(), "DEV-1", tc.id)

			require.NoError(t, err)
			assert.Equal(t, youtrack.NewMap(youtrack.Pair{Key: "id", Value: youtrack.NewString(tc.id)}), node)
			assert.Equal(t, []string{"/api/issues/DEV-1/comments/" + tc.id}, server.Paths())
		})
	}
}

func TestCommentWritesSendTheTextAsGiven(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		text  string
		write commentWrite
	}{
		{name: "one space", text: " ", write: commentCreatedOnAnIssue},
		{name: "one line feed", text: "\n", write: commentCreatedOnAnIssue},
		{name: "a vote", text: "+1", write: commentCreatedOnAnIssue},
		{name: "markup that reads as a tag", text: "[Tag] Title", write: commentCreatedOnAnIssue},
		{name: "a NUL", text: "First\x00Second", write: commentCreatedOnAnIssue},
		{name: "a new comment the server keeps byte for byte", text: commentKeptByteForByte, write: commentCreatedOnAnIssue},
		{name: "a comment rewritten byte for byte", text: commentKeptByteForByte, write: commentUpdatedOnAnIssue},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := commentServer(t, commentAnswers{read: commentStanding, written: commentWritten(t, "7-12", tc.text)})

			_, err := tc.write(t.Context(), client(t, server).Comments, tc.text)

			require.NoError(t, err)
			assert.Equal(t, map[string]any{"text": tc.text}, server.LastJSON(t))
		})
	}
}

func TestCommentWritesRefuseAnAnswerThatDisagreesWithTheWrite(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		write   commentWrite
		written string
		target  string
		actual  *youtrack.Node
	}{
		{
			name:    "a new comment the server stored in another letter case",
			write:   commentCreatedOnAnIssue,
			written: `{"$type":"IssueComment","id":"7-12","text":"text"}`,
			target:  "/api/issues/DEV-7/comments?fields=id,text",
			actual:  youtrack.NewString("text"),
		},
		{
			name:    "a new comment the server kept no text of",
			write:   commentCreatedOnAnIssue,
			written: `{"$type":"IssueComment","id":"7-12","text":null}`,
			target:  "/api/issues/DEV-7/comments?fields=id,text",
			actual:  youtrack.NewNull(),
		},
		{
			name:    "a comment taken back between the read and the rewrite",
			write:   commentUpdatedOnAnIssue,
			written: `{"$type":"IssueComment","id":"7-12","text":null}`,
			target:  "/api/issues/DEV-7/comments/7-12?fields=id,text",
			actual:  youtrack.NewNull(),
		},
		{
			name:    "a rewrite answered under another id, named by the id it was addressed by",
			write:   commentUpdatedOnAnIssue,
			written: `{"$type":"IssueComment","id":"7-99","text":"Other"}`,
			target:  "/api/issues/DEV-7/comments/7-12?fields=id,text",
			actual:  youtrack.NewString("Other"),
		},
		{
			name: "a rewrite answered with no id, which the expression does not ask for",
			write: func(ctx context.Context, comments *youtrack.CommentsService, text string) (*youtrack.Node, error) {
				return comments.Update(ctx, "DEV-7", "7-12", text, &youtrack.WriteOptions{Fields: "author(login)"})
			},
			written: `{"$type":"IssueComment","author":{"$type":"User","login":"author"},"text":"Other"}`,
			target:  "/api/issues/DEV-7/comments/7-12?fields=author(login),text",
			actual:  youtrack.NewString("Other"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := commentServer(t, commentAnswers{read: commentStanding, written: tc.written})

			_, err := tc.write(t.Context(), client(t, server).Comments, "Text")

			want := youtrack.Error{Code: youtrack.CodeUpstreamInvalid, AfterWrite: true, Details: []youtrack.Pair{
				requestTo(http.MethodPost, server, tc.target),
				{Key: "comment", Value: youtrack.NewString("7-12")},
				{Key: "mismatch", Value: youtrack.NewList(mismatch("text", youtrack.NewString("Text"), tc.actual))},
			}}
			assert.Equal(t, want, errorOf(t, err))
		})
	}
}

func TestUpdateCommentWritesNothingIntoACommentTakenBack(t *testing.T) {
	t.Parallel()
	server := commentServer(t, commentAnswers{read: `{"$type":"IssueComment","deleted":true}`})

	_, err := commentUpdatedOnAnIssue(t.Context(), client(t, server).Comments, "Text")

	assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage, Details: []youtrack.Pair{
		requestTo(http.MethodGet, server, "/api/issues/DEV-7/comments/7-12?fields=deleted"),
		{Key: "comment", Value: youtrack.NewString("7-12")},
	}}, errorOf(t, err))
	assert.Equal(t, []string{"GET /api/issues/DEV-7/comments/7-12"}, server.Routes())
}

func TestUpdateCommentWritesNothingWhereTheReadDoesNotSayWhetherItWasTakenBack(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		read string
	}{
		{name: "a null", read: `{"$type":"IssueComment","deleted":null}`},
		{name: "a word", read: `{"$type":"IssueComment","deleted":"true"}`},
		{name: "a number", read: `{"$type":"IssueComment","deleted":1}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := commentServer(t, commentAnswers{read: tc.read})

			_, err := commentUpdatedOnAnIssue(t.Context(), client(t, server).Comments, "Text")

			read := requestTo(http.MethodGet, server, "/api/issues/DEV-7/comments/7-12?fields=deleted")
			assert.Equal(t, unreadable(read, tc.read), errorOf(t, err))
			assert.Equal(t, []string{"GET /api/issues/DEV-7/comments/7-12"}, server.Routes())
		})
	}
}

func TestCommentWritesCheckMoreThanTheyPrint(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		call   func(ctx context.Context, comments *youtrack.CommentsService) (*youtrack.Node, error)
		fields string
	}{
		{
			name: "a creation",
			call: func(ctx context.Context, comments *youtrack.CommentsService) (*youtrack.Node, error) {
				return comments.Create(ctx, "DEV-7", "Text", &youtrack.WriteOptions{Fields: "author(login)"})
			},
			fields: "author(login),id,text",
		},
		{
			name: "a rewrite",
			call: func(ctx context.Context, comments *youtrack.CommentsService) (*youtrack.Node, error) {
				return comments.Update(ctx, "DEV-7", "7-12", "Text", &youtrack.WriteOptions{Fields: "author(login)"})
			},
			fields: "author(login),text",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := commentServer(t, commentAnswers{
				read:    commentStanding,
				written: `{"$type":"IssueComment","id":"7-12","author":{"$type":"User","login":"author"},"text":"Text"}`,
			})

			node, err := tc.call(t.Context(), client(t, server).Comments)

			require.NoError(t, err)
			author := youtrack.NewMap(youtrack.Pair{Key: "login", Value: youtrack.NewString("author")})
			assert.Equal(t, youtrack.NewMap(youtrack.Pair{Key: "author", Value: author}), node)
			assert.Equal(t, tc.fields, server.Last(t).URL.Query().Get("fields"))
		})
	}
}

func TestCommentCallsCheckTheAnswerAgainstTheSchemaOfTheOwner(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		call       func(ctx context.Context, comments *youtrack.CommentsService) error
		method     string
		target     string
		fields     string
		unknown    string
		nearest    *youtrack.Node
		afterWrite bool
	}{
		{
			name: "a flag only a comment of an issue carries, asked of a new comment of an article",
			call: func(ctx context.Context, comments *youtrack.CommentsService) error {
				_, err := comments.Create(ctx, "DEV-A-3", "Text", &youtrack.WriteOptions{Fields: "id,deleted"})
				return err
			},
			method:  http.MethodPost,
			target:  "/api/articles/DEV-A-3/comments?fields=id,deleted,text",
			fields:  "id,deleted,text",
			unknown: "deleted",
			nearest: texts("$type", "article", "attachments", "author", "created", "id", "pinned", "reactions",
				"text", "updated", "visibility"),
			afterWrite: true,
		},
		{
			name: "the owner of a comment of an article, asked of a new comment of an issue",
			call: func(ctx context.Context, comments *youtrack.CommentsService) error {
				_, err := comments.Create(ctx, "DEV-7", "Text", &youtrack.WriteOptions{Fields: "id,article(idReadable)"})
				return err
			},
			method:  http.MethodPost,
			target:  "/api/issues/DEV-7/comments?fields=id,article(idReadable),text",
			fields:  "id,article(idReadable),text",
			unknown: "article",
			nearest: texts("$type", "attachments", "author", "created", "deleted", "id", "issue", "pinned",
				"reactions", "text", "textPreview", "updated", "visibility"),
			afterWrite: true,
		},
		{
			name: "a flag only a comment of an issue carries, asked of the comments of an article",
			call: func(ctx context.Context, comments *youtrack.CommentsService) error {
				_, err := comments.List(ctx, "DEV-A-3", &youtrack.ListCommentsOptions{Fields: "id,deleted", Page: youtrack.Page{Limit: 1}})
				return err
			},
			method:  http.MethodGet,
			target:  "/api/articles/DEV-A-3/comments?fields=id,deleted&$top=1",
			fields:  "id,deleted",
			unknown: "deleted",
			nearest: texts("$type", "article", "attachments", "author", "created", "id", "pinned", "reactions",
				"text", "updated", "visibility"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := commentServer(t, commentAnswers{
				written: `{"id":"7-12","text":"Text"}`,
				listed:  `[{"id":"8-5"}]`,
			})

			err := tc.call(t.Context(), client(t, server).Comments)

			unknown := youtrack.NewMap(
				youtrack.Pair{Key: "field", Value: youtrack.NewString(tc.unknown)},
				youtrack.Pair{Key: "nearest", Value: tc.nearest})
			want := youtrack.Error{Code: youtrack.CodeUnknownName, AfterWrite: tc.afterWrite, Details: []youtrack.Pair{
				requestTo(tc.method, server, tc.target),
				{Key: "fields", Value: youtrack.NewString(tc.fields)},
				{Key: "unknown", Value: youtrack.NewList(unknown)},
			}}
			assert.Equal(t, want, errorOf(t, err))
		})
	}
}

func TestListCommentsAsksForDeletedOnlyOfAnIssue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		owner  string
		fields string
		sent   string
	}{
		{name: "an issue", owner: "DEV-7", sent: "id,author(login),created,text,deleted"},
		{name: "an article", owner: "DEV-A-3", sent: "id,author(login),created,text"},
		{name: "an article, with an addition", owner: "DEV-A-3", fields: "+updated",
			sent: "id,author(login),created,text,updated"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := commentServer(t, commentAnswers{listed: `[]`})

			_, err := client(t, server).Comments.List(t.Context(), tc.owner,
				&youtrack.ListCommentsOptions{Fields: tc.fields, Page: youtrack.Page{Limit: 1}})

			require.NoError(t, err)
			assert.Equal(t, []string{tc.sent}, server.Fields())
		})
	}
}

func TestListCommentsCountsTheCommentsOfAnArticleWhereTheyFillThePage(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("$top") == "-1" {
			fake.JSON(http.StatusOK, `[{"id":"8-1"},{"id":"8-2"},{"id":"8-3"}]`)(w, r)
			return
		}
		fake.JSON(http.StatusOK, `[{"$type":"ArticleComment","id":"8-1","text":"Text"}]`)(w, r)
	})

	node, err := client(t, server).Comments.List(t.Context(), "DEV-A-3",
		&youtrack.ListCommentsOptions{Fields: "id,text", Page: youtrack.Page{Limit: 1}})

	require.NoError(t, err)
	comment := youtrack.NewMap(
		youtrack.Pair{Key: "id", Value: youtrack.NewString("8-1")},
		youtrack.Pair{Key: "text", Value: youtrack.NewString("Text")})
	assert.Equal(t, youtrack.NewMap(
		youtrack.Pair{Key: "total", Value: number(3)},
		youtrack.Pair{Key: "returned", Value: number(1)},
		youtrack.Pair{Key: "truncated", Value: youtrack.NewBool(true)},
		youtrack.Pair{Key: "comments", Value: youtrack.NewList(comment)}), node)
	assert.Equal(t, []string{"/api/articles/DEV-A-3/comments", "/api/articles/DEV-A-3/comments"}, server.Paths())
	assert.Equal(t, []url.Values{
		{"fields": {"id,text"}, "$top": {"1"}},
		{"fields": {"id"}, "$top": {"-1"}},
	}, server.Queries())
}
