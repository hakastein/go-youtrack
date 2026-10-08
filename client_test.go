package youtrack_test

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/go-youtrack"
	"github.com/hakastein/go-youtrack/fake"
)

func TestNewClientRefusesAnAddressOrATokenItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		address string
		token   string
	}{
		{name: "an address of no scheme", address: "yt.example.org", token: fake.Token},
		{name: "an address of another scheme", address: "ftp://yt.example.org", token: fake.Token},
		{name: "an address of no host", address: "https:///youtrack", token: fake.Token},
		{name: "an address with a query", address: "https://yt.example.org/?a=b", token: fake.Token},
		{name: "an address with a fragment", address: "https://yt.example.org/#top", token: fake.Token},
		{name: "an address with an empty fragment", address: "https://yt.example.org/#", token: fake.Token},
		{name: "an address that is no URL", address: "https://yt.example.org/%zz", token: fake.Token},
		{name: "an empty token", address: "https://yt.example.org", token: ""},
		{name: "a token with a line break", address: "https://yt.example.org", token: "perm\ntoken"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := youtrack.NewClient(tc.address, tc.token)

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

func TestNewClientKeepsTheTokenOutOfItsRefusal(t *testing.T) {
	t.Parallel()

	_, err := youtrack.NewClient("https://yt.example.org", "perm-secret\n")

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "perm-secret")
}

func TestNewClientKeepsThePasswordOfTheAddressOutOfItsRefusal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		address string
	}{
		{name: "an address of another scheme", address: "ftp://user:pass-secret@yt.example.org"},
		{name: "an address with a query", address: "https://user:pass-secret@yt.example.org/?a=b"},
		{name: "an address that is no URL", address: "https://user:pass-secret@yt.example.org/%zz"},
		{name: "an address whose host is no URL", address: "https://user:pass-secret@yt example.org"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := youtrack.NewClient(tc.address, fake.Token)

			require.Error(t, err)
			assert.NotContains(t, err.Error(), "pass-secret")
		})
	}
}

func showDEV(ctx context.Context, c *youtrack.Client) error {
	_, err := c.Projects.Show(ctx, "DEV", &youtrack.ShowProjectOptions{Fields: "shortName"})
	return err
}

func TestClientSendsTheTokenAndAsksForJSON(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"Project","shortName":"DEV"}`))

	err := showDEV(t.Context(), client(t, server))

	require.NoError(t, err)
	sent := server.Last(t)
	assert.Equal(t, "Bearer "+fake.Token, sent.Header.Get("Authorization"))
	assert.Equal(t, "application/json", sent.Header.Get("Accept"))
}

func TestClientReachesTheInstanceUnderAPathOfTheAddress(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"Project","shortName":"DEV"}`))
	c, err := youtrack.NewClient(server.URL+"/", fake.Token)
	require.NoError(t, err)

	err = showDEV(t.Context(), c)

	require.NoError(t, err)
	assert.Equal(t, []string{"/api/admin/projects/DEV"}, server.Paths())
}

func TestClientDoesNotFollowARedirect(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "/elsewhere")
		w.WriteHeader(http.StatusFound)
	})

	err := showDEV(t.Context(), client(t, server))

	want := youtrack.Error{Code: youtrack.CodeUpstreamInvalid, Details: []youtrack.Pair{
		lastRequest(t, server),
		{Key: "upstream_status", Value: number(http.StatusFound)},
		{Key: "upstream_body", Value: youtrack.NewString("")},
	}}
	assert.Equal(t, want, errorOf(t, err))
	assert.Equal(t, []string{"/api/admin/projects/DEV"}, server.Paths())
}

func TestClientReportsAServerNobodyAnswersAt(t *testing.T) {
	t.Parallel()

	err := showDEV(t.Context(), client(t, fake.Unreachable()))

	want := youtrack.Error{Code: youtrack.CodeUpstreamFailed, Details: []youtrack.Pair{
		{Key: "request", Value: youtrack.NewString("GET " + fake.NobodyListens + "/api/admin/projects/DEV?fields=shortName")},
	}}
	assert.Equal(t, want, errorOf(t, err))
}

func TestClientTellsACancelledCall(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := showDEV(ctx, client(t, fake.ServeNothing(t)))

	assert.ErrorIs(t, err, context.Canceled)
	assert.ErrorIs(t, err, youtrack.ErrUpstreamFailed)
}

