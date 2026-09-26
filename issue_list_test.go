package youtrack_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/go-youtrack"
	"github.com/hakastein/go-youtrack/fake"
)

const (
	issueMarkupFields = "query,styleRanges(start,length,style)"
	issuesPath        = "/api/issues"
	issueCountPath    = "/api/issuesGetter/count"
	issueCountTarget  = issueCountPath + "?fields=count"
)

// The prose of a warning is not compared.
func issueSearch(t *testing.T, server *fake.Server, query, expression string, limit int) (*youtrack.Node, []youtrack.Warning, error) {
	t.Helper()
	var warned []youtrack.Warning
	node, err := client(t, server).Issues.List(t.Context(), query, &youtrack.ListIssuesOptions{
		Fields: expression,
		Page:   youtrack.Page{Limit: limit},
		Warn: func(w *youtrack.Warning) {
			assert.NotEmpty(t, w.Message)
			kept := *w
			kept.Message = ""
			warned = append(warned, kept)
		},
	})
	return node, warned, err
}

func issueMarkedUp(t *testing.T, marked string, rest http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == fake.AssistPath {
			fake.JSON(http.StatusOK, marked)(w, r)
			return
		}
		rest(w, r)
	})
}

func issueSearchBody(t *testing.T, query string) string {
	t.Helper()
	body, err := json.Marshal(map[string]string{"query": query})
	require.NoError(t, err)
	return string(body)
}

func issueFreeText(query string, parts ...string) youtrack.Warning {
	return youtrack.Warning{Code: youtrack.CodeUnknownName, Details: []youtrack.Pair{
		{Key: "query", Value: youtrack.NewString(query)},
		{Key: "free_text", Value: texts(parts...)},
	}}
}

func issueCount(count string) http.HandlerFunc {
	return fake.JSON(http.StatusOK, `{"$type":"IssueCountResponse","count":`+count+`}`)
}

func issueCounted(count http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == issueCountPath {
			count(w, r)
			return
		}
		fake.JSON(http.StatusOK, `[{"$type":"Issue","idReadable":"DEV-1"}]`)(w, r)
	}
}

func TestListIssuesSendsTheSearchAsWritten(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		query string
	}{
		{name: "spaces around the search", query: "  field: value  "},
		{name: "an empty search", query: ""},
		{name: "brackets and a quote that never close", query: `(((( "open`},
		{name: "characters a query escapes", query: "a&b=c?d#e%20+f"},
		{name: "a tab and a line feed inside", query: "field:\tvalue\nnext"},
		{name: "a line separator inside", query: "a\u2028b"},
		{name: "a character outside the basic plane", query: "\U0001F600"},
		{name: "four kilobytes of it", query: strings.Repeat("word ", 820)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.Searching(t, fake.JSON(http.StatusOK, `[]`)))

			_, _, err := issueSearch(t, server, tc.query, "idReadable", 50)

			require.NoError(t, err)
			assert.Equal(t, []string{fake.AssistPath, issuesPath}, server.Paths())
			marking := server.Request(t, 0)
			assert.Equal(t, http.MethodPost, marking.Method)
			assert.Equal(t, "application/json", marking.Header.Get("Content-Type"))
			assert.Equal(t, issueMarkupFields, marking.URL.Query().Get("fields"))
			assert.Equal(t, issueSearchBody(t, tc.query), marking.Body)
			assert.Equal(t, []string{tc.query}, server.Request(t, 1).URL.Query()["query"])
		})
	}
}

