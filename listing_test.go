package youtrack_test

import (
	"fmt"
	"math"
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

func projectsAnswer(ids ...int) string {
	records := make([]string, 0, len(ids))
	for _, id := range ids {
		records = append(records, fmt.Sprintf(`{"$type":"Project","id":"0-%d"}`, id))
	}
	return "[" + strings.Join(records, ",") + "]"
}

func pagedProjects(t *testing.T, records int) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		top, err := strconv.Atoi(query.Get("$top"))
		if !assert.NoError(t, err, "$top of %s", r.URL) {
			return
		}
		skip := 0
		if query.Has("$skip") {
			skip, err = strconv.Atoi(query.Get("$skip"))
			if !assert.NoError(t, err, "$skip of %s", r.URL) {
				return
			}
		}
		end := records
		if top >= 0 {
			end = min(records, skip+top)
		}
		var ids []int
		for id := skip; id < end; id++ {
			ids = append(ids, id)
		}
		fake.JSON(http.StatusOK, projectsAnswer(ids...))(w, r)
	}
}

func projectPageAndCount(page, count string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("$top") == "-1" {
			fake.JSON(http.StatusOK, count)(w, r)
			return
		}
		fake.JSON(http.StatusOK, page)(w, r)
	}
}

func pageOfProjects(total int, truncated bool, ids ...int) *youtrack.Node {
	records := make([]*youtrack.Node, 0, len(ids))
	for _, id := range ids {
		records = append(records, youtrack.NewMap(youtrack.Pair{Key: "id", Value: youtrack.NewString(fmt.Sprintf("0-%d", id))}))
	}
	return page("projects", number(total), youtrack.NewBool(truncated), records...)
}

func pageSent(top string, skip ...string) url.Values {
	sent := url.Values{"fields": {"id"}, "$top": {top}}
	if len(skip) > 0 {
		sent["$skip"] = skip
	}
	return sent
}

var projectsCounted = pageSent("-1")

func TestListProjectsRefusesAPageItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		page youtrack.Page
	}{
		{name: "a negative limit", page: youtrack.Page{Limit: -1}},
		{name: "a limit past the largest int32", page: youtrack.Page{Limit: math.MaxInt32 + 1}},
		{name: "a negative skip", page: youtrack.Page{Limit: 1, Skip: -1}},
		{name: "a skip past the largest int32", page: youtrack.Page{Limit: 1, Skip: math.MaxInt32 + 1}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := client(t, fake.ServeNothing(t)).Projects.List(t.Context(), &youtrack.ListProjectsOptions{Fields: "id", Page: tc.page})

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

func TestListProjectsPrintsThePageTheLimitAndTheSkipAskFor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		records int
		page    youtrack.Page
		want    *youtrack.Node
		sent    []url.Values
	}{
		{
			name:    "a first page the projects fill",
			records: 3,
			page:    youtrack.Page{Limit: 2},
			want:    pageOfProjects(3, true, 0, 1),
			sent:    []url.Values{pageSent("2"), projectsCounted},
		},
		{
			name:    "a page in the middle",
			records: 5,
			page:    youtrack.Page{Limit: 2, Skip: 2},
			want:    pageOfProjects(5, true, 2, 3),
			sent:    []url.Values{pageSent("2", "2"), projectsCounted},
		},
		{
			name:    "a last page the projects fill",
			records: 4,
			page:    youtrack.Page{Limit: 2, Skip: 2},
			want:    pageOfProjects(4, false, 2, 3),
			sent:    []url.Values{pageSent("2", "2"), projectsCounted},
		},
		{
			name:    "a last page short of the limit",
			records: 5,
			page:    youtrack.Page{Limit: 2, Skip: 4},
			want:    pageOfProjects(5, false, 4),
			sent:    []url.Values{pageSent("2", "4")},
		},
		{
			name:    "an empty first page",
			records: 0,
			page:    youtrack.Page{Limit: 2},
			want:    pageOfProjects(0, false),
			sent:    []url.Values{pageSent("2")},
		},
		{
			name:    "an empty page past the end",
			records: 5,
			page:    youtrack.Page{Limit: 2, Skip: 9},
			want:    pageOfProjects(5, false),
			sent:    []url.Values{pageSent("2", "9"), projectsCounted},
		},
		{
			name:    "no limit, which is the default one",
			records: 1,
			page:    youtrack.Page{},
			want:    pageOfProjects(1, false, 0),
			sent:    []url.Values{pageSent("50")},
		},
		{
			name:    "the largest limit",
			records: 1,
			page:    youtrack.Page{Limit: math.MaxInt32},
			want:    pageOfProjects(1, false, 0),
			sent:    []url.Values{pageSent("2147483647")},
		},
		{
			name:    "the largest skip",
			records: 1,
			page:    youtrack.Page{Limit: 1, Skip: math.MaxInt32},
			want:    pageOfProjects(1, false),
			sent:    []url.Values{pageSent("1", "2147483647"), projectsCounted},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, pagedProjects(t, tc.records))
			node, err := client(t, server).Projects.List(t.Context(), &youtrack.ListProjectsOptions{Fields: "id", Page: tc.page})

			require.NoError(t, err)
			assert.Equal(t, tc.want, node)
			assert.Equal(t, tc.sent, server.Queries())
		})
	}
}

func TestListProjectsRefusesAPageTheCollectionCannotHold(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		handler http.HandlerFunc
		page    youtrack.Page
		want    youtrack.Error
	}{
		{
			name:    "fewer projects counted than the skip and the page hold",
			handler: projectPageAndCount(projectsAnswer(2, 3), projectsAnswer(0, 1, 2)),
			page:    youtrack.Page{Limit: 2, Skip: 2},
			want: youtrack.Error{Code: youtrack.CodeUpstreamFailed, Details: []youtrack.Pair{
				{Key: "total", Value: number(3)},
				{Key: "returned", Value: number(2)},
			}},
		},
		{
			name:    "more projects than the limit",
			handler: fake.JSON(http.StatusOK, projectsAnswer(0, 1)),
			page:    youtrack.Page{Limit: 1},
			want: youtrack.Error{Code: youtrack.CodeUpstreamInvalid, Details: []youtrack.Pair{
				{Key: "limit", Value: number(1)},
				{Key: "returned", Value: number(2)},
			}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, tc.handler)
			_, err := client(t, server).Projects.List(t.Context(), &youtrack.ListProjectsOptions{Fields: "id", Page: tc.page})

			assert.Equal(t, tc.want, errorOf(t, err))
		})
	}
}

func TestListProjectsRefusesAnAnswerThatIsNoListOfObjects(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		answer string
	}{
		{name: "an object", answer: `{"$type":"Project","id":"0-1"}`},
		{name: "a list holding null", answer: `[null]`},
		{name: "a list holding a list after a project", answer: `[{"$type":"Project","id":"0-1"},[]]`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tc.answer))
			_, err := client(t, server).Projects.List(t.Context(), &youtrack.ListProjectsOptions{Fields: "id", Page: youtrack.Page{Limit: 2}})

			want := youtrack.Error{Code: youtrack.CodeUpstreamInvalid, Details: []youtrack.Pair{
				{Key: "request", Value: youtrack.NewString("GET " + server.URL + "/api/admin/projects?fields=id&$top=2")},
				{Key: "upstream_status", Value: number(http.StatusOK)},
				{Key: "upstream_body", Value: youtrack.NewString(tc.answer)},
			}}
			assert.Equal(t, want, errorOf(t, err))
		})
	}
}
