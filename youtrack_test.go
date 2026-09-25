package youtrack_test

import (
	"encoding/json"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/youtrack"
	"github.com/hakastein/youtrack/fake"
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
