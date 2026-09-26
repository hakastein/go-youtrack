package youtrack_test

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/hakastein/youtrack"
	"github.com/hakastein/youtrack/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type articleStep struct{ id, readable string }

var (
	articleWritten = articleStep{id: "177-7", readable: "DEV-A-7"}
	articleParent  = articleStep{id: "177-1", readable: "DEV-A-1"}
	articleChild   = articleStep{id: "177-9", readable: "DEV-A-9"}
	articleBetween = articleStep{id: "177-8", readable: "DEV-A-8"}
	articleOfDEMO  = articleStep{id: "177-50", readable: "DEMO-A-1"}
	articleDeepest = articleStep{id: "177-29", readable: "DEV-A-29"}
)

const articleOtherParent = "DEV-A-2"

const (
	articleRootAbove    = "null"
	articleNothingAbove = ""
)

func articleLine(project, top string, line ...articleStep) string {
	nested := top
	for at := len(line) - 1; at >= 0; at-- {
		object := `{"$type":"Article","id":` + strconv.Quote(line[at].id) + `,"idReadable":` + strconv.Quote(line[at].readable)
		if at == 0 {
			object += `,"project":{"$type":"Project","shortName":` + strconv.Quote(project) + `}`
		}
		if nested != articleNothingAbove {
			object += `,"parentArticle":` + nested
		}
		nested = object + `}`
	}
	return nested
}

func articleLineAbove(top articleStep, above int) []articleStep {
	line := []articleStep{top}
	for at := range above {
		line = append(line, articleStep{id: "177-" + strconv.Itoa(30+at), readable: "DEV-A-" + strconv.Itoa(30+at)})
	}
	return line
}

func articleChildAsDeepAsAsked(w http.ResponseWriter, r *http.Request) {
	asked := strings.Count(r.URL.Query().Get("fields"), "parentArticle(")
	line := append(articleLineAbove(articleChild, asked-1), articleDeepest)
	fake.JSON(http.StatusOK, articleLine("DEV", articleNothingAbove, line...))(w, r)
}

func articleRead(id, readable, project string) string {
	return `{"$type":"Article","id":` + id + `,"idReadable":` + readable + `,"project":` + project + `}`
}

func articleFiled(t *testing.T, changes map[string]any) string {
	t.Helper()
	filed := map[string]any{
		"$type":         "Article",
		"idReadable":    articleWritten.readable,
		"summary":       "Title",
		"content":       nil,
		"project":       map[string]any{"$type": "Project", "shortName": "DEV"},
		"parentArticle": nil,
	}
	maps.Copy(filed, changes)
	encoded, err := json.Marshal(filed)
	require.NoError(t, err)
	return string(encoded)
}

func articleFiledUnder(readable string) map[string]any {
	return map[string]any{"$type": "Article", "idReadable": readable}
}

func articleServer(t *testing.T, reads map[string]string, write http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			write(w, r)
			return
		}
		read, answered := reads[strings.TrimPrefix(r.URL.Path, "/api/articles/")]
		if !assert.True(t, answered, "%s was read and no answer was given for it", r.URL.Path) {
			http.NotFound(w, r)
			return
		}
		fake.JSON(http.StatusOK, read)(w, r)
	})
}

func articleNamed(readable string) *youtrack.Node {
	return youtrack.NewMap(youtrack.Pair{Key: "idReadable", Value: youtrack.NewString(readable)})
}

