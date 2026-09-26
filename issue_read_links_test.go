package youtrack_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/go-youtrack"
	"github.com/hakastein/go-youtrack/fake"
)

const (
	issuePhraseFields = "direction,linkType(sourceToTarget,targetToSource)"
	issueLinkTarget   = `[{"$type":"Issue","idReadable":"DEV-2","summary":"Second"}]`
)

func issueLinkSlot(issues, direction, linkType string) string {
	return `{"$type":"IssueLink","issues":` + issues + `,"direction":` + direction + `,"linkType":` + linkType + `}`
}

func issueLinkType(sourceToTarget, targetToSource string) string {
	return `{"$type":"IssueLinkType","sourceToTarget":` + sourceToTarget + `,"targetToSource":` + targetToSource + `}`
}

func issueDirected() string {
	return issueLinkType(`"source to target"`, `"target to source"`)
}

func issueLinked(phrase string, targets ...*youtrack.Node) *youtrack.Node {
	return youtrack.NewMap(youtrack.DataPair(phrase, youtrack.NewList(targets...)))
}

func issueTargetID() *youtrack.Node {
	return youtrack.NewMap(youtrack.Pair{Key: "idReadable", Value: youtrack.NewString("DEV-2")})
}

func TestShowIssuePrintsALinkUnderThePhraseOfItsEnd(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		direction string
		phrase    string
	}{
		{name: "at the source of a directed link", direction: `"OUTWARD"`, phrase: "source to target"},
		{name: "at the target of a directed link", direction: `"INWARD"`, phrase: "target to source"},
		{name: "at either end of an undirected link", direction: `"BOTH"`, phrase: "source to target"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `{"$type":"Issue","links":[` + issueLinkSlot(issueLinkTarget, tc.direction, issueDirected()) + `]}`
			server := fake.Serve(t, fake.JSON(http.StatusOK, body))

			node, err := issueShown(t, server, "links", youtrack.Comments{})

			require.NoError(t, err)
			assert.Equal(t, youtrack.NewMap(youtrack.Pair{Key: "links", Value: issueLinked(tc.phrase, issueTargetID())}), node)
		})
	}
}

func TestShowIssuePrintsTheParentAndTheSubtasksAsLinks(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
		direction  string
		phrase     string
	}{
		{name: "the parent", expression: "parent", direction: `"INWARD"`, phrase: "target to source"},
		{name: "the subtasks", expression: "subtasks", direction: `"OUTWARD"`, phrase: "source to target"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `{"$type":"Issue","` + tc.expression + `":` + issueLinkSlot(issueLinkTarget, tc.direction, issueDirected()) + `}`
			server := fake.Serve(t, fake.JSON(http.StatusOK, body))

			node, err := issueShown(t, server, tc.expression, youtrack.Comments{})

			require.NoError(t, err)
			assert.Equal(t, youtrack.NewMap(youtrack.Pair{Key: tc.expression, Value: issueLinked(tc.phrase, issueTargetID())}), node)
		})
	}
}