func TestListIssuesWarnsOfTheFreeTextOfTheSearch(t *testing.T) {
	t.Parallel()
	const pair = "a\U0001F600b"
	const tail = "field: value \U0001F600 word"
	tests := []struct {
		name     string
		query    string
		ranges   []string
		warnings []youtrack.Warning
	}{
		{
			name:     "ranges that touch, as one part",
			query:    "Unknown: value",
			ranges:   []string{fake.StyleRange(0, 7, "text"), fake.StyleRange(7, 1, "text"), fake.StyleRange(9, 5, "text")},
			warnings: []youtrack.Warning{issueFreeText("Unknown: value", "Unknown:", "value")},
		},
		{
			name:     "words apart",
			query:    "one two three",
			ranges:   []string{fake.StyleRange(0, 3, "text"), fake.StyleRange(4, 3, "text"), fake.StyleRange(8, 5, "text")},
			warnings: []youtrack.Warning{issueFreeText("one two three", "one", "two", "three")},
		},
		{
			name:     "a range over both halves of a pair",
			query:    pair,
			ranges:   []string{fake.StyleRange(1, 2, "text")},
			warnings: []youtrack.Warning{issueFreeText(pair, "\U0001F600")},
		},
		{
			name:     "a range that begins at the second half of a pair",
			query:    pair,
			ranges:   []string{fake.StyleRange(2, 2, "text")},
			warnings: []youtrack.Warning{issueFreeText(pair, "b")},
		},
		{
			name:     "a range that ends at the first half of a pair",
			query:    pair,
			ranges:   []string{fake.StyleRange(0, 2, "text")},
			warnings: []youtrack.Warning{issueFreeText(pair, "a")},
		},
		{
			name:     "a range that ends where the search ends",
			query:    tail,
			ranges:   []string{fake.StyleRange(13, 2, "text"), fake.StyleRange(16, 4, "text")},
			warnings: []youtrack.Warning{issueFreeText(tail, "\U0001F600", "word")},
		},
		{
			name:     "one range over a token holding a line separator",
			query:    "a\u2028b",
			ranges:   []string{fake.StyleRange(0, 3, "text")},
			warnings: []youtrack.Warning{issueFreeText("a\u2028b", "a\u2028b")},
		},
		{
			name:  "every style but text",
			query: "field: value word",
			ranges: []string{
				fake.StyleRange(0, 5, "field-name"), fake.StyleRange(5, 1, "operator"), fake.StyleRange(7, 5, "field-value"),
				fake.StyleRange(7, 5, "error"), fake.StyleRange(13, 4, "keyword"),
			},
		},
		{name: "no range at all", query: "field: value"},
		{name: "a range of no length", query: "field: value", ranges: []string{fake.StyleRange(5, 0, "text")}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := issueMarkedUp(t, fake.Markup(t, tc.query, tc.ranges...), fake.JSON(http.StatusOK, `[]`))

			_, warned, err := issueSearch(t, server, tc.query, "idReadable", 50)

			require.NoError(t, err)
			assert.Equal(t, tc.warnings, warned)
		})
	}
}

func TestListIssuesAsksForNoMarkupWithoutAWarnToHandTheFreeTextTo(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `[]`))

	node, err := client(t, server).Issues.List(t.Context(), "field: word", &youtrack.ListIssuesOptions{Fields: "idReadable"})

	require.NoError(t, err)
	assert.Equal(t, wholePage("issues"), node)
	assert.Equal(t, []string{issuesPath}, server.Paths())
}

func TestListIssuesWarnsOfTheFreeTextOfASearchTheServerThenRefuses(t *testing.T) {
	t.Parallel()
	const query = "field: word"
	const said = `{"error":"invalid_query","error_description":"refused"}`
	server := issueMarkedUp(t, fake.Markup(t, query, fake.StyleRange(7, 4, "text")), fake.JSON(http.StatusBadRequest, said))

	_, warned, err := issueSearch(t, server, query, "idReadable", 50)

	assert.Equal(t, []youtrack.Warning{issueFreeText(query, "word")}, warned)
	assert.Equal(t, youtrack.Error{Code: youtrack.CodeRejected, Details: []youtrack.Pair{
		requestTo(http.MethodGet, server, issuesPath+"?query=field%3A+word&fields=idReadable&$top=50"),
		{Key: "upstream_status", Value: number(http.StatusBadRequest)},
		{Key: "upstream_error", Value: youtrack.NewString("invalid_query")},
		{Key: "upstream_message", Value: youtrack.NewString("refused")},
	}}, errorOf(t, err))
}

func TestListIssuesSearchesNothingWhereTheSearchCannotBeMarkedUp(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusInternalServerError, `{"error":"server_error","error_description":"failed"}`))

	_, _, err := issueSearch(t, server, "field: value", "idReadable", 50)

	assert.Equal(t, youtrack.Error{Code: youtrack.CodeUpstreamFailed, Details: []youtrack.Pair{
		requestTo(http.MethodPost, server, fake.AssistPath+"?fields="+issueMarkupFields),
		{Key: "upstream_status", Value: number(http.StatusInternalServerError)},
		{Key: "upstream_error", Value: youtrack.NewString("server_error")},
		{Key: "upstream_message", Value: youtrack.NewString("failed")},
	}}, errorOf(t, err))
	assert.Equal(t, []string{fake.AssistPath}, server.Paths())
}

