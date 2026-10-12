package youtrack_test

import (
	"context"
	"testing"

	"github.com/hakastein/go-youtrack"
	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOwnerOfReadsTheKindFromTheFormOfTheID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   string
		want youtrack.Owner
	}{
		{name: "an issue", id: "DEV-1", want: youtrack.IssueID("DEV-1")},
		{name: "an article", id: "DEV-A-1", want: youtrack.ArticleID("DEV-A-1")},
		{name: "an article under a lower case code", id: "dev-A-1", want: youtrack.ArticleID("dev-A-1")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			found, err := youtrack.OwnerOf(tc.id)

			require.NoError(t, err)
			assert.Equal(t, tc.want, found)
		})
	}
}

func TestOwnerOfRefusesAnIDOfNeitherForm(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   string
	}{
		{name: "an internal id", id: "3-19"},
		{name: "an article under a lower case marker", id: "DEV-a-1"},
		{name: "a project code alone", id: "DEV"},
		{name: "nothing", id: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			found, err := youtrack.OwnerOf(tc.id)

			assert.Nil(t, found)
			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

// An owner is checked against the kind its type names: an IssueID holding an article id is refused before a
// request, not sent to the issues.
func TestOwnerCallsRefuseAnIDOfTheOtherKindAndNoOwner(t *testing.T) {
	t.Parallel()
	calls := []struct {
		name string
		call func(ctx context.Context, c *youtrack.Client, owner youtrack.Owner) error
	}{
		{name: "comments", call: func(ctx context.Context, c *youtrack.Client, owner youtrack.Owner) error {
			_, err := c.Comments.List(ctx, owner, &youtrack.ListCommentsOptions{Fields: "id"})
			return err
		}},
		{name: "attachments", call: func(ctx context.Context, c *youtrack.Client, owner youtrack.Owner) error {
			_, err := c.Attachments.List(ctx, owner, &youtrack.ListAttachmentsOptions{Fields: "id"})
			return err
		}},
		{name: "tags", call: func(ctx context.Context, c *youtrack.Client, owner youtrack.Owner) error {
			_, err := c.Tags.Add(ctx, owner, "Early", nil)
			return err
		}},
	}
	owners := []struct {
		name  string
		owner youtrack.Owner
	}{
		{name: "an issue holding the id of an article", owner: youtrack.IssueID("DEV-A-1")},
		{name: "an article holding the id of an issue", owner: youtrack.ArticleID("DEV-1")},
		{name: "no owner", owner: nil},
	}
	for _, call := range calls {
		for _, owner := range owners {
			t.Run(call.name+", "+owner.name, func(t *testing.T) {
				t.Parallel()
				err := call.call(t.Context(), client(t, fake.ServeNothing(t)), owner.owner)

				assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
			})
		}
	}
}
