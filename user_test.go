package youtrack_test

import (
	"context"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/hakastein/youtrack"
	"github.com/hakastein/youtrack/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const userFieldsSent = "id,login,fullName,email,banned"

func TestShowUserReadsTheDefaultFields(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK,
		`{"$type":"User","login":"first","fullName":"First Last","email":"first@example.org","banned":false}`))

	node, err := client(t, server).Users.Show(t.Context(), "first", nil)

	require.NoError(t, err)
	assert.Equal(t, youtrack.NewMap(
		youtrack.Pair{Key: "login", Value: youtrack.NewString("first")},
		youtrack.Pair{Key: "fullName", Value: youtrack.NewString("First Last")},
		youtrack.Pair{Key: "email", Value: youtrack.NewString("first@example.org")},
		youtrack.Pair{Key: "banned", Value: youtrack.NewBool(false)},
	), node)
	assert.Equal(t, requestTo(http.MethodGet, server, "/api/users/first?fields=login,fullName,email,banned"), lastRequest(t, server))
}

func TestShowUserSendsALoginAsOneSegment(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		login   string
		escaped string
	}{
		{name: "a slash", login: "a/b", escaped: "a%2Fb"},
		{name: "me in upper case", login: "ME", escaped: "ME"},
		{name: "an internal id with a letter after it", login: "2-1x", escaped: "2-1x"},
		{name: "hex digits as many as the first group of a Hub id", login: "deadbeef", escaped: "deadbeef"},
		{name: "a Hub id without the dashes", login: "7fae4e4101f842c09cc4960c478d8a72", escaped: "7fae4e4101f842c09cc4960c478d8a72"},
		{name: "a Hub id one digit short", login: "7fae4e41-01f8-42c0-9cc4-960c478d8a7", escaped: "7fae4e41-01f8-42c0-9cc4-960c478d8a7"},
		{name: "a Hub id with a letter past f", login: "7fae4e41-01f8-42c0-9cc4-960c478d8a7g", escaped: "7fae4e41-01f8-42c0-9cc4-960c478d8a7g"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"User","login":"first"}`))

			_, err := client(t, server).Users.Show(t.Context(), tc.login, &youtrack.ShowUserOptions{Fields: "login"})

			require.NoError(t, err)
			assert.Equal(t, "/api/users/"+tc.escaped, server.Last(t).URL.EscapedPath())
		})
	}
}

func TestShowUserRefusesEveryFormTheServerReadsAsSomethingOtherThanALogin(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		login string
	}{
		{name: "empty", login: ""},
		{name: "a dot", login: "."},
		{name: "two dots", login: ".."},
		{name: "a full name", login: "First Last"},
		{name: "an internal id", login: "2-1"},
		{name: "a Hub id", login: "7fae4e41-01f8-42c0-9cc4-960c478d8a72"},
		{name: "a Hub id in upper case", login: "7FAE4E41-01F8-42C0-9CC4-960C478D8A72"},
		{name: "me", login: "me"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := client(t, fake.ServeNothing(t)).Users.Show(t.Context(), tc.login, &youtrack.ShowUserOptions{Fields: "login"})

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

func TestShowUserRefusesANameNoSchemaOfItsNodeDeclares(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `{"login":"leader","$type":"User"}`))

	_, err := client(t, server).Users.Show(t.Context(), "leader", &youtrack.ShowUserOptions{Fields: "login,logn"})

	want := youtrack.Error{Code: youtrack.CodeUnknownName, Details: []youtrack.Pair{
		requestTo(http.MethodGet, server, "/api/users/leader?fields=login,logn"),
		{Key: "fields", Value: youtrack.NewString("login,logn")},
		{Key: "unknown", Value: youtrack.NewList(withNearest("field", "logn", "login"))},
	}}
	assert.Equal(t, want, errorOf(t, err))
}

func TestShowUserKeepsWhatTheServerSaidOfALoginItLacks(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusNotFound, `{"error":"Not Found","error_description":"Entity with id nobody not found"}`))

	_, err := client(t, server).Users.Show(t.Context(), "nobody", &youtrack.ShowUserOptions{Fields: "login"})

	want := youtrack.Error{Code: youtrack.CodeNotFound, Details: []youtrack.Pair{
		requestTo(http.MethodGet, server, "/api/users/nobody?fields=login"),
		{Key: "upstream_status", Value: number(http.StatusNotFound)},
		{Key: "upstream_error", Value: youtrack.NewString("Not Found")},
		{Key: "upstream_message", Value: youtrack.NewString("Entity with id nobody not found")},
	}}
	assert.Equal(t, want, errorOf(t, err))
}

func TestListUsersReadsTheDefaultFields(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `[{"$type":"User","login":"first","fullName":"First Last","banned":true}]`))

	node, err := client(t, server).Users.List(t.Context(), "fir", nil)

	require.NoError(t, err)
	assert.Equal(t, wholePage("users", youtrack.NewMap(
		youtrack.Pair{Key: "login", Value: youtrack.NewString("first")},
		youtrack.Pair{Key: "fullName", Value: youtrack.NewString("First Last")},
		youtrack.Pair{Key: "banned", Value: youtrack.NewBool(true)},
	)), node)
	assert.Equal(t, []url.Values{{"fields": {"login,fullName,banned"}, "$top": {"50"}, "query": {"fir"}}}, server.Queries())
}

func TestListUsersSendsTheSearchAsWrittenWithThePageAndTheCount(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		search string
	}{
		{name: "an empty search", search: ""},
		{name: "spaces around a name", search: "  First Last  "},
		{name: "characters a query escapes", search: "a&b=c+d%#"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, `[{"$type":"User","id":"1-1","login":"first"}]`))

			_, err := client(t, server).Users.List(t.Context(), tc.search, &youtrack.ListUsersOptions{Fields: "login", Page: youtrack.Page{Limit: 1}})

			require.NoError(t, err)
			assert.Equal(t, []url.Values{
				{"fields": {"login"}, "$top": {"1"}, "query": {tc.search}},
				{"fields": {"id"}, "$top": {"-1"}, "query": {tc.search}},
			}, server.Queries())
		})
	}
}

func TestUsersRefuseASearchThatIsNoUTF8(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		search func(ctx context.Context, users *youtrack.UsersService, query string) error
		query  string
	}{
		{name: "a list of a byte that is no UTF-8", search: usersListed, query: "\xff"},
		{name: "a list of a truncated sequence inside a name", search: usersListed, query: "Fir\xc3\x28"},
		{name: "a find of a byte that is no UTF-8", search: usersFound, query: "\xff"},
		{name: "a find of a truncated sequence inside a name", search: usersFound, query: "Fir\xc3\x28"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.search(t.Context(), client(t, fake.ServeNothing(t)).Users, tc.query)

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

func usersListed(ctx context.Context, users *youtrack.UsersService, query string) error {
	_, err := users.List(ctx, query, nil)
	return err
}

func usersFound(ctx context.Context, users *youtrack.UsersService, query string) error {
	_, err := users.Find(ctx, query, 0)
	return err
}

func TestMeReadsTheOwnerOfTheToken(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK,
		`{"$type":"Me","id":"1-1","login":"first","fullName":"First Last","email":"first@example.org","banned":false}`))

	user, err := client(t, server).Users.Me(t.Context())

	require.NoError(t, err)
	assert.Equal(t, &youtrack.User{ID: "1-1", Login: "first", FullName: "First Last", Email: "first@example.org"}, user)
	assert.Equal(t, requestTo(http.MethodGet, server, "/api/users/me?fields="+userFieldsSent), lastRequest(t, server))
}

func TestMeRefusesAnOwnerOfAnotherShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
	}{
		{name: "a list where an object is", body: `[{"$type":"Me","id":"1-1","login":"first","fullName":"First","email":null,"banned":false}]`},
		{name: "a full name that is null", body: `{"$type":"Me","id":"1-1","login":"first","fullName":null,"email":null,"banned":false}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tc.body))

			_, err := client(t, server).Users.Me(t.Context())

			assert.Equal(t, unreadable(lastRequest(t, server), tc.body), errorOf(t, err))
		})
	}
}