func TestShowIssueLeavesOutALinkThatHoldsNoIssue(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"Issue","links":[`+
		issueLinkSlot(`[]`, `"OUTWARD"`, issueDirected())+`,`+
		issueLinkSlot(`[]`, `"INWARD"`, issueDirected())+`,`+
		issueLinkSlot(`[]`, `"BOTH"`, issueLinkType(`"undirected"`, `""`))+`,`+
		issueLinkSlot(`[]`, `"INWARD"`, issueLinkType(`"source to target"`, `null`))+`]}`))

	node, err := issueShown(t, server, "links", youtrack.Comments{})

	require.NoError(t, err)
	assert.Equal(t, youtrack.NewMap(youtrack.Pair{Key: "links", Value: youtrack.NewMap()}), node)
}

func TestShowIssuePrintsTheTargetsOfALinkByTheirReadableIDUnlessAskedOtherwise(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
		target     *youtrack.Node
	}{
		{name: "the slot alone", expression: "links", target: issueTargetID()},
		{name: "the issues of the slot alone", expression: "links(issues)", target: issueTargetID()},
		{
			name:       "a field of the issues",
			expression: "links(issues(summary))",
			target:     youtrack.NewMap(youtrack.Pair{Key: "summary", Value: youtrack.NewString("Second")}),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `{"$type":"Issue","links":[` + issueLinkSlot(issueLinkTarget, `"OUTWARD"`, issueDirected()) + `]}`
			server := fake.Serve(t, fake.JSON(http.StatusOK, body))

			node, err := issueShown(t, server, tc.expression, youtrack.Comments{})

			require.NoError(t, err)
			assert.Equal(t, youtrack.NewMap(youtrack.Pair{Key: "links", Value: issueLinked("source to target", tc.target)}), node)
		})
	}
}

func TestShowIssueAsksForThePhraseOfEveryLink(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
		sent       string
	}{
		{name: "the slot alone", expression: "links", sent: "links(issues(idReadable)," + issuePhraseFields + ")"},
		{
			name:       "a field of the issues",
			expression: "links(issues(summary))",
			sent:       "links(issues(summary)," + issuePhraseFields + ")",
		},
		{name: "the parent", expression: "parent", sent: "parent(issues(idReadable)," + issuePhraseFields + ")"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"Issue","links":[],"parent":null}`))

			_, err := issueShown(t, server, tc.expression, youtrack.Comments{})

			require.NoError(t, err)
			assert.Equal(t, []string{tc.sent}, server.Fields())
		})
	}
}

func TestShowIssueRefusesLinksOfAnotherShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		links string
	}{
		{
			name: "two links of one phrase",
			links: `[` + issueLinkSlot(issueLinkTarget, `"OUTWARD"`, issueLinkType(`"twin"`, `"other"`)) + `,` +
				issueLinkSlot(issueLinkTarget, `"BOTH"`, issueLinkType(`"twin"`, `""`)) + `]`,
		},
		{
			name:  "a link holding issues under an empty phrase",
			links: `[` + issueLinkSlot(issueLinkTarget, `"INWARD"`, issueLinkType(`"source to target"`, `""`)) + `]`,
		},
		{
			name:  "a link holding issues under a phrase that is no text",
			links: `[` + issueLinkSlot(issueLinkTarget, `"INWARD"`, issueLinkType(`"source to target"`, `null`)) + `]`,
		},
		{name: "a slot that is no object", links: `[[` + issueLinkSlot(issueLinkTarget, `"BOTH"`, issueDirected()) + `]]`},
		{name: "a slot that is null", links: `[null,` + issueLinkSlot(issueLinkTarget, `"BOTH"`, issueDirected()) + `]`},
		{name: "the issues of a slot are no array", links: `[` + issueLinkSlot(`null`, `"BOTH"`, issueDirected()) + `]`},
		{name: "an issue at the other end is no object", links: `[` + issueLinkSlot(`[null]`, `"BOTH"`, issueDirected()) + `]`},
		{name: "the end the issue stands at is no text", links: `[` + issueLinkSlot(issueLinkTarget, `null`, issueDirected()) + `]`},
		{name: "the type of a link is no object", links: `[` + issueLinkSlot(issueLinkTarget, `"BOTH"`, `null`) + `]`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `{"$type":"Issue","links":` + tc.links + `}`
			server := fake.Serve(t, fake.JSON(http.StatusOK, body))

			_, err := issueShown(t, server, "links", youtrack.Comments{})

			target := issuePath + "?fields=links(issues(idReadable)," + issuePhraseFields + ")"
			assert.Equal(t, unreadable(requestTo(http.MethodGet, server, target), body), errorOf(t, err))
		})
	}
}

func TestShowIssuePrintsLinksTheServerSentAsNullAsNull(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
		body       string
	}{
		{name: "the links", expression: "links", body: `{"$type":"Issue","links":null}`},
		{name: "the parent", expression: "parent", body: `{"$type":"Issue","parent":null}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tc.body))

			node, err := issueShown(t, server, tc.expression, youtrack.Comments{})

			require.NoError(t, err)
			assert.Equal(t, youtrack.NewMap(youtrack.Pair{Key: tc.expression, Value: youtrack.NewNull()}), node)
		})
	}
}
