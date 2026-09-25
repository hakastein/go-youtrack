package youtrack_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/youtrack"
	"github.com/hakastein/youtrack/fake"
)

func TestNewRefusesAnAddressOrATokenItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		address  string
		token    string
		argument string
	}{
		{name: "an address of no scheme", address: "yt.example.org", token: fake.Token, argument: "address"},
		{name: "an address of another scheme", address: "ftp://yt.example.org", token: fake.Token, argument: "address"},
		{name: "an address of no host", address: "https:///youtrack", token: fake.Token, argument: "address"},
		{name: "an address with a query", address: "https://yt.example.org/?a=b", token: fake.Token, argument: "address"},
		{name: "an address with a fragment", address: "https://yt.example.org/#top", token: fake.Token, argument: "address"},
		{name: "an address that is no URL", address: "https://yt.example.org/%zz", token: fake.Token, argument: "address"},
		{name: "an empty token", address: "https://yt.example.org", token: "", argument: "token"},
		{name: "a token with a line break", address: "https://yt.example.org", token: "perm\ntoken", argument: "token"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := youtrack.New(tc.address, tc.token)

			got := argumentErrorOf(t, err)
			assert.Equal(t, tc.argument, got.Argument)
		})
	}
}

func TestClientSendsTheTokenAndAsksForJSON(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `[]`))

	_, err := client(t, server).Users(t.Context(), "", 5)

	require.NoError(t, err)
	sent := server.Last(t)
	assert.Equal(t, "Bearer "+fake.Token, sent.Header.Get("Authorization"))
	assert.Equal(t, "application/json", sent.Header.Get("Accept"))
}

func TestClientReachesTheInstanceUnderAPathOfTheAddress(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `[]`))
	c, err := youtrack.New(server.URL+"/", fake.Token)
	require.NoError(t, err)

	_, err = c.Users(t.Context(), "", 5)

	require.NoError(t, err)
	assert.Equal(t, []string{"/api/users"}, server.Paths())
}

func TestClientDoesNotFollowARedirect(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	})

	_, err := client(t, server).Users(t.Context(), "", 5)

	assert.Equal(t, http.StatusFound, responseErrorOf(t, err).Status)
	assert.Equal(t, []string{"/api/users"}, server.Paths())
}

func TestClientReportsAServerNobodyAnswersAt(t *testing.T) {
	t.Parallel()
	c, err := youtrack.New(fake.NobodyListens, fake.Token)
	require.NoError(t, err)

	_, err = c.Users(t.Context(), "", 5)

	var failed *youtrack.TransportError
	require.ErrorAs(t, err, &failed)
	assert.Equal(t, youtrack.Request{Method: http.MethodGet, URL: fake.NobodyListens + "/api/users?fields=id,login,fullName,email,banned&$top=5&query="}, failed.Request)
	assert.False(t, failed.Written)
	assert.Error(t, failed.Err)
}

func TestClientPassesOnTheWordsOfTheServer(t *testing.T) {
	t.Parallel()
	const refused = `{"error":"Not Found","error_description":"Entity with id DEV-1 not found"}`
	tests := []struct {
		name   string
		status int
		body   string
		want   youtrack.StatusError
	}{
		{name: "a refusal of YouTrack", status: http.StatusNotFound, body: refused,
			want: youtrack.StatusError{Status: http.StatusNotFound, Code: "Not Found", Description: "Entity with id DEV-1 not found", Body: []byte(refused)}},
		{name: "a refusal with an error alone", status: http.StatusForbidden, body: `{"error":"Forbidden"}`,
			want: youtrack.StatusError{Status: http.StatusForbidden, Code: "Forbidden", Body: []byte(`{"error":"Forbidden"}`)}},
		{name: "a failure of YouTrack", status: http.StatusInternalServerError, body: `{"error":"server_error","error_description":"NPE"}`,
			want: youtrack.StatusError{Status: http.StatusInternalServerError, Code: "server_error", Description: "NPE", Body: []byte(`{"error":"server_error","error_description":"NPE"}`)}},
		{name: "a page of a gateway", status: http.StatusBadGateway, body: "<html>Bad Gateway</html>",
			want: youtrack.StatusError{Status: http.StatusBadGateway, Body: []byte("<html>Bad Gateway</html>")}},
		{name: "an error that is no text", status: http.StatusBadRequest, body: `{"error":5}`,
			want: youtrack.StatusError{Status: http.StatusBadRequest, Body: []byte(`{"error":5}`)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(tc.status, tc.body))

			_, err := client(t, server).Issue(t.Context(), "DEV-1", "")

			tc.want.Request = lastRequest(t, server)
			assert.Equal(t, tc.want, statusErrorOf(t, err))
			assert.False(t, tc.want.Uncertain())
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

			_, err := client(t, server).Issue(t.Context(), "DEV-1", "")

			want := youtrack.ResponseError{Request: lastRequest(t, server), Status: tc.status, Body: []byte(tc.body)}
			assert.Equal(t, want, responseErrorOf(t, err))
		})
	}
}

func breakOff(w http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	if conn, _, err := http.NewResponseController(w).Hijack(); err == nil {
		_ = conn.Close()
	}
}

func brokenOffUnder(status int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "10")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, "abc")
	}
}

func cancellingOnArrival(cancel context.CancelFunc) http.HandlerFunc {
	return func(_ http.ResponseWriter, r *http.Request) {
		cancel()
		<-r.Context().Done()
	}
}

func oneFieldProject() string {
	return projectJSON(enumField("1-1", "Field"))
}