func TestListIssuesRefusesAMarkupThatDoesNotFitTheSearch(t *testing.T) {
	t.Parallel()
	const query = "field: value"
	shaped := func(marked string) []youtrack.Pair {
		return []youtrack.Pair{
			{Key: "upstream_status", Value: number(http.StatusOK)},
			{Key: "upstream_body", Value: youtrack.NewString(marked)},
		}
	}
	missing := func(field, schema string) []youtrack.Pair {
		return []youtrack.Pair{
			{Key: "fields", Value: youtrack.NewString(issueMarkupFields)},
			{Key: "missing", Value: youtrack.NewList(youtrack.NewMap(
				youtrack.Pair{Key: "field", Value: youtrack.NewString(field)},
				youtrack.Pair{Key: "type", Value: youtrack.NewString(schema)},
			))},
		}
	}
	ranged := func(ranges string) string {
		return `{"$type":"SearchSuggestions","query":"field: value","styleRanges":` + ranges + `}`
	}
	echoedWithASpace := `{"$type":"SearchSuggestions","query":"field: value ","styleRanges":[]}`
	oneRange := ranged(`{"$type":"SearchStyleRange","start":0,"length":5,"style":"field-name"}`)
	nullRange := ranged(`[null]`)
	startAsText := ranged(`[{"$type":"SearchStyleRange","start":"0","length":5,"style":"field-name"}]`)
	lengthAsFraction := ranged(`[{"$type":"SearchStyleRange","start":0,"length":1.5,"style":"field-name"}]`)
	styleAsNull := ranged(`[{"$type":"SearchStyleRange","start":0,"length":5,"style":null}]`)
	pastTheEnd := ranged(`[{"$type":"SearchStyleRange","start":8,"length":5,"style":"text"}]`)
	beforeTheStart := ranged(`[{"$type":"SearchStyleRange","start":-1,"length":5,"style":"text"}]`)
	tests := []struct {
		name         string
		marked       string
		afterRequest []youtrack.Pair
	}{
		{name: "a search that came back with a space of its own", marked: echoedWithASpace, afterRequest: shaped(echoedWithASpace)},
		{name: "the ranges as one range", marked: oneRange, afterRequest: shaped(oneRange)},
		{name: "a range that is no object", marked: nullRange, afterRequest: shaped(nullRange)},
		{name: "where a range begins written as text", marked: startAsText, afterRequest: shaped(startAsText)},
		{name: "how far a range runs written as a fraction", marked: lengthAsFraction, afterRequest: shaped(lengthAsFraction)},
		{name: "a range of no style", marked: styleAsNull, afterRequest: shaped(styleAsNull)},
		{name: "a range that runs past the end", marked: pastTheEnd, afterRequest: shaped(pastTheEnd)},
		{name: "a range that begins before the start", marked: beforeTheStart, afterRequest: shaped(beforeTheStart)},
		{
			name:         "no ranges at all",
			marked:       `{"$type":"SearchSuggestions","query":"field: value"}`,
			afterRequest: missing("styleRanges", "SearchSuggestions"),
		},
		{
			name:         "a range with no style",
			marked:       ranged(`[{"$type":"SearchStyleRange","start":0,"length":5}]`),
			afterRequest: missing("styleRanges(style)", "SearchStyleRange"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := issueMarkedUp(t, tc.marked, fake.JSON(http.StatusOK, `[]`))

			_, _, err := issueSearch(t, server, query, "idReadable", 50)

			request := requestTo(http.MethodPost, server, fake.AssistPath+"?fields="+issueMarkupFields)
			want := youtrack.Error{Code: youtrack.CodeUpstreamInvalid, Details: append([]youtrack.Pair{request}, tc.afterRequest...)}
			assert.Equal(t, want, errorOf(t, err))
			assert.Equal(t, []string{fake.AssistPath}, server.Paths())
		})
	}
}

func TestListIssuesAsksTheCounterTheSearchOfThePage(t *testing.T) {
	t.Parallel()
	const query = `  field: "exact phrase" \ "  `
	server := fake.Serve(t, fake.Searching(t, issueCounted(issueCount("7"))))

	_, _, err := issueSearch(t, server, query, "idReadable", 1)

	require.NoError(t, err)
	assert.Equal(t, []string{fake.AssistPath, issuesPath, issueCountPath}, server.Paths())
	counting := server.Last(t)
	assert.Equal(t, http.MethodPost, counting.Method)
	assert.Equal(t, "application/json", counting.Header.Get("Content-Type"))
	assert.Equal(t, "count", counting.URL.Query().Get("fields"))
	assert.Equal(t, issueSearchBody(t, query), counting.Body)
}

