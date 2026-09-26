package youtrack_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/go-youtrack"
	"github.com/hakastein/go-youtrack/fake"
)

func projectReading(expression string) func(context.Context, *youtrack.Client) (*youtrack.Node, error) {
	return func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
		return c.Projects.Show(ctx, "DEV", &youtrack.ShowProjectOptions{Fields: expression})
	}
}

func TestAnAddressOfTheInstanceIsResolvedFromTheAddressOfTheClient(t *testing.T) {
	t.Parallel()
	linked := func(key string, link *youtrack.Node) *youtrack.Node {
		return youtrack.NewList(youtrack.NewMap(youtrack.Pair{Key: key, Value: link}))
	}
	tests := []struct {
		name string
		read func(context.Context, *youtrack.Client) (*youtrack.Node, error)
		body string
		want func(origin string) *youtrack.Node
	}{
		{
			name: "an object of no declared schema the server named an attachment",
			read: projectReading("customFields(url)"),
			body: `{"$type":"Project","customFields":[{"$type":"IssueAttachment","url":"/api/files/12-2?sign=s"}]}`,
			want: func(origin string) *youtrack.Node {
				return youtrack.NewMap(youtrack.Pair{Key: "customFields",
					Value: linked("url", youtrack.NewString(origin+"/api/files/12-2?sign=s"))})
			},
		},
		{
			name: "an object of no declared schema the server named nothing",
			read: projectReading("customFields(url)"),
			body: `{"$type":"Project","customFields":[{"url":"/api/files/12-2?sign=s"}]}`,
			want: func(string) *youtrack.Node {
				return youtrack.NewMap(youtrack.Pair{Key: "customFields",
					Value: linked("url", youtrack.NewString("/api/files/12-2?sign=s"))})
			},
		},
		{
			name: "the avatar of the user a call reads",
			read: func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
				return c.Users.Show(ctx, "first", &youtrack.ShowUserOptions{Fields: "avatarUrl"})
			},
			body: `{"$type":"User","avatarUrl":"/avatar/1"}`,
			want: func(origin string) *youtrack.Node {
				return youtrack.NewMap(youtrack.Pair{Key: "avatarUrl", Value: youtrack.NewString(origin + "/avatar/1")})
			},
		},
		{
			name: "the icon of a project",
			read: projectReading("iconUrl"),
			body: `{"$type":"Project","iconUrl":"/api/entityIcons/0-3"}`,
			want: func(origin string) *youtrack.Node {
				return youtrack.NewMap(youtrack.Pair{Key: "iconUrl", Value: youtrack.NewString(origin + "/api/entityIcons/0-3")})
			},
		},
		{
			name: "no icon",
			read: projectReading("iconUrl"),
			body: `{"$type":"Project","iconUrl":null}`,
			want: func(string) *youtrack.Node {
				return youtrack.NewMap(youtrack.Pair{Key: "iconUrl", Value: youtrack.NewNull()})
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tc.body))

			node, err := tc.read(t.Context(), client(t, server))

			require.NoError(t, err)
			assert.Equal(t, tc.want(server.Origin), node)
		})
	}
}

func TestAnAddressOfTheInstanceLeavesOutTheUserOfTheAddressOfTheClient(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"Project","iconUrl":"/api/entityIcons/0-3"}`))
	address := server.Address(t)
	address.User = url.UserPassword("alice", "secret")
	c, err := youtrack.NewClient(address.String(), fake.Token)
	require.NoError(t, err)

	node, err := projectReading("iconUrl")(t.Context(), c)

	require.NoError(t, err)
	assert.Equal(t, youtrack.NewMap(youtrack.Pair{Key: "iconUrl", Value: youtrack.NewString(server.Origin + "/api/entityIcons/0-3")}), node)
}

func TestShowProjectRefusesAnAddressOfTheInstanceThatIsNoAbsolutePath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		field    string
		answer   string
		received *youtrack.Node
	}{
		{
			name:     "a relative path",
			field:    "iconUrl",
			answer:   `{"$type":"Project","iconUrl":"icon/1"}`,
			received: youtrack.NewString("icon/1"),
		},
		{
			name:     "an empty string",
			field:    "iconUrl",
			answer:   `{"$type":"Project","iconUrl":""}`,
			received: youtrack.NewString(""),
		},
		{
			name:     "another authority",
			field:    "iconUrl",
			answer:   `{"$type":"Project","iconUrl":"//elsewhere/icon/1"}`,
			received: youtrack.NewString("//elsewhere/icon/1"),
		},
		{
			name:     "another scheme",
			field:    "iconUrl",
			answer:   `{"$type":"Project","iconUrl":"https://elsewhere/icon/1"}`,
			received: youtrack.NewString("https://elsewhere/icon/1"),
		},
		{
			name:     "a scheme without a host",
			field:    "iconUrl",
			answer:   `{"$type":"Project","iconUrl":"https:/icon/1"}`,
			received: youtrack.NewString("https:/icon/1"),
		},
		{
			name:     "a user without a host",
			field:    "iconUrl",
			answer:   `{"$type":"Project","iconUrl":"//first@/icon/1"}`,
			received: youtrack.NewString("//first@/icon/1"),
		},
		{
			name:     "an opaque reference",
			field:    "iconUrl",
			answer:   `{"$type":"Project","iconUrl":"mailto:first@example.com"}`,
			received: youtrack.NewString("mailto:first@example.com"),
		},
		{
			name:     "a broken escape",
			field:    "iconUrl",
			answer:   `{"$type":"Project","iconUrl":"/icon/%zz"}`,
			received: youtrack.NewString("/icon/%zz"),
		},
		{
			name:     "a number",
			field:    "iconUrl",
			answer:   `{"$type":"Project","iconUrl":7}`,
			received: youtrack.NewNumber("7"),
		},
		{
			name:     "a boolean",
			field:    "iconUrl",
			answer:   `{"$type":"Project","iconUrl":true}`,
			received: youtrack.NewBool(true),
		},
		{
			name:     "an object",
			field:    "iconUrl",
			answer:   `{"$type":"Project","iconUrl":{"path":"/icon/1"}}`,
			received: youtrack.NewString(`{"path":"/icon/1"}`),
		},
		{
			name:     "the avatar of a user under the project",
			field:    "leader(avatarUrl)",
			answer:   `{"$type":"Project","leader":{"$type":"User","avatarUrl":"avatar/1"}}`,
			received: youtrack.NewString("avatar/1"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tc.answer))

			_, err := projectShown(t, server, tc.field)

			want := youtrack.Error{Code: youtrack.CodeUpstreamInvalid, Details: []youtrack.Pair{
				requestTo(http.MethodGet, server, "/api/admin/projects/DEV?fields="+tc.field),
				{Key: "field", Value: youtrack.NewString(tc.field)},
				{Key: "upstream_value", Value: tc.received},
			}}
			assert.Equal(t, want, errorOf(t, err))
		})
	}
}