func TestArticleCallsRefuseTheCommentsInTheExpression(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		call func(ctx context.Context, articles *youtrack.ArticlesService) error
	}{
		{
			name: "an update, added to the default",
			call: func(ctx context.Context, articles *youtrack.ArticlesService) error {
				_, err := articles.Update(ctx, "DEV-A-7", &youtrack.ArticleUpdate{Summary: new("Title")},
					answeredWith("+comments(text)"))
				return err
			},
		},
		{
			name: "an update, in place of the default",
			call: func(ctx context.Context, articles *youtrack.ArticlesService) error {
				_, err := articles.Update(ctx, "DEV-A-7", &youtrack.ArticleUpdate{Summary: new("Title")},
					answeredWith("idReadable,comments"))
				return err
			},
		},
		{
			name: "a creation",
			call: func(ctx context.Context, articles *youtrack.ArticlesService) error {
				_, err := articles.Create(ctx, "DEV", &youtrack.ArticleInput{Summary: "Title"},
					answeredWith("+comments(text)"))
				return err
			},
		},
		{
			name: "a show, in place of the default",
			call: func(ctx context.Context, articles *youtrack.ArticlesService) error {
				_, err := articles.Show(ctx, "DEV-A-7",
					&youtrack.ShowArticleOptions{Fields: "comments(text)", Comments: youtrack.AllComments()})
				return err
			},
		},
		{
			name: "a show, under a child article",
			call: func(ctx context.Context, articles *youtrack.ArticlesService) error {
				_, err := articles.Show(ctx, "DEV-A-7",
					&youtrack.ShowArticleOptions{Fields: "childArticles(comments(id))", Comments: youtrack.AllComments()})
				return err
			},
		},
		{
			name: "a search",
			call: func(ctx context.Context, articles *youtrack.ArticlesService) error {
				_, err := articles.List(ctx, "", &youtrack.ListArticlesOptions{Fields: "+comments"})
				return err
			},
		},
		{
			name: "a list of children",
			call: func(ctx context.Context, articles *youtrack.ArticlesService) error {
				_, err := articles.Children(ctx, "DEV-A-7", &youtrack.ListArticlesOptions{Fields: "+comments"})
				return err
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.call(t.Context(), client(t, fake.ServeNothing(t)).Articles)

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

func TestShowArticleSendsEveryFormOfAnArticleAsWritten(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   string
	}{
		{name: "a code in upper case", id: "DEV-A-1"},
		{name: "a code in lower case", id: "dev-A-1"},
		{name: "a number with a leading zero", id: "DEV-A-01"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"Article","id":"3-1"}`))

			_, err := client(t, server).Articles.Show(t.Context(), tc.id, &youtrack.ShowArticleOptions{Fields: "id"})

			require.NoError(t, err)
			assert.Equal(t, []string{"/api/articles/" + tc.id}, server.Paths())
		})
	}
}

func TestShowArticleRefusesAnIDOfAnyOtherForm(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   string
	}{
		{name: "an issue", id: "DEV-1"},
		{name: "the marker in lower case", id: "DEV-a-1"},
		{name: "a dash in place of the marker", id: "DEV--1"},
		{name: "the marker twice", id: "DEV-A-A-1"},
		{name: "no number after the marker", id: "DEV-A-"},
		{name: "a sign before the number", id: "DEV-A-+1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := client(t, fake.ServeNothing(t)).Articles.Show(t.Context(), tc.id, &youtrack.ShowArticleOptions{Fields: "id"})

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

func TestShowArticleResolvesTheLinkOfAnAttachmentFromTheAddressOfTheClient(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK,
		`{"$type":"Article","attachments":[{"$type":"ArticleAttachment","thumbnailURL":"/api/files/12-3?sign=t"}]}`))

	node, err := client(t, server).Articles.Show(t.Context(), "DEV-A-1",
		&youtrack.ShowArticleOptions{Fields: "attachments(thumbnailURL)"})

	require.NoError(t, err)
	assert.Equal(t, youtrack.NewMap(youtrack.Pair{Key: "attachments", Value: youtrack.NewList(youtrack.NewMap(
		youtrack.Pair{Key: "thumbnailURL", Value: youtrack.NewString(server.Origin + "/api/files/12-3?sign=t")}))}), node)
}

func TestShowArticleRefusesTheContentOfAFileInTheFields(t *testing.T) {
	t.Parallel()

	_, err := client(t, fake.ServeNothing(t)).Articles.Show(t.Context(), "DEV-A-1",
		&youtrack.ShowArticleOptions{Fields: "attachments(base64Content)"})

	assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
}

func TestCreateArticleRefusesBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		project string
		in      youtrack.ArticleInput
	}{
		{name: "a project code of another form", project: "1DEV", in: youtrack.ArticleInput{Summary: "Title"}},
		{name: "an empty title", project: "DEV", in: youtrack.ArticleInput{}},
		{name: "a line feed in the title", project: "DEV", in: youtrack.ArticleInput{Summary: "First\nSecond"}},
		{name: "a carriage return in the title", project: "DEV", in: youtrack.ArticleInput{Summary: "First\rSecond"}},
		{name: "a title that is no UTF-8", project: "DEV", in: youtrack.ArticleInput{Summary: "First\xffSecond"}},
		{name: "content that is no UTF-8", project: "DEV", in: youtrack.ArticleInput{Summary: "Title", Content: "First\xffSecond"}},
		{name: "a parent that is an issue", project: "DEV", in: youtrack.ArticleInput{Summary: "Title", Parent: "DEV-1"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := client(t, fake.ServeNothing(t)).Articles.Create(t.Context(), tc.project, &tc.in, nil)

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

func TestUpdateArticleRefusesBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   string
		in   *youtrack.ArticleUpdate
	}{
		{name: "no update at all", id: "DEV-A-7"},
		{name: "nothing to write", id: "DEV-A-7", in: &youtrack.ArticleUpdate{}},
		{name: "an id of an issue", id: "DEV-7", in: &youtrack.ArticleUpdate{Summary: new("Title")}},
		{name: "content written and taken away", id: "DEV-A-7",
			in: &youtrack.ArticleUpdate{Content: new("Text"), ClearContent: true}},
		{name: "a parent written and taken away", id: "DEV-A-7",
			in: &youtrack.ArticleUpdate{Parent: new("DEV-A-1"), ClearParent: true}},
		{name: "an empty parent", id: "DEV-A-7", in: &youtrack.ArticleUpdate{Parent: new("")}},
		{name: "a parent that is an issue", id: "DEV-A-7", in: &youtrack.ArticleUpdate{Parent: new("DEV-1")}},
		{name: "an empty title", id: "DEV-A-7", in: &youtrack.ArticleUpdate{Summary: new("")}},
		{name: "a line feed in the title", id: "DEV-A-7", in: &youtrack.ArticleUpdate{Summary: new("First\nSecond")}},
		{name: "a carriage return in the title", id: "DEV-A-7", in: &youtrack.ArticleUpdate{Summary: new("First\rSecond")}},
		{name: "a title that is no UTF-8", id: "DEV-A-7", in: &youtrack.ArticleUpdate{Summary: new("First\xffSecond")}},
		{name: "empty content", id: "DEV-A-7", in: &youtrack.ArticleUpdate{Content: new("")}},
		{name: "content that is no UTF-8", id: "DEV-A-7", in: &youtrack.ArticleUpdate{Content: new("First\xffSecond")}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := client(t, fake.ServeNothing(t)).Articles.Update(t.Context(), tc.id, tc.in, nil)

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

func TestCreateArticleSendsWhatItWasGiven(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		in    youtrack.ArticleInput
		filed map[string]any
		sent  map[string]any
	}{
		{
			name:  "a title with a tab inside and spaces around",
			in:    youtrack.ArticleInput{Summary: "  First\tSecond  "},
			filed: map[string]any{"summary": "  First\tSecond  "},
			sent:  map[string]any{"project": map[string]any{"shortName": "DEV"}, "summary": "  First\tSecond  "},
		},
		{
			name:  "a title with a NEL, a line separator and a paragraph separator",
			in:    youtrack.ArticleInput{Summary: "First\u0085Second\xe2\x80\xa8Third\xe2\x80\xa9Fourth"},
			filed: map[string]any{"summary": "First\u0085Second\xe2\x80\xa8Third\xe2\x80\xa9Fourth"},
			sent: map[string]any{"project": map[string]any{"shortName": "DEV"},
				"summary": "First\u0085Second\xe2\x80\xa8Third\xe2\x80\xa9Fourth"},
		},
		{
			name:  "a title of spaces alone",
			in:    youtrack.ArticleInput{Summary: "   "},
			filed: map[string]any{"summary": "   "},
			sent:  map[string]any{"project": map[string]any{"shortName": "DEV"}, "summary": "   "},
		},
		{
			name:  "content the server keeps byte for byte",
			in:    youtrack.ArticleInput{Summary: "Title", Content: keptByteForByte},
			filed: map[string]any{"content": keptByteForByte},
			sent: map[string]any{"project": map[string]any{"shortName": "DEV"}, "summary": "Title",
				"content": keptByteForByte},
		},
		{
			name:  "a parent, by the internal id the read gave",
			in:    youtrack.ArticleInput{Summary: "Title", Parent: "dev-A-1"},
			filed: map[string]any{"parentArticle": articleFiledUnder(articleParent.readable)},
			sent: map[string]any{"project": map[string]any{"shortName": "DEV"}, "summary": "Title",
				"parentArticle": map[string]any{"id": articleParent.id}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := articleServer(t, map[string]string{"dev-A-1": articleLine("DEV", articleNothingAbove, articleParent)},
				fake.JSON(http.StatusOK, articleFiled(t, tc.filed)))

			_, err := client(t, server).Articles.Create(t.Context(), "DEV", &tc.in, answeredWith("idReadable"))

			require.NoError(t, err)
			assert.Equal(t, tc.sent, server.LastJSON(t))
		})
	}
}

func TestUpdateArticleSendsWhatItWasGiven(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		in    youtrack.ArticleUpdate
		filed map[string]any
		sent  map[string]any
	}{
		{
			name:  "a title alone",
			in:    youtrack.ArticleUpdate{Summary: new("  First\tSecond\xe2\x80\xa8Third")},
			filed: map[string]any{"summary": "  First\tSecond\xe2\x80\xa8Third"},
			sent:  map[string]any{"summary": "  First\tSecond\xe2\x80\xa8Third"},
		},
		{
			name:  "content alone",
			in:    youtrack.ArticleUpdate{Content: new(keptByteForByte)},
			filed: map[string]any{"content": keptByteForByte},
			sent:  map[string]any{"content": keptByteForByte},
		},
		{
			name:  "a title and content both",
			in:    youtrack.ArticleUpdate{Summary: new("Title"), Content: new("Text")},
			filed: map[string]any{"content": "Text"},
			sent:  map[string]any{"summary": "Title", "content": "Text"},
		},
		{
			name: "content taken away",
			in:   youtrack.ArticleUpdate{ClearContent: true},
			sent: map[string]any{"content": nil},
		},
		{
			name: "the parent taken away",
			in:   youtrack.ArticleUpdate{ClearParent: true},
			sent: map[string]any{"parentArticle": nil},
		},
		{
			name:  "a parent, by the internal id the read gave",
			in:    youtrack.ArticleUpdate{Parent: new("dev-A-1")},
			filed: map[string]any{"parentArticle": articleFiledUnder(articleParent.readable)},
			sent:  map[string]any{"parentArticle": map[string]any{"id": articleParent.id}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := articleServer(t, map[string]string{
				"DEV-A-7": articleLine("DEV", articleNothingAbove, articleWritten),
				"dev-A-1": articleLine("DEV", articleRootAbove, articleParent),
			}, fake.JSON(http.StatusOK, articleFiled(t, tc.filed)))

			_, err := client(t, server).Articles.Update(t.Context(), "DEV-A-7", &tc.in, answeredWith("idReadable"))

			require.NoError(t, err)
			assert.Equal(t, tc.sent, server.LastJSON(t))
		})
	}
}

func TestUpdateArticleWritesByTheReadableIDTheReadGave(t *testing.T) {
	t.Parallel()
	server := articleServer(t, map[string]string{"dev-A-7": articleLine("DEV", articleNothingAbove, articleWritten)},
		fake.JSON(http.StatusOK, articleFiled(t, nil)))

	_, err := client(t, server).Articles.Update(t.Context(), "dev-A-7", &youtrack.ArticleUpdate{Summary: new("Title")},
		answeredWith("idReadable"))

	require.NoError(t, err)
	assert.Equal(t, []string{"/api/articles/dev-A-7", "/api/articles/DEV-A-7"}, server.Paths())
}

func TestArticleWritesCheckMoreThanTheyPrint(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		write  func(ctx context.Context, articles *youtrack.ArticlesService) (*youtrack.Node, error)
		filed  map[string]any
		fields string
	}{
		{
			name: "a creation",
			write: func(ctx context.Context, articles *youtrack.ArticlesService) (*youtrack.Node, error) {
				return articles.Create(ctx, "DEV", &youtrack.ArticleInput{Summary: "Title"}, answeredWith("idReadable"))
			},
			fields: "idReadable,summary,content,project(shortName)",
		},
		{
			name: "a creation under a parent",
			write: func(ctx context.Context, articles *youtrack.ArticlesService) (*youtrack.Node, error) {
				return articles.Create(ctx, "DEV", &youtrack.ArticleInput{Summary: "Title", Parent: "DEV-A-1"}, answeredWith("idReadable"))
			},
			filed:  map[string]any{"parentArticle": articleFiledUnder(articleParent.readable)},
			fields: "idReadable,summary,content,project(shortName),parentArticle(idReadable)",
		},
		{
			name: "an update of the title",
			write: func(ctx context.Context, articles *youtrack.ArticlesService) (*youtrack.Node, error) {
				return articles.Update(ctx, "DEV-A-7", &youtrack.ArticleUpdate{Summary: new("Title")}, answeredWith("idReadable"))
			},
			fields: "idReadable,summary",
		},
		{
			name: "an update of the title and content",
			write: func(ctx context.Context, articles *youtrack.ArticlesService) (*youtrack.Node, error) {
				return articles.Update(ctx, "DEV-A-7", &youtrack.ArticleUpdate{Summary: new("Title"), Content: new("Text")},
					answeredWith("idReadable"))
			},
			filed:  map[string]any{"content": "Text"},
			fields: "idReadable,summary,content",
		},
		{
			name: "content taken away",
			write: func(ctx context.Context, articles *youtrack.ArticlesService) (*youtrack.Node, error) {
				return articles.Update(ctx, "DEV-A-7", &youtrack.ArticleUpdate{ClearContent: true}, answeredWith("idReadable"))
			},
			fields: "idReadable,content",
		},
		{
			name: "the parent taken away",
			write: func(ctx context.Context, articles *youtrack.ArticlesService) (*youtrack.Node, error) {
				return articles.Update(ctx, "DEV-A-7", &youtrack.ArticleUpdate{ClearParent: true}, answeredWith("idReadable"))
			},
			fields: "idReadable,parentArticle(idReadable)",
		},
		{
			name: "an update of the parent",
			write: func(ctx context.Context, articles *youtrack.ArticlesService) (*youtrack.Node, error) {
				return articles.Update(ctx, "DEV-A-7", &youtrack.ArticleUpdate{Parent: new("DEV-A-1")}, answeredWith("idReadable"))
			},
			filed:  map[string]any{"parentArticle": articleFiledUnder(articleParent.readable)},
			fields: "idReadable,parentArticle(idReadable)",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := articleServer(t, map[string]string{
				"DEV-A-7": articleLine("DEV", articleNothingAbove, articleWritten),
				"DEV-A-1": articleLine("DEV", articleRootAbove, articleParent),
			}, fake.JSON(http.StatusOK, articleFiled(t, tc.filed)))

			node, err := tc.write(t.Context(), client(t, server).Articles)

			require.NoError(t, err)
			assert.Equal(t, articleNamed(articleWritten.readable), node)
			assert.Equal(t, tc.fields, server.Last(t).URL.Query().Get("fields"))
		})
	}
}

func TestCreateArticleRefusesAnAnswerThatDisagreesWithTheWrite(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		in       youtrack.ArticleInput
		filed    map[string]any
		article  string
		mismatch []*youtrack.Node
	}{
		{
			name:     "a title the server stored in another letter case",
			in:       youtrack.ArticleInput{Summary: "Title"},
			filed:    map[string]any{"summary": "title"},
			article:  articleWritten.readable,
			mismatch: []*youtrack.Node{mismatch("summary", youtrack.NewString("Title"), youtrack.NewString("title"))},
		},
		{
			name:     "content the server kept none of",
			in:       youtrack.ArticleInput{Summary: "Title", Content: "Text"},
			article:  articleWritten.readable,
			mismatch: []*youtrack.Node{mismatch("content", youtrack.NewString("Text"), youtrack.NewNull())},
		},
		{
			name:    "content the server cut a carriage return out of",
			in:      youtrack.ArticleInput{Summary: "Title", Content: "First\rSecond"},
			filed:   map[string]any{"content": "FirstSecond"},
			article: articleWritten.readable,
			mismatch: []*youtrack.Node{
				mismatch("content", youtrack.NewString("First\rSecond"), youtrack.NewString("FirstSecond")),
			},
		},
		{
			name: "an article filed in another project",
			in:   youtrack.ArticleInput{Summary: "Title"},
			filed: map[string]any{"idReadable": articleOfDEMO.readable,
				"project": map[string]any{"$type": "Project", "shortName": "DEMO"}},
			article:  articleOfDEMO.readable,
			mismatch: []*youtrack.Node{mismatch("project", youtrack.NewString("DEV"), youtrack.NewString("DEMO"))},
		},
		{
			name:     "an article filed in no project",
			in:       youtrack.ArticleInput{Summary: "Title"},
			filed:    map[string]any{"project": nil},
			article:  articleWritten.readable,
			mismatch: []*youtrack.Node{mismatch("project", youtrack.NewString("DEV"), youtrack.NewNull())},
		},
		{
			name:    "the title and the content both",
			in:      youtrack.ArticleInput{Summary: "Title", Content: "Text"},
			filed:   map[string]any{"summary": "title", "content": "text"},
			article: articleWritten.readable,
			mismatch: []*youtrack.Node{
				mismatch("summary", youtrack.NewString("Title"), youtrack.NewString("title")),
				mismatch("content", youtrack.NewString("Text"), youtrack.NewString("text")),
			},
		},
		{
			name:    "no parent where the call named one",
			in:      youtrack.ArticleInput{Summary: "Title", Parent: "DEV-A-1"},
			article: articleWritten.readable,
			mismatch: []*youtrack.Node{
				mismatch("parentArticle", youtrack.NewString(articleParent.readable), youtrack.NewNull()),
			},
		},
		{
			name:    "another parent than the one the call named",
			in:      youtrack.ArticleInput{Summary: "Title", Parent: "DEV-A-1"},
			filed:   map[string]any{"parentArticle": articleFiledUnder(articleOtherParent)},
			article: articleWritten.readable,
			mismatch: []*youtrack.Node{
				mismatch("parentArticle", youtrack.NewString(articleParent.readable), youtrack.NewString(articleOtherParent)),
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := articleServer(t, map[string]string{"DEV-A-1": articleLine("DEV", articleNothingAbove, articleParent)},
				fake.JSON(http.StatusOK, articleFiled(t, tc.filed)))

			_, err := client(t, server).Articles.Create(t.Context(), "DEV", &tc.in, answeredWith("idReadable"))

			want := youtrack.Error{Code: youtrack.CodeUpstreamInvalid, AfterWrite: true, Details: []youtrack.Pair{
				lastRequest(t, server),
				{Key: "article", Value: youtrack.NewString(tc.article)},
				{Key: "mismatch", Value: youtrack.NewList(tc.mismatch...)},
			}}
			assert.Equal(t, want, errorOf(t, err))
		})
	}
}

func TestUpdateArticleRefusesAnAnswerThatDisagreesWithTheWrite(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		in       youtrack.ArticleUpdate
		filed    map[string]any
		mismatch *youtrack.Node
	}{
		{
			name:     "a title the server stored in another letter case",
			in:       youtrack.ArticleUpdate{Summary: new("Title")},
			filed:    map[string]any{"summary": "title"},
			mismatch: mismatch("summary", youtrack.NewString("Title"), youtrack.NewString("title")),
		},
		{
			name:     "content the server cut a carriage return out of",
			in:       youtrack.ArticleUpdate{Content: new("First\rSecond")},
			filed:    map[string]any{"content": "FirstSecond"},
			mismatch: mismatch("content", youtrack.NewString("First\rSecond"), youtrack.NewString("FirstSecond")),
		},
		{
			name:     "content still standing where the call took it away",
			in:       youtrack.ArticleUpdate{ClearContent: true},
			filed:    map[string]any{"content": "Text"},
			mismatch: mismatch("content", youtrack.NewNull(), youtrack.NewString("Text")),
		},
		{
			name:     "a parent still standing where the call took it away",
			in:       youtrack.ArticleUpdate{ClearParent: true},
			filed:    map[string]any{"parentArticle": articleFiledUnder(articleParent.readable)},
			mismatch: mismatch("parentArticle", youtrack.NewNull(), youtrack.NewString(articleParent.readable)),
		},
		{
			name:     "no parent where the call wrote one",
			in:       youtrack.ArticleUpdate{Parent: new("DEV-A-1")},
			mismatch: mismatch("parentArticle", youtrack.NewString(articleParent.readable), youtrack.NewNull()),
		},
		{
			name:     "another parent than the one the call wrote",
			in:       youtrack.ArticleUpdate{Parent: new("DEV-A-1")},
			filed:    map[string]any{"parentArticle": articleFiledUnder(articleOtherParent)},
			mismatch: mismatch("parentArticle", youtrack.NewString(articleParent.readable), youtrack.NewString(articleOtherParent)),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := articleServer(t, map[string]string{
				"DEV-A-7": articleLine("DEV", articleNothingAbove, articleWritten),
				"DEV-A-1": articleLine("DEV", articleRootAbove, articleParent),
			}, fake.JSON(http.StatusOK, articleFiled(t, tc.filed)))

			_, err := client(t, server).Articles.Update(t.Context(), "DEV-A-7", &tc.in, answeredWith("idReadable"))

			want := youtrack.Error{Code: youtrack.CodeUpstreamInvalid, AfterWrite: true, Details: []youtrack.Pair{
				lastRequest(t, server),
				{Key: "article", Value: youtrack.NewString(articleWritten.readable)},
				{Key: "mismatch", Value: youtrack.NewList(tc.mismatch)},
			}}
			assert.Equal(t, want, errorOf(t, err))
		})
	}
}

func TestArticleWritesTakeAProjectInAnyLetterCase(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		write func(ctx context.Context, articles *youtrack.ArticlesService) (*youtrack.Node, error)
		reads map[string]string
		filed map[string]any
	}{
		{
			name: "a creation the answer files under the code in capitals",
			write: func(ctx context.Context, articles *youtrack.ArticlesService) (*youtrack.Node, error) {
				return articles.Create(ctx, "dev", &youtrack.ArticleInput{Summary: "Title"}, answeredWith("idReadable"))
			},
		},
		{
			name: "a creation under a parent of the code in capitals",
			write: func(ctx context.Context, articles *youtrack.ArticlesService) (*youtrack.Node, error) {
				return articles.Create(ctx, "dev", &youtrack.ArticleInput{Summary: "Title", Parent: "DEV-A-1"}, answeredWith("idReadable"))
			},
			reads: map[string]string{"DEV-A-1": articleLine("DEV", articleNothingAbove, articleParent)},
			filed: map[string]any{"parentArticle": articleFiledUnder(articleParent.readable)},
		},
		{
			name: "a move under a parent whose code the read spelled otherwise",
			write: func(ctx context.Context, articles *youtrack.ArticlesService) (*youtrack.Node, error) {
				return articles.Update(ctx, "DEV-A-7", &youtrack.ArticleUpdate{Parent: new("DEV-A-1")}, answeredWith("idReadable"))
			},
			reads: map[string]string{
				"DEV-A-7": articleLine("DEV", articleNothingAbove, articleWritten),
				"DEV-A-1": articleLine("dev", articleRootAbove, articleParent),
			},
			filed: map[string]any{"parentArticle": articleFiledUnder(articleParent.readable)},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := articleServer(t, tc.reads, fake.JSON(http.StatusOK, articleFiled(t, tc.filed)))

			node, err := tc.write(t.Context(), client(t, server).Articles)

			require.NoError(t, err)
			assert.Equal(t, articleNamed(articleWritten.readable), node)
		})
	}
}

func TestArticleWritesRefuseAParentOfAnotherProject(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		write func(ctx context.Context, articles *youtrack.ArticlesService) (*youtrack.Node, error)
	}{
		{
			name: "a creation",
			write: func(ctx context.Context, articles *youtrack.ArticlesService) (*youtrack.Node, error) {
				return articles.Create(ctx, "DEV", &youtrack.ArticleInput{Summary: "Title", Parent: "DEMO-A-1"}, nil)
			},
		},
		{
			name: "a move",
			write: func(ctx context.Context, articles *youtrack.ArticlesService) (*youtrack.Node, error) {
				return articles.Update(ctx, "DEV-A-7", &youtrack.ArticleUpdate{Parent: new("DEMO-A-1")}, nil)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := articleServer(t, map[string]string{
				"DEV-A-7":  articleLine("DEV", articleNothingAbove, articleWritten),
				"DEMO-A-1": articleLine("DEMO", articleRootAbove, articleOfDEMO),
			}, fake.Unexpected(t))

			_, err := tc.write(t.Context(), client(t, server).Articles)

			want := youtrack.Error{Code: youtrack.CodeBadUsage, Details: []youtrack.Pair{
				lastRequest(t, server),
				{Key: "project", Value: youtrack.NewString("DEV")},
				{Key: "parent", Value: youtrack.NewString(articleOfDEMO.readable)},
				{Key: "parent_project", Value: youtrack.NewString("DEMO")},
			}}
			assert.Equal(t, want, errorOf(t, err))
		})
	}
}

func TestUpdateArticleRefusesAnArticleItCannotWriteByTheRead(t *testing.T) {
	t.Parallel()
	const project = `{"$type":"Project","shortName":"DEV"}`
	tests := []struct {
		name string
		read string
	}{
		{name: "two dots for a readable id", read: articleRead(`"177-7"`, `".."`, project)},
		{name: "a path after the readable id", read: articleRead(`"177-7"`, `"DEV-A-7/.."`, project)},
		{name: "the readable id of an issue", read: articleRead(`"177-7"`, `"DEV-1"`, project)},
		{name: "an empty readable id", read: articleRead(`"177-7"`, `""`, project)},
		{name: "a number for a readable id", read: articleRead(`"177-7"`, `7`, project)},
		{name: "no internal id", read: articleRead(`null`, `"DEV-A-7"`, project)},
		{name: "no project", read: articleRead(`"177-7"`, `"DEV-A-7"`, `null`)},
		{name: "a project of no code", read: articleRead(`"177-7"`, `"DEV-A-7"`, `{"$type":"Project","shortName":null}`)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := articleServer(t, map[string]string{"DEV-A-7": tc.read}, fake.Unexpected(t))

			_, err := client(t, server).Articles.Update(t.Context(), "DEV-A-7", &youtrack.ArticleUpdate{Summary: new("Title")}, nil)

			assert.Equal(t, unreadable(lastRequest(t, server), tc.read), errorOf(t, err))
		})
	}
}

func TestUpdateArticleRefusesAParentThatClosesTheLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		parent string
		line   string
		chain  []*youtrack.Node
	}{
		{
			name:   "the article itself",
			parent: "dev-A-7",
			line:   articleLine("DEV", articleRootAbove, articleWritten),
			chain:  []*youtrack.Node{youtrack.NewString("DEV-A-7")},
		},
		{
			name:   "an article written under it",
			parent: "DEV-A-9",
			line:   articleLine("DEV", articleRootAbove, articleChild, articleBetween, articleWritten),
			chain:  []*youtrack.Node{youtrack.NewString("DEV-A-9"), youtrack.NewString("DEV-A-8"), youtrack.NewString("DEV-A-7")},
		},
		{
			name:   "an article written under it, with a root above them both",
			parent: "DEV-A-9",
			line:   articleLine("DEV", articleRootAbove, articleChild, articleWritten, articleParent),
			chain:  []*youtrack.Node{youtrack.NewString("DEV-A-9"), youtrack.NewString("DEV-A-7")},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := articleServer(t, map[string]string{
				"DEV-A-7": articleLine("DEV", articleNothingAbove, articleWritten),
				tc.parent: tc.line,
			}, fake.Unexpected(t))

			_, err := client(t, server).Articles.Update(t.Context(), "DEV-A-7", &youtrack.ArticleUpdate{Parent: &tc.parent}, nil)

			want := youtrack.Error{Code: youtrack.CodeBadUsage, Details: []youtrack.Pair{
				lastRequest(t, server),
				{Key: "article", Value: youtrack.NewString(articleWritten.readable)},
				{Key: "parent", Value: tc.chain[0]},
				{Key: "chain", Value: youtrack.NewList(tc.chain...)},
			}}
			assert.Equal(t, want, errorOf(t, err))
		})
	}
}

func TestUpdateArticleRefusesALineOfParentsTheServerBrokeOff(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		child http.HandlerFunc
	}{
		{name: "at the parent", child: fake.JSON(http.StatusOK, articleLine("DEV", articleNothingAbove, articleChild))},
		{name: "where the line is read on", child: articleChildAsDeepAsAsked},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := routes(t, map[string]http.HandlerFunc{
				"GET /api/articles/DEV-A-7":              fake.JSON(http.StatusOK, articleLine("DEV", articleNothingAbove, articleWritten)),
				"GET /api/articles/DEV-A-9":              tc.child,
				"GET /api/articles/" + articleDeepest.id: fake.JSON(http.StatusOK, articleLine("DEV", articleNothingAbove, articleDeepest)),
				"POST /api/articles/DEV-A-7":             fake.Unexpected(t),
			})

			_, err := client(t, server).Articles.Update(t.Context(), "DEV-A-7", &youtrack.ArticleUpdate{Parent: new("DEV-A-9")}, nil)

			missing := youtrack.NewMap(
				youtrack.Pair{Key: "field", Value: youtrack.NewString("parentArticle")},
				youtrack.Pair{Key: "type", Value: youtrack.NewString("Article")})
			want := youtrack.Error{Code: youtrack.CodeUpstreamInvalid, Details: []youtrack.Pair{
				lastRequest(t, server),
				{Key: "fields", Value: youtrack.NewString(server.Last(t).URL.Query().Get("fields"))},
				{Key: "missing", Value: youtrack.NewList(missing)},
			}}
			assert.Equal(t, want, errorOf(t, err))
		})
	}
}

func TestUpdateArticleRefusesAnAncestorItCannotRead(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		line string
	}{
		{
			name: "an ancestor with no internal id",
			line: `{"$type":"Article","id":"177-9","idReadable":"DEV-A-9","project":{"$type":"Project","shortName":"DEV"},` +
				`"parentArticle":{"$type":"Article","id":null,"idReadable":"DEV-A-8","parentArticle":null}}`,
		},
		{
			name: "an ancestor with no readable id",
			line: `{"$type":"Article","id":"177-9","idReadable":"DEV-A-9","project":{"$type":"Project","shortName":"DEV"},` +
				`"parentArticle":{"$type":"Article","id":"177-8","idReadable":null,"parentArticle":null}}`,
		},
		{
			name: "a parent with the readable id of an issue",
			line: articleLine("DEV", articleRootAbove, articleStep{id: "177-9", readable: "DEV-9"}),
		},
		{
			name: "a parent of another shape",
			line: `{"$type":"Article","id":"177-9","idReadable":"DEV-A-9","project":{"$type":"Project","shortName":"DEV"},` +
				`"parentArticle":[]}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := articleServer(t, map[string]string{
				"DEV-A-7": articleLine("DEV", articleNothingAbove, articleWritten),
				"DEV-A-9": tc.line,
			}, fake.Unexpected(t))

			_, err := client(t, server).Articles.Update(t.Context(), "DEV-A-7", &youtrack.ArticleUpdate{Parent: new("DEV-A-9")}, nil)

			assert.Equal(t, unreadable(lastRequest(t, server), tc.line), errorOf(t, err))
		})
	}
}