func TestListIssuesPrintsTheCountOfTheSearch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		counts    []http.HandlerFunc
		total     *youtrack.Node
		truncated *youtrack.Node
		paths     []string
	}{
		{
			name:      "a count ready at once",
			counts:    []http.HandlerFunc{issueCount("7")},
			total:     number(7),
			truncated: youtrack.NewBool(true),
			paths:     []string{fake.AssistPath, issuesPath, issueCountPath},
		},
		{
			name:      "a count ready when asked again",
			counts:    []http.HandlerFunc{issueCount("-1"), issueCount("7")},
			total:     number(7),
			truncated: youtrack.NewBool(true),
			paths:     []string{fake.AssistPath, issuesPath, issueCountPath, issueCountPath},
		},
		{
			name:      "a count not ready when asked again",
			counts:    []http.HandlerFunc{issueCount("-1"), issueCount("-1"), issueCount("7")},
			total:     youtrack.NewNull(),
			truncated: youtrack.NewNull(),
			paths:     []string{fake.AssistPath, issuesPath, issueCountPath, issueCountPath},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.Searching(t, issueCounted(fake.InTurn(tc.counts...))))

			node, _, err := issueSearch(t, server, "field: value", "idReadable", 1)

			require.NoError(t, err)
			issue := youtrack.NewMap(youtrack.Pair{Key: "idReadable", Value: youtrack.NewString("DEV-1")})
			assert.Equal(t, page("issues", tc.total, tc.truncated, issue), node)
			assert.Equal(t, tc.paths, server.Paths())
		})
	}
}

func TestListIssuesRefusesACountThatIsNoNumberOfIssues(t *testing.T) {
	t.Parallel()
	shaped := func(answer string) []youtrack.Pair {
		return []youtrack.Pair{
			{Key: "upstream_status", Value: number(http.StatusOK)},
			{Key: "upstream_body", Value: youtrack.NewString(answer)},
		}
	}
	counted := func(count string) string { return `{"$type":"IssueCountResponse","count":` + count + `}` }
	tests := []struct {
		name         string
		answer       string
		afterRequest []youtrack.Pair
	}{
		{name: "a negative number other than -1", answer: counted("-2"), afterRequest: shaped(counted("-2"))},
		{name: "a fraction", answer: counted("1.5"), afterRequest: shaped(counted("1.5"))},
		{name: "a number in quotes", answer: counted(`"3"`), afterRequest: shaped(counted(`"3"`))},
		{name: "nothing at all", answer: counted("null"), afterRequest: shaped(counted("null"))},
		{
			name:   "no count in the answer",
			answer: `{"$type":"IssueCountResponse","id":"count"}`,
			afterRequest: []youtrack.Pair{
				{Key: "fields", Value: youtrack.NewString("count")},
				{Key: "missing", Value: youtrack.NewList(youtrack.NewMap(
					youtrack.Pair{Key: "field", Value: youtrack.NewString("count")},
					youtrack.Pair{Key: "type", Value: youtrack.NewString("IssueCountResponse")},
				))},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.Searching(t, issueCounted(fake.JSON(http.StatusOK, tc.answer))))

			_, _, err := issueSearch(t, server, "field: value", "idReadable", 1)

			request := requestTo(http.MethodPost, server, issueCountTarget)
			want := youtrack.Error{Code: youtrack.CodeUpstreamInvalid, Details: append([]youtrack.Pair{request}, tc.afterRequest...)}
			assert.Equal(t, want, errorOf(t, err))
		})
	}
}

func TestListIssuesRefusesWhereTheCounterFailsWhenAskedAgain(t *testing.T) {
	t.Parallel()
	failed := fake.JSON(http.StatusInternalServerError, `{"error":"server_error","error_description":"failed"}`)
	server := fake.Serve(t, fake.Searching(t, issueCounted(fake.InTurn(issueCount("-1"), failed))))

	_, _, err := issueSearch(t, server, "field: value", "idReadable", 1)

	assert.Equal(t, youtrack.Error{Code: youtrack.CodeUpstreamFailed, Details: []youtrack.Pair{
		requestTo(http.MethodPost, server, issueCountTarget),
		{Key: "upstream_status", Value: number(http.StatusInternalServerError)},
		{Key: "upstream_error", Value: youtrack.NewString("server_error")},
		{Key: "upstream_message", Value: youtrack.NewString("failed")},
	}}, errorOf(t, err))
	assert.Equal(t, []string{fake.AssistPath, issuesPath, issueCountPath, issueCountPath}, server.Paths())
}
