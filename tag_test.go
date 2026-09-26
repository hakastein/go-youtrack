package youtrack_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/hakastein/go-youtrack"
	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const tagCatalogueTarget = "/api/tags?fields=id,name,owner(login)&$top=-1"

type tagCall func(ctx context.Context, tags *youtrack.TagsService) (*youtrack.Node, error)

func tagText(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func tagOf(id, name, owner string) string {
	return `{"$type":"Tag","id":` + tagText(id) + `,"name":` + tagText(name) +
		`,"owner":{"$type":"User","login":` + tagText(owner) + `}}`
}

func tagCatalogue(tags ...string) string {
	return "[" + strings.Join(tags, ",") + "]"
}

// Out of order on purpose: a candidate list comes out sorted only if the module sorts it.
func tagsShown() string {
	return tagCatalogue(
		tagOf("10-1", "Early", "first"),
		tagOf("10-2", "mixed", "first"),
		tagOf("10-3", "Mixed", "second"),
		tagOf("10-4", "Mixed", "first"),
		tagOf("10-5", "10-1", "first"),
	)
}

func tagOwner(schema, readable string) string {
	return `{"$type":` + tagText(schema) + `,"idReadable":` + tagText(readable) + `}`
}

func tagNode(name, owner string) *youtrack.Node {
	return youtrack.NewMap(
		youtrack.Pair{Key: "name", Value: youtrack.NewString(name)},
		youtrack.Pair{Key: "owner", Value: youtrack.NewMap(youtrack.Pair{Key: "login", Value: youtrack.NewString(owner)})})
}

func tagCandidate(name, owner string) *youtrack.Node {
	return youtrack.NewMap(
		youtrack.Pair{Key: "name", Value: youtrack.NewString(name)},
		youtrack.Pair{Key: "owner", Value: youtrack.NewString(owner)})
}

func tagCreationEchoed(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	fake.JSON(http.StatusOK, `{"$type":"Tag",`+strings.TrimPrefix(string(body), "{"))(w, r)
}

func TestListTagsPrintsAPageOfTheDefaultFields(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `[{"$type":"Tag","owner":{"$type":"User","login":"first"},`+
		`"readSharingSettings":{"$type":"WatchFolderSharingSettings","permittedUsers":[{"$type":"User","login":"third"}],`+
		`"permittedGroups":[{"$type":"UserGroup","name":"First"}]},"name":"Early"}]`))

	node, err := client(t, server).Tags.List(t.Context(), nil)

	require.NoError(t, err)
	assert.Equal(t, wholePage("tags", youtrack.NewMap(
		youtrack.Pair{Key: "name", Value: youtrack.NewString("Early")},
		youtrack.Pair{Key: "owner", Value: youtrack.NewMap(youtrack.Pair{Key: "login", Value: youtrack.NewString("first")})},
		youtrack.Pair{Key: "readSharingSettings", Value: youtrack.NewMap(
			youtrack.Pair{Key: "permittedGroups", Value: youtrack.NewList(youtrack.NewMap(
				youtrack.Pair{Key: "name", Value: youtrack.NewString("First")}))},
			youtrack.Pair{Key: "permittedUsers", Value: youtrack.NewList(youtrack.NewMap(
				youtrack.Pair{Key: "login", Value: youtrack.NewString("third")}))})})), node)
	assert.Equal(t, []string{"/api/tags"}, server.Paths())
	assert.Equal(t, []url.Values{{"fields": {youtrack.TagListFields}, "$top": {"50"}}}, server.Queries())
}

func TestTagRefusesACallBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		call tagCall
	}{
		{
			name: "a creation with an empty name",
			call: func(ctx context.Context, tags *youtrack.TagsService) (*youtrack.Node, error) {
				return tags.Create(ctx, "", youtrack.TagSharing{}, nil)
			},
		},
		{
			name: "a creation with a name of no UTF-8",
			call: func(ctx context.Context, tags *youtrack.TagsService) (*youtrack.Node, error) {
				return tags.Create(ctx, "\xff", youtrack.TagSharing{}, nil)
			},
		},
		{
			name: "a deletion with an empty name",
			call: func(ctx context.Context, tags *youtrack.TagsService) (*youtrack.Node, error) {
				return tags.Delete(ctx, "", nil)
			},
		},
		{
			name: "a deletion with a name that begins with a space",
			call: func(ctx context.Context, tags *youtrack.TagsService) (*youtrack.Node, error) {
				return tags.Delete(ctx, " Early", nil)
			},
		},
		{
			name: "a tagging with an empty name",
			call: func(ctx context.Context, tags *youtrack.TagsService) (*youtrack.Node, error) {
				return tags.Add(ctx, "DEV-7", "", nil)
			},
		},
		{
			name: "a removal with an empty name",
			call: func(ctx context.Context, tags *youtrack.TagsService) (*youtrack.Node, error) {
				return tags.Remove(ctx, "DEV-7", "", nil)
			},
		},
		{
			name: "a tagging of an internal id",
			call: func(ctx context.Context, tags *youtrack.TagsService) (*youtrack.Node, error) {
				return tags.Add(ctx, "7-12", "Early", nil)
			},
		},
		{
			name: "a removal from a project code",
			call: func(ctx context.Context, tags *youtrack.TagsService) (*youtrack.Node, error) {
				return tags.Remove(ctx, "DEV", "Early", nil)
			},
		},
		{
			name: "a creation shared with a group of no name to see it",
			call: func(ctx context.Context, tags *youtrack.TagsService) (*youtrack.Node, error) {
				return tags.Create(ctx, "Early", youtrack.TagSharing{VisibleFor: []string{""}}, nil)
			},
		},
		{
			name: "a creation shared with a group of no name to update it",
			call: func(ctx context.Context, tags *youtrack.TagsService) (*youtrack.Node, error) {
				return tags.Create(ctx, "Early", youtrack.TagSharing{UpdatableBy: []string{""}}, nil)
			},
		},
		{
			name: "a creation shared with a group of no name to tag with it",
			call: func(ctx context.Context, tags *youtrack.TagsService) (*youtrack.Node, error) {
				return tags.Create(ctx, "Early", youtrack.TagSharing{TaggableBy: []string{""}}, nil)
			},
		},
		{
			name: "a creation shared with a group and a group of no name after it",
			call: func(ctx context.Context, tags *youtrack.TagsService) (*youtrack.Node, error) {
				return tags.Create(ctx, "Early", youtrack.TagSharing{VisibleFor: []string{"First", ""}}, nil)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := tc.call(t.Context(), client(t, fake.ServeNothing(t)).Tags)

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

func TestCreateTagRefusesANameTheServerWouldCut(t *testing.T) {
	t.Parallel()
	type edge struct {
		name    string
		written string
	}
	var tests []edge
	for _, r := range []rune{' ', '\t', '\n', '\r', '\v', '\f', 0x1C, 0x1D, 0x1E, 0x1F, 0xA0, 0x2028, 0x2029, 0x3000} {
		tests = append(tests,
			edge{name: fmt.Sprintf("U+%04X at the beginning", r), written: string(r) + "Early"},
			edge{name: fmt.Sprintf("U+%04X at the end", r), written: "Early" + string(r)})
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := client(t, fake.ServeNothing(t)).Tags.Create(t.Context(), tc.written, youtrack.TagSharing{}, nil)

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

func TestCreateTagSendsTheNameAsWritten(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		written string
	}{
		{name: "brackets and a space", written: "[bug] fix login"},
		{name: "a tab inside", written: "two\twords"},
		{name: "a line feed inside", written: "two\nlines"},
		{name: "a carriage return inside", written: "two\rlines"},
		{name: "a line separator inside", written: "two\U00002028lines"},
		{name: "a start of heading at the edge", written: "\x01Early"},
		{name: "a next line at the edge", written: "Early\U00000085"},
		{name: "a zero width space at the edge", written: "\U0000200BEarly"},
		{name: "a byte order mark at the edge", written: "Early\U0000FEFF"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"Tag","name":`+tagText(tc.written)+`}`))

			_, err := client(t, server).Tags.Create(t.Context(), tc.written, youtrack.TagSharing{}, answeredWith("name"))

			require.NoError(t, err)
			assert.Equal(t, map[string]any{"name": tc.written}, server.LastJSON(t))
		})
	}
}

func TestCreateTagAsksForTheNameItChecks(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, tagOf("10-1", "Early", "first")))

	node, err := client(t, server).Tags.Create(t.Context(), "Early", youtrack.TagSharing{}, answeredWith("owner(login)"))

	require.NoError(t, err)
	assert.Equal(t, youtrack.NewMap(youtrack.Pair{Key: "owner", Value: youtrack.NewMap(
		youtrack.Pair{Key: "login", Value: youtrack.NewString("first")})}), node)
	assert.Equal(t, []string{"/api/tags?fields=owner(login),name"}, server.Targets(t))
}

func TestCreateTagRefusesANameTheServerKeptAsAnother(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		written string
		kept    string
		actual  *youtrack.Node
	}{
		{
			name:    "a rune cut off the end after all",
			written: "Early\U00000085",
			kept:    `{"$type":"Tag","name":"Early"}`,
			actual:  youtrack.NewString("Early"),
		},
		{
			name:    "another letter case",
			written: "Early",
			kept:    `{"$type":"Tag","name":"early"}`,
			actual:  youtrack.NewString("early"),
		},
		{
			name:    "no name at all",
			written: "Early",
			kept:    `{"$type":"Tag","name":null}`,
			actual:  youtrack.NewNull(),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tc.kept))

			_, err := client(t, server).Tags.Create(t.Context(), tc.written, youtrack.TagSharing{}, answeredWith("name"))

			want := youtrack.Error{Code: youtrack.CodeUpstreamInvalid, AfterWrite: true, Details: []youtrack.Pair{
				requestTo(http.MethodPost, server, "/api/tags?fields=name"),
				{Key: "tag", Value: youtrack.NewString(tc.written)},
				{Key: "mismatch", Value: youtrack.NewList(mismatch("name", youtrack.NewString(tc.written), tc.actual))},
			}}
			assert.Equal(t, want, errorOf(t, err))
		})
	}
}

// Out of order on purpose, as tagsShown is.
func tagGroups() string {
	return `[{"$type":"UserGroup","id":"6-1","name":"First"},{"$type":"UserGroup","id":"6-2","name":"Second"},` +
		`{"$type":"UserGroup","id":"6-4","name":"team"},{"$type":"UserGroup","id":"6-3","name":"Team"}]`
}

func tagGroupsRequest(server *fake.Server) youtrack.Pair {
	return requestTo(http.MethodGet, server, "/api/groups?fields=id,name&$top=-1")
}

func tagSharedWith(ids ...string) map[string]any {
	groups := make([]any, 0, len(ids))
	for _, id := range ids {
		groups = append(groups, map[string]any{"id": id})
	}
	return map[string]any{"permittedGroups": groups}
}

func TestCreateTagWritesEachSetOfGroupsItWasGiven(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		sharing youtrack.TagSharing
		body    map[string]any
	}{
		{
			name:    "the groups that see it",
			sharing: youtrack.TagSharing{VisibleFor: []string{"First", "Second"}},
			body:    map[string]any{"name": "Early", "readSharingSettings": tagSharedWith("6-1", "6-2")},
		},
		{
			name:    "the groups that update it",
			sharing: youtrack.TagSharing{UpdatableBy: []string{"Second"}},
			body:    map[string]any{"name": "Early", "updateSharingSettings": tagSharedWith("6-2")},
		},
		{
			name:    "the groups that tag with it",
			sharing: youtrack.TagSharing{TaggableBy: []string{"First"}},
			body:    map[string]any{"name": "Early", "tagSharingSettings": tagSharedWith("6-1")},
		},
		{
			name: "each set of its own",
			sharing: youtrack.TagSharing{
				VisibleFor:  []string{"First"},
				UpdatableBy: []string{"Second"},
				TaggableBy:  []string{"First", "Second"},
			},
			body: map[string]any{
				"name":                  "Early",
				"readSharingSettings":   tagSharedWith("6-1"),
				"updateSharingSettings": tagSharedWith("6-2"),
				"tagSharingSettings":    tagSharedWith("6-1", "6-2"),
			},
		},
		{
			name:    "a group named in another letter case",
			sharing: youtrack.TagSharing{VisibleFor: []string{"FIRST"}},
			body:    map[string]any{"name": "Early", "readSharingSettings": tagSharedWith("6-1")},
		},
		{
			name:    "one group named three times",
			sharing: youtrack.TagSharing{TaggableBy: []string{"first", "First", "FIRST"}},
			body:    map[string]any{"name": "Early", "tagSharingSettings": tagSharedWith("6-1")},
		},
		{
			name:    "the upper case one of two groups apart by letter case",
			sharing: youtrack.TagSharing{VisibleFor: []string{"Team"}},
			body:    map[string]any{"name": "Early", "readSharingSettings": tagSharedWith("6-3")},
		},
		{
			name:    "the lower case one of the two",
			sharing: youtrack.TagSharing{VisibleFor: []string{"team"}},
			body:    map[string]any{"name": "Early", "readSharingSettings": tagSharedWith("6-4")},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := routes(t, map[string]http.HandlerFunc{
				"GET /api/groups": fake.JSON(http.StatusOK, tagGroups()),
				"POST /api/tags":  tagCreationEchoed,
			})

			_, err := client(t, server).Tags.Create(t.Context(), "Early", tc.sharing, answeredWith("name"))

			require.NoError(t, err)
			assert.Equal(t, tc.body, server.LastJSON(t))
			assert.Equal(t, []string{"/api/groups", "/api/tags"}, server.Paths())
		})
	}
}

func TestCreateTagAsksForTheGroupsOfEachSetItWrote(t *testing.T) {
	t.Parallel()
	server := routes(t, map[string]http.HandlerFunc{
		"GET /api/groups": fake.JSON(http.StatusOK, tagGroups()),
		"POST /api/tags":  tagCreationEchoed,
	})
	sharing := youtrack.TagSharing{VisibleFor: []string{"First"}, TaggableBy: []string{"Second"}}

	_, err := client(t, server).Tags.Create(t.Context(), "Early", sharing, answeredWith("name"))

	require.NoError(t, err)
	assert.Equal(t, []string{
		"/api/groups?fields=id,name&$top=-1",
		"/api/tags?fields=name,readSharingSettings(permittedGroups(id)),tagSharingSettings(permittedGroups(id))",
	}, server.Targets(t))
}

func TestCreateTagRefusesGroupNamesItCannotResolve(t *testing.T) {
	t.Parallel()
	every := []string{"First", "Second", "Team", "team"}
	tests := []struct {
		name    string
		sharing youtrack.TagSharing
		details []youtrack.Pair
	}{
		{
			name:    "a name near a group",
			sharing: youtrack.TagSharing{VisibleFor: []string{"Frist"}},
			details: []youtrack.Pair{{Key: "unknown", Value: youtrack.NewList(withNearest("group", "Frist", "First"))}},
		},
		{
			name: "a name near no group in each set",
			sharing: youtrack.TagSharing{
				VisibleFor:  []string{"Nobody"},
				UpdatableBy: []string{"Nil"},
				TaggableBy:  []string{"None"},
			},
			details: []youtrack.Pair{{Key: "unknown", Value: youtrack.NewList(withNearest("group", "Nobody", every...),
				withNearest("group", "Nil", every...), withNearest("group", "None", every...))}},
		},
		{
			name:    "a name two groups answer to by letter case",
			sharing: youtrack.TagSharing{UpdatableBy: []string{"TEAM"}},
			details: []youtrack.Pair{{Key: "ambiguous", Value: youtrack.NewList(youtrack.NewMap(
				youtrack.Pair{Key: "group", Value: youtrack.NewString("TEAM")},
				youtrack.Pair{Key: "candidates", Value: texts("Team", "team")}))}},
		},
		{
			name: "an unknown name and an ambiguous one",
			sharing: youtrack.TagSharing{
				VisibleFor: []string{"Nobody"},
				TaggableBy: []string{"TEAM"},
			},
			details: []youtrack.Pair{
				{Key: "unknown", Value: youtrack.NewList(withNearest("group", "Nobody", every...))},
				{Key: "ambiguous", Value: youtrack.NewList(youtrack.NewMap(
					youtrack.Pair{Key: "group", Value: youtrack.NewString("TEAM")},
					youtrack.Pair{Key: "candidates", Value: texts("Team", "team")}))},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tagGroups()))

			_, err := client(t, server).Tags.Create(t.Context(), "Early", tc.sharing, nil)

			want := youtrack.Error{
				Code:    youtrack.CodeUnknownName,
				Details: append([]youtrack.Pair{tagGroupsRequest(server)}, tc.details...),
			}
			assert.Equal(t, want, errorOf(t, err))
			assert.Equal(t, []string{"/api/groups"}, server.Paths())
		})
	}
}

func TestCreateTagRefusesGroupsItCannotShareTheTagWith(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		groups  string
		sharing youtrack.TagSharing
	}{
		{
			name:    "an id with no dash",
			groups:  `[{"$type":"UserGroup","id":"6","name":"First"}]`,
			sharing: youtrack.TagSharing{VisibleFor: []string{"First"}},
		},
		{
			name:    "an id of two dots",
			groups:  `[{"$type":"UserGroup","id":"..","name":"First"}]`,
			sharing: youtrack.TagSharing{UpdatableBy: []string{"First"}},
		},
		{
			name:    "an id with a letter after the dash",
			groups:  `[{"$type":"UserGroup","id":"6-x","name":"First"}]`,
			sharing: youtrack.TagSharing{TaggableBy: []string{"First"}},
		},
		{
			name:    "a name that is no text",
			groups:  `[{"$type":"UserGroup","id":"6-1","name":5}]`,
			sharing: youtrack.TagSharing{VisibleFor: []string{"First"}},
		},
		{
			name:    "an id that is no text",
			groups:  `[{"$type":"UserGroup","id":7,"name":"First"}]`,
			sharing: youtrack.TagSharing{VisibleFor: []string{"First"}},
		},
		{
			name: "a broken id beside names that resolve to nothing",
			groups: `[{"$type":"UserGroup","id":"6","name":"First"},{"$type":"UserGroup","id":"6-3","name":"Team"},` +
				`{"$type":"UserGroup","id":"6-4","name":"team"}]`,
			sharing: youtrack.TagSharing{
				VisibleFor:  []string{"First"},
				UpdatableBy: []string{"Nobody"},
				TaggableBy:  []string{"TEAM"},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tc.groups))

			_, err := client(t, server).Tags.Create(t.Context(), "Early", tc.sharing, nil)

			assert.Equal(t, unreadable(tagGroupsRequest(server), tc.groups), errorOf(t, err))
			assert.Equal(t, []string{"/api/groups"}, server.Paths())
		})
	}
}

func tagKeptWith(set, groups string) string {
	return `{"$type":"Tag","name":"Early","` + set + `":{"$type":"WatchFolderSharingSettings","permittedGroups":` + groups + `}}`
}

func TestCreateTagRefusesASetOfGroupsThatCameBackAsAnother(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		sharing  youtrack.TagSharing
		kept     string
		fields   string
		field    string
		expected *youtrack.Node
		actual   *youtrack.Node
	}{
		{
			name:     "one of the two that see it dropped",
			sharing:  youtrack.TagSharing{VisibleFor: []string{"First", "Second"}},
			kept:     tagKeptWith("readSharingSettings", `[{"id":"6-1"}]`),
			fields:   "name,readSharingSettings(permittedGroups(id))",
			field:    "readSharingSettings.permittedGroups",
			expected: texts("6-1", "6-2"),
			actual:   texts("6-1"),
		},
		{
			name:     "a group nobody wrote left among those that update it",
			sharing:  youtrack.TagSharing{UpdatableBy: []string{"First"}},
			kept:     tagKeptWith("updateSharingSettings", `[{"id":"6-1"},{"id":"6-2"}]`),
			fields:   "name,updateSharingSettings(permittedGroups(id))",
			field:    "updateSharingSettings.permittedGroups",
			expected: texts("6-1"),
			actual:   texts("6-1", "6-2"),
		},
		{
			name:     "another group among those that tag with it",
			sharing:  youtrack.TagSharing{TaggableBy: []string{"First"}},
			kept:     tagKeptWith("tagSharingSettings", `[{"id":"6-2"}]`),
			fields:   "name,tagSharingSettings(permittedGroups(id))",
			field:    "tagSharingSettings.permittedGroups",
			expected: texts("6-1"),
			actual:   texts("6-2"),
		},
		{
			name:     "no list of groups at all",
			sharing:  youtrack.TagSharing{VisibleFor: []string{"First"}},
			kept:     tagKeptWith("readSharingSettings", `null`),
			fields:   "name,readSharingSettings(permittedGroups(id))",
			field:    "readSharingSettings.permittedGroups",
			expected: texts("6-1"),
			actual:   youtrack.NewNull(),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := routes(t, map[string]http.HandlerFunc{
				"GET /api/groups": fake.JSON(http.StatusOK, tagGroups()),
				"POST /api/tags":  fake.JSON(http.StatusOK, tc.kept),
			})

			_, err := client(t, server).Tags.Create(t.Context(), "Early", tc.sharing, answeredWith("name"))

			want := youtrack.Error{Code: youtrack.CodeUpstreamInvalid, AfterWrite: true, Details: []youtrack.Pair{
				requestTo(http.MethodPost, server, "/api/tags?fields="+tc.fields),
				{Key: "tag", Value: youtrack.NewString("Early")},
				{Key: "mismatch", Value: youtrack.NewList(mismatch(tc.field, tc.expected, tc.actual))},
			}}
			assert.Equal(t, want, errorOf(t, err))
		})
	}
}

func TestCreateTagTakesASetOfGroupsThatCameBackInAnotherOrder(t *testing.T) {
	t.Parallel()
	server := routes(t, map[string]http.HandlerFunc{
		"GET /api/groups": fake.JSON(http.StatusOK, tagGroups()),
		"POST /api/tags":  fake.JSON(http.StatusOK, tagKeptWith("readSharingSettings", `[{"id":"6-2"},{"id":"6-1"}]`)),
	})
	sharing := youtrack.TagSharing{VisibleFor: []string{"First", "Second"}}

	node, err := client(t, server).Tags.Create(t.Context(), "Early", sharing, answeredWith("name"))

	require.NoError(t, err)
	assert.Equal(t, youtrack.NewMap(youtrack.Pair{Key: "name", Value: youtrack.NewString("Early")}), node)
}

func TestDeleteTagResolvesTheNameAgainstTheTagsItIsShown(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		written string
		ownedBy string
		id      string
	}{
		{name: "another letter case", written: "EARLY", id: "10-1"},
		{name: "the exact spelling among names apart by letter case", written: "mixed", id: "10-2"},
		{name: "a name shaped like an internal id", written: "10-1", id: "10-5"},
		{name: "one name of two owners, of one of them", written: "Mixed", ownedBy: "second", id: "10-3"},
		{name: "the same name of the other owner", written: "Mixed", ownedBy: "first", id: "10-4"},
		{name: "a login in another letter case", written: "Mixed", ownedBy: "SECOND", id: "10-3"},
		{name: "no exact spelling, of the owner of one of them", written: "MIXED", ownedBy: "second", id: "10-3"},
		{name: "the exact spelling among the tags of one owner", written: "mixed", ownedBy: "first", id: "10-2"},
		{name: "the one tag of that name, of its owner", written: "early", ownedBy: "first", id: "10-1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := routes(t, map[string]http.HandlerFunc{
				"GET /api/tags":         fake.JSON(http.StatusOK, tagsShown()),
				"DELETE /api/tags/{id}": fake.JSON(http.StatusOK, ""),
			})

			_, err := client(t, server).Tags.Delete(t.Context(), tc.written, &youtrack.TagOptions{OwnedBy: tc.ownedBy})

			require.NoError(t, err)
			assert.Equal(t, []string{"/api/tags", "/api/tags/" + tc.id}, server.Paths())
		})
	}
}

func TestDeleteTagReadsEveryTagOnceAndPrintsTheOneItDeleted(t *testing.T) {
	t.Parallel()
	server := routes(t, map[string]http.HandlerFunc{
		"GET /api/tags":         fake.JSON(http.StatusOK, tagsShown()),
		"DELETE /api/tags/{id}": fake.JSON(http.StatusOK, ""),
	})

	node, err := client(t, server).Tags.Delete(t.Context(), "early", nil)

	require.NoError(t, err)
	assert.Equal(t, tagNode("Early", "first"), node)
	assert.Equal(t, []string{tagCatalogueTarget, "/api/tags/10-1?"}, server.Targets(t))
	assert.Equal(t, []string{"", ""}, server.Bodies())
}

func TestDeleteTagRefusesANameThatNamesNoOneTag(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		written string
		ownedBy string
		detail  youtrack.Pair
	}{
		{
			name:    "a name near a tag",
			written: "Erly",
			detail:  youtrack.Pair{Key: "unknown", Value: youtrack.NewList(withNearest("tag", "Erly", "Early"))},
		},
		{
			name:    "a name near no tag",
			written: "zzzzzz",
			detail: youtrack.Pair{Key: "unknown", Value: youtrack.NewList(
				withNearest("tag", "zzzzzz", "10-1", "Early", "Mixed", "Mixed", "mixed"))},
		},
		{
			name:    "a name of several tags and no exact spelling",
			written: "MIXED",
			detail: youtrack.Pair{Key: "ambiguous", Value: youtrack.NewList(youtrack.NewMap(
				youtrack.Pair{Key: "tag", Value: youtrack.NewString("MIXED")},
				youtrack.Pair{Key: "candidates", Value: youtrack.NewList(
					tagCandidate("Mixed", "first"), tagCandidate("Mixed", "second"), tagCandidate("mixed", "first"))}))},
		},
		{
			name:    "an exact spelling two owners hold",
			written: "Mixed",
			detail: youtrack.Pair{Key: "ambiguous", Value: youtrack.NewList(youtrack.NewMap(
				youtrack.Pair{Key: "tag", Value: youtrack.NewString("Mixed")},
				youtrack.Pair{Key: "candidates", Value: youtrack.NewList(
					tagCandidate("Mixed", "first"), tagCandidate("Mixed", "second"), tagCandidate("mixed", "first"))}))},
		},
		{
			name:    "no exact spelling among the tags of one owner",
			written: "MIXED",
			ownedBy: "first",
			detail: youtrack.Pair{Key: "ambiguous", Value: youtrack.NewList(youtrack.NewMap(
				youtrack.Pair{Key: "tag", Value: youtrack.NewString("MIXED")},
				youtrack.Pair{Key: "candidates", Value: youtrack.NewList(
					tagCandidate("Mixed", "first"), tagCandidate("mixed", "first"))}))},
		},
		{
			name:    "a login no tag of that name belongs to",
			written: "Mixed",
			ownedBy: "third",
			detail: youtrack.Pair{Key: "unknown", Value: youtrack.NewList(youtrack.NewMap(
				youtrack.Pair{Key: "tag", Value: youtrack.NewString("Mixed")},
				youtrack.Pair{Key: "owned_by", Value: youtrack.NewString("third")},
				youtrack.Pair{Key: "candidates", Value: youtrack.NewList(
					tagCandidate("Mixed", "first"), tagCandidate("Mixed", "second"), tagCandidate("mixed", "first"))}))},
		},
		{
			name:    "the one tag of that name belonging to someone else",
			written: "early",
			ownedBy: "second",
			detail: youtrack.Pair{Key: "unknown", Value: youtrack.NewList(youtrack.NewMap(
				youtrack.Pair{Key: "tag", Value: youtrack.NewString("early")},
				youtrack.Pair{Key: "owned_by", Value: youtrack.NewString("second")},
				youtrack.Pair{Key: "candidates", Value: youtrack.NewList(tagCandidate("Early", "first"))}))},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tagsShown()))

			_, err := client(t, server).Tags.Delete(t.Context(), tc.written, &youtrack.TagOptions{OwnedBy: tc.ownedBy})

			want := youtrack.Error{Code: youtrack.CodeUnknownName, Details: []youtrack.Pair{
				requestTo(http.MethodGet, server, tagCatalogueTarget),
				tc.detail,
			}}
			assert.Equal(t, want, errorOf(t, err))
			assert.Equal(t, []string{"/api/tags"}, server.Paths())
		})
	}
}

func TestDeleteTagRefusesTagsItCannotAddressTheDeletionBy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		tag  string
	}{
		{name: "a name that is no text", tag: `{"$type":"Tag","id":"10-1","name":5,"owner":{"$type":"User","login":"first"}}`},
		{name: "an owner whose login is null", tag: `{"$type":"Tag","id":"10-1","name":"Early","owner":{"$type":"User","login":null}}`},
		{name: "an id that is no text", tag: `{"$type":"Tag","id":101,"name":"Early","owner":{"$type":"User","login":"first"}}`},
		{name: "an id that is null", tag: `{"$type":"Tag","id":null,"name":"Early","owner":{"$type":"User","login":"first"}}`},
		{name: "an id of two dots", tag: tagOf("..", "Early", "first")},
		{name: "an id with a letter after the dash", tag: tagOf("10-x", "Early", "first")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			catalogue := tagCatalogue(tc.tag)
			server := fake.Serve(t, fake.JSON(http.StatusOK, catalogue))

			_, err := client(t, server).Tags.Delete(t.Context(), "Early", nil)

			assert.Equal(t, unreadable(requestTo(http.MethodGet, server, tagCatalogueTarget), catalogue), errorOf(t, err))
			assert.Equal(t, []string{"/api/tags"}, server.Paths())
		})
	}
}

func TestAddAndRemoveTagRefuseAnOwnerTheReadNamedByAnIDOfAnotherShape(t *testing.T) {
	t.Parallel()
	add := func(id string) tagCall {
		return func(ctx context.Context, tags *youtrack.TagsService) (*youtrack.Node, error) {
			return tags.Add(ctx, id, "Early", nil)
		}
	}
	remove := func(id string) tagCall {
		return func(ctx context.Context, tags *youtrack.TagsService) (*youtrack.Node, error) {
			return tags.Remove(ctx, id, "Early", nil)
		}
	}
	tests := []struct {
		name  string
		call  tagCall
		owner string
		read  string
	}{
		{
			name:  "a tagging of an issue answered with two dots",
			call:  add("DEV-7"),
			owner: tagOwner("Issue", ".."),
			read:  "/api/issues/DEV-7",
		},
		{
			name:  "a tagging of an issue answered with the id of an article",
			call:  add("DEV-7"),
			owner: tagOwner("Issue", "DEV-A-7"),
			read:  "/api/issues/DEV-7",
		},
		{
			name:  "a tagging of an article answered with the id of an issue",
			call:  add("DEV-A-7"),
			owner: tagOwner("Article", "DEV-7"),
			read:  "/api/articles/DEV-A-7",
		},
		{
			name:  "a tagging of an issue answered with no readable id",
			call:  add("DEV-7"),
			owner: `{"$type":"Issue","idReadable":null}`,
			read:  "/api/issues/DEV-7",
		},
		{
			name:  "a removal from an issue answered with two dots",
			call:  remove("DEV-7"),
			owner: tagOwner("Issue", ".."),
			read:  "/api/issues/DEV-7",
		},
		{
			name:  "a removal from an issue answered with the id of an article",
			call:  remove("DEV-7"),
			owner: tagOwner("Issue", "DEV-A-7"),
			read:  "/api/issues/DEV-7",
		},
		{
			name:  "a removal from an article answered with the id of an issue",
			call:  remove("DEV-A-7"),
			owner: tagOwner("Article", "DEV-7"),
			read:  "/api/articles/DEV-A-7",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tc.owner))

			_, err := tc.call(t.Context(), client(t, server).Tags)

			assert.Equal(t, unreadable(requestTo(http.MethodGet, server, tc.read+"?fields=idReadable"), tc.owner), errorOf(t, err))
			assert.Equal(t, []string{tc.read}, server.Paths())
		})
	}
}

func TestAddAndRemoveTagNarrowTheNameByTheOwnerOfTheTag(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		call  tagCall
		paths []string
	}{
		{
			name: "a tagging",
			call: func(ctx context.Context, tags *youtrack.TagsService) (*youtrack.Node, error) {
				return tags.Add(ctx, "DEV-7", "Mixed", &youtrack.TagOptions{OwnedBy: "second"})
			},
			paths: []string{"/api/issues/DEV-7", "/api/tags", "/api/issues/DEV-7/tags"},
		},
		{
			name: "a removal",
			call: func(ctx context.Context, tags *youtrack.TagsService) (*youtrack.Node, error) {
				return tags.Remove(ctx, "DEV-7", "Mixed", &youtrack.TagOptions{OwnedBy: "second"})
			},
			paths: []string{"/api/issues/DEV-7", "/api/tags", "/api/issues/DEV-7/tags/10-3"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := routes(t, map[string]http.HandlerFunc{
				"GET /api/issues/{id}":               fake.JSON(http.StatusOK, tagOwner("Issue", "DEV-7")),
				"GET /api/tags":                      fake.JSON(http.StatusOK, tagsShown()),
				"POST /api/issues/{id}/tags":         fake.JSON(http.StatusOK, tagOf("10-3", "Mixed", "second")),
				"DELETE /api/issues/{id}/tags/{tag}": fake.JSON(http.StatusOK, ""),
			})

			_, err := tc.call(t.Context(), client(t, server).Tags)

			require.NoError(t, err)
			assert.Equal(t, tc.paths, server.Paths())
		})
	}
}

func TestAddTagWritesUnderTheIDsTheReadsFound(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		written string
		owner   string
		targets []string
	}{
		{
			name:    "an issue in lower case",
			written: "dev-7",
			owner:   tagOwner("Issue", "DEV-7"),
			targets: []string{
				"/api/issues/dev-7?fields=idReadable",
				tagCatalogueTarget,
				"/api/issues/DEV-7/tags?fields=id,name,owner(login)",
			},
		},
		{
			name:    "an article in mixed case",
			written: "dev-A-7",
			owner:   tagOwner("Article", "DEV-A-7"),
			targets: []string{
				"/api/articles/dev-A-7?fields=idReadable",
				tagCatalogueTarget,
				"/api/articles/DEV-A-7/tags?fields=id,name,owner(login)",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := routes(t, map[string]http.HandlerFunc{
				"GET /api/issues/{id}":         fake.JSON(http.StatusOK, tc.owner),
				"GET /api/articles/{id}":       fake.JSON(http.StatusOK, tc.owner),
				"GET /api/tags":                fake.JSON(http.StatusOK, tagsShown()),
				"POST /api/issues/{id}/tags":   fake.JSON(http.StatusOK, tagOf("10-1", "Early", "first")),
				"POST /api/articles/{id}/tags": fake.JSON(http.StatusOK, tagOf("10-1", "Early", "first")),
			})

			_, err := client(t, server).Tags.Add(t.Context(), tc.written, "early", nil)

			require.NoError(t, err)
			assert.Equal(t, tc.targets, server.Targets(t))
			assert.Equal(t, []string{"", "", `{"id":"10-1"}`}, server.Bodies())
		})
	}
}

func TestAddTagPrintsTheTagTheWriteAnsweredWith(t *testing.T) {
	t.Parallel()
	server := routes(t, map[string]http.HandlerFunc{
		"GET /api/issues/{id}":       fake.JSON(http.StatusOK, tagOwner("Issue", "DEV-7")),
		"GET /api/tags":              fake.JSON(http.StatusOK, tagsShown()),
		"POST /api/issues/{id}/tags": fake.JSON(http.StatusOK, tagOf("10-1", "Late", "second")),
	})

	node, err := client(t, server).Tags.Add(t.Context(), "dev-7", "early", nil)

	require.NoError(t, err)
	assert.Equal(t, youtrack.NewMap(
		youtrack.Pair{Key: "idReadable", Value: youtrack.NewString("DEV-7")},
		youtrack.Pair{Key: "added", Value: tagNode("Late", "second")}), node)
}

func TestAddTagRefusesATagOtherThanTheOneResolved(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		written string
		owner   string
		kind    string
		write   string
	}{
		{
			name:    "on an issue",
			written: "DEV-7",
			owner:   tagOwner("Issue", "DEV-7"),
			kind:    "issue",
			write:   "/api/issues/DEV-7/tags",
		},
		{
			name:    "on an article",
			written: "DEV-A-7",
			owner:   tagOwner("Article", "DEV-A-7"),
			kind:    "article",
			write:   "/api/articles/DEV-A-7/tags",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			other := tagOf("10-2", "Early", "first")
			server := routes(t, map[string]http.HandlerFunc{
				"GET /api/issues/{id}":         fake.JSON(http.StatusOK, tc.owner),
				"GET /api/articles/{id}":       fake.JSON(http.StatusOK, tc.owner),
				"GET /api/tags":                fake.JSON(http.StatusOK, tagsShown()),
				"POST /api/issues/{id}/tags":   fake.JSON(http.StatusOK, other),
				"POST /api/articles/{id}/tags": fake.JSON(http.StatusOK, other),
			})

			_, err := client(t, server).Tags.Add(t.Context(), tc.written, "early", nil)

			want := youtrack.Error{Code: youtrack.CodeUpstreamInvalid, AfterWrite: true, Details: []youtrack.Pair{
				requestTo(http.MethodPost, server, tc.write+"?fields=id,name,owner(login)"),
				{Key: tc.kind, Value: youtrack.NewString(tc.written)},
				{Key: "tag", Value: youtrack.NewString("early")},
			}}
			assert.Equal(t, want, errorOf(t, err))
		})
	}
}

func TestRemoveTagTakesTheTagOffUnderTheIDsTheReadsFound(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		written  string
		owner    string
		readable string
		targets  []string
	}{
		{
			name:     "an issue in lower case",
			written:  "dev-7",
			owner:    tagOwner("Issue", "DEV-7"),
			readable: "DEV-7",
			targets: []string{
				"/api/issues/dev-7?fields=idReadable",
				tagCatalogueTarget,
				"/api/issues/DEV-7/tags/10-1?",
			},
		},
		{
			name:     "an article in mixed case",
			written:  "dev-A-7",
			owner:    tagOwner("Article", "DEV-A-7"),
			readable: "DEV-A-7",
			targets: []string{
				"/api/articles/dev-A-7?fields=idReadable",
				tagCatalogueTarget,
				"/api/articles/DEV-A-7/tags/10-1?",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := routes(t, map[string]http.HandlerFunc{
				"GET /api/issues/{id}":                 fake.JSON(http.StatusOK, tc.owner),
				"GET /api/articles/{id}":               fake.JSON(http.StatusOK, tc.owner),
				"GET /api/tags":                        fake.JSON(http.StatusOK, tagsShown()),
				"DELETE /api/issues/{id}/tags/{tag}":   fake.JSON(http.StatusOK, ""),
				"DELETE /api/articles/{id}/tags/{tag}": fake.JSON(http.StatusOK, ""),
			})

			node, err := client(t, server).Tags.Remove(t.Context(), tc.written, "early", nil)

			require.NoError(t, err)
			assert.Equal(t, youtrack.NewMap(
				youtrack.Pair{Key: "idReadable", Value: youtrack.NewString(tc.readable)},
				youtrack.Pair{Key: "removed", Value: tagNode("Early", "first")}), node)
			assert.Equal(t, tc.targets, server.Targets(t))
			assert.Equal(t, []string{"", "", ""}, server.Bodies())
		})
	}
}

func TestRemoveTagRefusesATagTheOwnerDoesNotCarry(t *testing.T) {
	t.Parallel()
	server := routes(t, map[string]http.HandlerFunc{
		"GET /api/issues/{id}": fake.JSON(http.StatusOK, tagOwner("Issue", "DEV-7")),
		"GET /api/tags":        fake.JSON(http.StatusOK, tagsShown()),
		"DELETE /api/issues/{id}/tags/{tag}": fake.JSON(http.StatusNotFound,
			`{"error":"Not Found","error_description":"Entity with id 10-1 not found"}`),
	})

	_, err := client(t, server).Tags.Remove(t.Context(), "DEV-7", "early", nil)

	want := youtrack.Error{Code: youtrack.CodeNotFound, Details: []youtrack.Pair{
		requestTo(http.MethodDelete, server, "/api/issues/DEV-7/tags/10-1"),
		{Key: "issue", Value: youtrack.NewString("DEV-7")},
		{Key: "tag", Value: youtrack.NewString("early")},
		{Key: "upstream_status", Value: number(http.StatusNotFound)},
		{Key: "upstream_error", Value: youtrack.NewString("Not Found")},
		{Key: "upstream_message", Value: youtrack.NewString("Entity with id 10-1 not found")},
	}}
	assert.Equal(t, want, errorOf(t, err))
}