func TestClientPassesOnTheWordsOfTheServer(t *testing.T) {
	t.Parallel()
	const refused = `{"error":"Not Found","error_description":"Entity with id DEV not found"}`
	upstream := func(key, value string) youtrack.Pair {
		return youtrack.Pair{Key: key, Value: youtrack.NewString(value)}
	}
	tests := []struct {
		name   string
		status int
		body   string
		code   youtrack.Code
		words  []youtrack.Pair
	}{
		{name: "a refusal of YouTrack", status: http.StatusNotFound, body: refused, code: youtrack.CodeNotFound,
			words: []youtrack.Pair{upstream("upstream_error", "Not Found"), upstream("upstream_message", "Entity with id DEV not found")}},
		{name: "a refusal with an error alone", status: http.StatusForbidden, body: `{"error":"Forbidden"}`, code: youtrack.CodeDenied,
			words: []youtrack.Pair{upstream("upstream_error", "Forbidden")}},
		{name: "a failure of YouTrack", status: http.StatusInternalServerError, body: `{"error":"server_error","error_description":"NPE"}`,
			code:  youtrack.CodeUpstreamFailed,
			words: []youtrack.Pair{upstream("upstream_error", "server_error"), upstream("upstream_message", "NPE")}},
		{name: "a page of a gateway", status: http.StatusBadGateway, body: "<html>Bad Gateway</html>", code: youtrack.CodeUpstreamFailed,
			words: []youtrack.Pair{upstream("upstream_body", "<html>Bad Gateway</html>")}},
		{name: "an error that is no text", status: http.StatusBadRequest, body: `{"error":5}`, code: youtrack.CodeRejected,
			words: []youtrack.Pair{upstream("upstream_body", `{"error":5}`)}},
		{name: "a status of no code of its own", status: http.StatusConflict, body: refused, code: youtrack.CodeUpstreamFailed,
			words: []youtrack.Pair{upstream("upstream_error", "Not Found"), upstream("upstream_message", "Entity with id DEV not found"), upstream("upstream_body", refused)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(tc.status, tc.body))

			err := showDEV(t.Context(), client(t, server))

			details := append([]youtrack.Pair{lastRequest(t, server), {Key: "upstream_status", Value: number(tc.status)}}, tc.words...)
			assert.Equal(t, youtrack.Error{Code: tc.code, Details: details}, errorOf(t, err))
		})
	}
}

func TestClientRefusesAnAnswerThatIsNoJSONUnderAStatusThatCarriesJSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{name: "a page under 200", status: http.StatusOK, body: "<html>YouTrack</html>"},
		{name: "a page under 404", status: http.StatusNotFound, body: "<html>Not Found</html>"},
		{name: "two values under 200", status: http.StatusOK, body: `{} {}`},
		{name: "nothing under 200", status: http.StatusOK, body: ``},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(tc.status, tc.body))

			err := showDEV(t.Context(), client(t, server))

			want := youtrack.Error{Code: youtrack.CodeUpstreamInvalid, Details: []youtrack.Pair{
				lastRequest(t, server),
				{Key: "upstream_status", Value: number(tc.status)},
				{Key: "upstream_body", Value: youtrack.NewString(tc.body)},
			}}
			assert.Equal(t, want, errorOf(t, err))
		})
	}
}

func TestClientTakesWhitespaceAroundTheJSONOfAnAnswer(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, " \t\r\n"+`{"$type":"Project","shortName":"DEV"}`+" \t\r\n"))

	node, err := client(t, server).Projects.Show(t.Context(), "DEV", &youtrack.ShowProjectOptions{Fields: "shortName"})

	require.NoError(t, err)
	assert.Equal(t, youtrack.NewMap(youtrack.Pair{Key: "shortName", Value: youtrack.NewString("DEV")}), node)
}

func breakOff(w http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	if conn, _, err := http.NewResponseController(w).Hijack(); err == nil {
		_ = conn.Close()
	}
}

func answerCutShort(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"$type":`)
	controller := http.NewResponseController(w)
	_ = controller.Flush()
	if conn, _, err := controller.Hijack(); err == nil {
		_ = conn.Close()
	}
}

func deleteDEV1(ctx context.Context, c *youtrack.Client) error {
	_, err := c.Issues.Delete(ctx, "DEV-1")
	return err
}

func TestClientUnwrapsAnAnswerCutShortToTheFailureOfTheTransport(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		serve http.HandlerFunc
		call  func(ctx context.Context, c *youtrack.Client) error
		code  error
	}{
		{name: "a read", serve: answerCutShort, call: showDEV, code: youtrack.ErrUpstreamFailed},
		{name: "a write", serve: fake.InTurn(fake.JSON(http.StatusOK, `{"$type":"Issue","idReadable":"DEV-1"}`), answerCutShort),
			call: deleteDEV1, code: youtrack.ErrWriteUncertain},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.call(t.Context(), client(t, fake.Serve(t, tc.serve)))

			assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
			assert.ErrorIs(t, err, tc.code)
		})
	}
}

func TestSendTellsAWriteThatLeftFromOneThatNeverDid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		server func(t *testing.T) *fake.Server
		want   youtrack.Code
	}{
		{name: "the answer never came", server: func(t *testing.T) *fake.Server { return fake.Serve(t, breakOff) },
			want: youtrack.CodeWriteUncertain},
		{name: "nobody listens", server: func(*testing.T) *fake.Server { return fake.Unreachable() },
			want: youtrack.CodeUpstreamFailed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := tc.server(t)
			c := client(t, server)

			_, err := youtrack.Send(t.Context(), func(ctx context.Context) (*http.Response, error) {
				return c.API().DeleteIssue(ctx, "DEV-1")
			})

			want := youtrack.Error{Code: tc.want, Details: []youtrack.Pair{requestTo(http.MethodDelete, server, "/api/issues/DEV-1")}}
			assert.Equal(t, want, errorOf(t, err))
		})
	}
}