func TestFindUsersSendsTheQueryAsWrittenWithTheLimit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		query string
	}{
		{name: "an empty query", query: ""},
		{name: "spaces around a name", query: "  First Last  "},
		{name: "characters a query escapes", query: "a&b=c+d%#"},
		{name: "cyrillic", query: "Колесов"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, `[]`))

			_, err := client(t, server).Users.Find(t.Context(), tc.query, 7)

			require.NoError(t, err)
			assert.Equal(t, []string{"/api/users"}, server.Paths())
			assert.Equal(t, []url.Values{{"fields": {userFieldsSent}, "$top": {"7"}, "query": {tc.query}}}, server.Queries())
		})
	}
}

func TestFindUsersTakesTheDefaultLimitForNone(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `[]`))

	_, err := client(t, server).Users.Find(t.Context(), "", 0)

	require.NoError(t, err)
	assert.Equal(t, []url.Values{{"fields": {userFieldsSent}, "$top": {"50"}, "query": {""}}}, server.Queries())
}

func TestFindUsersReadsTheUsersTheServerFound(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `[`+
		`{"$type":"User","id":"1-1","login":"first","fullName":"First Last","email":"first@example.org","banned":false},`+
		`{"$type":"User","id":"1-2","login":"second","fullName":"Second","email":null,"banned":true}]`))

	users, err := client(t, server).Users.Find(t.Context(), "", 7)

	require.NoError(t, err)
	assert.Equal(t, []youtrack.User{
		{ID: "1-1", Login: "first", FullName: "First Last", Email: "first@example.org"},
		{ID: "1-2", Login: "second", FullName: "Second", Banned: true},
	}, users)
}

