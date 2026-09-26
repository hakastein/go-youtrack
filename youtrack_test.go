package youtrack_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/go-youtrack"
	"github.com/hakastein/go-youtrack/fake"
)

// Under a zone of UTC a moment printed in the zone of the process reads as one printed in UTC.
func TestMain(m *testing.M) {
	time.Local = time.FixedZone("UTC+5", 5*60*60)
	os.Exit(m.Run())
}

func client(t *testing.T, server *fake.Server, opts ...youtrack.Option) *youtrack.Client {
	t.Helper()
	c, err := youtrack.NewClient(server.URL, fake.Token, opts...)
	require.NoError(t, err)
	return c
}

// The prose of a message is not compared: it changes without the behaviour changing.
func errorOf(t *testing.T, err error) youtrack.Error {
	t.Helper()
	var failed *youtrack.Error
	require.ErrorAs(t, err, &failed)
	assert.NotEmpty(t, failed.Message)
	kept := *failed
	kept.Message = ""
	kept.Err = nil
	return kept
}

func routes(t *testing.T, answers map[string]http.HandlerFunc) *fake.Server {
	t.Helper()
	mux := http.NewServeMux()
	for pattern, answer := range answers {
		mux.Handle(pattern, answer)
	}
	return fake.Serve(t, mux.ServeHTTP)
}

func requestTo(method string, server *fake.Server, target string) youtrack.Pair {
	return youtrack.Pair{Key: "request", Value: youtrack.NewString(method + " " + server.URL + target)}
}

func lastRequest(t *testing.T, server *fake.Server) youtrack.Pair {
	t.Helper()
	sent := server.Last(t)
	query, err := url.QueryUnescape(sent.URL.RawQuery)
	require.NoError(t, err)
	return requestTo(sent.Method, server, sent.URL.Path+"?"+query)
}

func unreadable(request youtrack.Pair, body string) youtrack.Error {
	return youtrack.Error{Code: youtrack.CodeUpstreamInvalid, Details: []youtrack.Pair{
		request,
		{Key: "upstream_status", Value: youtrack.NewNumber("200")},
		{Key: "upstream_body", Value: youtrack.NewString(body)},
	}}
}

func mismatch(field string, expected, actual *youtrack.Node) *youtrack.Node {
	return youtrack.NewMap(
		youtrack.Pair{Key: "field", Value: youtrack.NewString(field)},
		youtrack.Pair{Key: "expected", Value: expected},
		youtrack.Pair{Key: "actual", Value: actual})
}

func number(n int) *youtrack.Node {
	return youtrack.NewNumber(json.Number(strconv.Itoa(n)))
}

func texts(values ...string) *youtrack.Node {
	items := make([]*youtrack.Node, 0, len(values))
	for _, value := range values {
		items = append(items, youtrack.NewString(value))
	}
	return youtrack.NewList(items...)
}

func withNearest(key, written string, nearest ...string) *youtrack.Node {
	return youtrack.NewMap(
		youtrack.Pair{Key: key, Value: youtrack.NewString(written)},
		youtrack.Pair{Key: "nearest", Value: texts(nearest...)})
}

func page(plural string, total, truncated *youtrack.Node, records ...*youtrack.Node) *youtrack.Node {
	return youtrack.NewMap(
		youtrack.Pair{Key: "total", Value: total},
		youtrack.Pair{Key: "returned", Value: number(len(records))},
		youtrack.Pair{Key: "truncated", Value: truncated},
		youtrack.Pair{Key: plural, Value: youtrack.NewList(records...)})
}

func wholePage(plural string, records ...*youtrack.Node) *youtrack.Node {
	return page(plural, number(len(records)), youtrack.NewBool(false), records...)
}

func answeredWith(fields string) *youtrack.WriteOptions {
	return &youtrack.WriteOptions{Fields: fields}
}

const keptByteForByte = "  First\r\nSecond\rThird   \n---\n~~~\n\u0085\xe2\x80\xa8\xef\xbb\xbf\U0001F600\n  "

func printedComment(id, created, text string) *youtrack.Node {
	return youtrack.NewMap(
		youtrack.Pair{Key: "id", Value: youtrack.NewString(id)},
		youtrack.Pair{Key: "author", Value: youtrack.NewMap(youtrack.Pair{Key: "login", Value: youtrack.NewString("author")})},
		youtrack.Pair{Key: "created", Value: youtrack.NewString(created)},
		youtrack.Pair{Key: "text", Value: youtrack.NewText(text)})
}
