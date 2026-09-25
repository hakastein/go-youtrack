package youtrack_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/youtrack"
	"github.com/hakastein/youtrack/fake"
)

func TestUsersSendsTheQueryAsWrittenWithTheLimit(t *testing.T) {
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

			_, err := client(t, server).Users(t.Context(), tc.query, 7)

			require.NoError(t, err)
			sent := server.Last(t)
			assert.Equal(t, "/api/users", sent.URL.Path)
			assert.Equal(t, []string{tc.query}, sent.URL.Query()["query"])
			assert.Equal(t, []string{"7"}, sent.URL.Query()["$top"])
			assert.Equal(t, []string{"id,login,fullName,email,banned"}, sent.URL.Query()["fields"])
		})
	}
}

func TestUsersReadsTheUsersTheServerFound(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `[`+
		`{"$type":"User","id":"1-1","login":"first","fullName":"First Last","email":"first@example.org","banned":false},`+
		`{"$type":"User","id":"1-2","login":"second","fullName":"Second","email":null,"banned":true}]`))

	users, err := client(t, server).Users(t.Context(), "", 7)

	require.NoError(t, err)
	assert.Equal(t, []youtrack.User{
		{ID: "1-1", Login: "first", FullName: "First Last", Email: "first@example.org"},
		{ID: "1-2", Login: "second", FullName: "Second", Banned: true},
	}, users)
}

func TestUsersRefusesALimitItCannotSend(t *testing.T) {
	t.Parallel()
	for _, limit := range []int{0, -1, 1 << 31} {
		t.Run(strconv.Itoa(limit), func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			_, err := client(t, server).Users(t.Context(), "", limit)

			assert.Equal(t, youtrack.ArgumentError{Argument: "limit", Value: strconv.Itoa(limit)}, argumentErrorOf(t, err))
		})
	}
}

func TestUsersRefusesUsersOfAnotherShape(t *testing.T) {
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

			_, err := client(t, server).Users(t.Context(), "", 7)

			assert.Equal(t, invalidAnswer(lastRequest(t, server), tc.body), responseErrorOf(t, err))
		})
	}
}