func TestFindUsersRefusesALimitItCannotSend(t *testing.T) {
	t.Parallel()
	for _, limit := range []int{-1, math.MaxInt32 + 1} {
		t.Run(strconv.Itoa(limit), func(t *testing.T) {
			t.Parallel()
			_, err := client(t, fake.ServeNothing(t)).Users.Find(t.Context(), "", limit)

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

func TestFindUsersRefusesMoreUsersThanTheLimit(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `[`+
		`{"$type":"User","id":"1-1","login":"first","fullName":"First","email":null,"banned":false},`+
		`{"$type":"User","id":"1-2","login":"second","fullName":"Second","email":null,"banned":false}]`))

	_, err := client(t, server).Users.Find(t.Context(), "", 1)

	assert.Equal(t, youtrack.Error{Code: youtrack.CodeUpstreamInvalid, Details: []youtrack.Pair{
		lastRequest(t, server),
		{Key: "limit", Value: number(1)},
		{Key: "returned", Value: number(2)},
	}}, errorOf(t, err))
}

func TestFindUsersRefusesUsersOfAnotherShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
	}{
		{name: "an object where a list is", body: `{"$type":"User","id":"1-1","login":"first","fullName":"First","email":null,"banned":false}`},
		{name: "a user that is no object", body: `[5]`},
		{name: "a login that is no text", body: `[{"$type":"User","id":"1-1","login":5,"fullName":"First","email":null,"banned":false}]`},
		{name: "an email that is a number", body: `[{"$type":"User","id":"1-1","login":"first","fullName":"First","email":5,"banned":false}]`},
		{name: "a ban that is no bool", body: `[{"$type":"User","id":"1-1","login":"first","fullName":"First","email":null,"banned":"yes"}]`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tc.body))

			_, err := client(t, server).Users.Find(t.Context(), "", 7)

			assert.Equal(t, unreadable(lastRequest(t, server), tc.body), errorOf(t, err))
		})
	}
}