func writeOne(ctx context.Context, c *youtrack.Client) error {
	_, err := c.WriteFields(ctx, "DEV-1", []youtrack.FieldWrite{{Name: "Field", Values: []string{"First"}}})
	return err
}

func TestWriteFieldsTellsAWriteThatNeverLeftFromOneTheServerMayHaveActedOn(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		serve   func(t *testing.T, cancel context.CancelFunc) *fake.Server
		written bool
		methods []string
	}{
		{
			name: "the answer never came",
			serve: func(t *testing.T, _ context.CancelFunc) *fake.Server {
				return servingWrite(t, oneFieldProject(), "[]", breakOff)
			},
			written: true,
			methods: []string{http.MethodGet, http.MethodPost},
		},
		{
			name: "the write never left",
			serve: func(t *testing.T, _ context.CancelFunc) *fake.Server {
				var server *fake.Server
				server = fake.ServeAlone(t, func(w http.ResponseWriter, r *http.Request) {
					server.StopListening(t)
					fake.JSON(http.StatusOK, issueToWriteJSON(oneFieldProject(), "[]"))(w, r)
				})
				return server
			},
			written: false,
			methods: []string{http.MethodGet},
		},
		{
			name: "the call was cancelled after the write was sent",
			serve: func(t *testing.T, cancel context.CancelFunc) *fake.Server {
				return servingWrite(t, oneFieldProject(), "[]", cancellingOnArrival(cancel))
			},
			written: true,
			methods: []string{http.MethodGet, http.MethodPost},
		},
		{
			name: "the answer to the write broke off under 200",
			serve: func(t *testing.T, _ context.CancelFunc) *fake.Server {
				return servingWrite(t, oneFieldProject(), "[]", brokenOffUnder(http.StatusOK))
			},
			written: true,
			methods: []string{http.MethodGet, http.MethodPost},
		},
		{
			name: "the answer to the write broke off under 400",
			serve: func(t *testing.T, _ context.CancelFunc) *fake.Server {
				return servingWrite(t, oneFieldProject(), "[]", brokenOffUnder(http.StatusBadRequest))
			},
			written: false,
			methods: []string{http.MethodGet, http.MethodPost},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			server := tc.serve(t, cancel)

			err := writeOne(ctx, client(t, server))

			var failed *youtrack.TransportError
			require.ErrorAs(t, err, &failed)
			assert.Equal(t, tc.written, failed.Written)
			assert.Equal(t, tc.methods, server.Methods())
		})
	}
}

func TestWriteFieldsTellsAFailureOfYouTrackFromAnAnswerOfSomethingElse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		status    int
		body      string
		uncertain bool
		accepted  bool
	}{
		{name: "YouTrack failed the write", status: http.StatusInternalServerError, body: `{"error":"server_error","error_description":"NPE"}`},
		{name: "something else answered the write with 502", status: http.StatusBadGateway, body: "<html>Bad Gateway</html>", uncertain: true},
		{name: "something else answered the write with JSON that names no error", status: http.StatusServiceUnavailable, body: `{"status":"down"}`, uncertain: true},
		{name: "YouTrack refused the write", status: http.StatusBadRequest, body: `{"error":"bad_request","error_description":"Bad Request"}`},
		{name: "YouTrack took the write and answered 201", status: http.StatusCreated, body: `{}`, accepted: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := servingWrite(t, oneFieldProject(), "[]", fake.JSON(tc.status, tc.body))

			err := writeOne(t.Context(), client(t, server))

			got := statusErrorOf(t, err)
			assert.Equal(t, tc.status, got.Status)
			assert.True(t, got.Write)
			assert.Equal(t, tc.uncertain, got.Uncertain())
			assert.Equal(t, tc.accepted, got.Accepted())
			assert.Equal(t, lastRequest(t, server), got.Request)
		})
	}
}

func TestWriteFieldsRefusesAnAnswerToTheWriteThatIsNoJSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		status int
	}{
		{name: "under 200", status: http.StatusOK},
		{name: "under 404", status: http.StatusNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := servingWrite(t, oneFieldProject(), "[]", fake.JSON(tc.status, "<html/>"))

			err := writeOne(t.Context(), client(t, server))

			want := youtrack.ResponseError{Request: lastRequest(t, server), Status: tc.status, Body: []byte("<html/>"), Write: true}
			assert.Equal(t, want, responseErrorOf(t, err))
		})
	}
}

func TestSendTellsAWriteThatNeverLeftFromOneTheServerMayHaveActedOn(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, breakOff)
	c := client(t, server)

	_, err := youtrack.Send(t.Context(), func(ctx context.Context) (*http.Response, error) {
		return c.API().DeleteIssue(ctx, "DEV-1")
	})

	var failed *youtrack.TransportError
	require.ErrorAs(t, err, &failed)
	assert.True(t, failed.Written)
	assert.Equal(t, requestTo(http.MethodDelete, server, "/api/issues/DEV-1"), failed.Request)
	assert.True(t, errors.Is(err, failed.Err))
}

func TestStatusErrorReadsAsTheStatusAndTheWordsOfTheServer(t *testing.T) {
	t.Parallel()
	err := youtrack.StatusError{Request: youtrack.Request{Method: "GET", URL: "https://yt/api/issues/DEV-1"},
		Status: http.StatusNotFound, Code: "Not Found", Description: "Entity with id DEV-1 not found"}

	assert.Equal(t, "GET https://yt/api/issues/DEV-1: status "+strconv.Itoa(http.StatusNotFound)+" Not Found: Entity with id DEV-1 not found", err.Error())
}
