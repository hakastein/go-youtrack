// Package fake is a YouTrack server for tests: it journals every request and answers with the handler a test gives it.
package fake

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const Token = "perm-ytrack-test-token"

// Port 1 is tcpmux, which nothing on a test machine serves.
const NobodyListens = "http://127.0.0.1:1"

type Server struct {
	URL string
	// Origin is where a path the server answers with resolves: the test's prefix is not part of it.
	Origin string

	alone    *httptest.Server
	mu       sync.Mutex
	received []Request
}

type Request struct {
	Method           string
	URL              *url.URL
	Header           http.Header
	ContentLength    int64
	TransferEncoding []string
	Body             string
}

var (
	shared   = sync.OnceValue(startShared)
	handlers sync.Map
	lastTest atomic.Int64
)

func startShared() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prefix, _, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
		handler, served := handlers.Load("/" + prefix)
		if !served {
			http.Error(w, "no test serves "+r.URL.Path, http.StatusNotFound)
			return
		}
		http.StripPrefix("/"+prefix, handler.(http.Handler)).ServeHTTP(w, r)
	}))
}

func Serve(t *testing.T, handler http.HandlerFunc) *Server {
	t.Helper()
	prefix := "/t" + strconv.FormatInt(lastTest.Add(1), 10)
	s := &Server{URL: shared().URL + prefix, Origin: shared().URL}
	handlers.Store(prefix, s.journaling(t, handler))
	t.Cleanup(func() { handlers.Delete(prefix) })
	return s
}

// ServeAlone is for a test that stops listening between two requests: the shared listener cannot.
func ServeAlone(t *testing.T, handler http.HandlerFunc) *Server {
	t.Helper()
	s := &Server{}
	s.alone = httptest.NewUnstartedServer(s.journaling(t, handler))
	s.alone.Config.SetKeepAlivesEnabled(false)
	s.alone.Start()
	t.Cleanup(s.alone.Close)
	s.URL = s.alone.URL
	s.Origin = s.alone.URL
	return s
}

func (s *Server) StopListening(t *testing.T) {
	t.Helper()
	require.NotNil(t, s.alone, "only a server of ServeAlone stops listening")
	address := s.alone.Listener.Addr().String()
	assert.NoError(t, s.alone.Listener.Close())
	assert.Eventually(t, func() bool { return dialRefused(address) }, time.Minute, time.Millisecond,
		"the kernel still completes handshakes on %s after Close", address)
}

func dialRefused(address string) bool {
	conn, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		return true
	}
	_ = conn.Close()
	return false
}

func Unreachable() *Server {
	return &Server{URL: NobodyListens, Origin: NobodyListens}
}

func ServeNothing(t *testing.T) *Server {
	t.Helper()
	return Serve(t, Unexpected(t))
}

func Unexpected(t *testing.T) http.HandlerFunc {
	return func(_ http.ResponseWriter, r *http.Request) {
		assert.Fail(t, "a request reached the server", "%s %s", r.Method, r.URL)
	}
}

func (s *Server) journaling(t *testing.T, handler http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err, "reading the body of %s %s", r.Method, r.URL)
		s.mu.Lock()
		s.received = append(s.received, Request{
			Method:           r.Method,
			URL:              new(*r.URL),
			Header:           r.Header.Clone(),
			ContentLength:    r.ContentLength,
			TransferEncoding: r.TransferEncoding,
			Body:             string(body),
		})
		s.mu.Unlock()
		r.Body = io.NopCloser(bytes.NewReader(body))
		handler(w, r)
	})
}

func (s *Server) Env() []string {
	return []string{"YTRACK_URL=" + s.URL, "YTRACK_TOKEN=" + Token}
}

func (s *Server) Address(t *testing.T) *url.URL {
	t.Helper()
	address, err := url.Parse(s.URL)
	require.NoError(t, err)
	return address
}

func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.received)
}

func (s *Server) Request(t *testing.T, n int) Request {
	t.Helper()
	received := s.Requests()
	require.Greater(t, len(received), n, "the server was sent no request %d", n)
	return received[n]
}

func (s *Server) Last(t *testing.T) Request {
	t.Helper()
	received := s.Requests()
	require.NotEmpty(t, received, "no request reached the server")
	return received[len(received)-1]
}

func (s *Server) LastJSON(t *testing.T) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(s.Last(t).Body), &body))
	return body
}

func (s *Server) Methods() []string {
	var methods []string
	for _, r := range s.Requests() {
		methods = append(methods, r.Method)
	}
	return methods
}

func (s *Server) Routes() []string {
	var routes []string
	for _, r := range s.Requests() {
		routes = append(routes, r.Method+" "+r.URL.Path)
	}
	return routes
}

func (s *Server) Paths() []string {
	var paths []string
	for _, r := range s.Requests() {
		paths = append(paths, r.URL.Path)
	}
	return paths
}

func (s *Server) Fields() []string {
	var fields []string
	for _, r := range s.Requests() {
		fields = append(fields, r.URL.Query().Get("fields"))
	}
	return fields
}

func (s *Server) Queries() []url.Values {
	var queries []url.Values
	for _, r := range s.Requests() {
		queries = append(queries, r.URL.Query())
	}
	return queries
}

func (s *Server) Targets(t *testing.T) []string {
	t.Helper()
	var targets []string
	for _, r := range s.Requests() {
		query, err := url.QueryUnescape(r.URL.RawQuery)
		require.NoError(t, err, "the query of %s %s", r.Method, r.URL)
		targets = append(targets, r.URL.Path+"?"+query)
	}
	return targets
}

func (s *Server) Bodies() []string {
	var bodies []string
	for _, r := range s.Requests() {
		bodies = append(bodies, r.Body)
	}
	return bodies
}

func JSON(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

const AssistPath = "/api/search/assist"

func Searching(t *testing.T, handler http.HandlerFunc) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != AssistPath {
			handler(w, r)
			return
		}
		JSON(http.StatusOK, Markup(t, searchOf(t, r)))(w, r)
	}
}

func searchOf(t *testing.T, r *http.Request) string {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	assert.NoError(t, err)
	var asked struct {
		Query string `json:"query"`
	}
	assert.NoError(t, json.Unmarshal(body, &asked), "the body of %s %s: %s", r.Method, r.URL, body)
	return asked.Query
}

func Markup(t *testing.T, query string, ranges ...string) string {
	t.Helper()
	echoed, err := json.Marshal(query)
	assert.NoError(t, err)
	return `{"$type":"SearchSuggestions","query":` + string(echoed) + `,"styleRanges":[` + strings.Join(ranges, ",") + `]}`
}

func StyleRange(startInUTF16, lengthInUTF16 int, style string) string {
	return fmt.Sprintf(`{"$type":"SearchStyleRange","start":%d,"length":%d,"style":%q}`,
		startInUTF16, lengthInUTF16, style)
}

func InTurn(answers ...http.HandlerFunc) http.HandlerFunc {
	var served atomic.Int64
	return func(w http.ResponseWriter, r *http.Request) {
		answers[min(int(served.Add(1)), len(answers))-1](w, r)
	}
}
