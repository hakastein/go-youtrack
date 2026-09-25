package youtrack_test

import (
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hakastein/youtrack"
	"github.com/hakastein/youtrack/fake"
)

// Under a zone of UTC a moment printed in the zone of the process reads as one printed in UTC.
func TestMain(m *testing.M) {
	time.Local = time.FixedZone("UTC+5", 5*60*60)
	os.Exit(m.Run())
}

func client(t *testing.T, server *fake.Server, options ...youtrack.Option) *youtrack.Client {
	t.Helper()
	c, err := youtrack.New(server.URL, fake.Token, options...)
	require.NoError(t, err)
	return c
}

func requestTo(method string, server *fake.Server, target string) youtrack.Request {
	return youtrack.Request{Method: method, URL: server.URL + target}
}

func lastRequest(t *testing.T, server *fake.Server) youtrack.Request {
	t.Helper()
	sent := server.Last(t)
	query, err := url.QueryUnescape(sent.URL.RawQuery)
	require.NoError(t, err)
	return requestTo(sent.Method, server, sent.URL.Path+"?"+query)
}

func requestAt(t *testing.T, server *fake.Server, n int) youtrack.Request {
	t.Helper()
	sent := server.Request(t, n)
	query, err := url.QueryUnescape(sent.URL.RawQuery)
	require.NoError(t, err)
	return requestTo(sent.Method, server, sent.URL.Path+"?"+query)
}

func invalidAnswer(request youtrack.Request, body string) youtrack.ResponseError {
	return youtrack.ResponseError{Request: request, Status: http.StatusOK, Body: []byte(body)}
}

// The prose of a reason is not compared: it changes without the behaviour changing.
func withoutReason(err youtrack.ResponseError) youtrack.ResponseError {
	err.Reason = ""
	return err
}

func responseErrorOf(t *testing.T, err error) youtrack.ResponseError {
	t.Helper()
	var failed *youtrack.ResponseError
	require.ErrorAs(t, err, &failed)
	require.NotEmpty(t, failed.Reason)
	return withoutReason(*failed)
}

func argumentErrorOf(t *testing.T, err error) youtrack.ArgumentError {
	t.Helper()
	var failed *youtrack.ArgumentError
	require.ErrorAs(t, err, &failed)
	require.NotEmpty(t, failed.Reason)
	failed.Reason = ""
	return *failed
}

func statusErrorOf(t *testing.T, err error) youtrack.StatusError {
	t.Helper()
	var failed *youtrack.StatusError
	require.ErrorAs(t, err, &failed)
	return *failed
}