func TestUpdateArticleRefusesALineThatRepeatsAnArticle(t *testing.T) {
	t.Parallel()
	server := articleServer(t, map[string]string{
		"DEV-A-7": articleLine("DEV", articleNothingAbove, articleWritten),
		"DEV-A-9": articleLine("DEV", articleRootAbove, articleChild, articleBetween, articleChild),
	}, fake.Unexpected(t))

	_, err := client(t, server).Articles.Update(t.Context(), "DEV-A-7", &youtrack.ArticleUpdate{Parent: new("DEV-A-9")}, nil)

	want := youtrack.Error{Code: youtrack.CodeUpstreamInvalid, Details: []youtrack.Pair{
		lastRequest(t, server),
		{Key: "article", Value: youtrack.NewString(articleChild.readable)},
	}}
	assert.Equal(t, want, errorOf(t, err))
}

func TestUpdateArticleReadsTheLineOnWhereItIsDeeperThanOneRequest(t *testing.T) {
	t.Parallel()
	server := routes(t, map[string]http.HandlerFunc{
		"GET /api/articles/DEV-A-7":              fake.JSON(http.StatusOK, articleLine("DEV", articleNothingAbove, articleWritten)),
		"GET /api/articles/DEV-A-9":              articleChildAsDeepAsAsked,
		"GET /api/articles/" + articleDeepest.id: fake.JSON(http.StatusOK, articleLine("DEV", articleRootAbove, articleDeepest, articleParent)),
		"POST /api/articles/DEV-A-7": fake.JSON(http.StatusOK,
			articleFiled(t, map[string]any{"parentArticle": articleFiledUnder(articleChild.readable)})),
	})

	_, err := client(t, server).Articles.Update(t.Context(), "DEV-A-7", &youtrack.ArticleUpdate{Parent: new("DEV-A-9")},
		answeredWith("idReadable"))

	require.NoError(t, err)
	assert.Equal(t, []string{"/api/articles/DEV-A-7", "/api/articles/DEV-A-9", "/api/articles/" + articleDeepest.id,
		"/api/articles/DEV-A-7"}, server.Paths())
	assert.Equal(t, server.Request(t, 1).URL.Query().Get("fields"), server.Request(t, 2).URL.Query().Get("fields"))
	assert.Equal(t, map[string]any{"parentArticle": map[string]any{"id": articleChild.id}}, server.LastJSON(t))
}

func TestDeleteArticleDeletesByTheReadableIDTheReadGave(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.InTurn(
		fake.JSON(http.StatusOK, `{"$type":"Article","idReadable":"DEV-A-7"}`),
		fake.JSON(http.StatusOK, "")))

	node, err := client(t, server).Articles.Delete(t.Context(), "dev-A-7")

	require.NoError(t, err)
	assert.Equal(t, articleNamed("DEV-A-7"), node)
	assert.Equal(t, []string{"GET /api/articles/dev-A-7", "DELETE /api/articles/DEV-A-7"}, server.Routes())
}

func TestArticleCallsRefuseAnIDOfAnIssue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		call func(ctx context.Context, articles *youtrack.ArticlesService) error
	}{
		{
			name: "a deletion",
			call: func(ctx context.Context, articles *youtrack.ArticlesService) error {
				_, err := articles.Delete(ctx, "DEV-7")
				return err
			},
		},
		{
			name: "a list of children",
			call: func(ctx context.Context, articles *youtrack.ArticlesService) error {
				_, err := articles.Children(ctx, "DEV-7", nil)
				return err
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.call(t.Context(), client(t, fake.ServeNothing(t)).Articles)

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

func TestListChildArticlesReadsTheChildrenOfTheParent(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `[{"$type":"Article","idReadable":"DEV-A-9"}]`))

	node, err := client(t, server).Articles.Children(t.Context(), "DEV-A-7",
		&youtrack.ListArticlesOptions{Fields: "idReadable", Page: youtrack.Page{Limit: 2}})

	require.NoError(t, err)
	assert.Equal(t, wholePage("articles", articleNamed("DEV-A-9")), node)
	assert.Equal(t, []string{"/api/articles/DEV-A-7/childArticles"}, server.Paths())
	assert.Equal(t, []url.Values{{"fields": {"idReadable"}, "$top": {"2"}}}, server.Queries())
}

func articleComment(id string, created int64, text string) map[string]any {
	return map[string]any{
		"$type":   "ArticleComment",
		"id":      id,
		"author":  map[string]any{"$type": "User", "login": "author"},
		"created": created,
		"text":    text,
	}
}

func TestShowArticlePrintsTheCommentsOldestFirst(t *testing.T) {
	t.Parallel()
	first := printedComment("8-3", "1970-01-01T00:00:01Z", "First")
	second := printedComment("8-2", "1970-01-01T00:00:02Z", "Second")
	third := printedComment("8-1", "1970-01-01T00:00:03Z", "Third")
	tests := []struct {
		name     string
		comments youtrack.Comments
		want     *youtrack.Node
	}{
		{
			name:     "every one of them",
			comments: youtrack.AllComments(),
			want: youtrack.NewMap(youtrack.Pair{Key: "idReadable", Value: youtrack.NewString("DEV-A-7")},
				youtrack.Pair{Key: "comments", Value: youtrack.NewList(first, second, third)}),
		},
		{
			name:     "the last two",
			comments: youtrack.LastComments(2),
			want: youtrack.NewMap(youtrack.Pair{Key: "idReadable", Value: youtrack.NewString("DEV-A-7")},
				youtrack.Pair{Key: "comments", Value: youtrack.NewList(second, third)}),
		},
		{
			name:     "more than there are",
			comments: youtrack.LastComments(5),
			want: youtrack.NewMap(youtrack.Pair{Key: "idReadable", Value: youtrack.NewString("DEV-A-7")},
				youtrack.Pair{Key: "comments", Value: youtrack.NewList(first, second, third)}),
		},
		{
			name:     "none at all",
			comments: youtrack.LastComments(0),
			want:     articleNamed("DEV-A-7"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			article, err := json.Marshal(map[string]any{"$type": "Article", "idReadable": "DEV-A-7", "comments": []any{
				articleComment("8-2", 2000, "Second"),
				articleComment("8-3", 1000, "First"),
				articleComment("8-1", 3000, "Third"),
			}})
			require.NoError(t, err)
			server := fake.Serve(t, fake.JSON(http.StatusOK, string(article)))

			node, err := client(t, server).Articles.Show(t.Context(), "DEV-A-7",
				&youtrack.ShowArticleOptions{Fields: "idReadable", Comments: tc.comments})

			require.NoError(t, err)
			assert.Equal(t, tc.want, node)
		})
	}
}

func TestShowArticleAsksForTheCommentsItPrints(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		comments youtrack.Comments
		fields   string
	}{
		{name: "every one of them", comments: youtrack.AllComments(), fields: "idReadable,comments(id,author(login),created,text)"},
		{name: "the last one", comments: youtrack.LastComments(1), fields: "idReadable,comments(id,author(login),created,text)"},
		{name: "none at all", comments: youtrack.LastComments(0), fields: "idReadable"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"Article","idReadable":"DEV-A-7","comments":[]}`))

			_, err := client(t, server).Articles.Show(t.Context(), "DEV-A-7",
				&youtrack.ShowArticleOptions{Fields: "idReadable", Comments: tc.comments})

			require.NoError(t, err)
			assert.Equal(t, []string{tc.fields}, server.Fields())
		})
	}
}

func TestListArticlesSendsTheSearchAsWritten(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		search string
	}{
		{name: "spaces around it", search: "  project: DEV  "},
		{name: "a tab and a line feed around it", search: "\tproject: DEV\n"},
		{name: "a line separator inside", search: "title: First\xe2\x80\xa8Second"},
		{name: "brackets that open and never close", search: "(((("},
		{name: "a saved search of the language of issues", search: "#Unresolved"},
		{name: "characters a query escapes", search: "title: a&b=c?d#e%20+f"},
		{name: "an empty search", search: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, `[]`))

			_, err := client(t, server).Articles.List(t.Context(), tc.search,
				&youtrack.ListArticlesOptions{Fields: "idReadable", Page: youtrack.Page{Limit: 1}})

			require.NoError(t, err)
			assert.Equal(t, []string{"/api/articles"}, server.Paths())
			assert.Equal(t, []url.Values{{"fields": {"idReadable"}, "$top": {"1"}, "query": {tc.search}}}, server.Queries())
		})
	}
}

func TestListArticlesRefusesASearchThatIsNoUTF8(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		search string
	}{
		{name: "a byte that is no UTF-8", search: "\xff"},
		{name: "a truncated sequence inside a search that parses", search: "title: \xc3\x28"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := client(t, fake.ServeNothing(t)).Articles.List(t.Context(), tc.search, nil)

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}
