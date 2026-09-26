package youtrack_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/go-youtrack"
	"github.com/hakastein/go-youtrack/fake"
)

const (
	issueCataloguePath   = "/api/admin/customFieldSettings/customFields"
	issueCatalogueTarget = issueCataloguePath + "?fields=name,localizedName&$top=-1"
)

func issueCatalogue(names ...string) string {
	return "[" + strings.Join(names, ",") + "]"
}

func issueCatalogued(name, translation string) string {
	return `{"$type":"CustomField","name":` + strconv.Quote(name) + `,"localizedName":` + translation + `}`
}

func issueEnum(name, value string) string {
	return issueHeld{name: name, valueType: "enum", value: issueElement(value), ordinal: "1"}.json()
}

func issueWithFields(fields ...string) string {
	return `{"$type":"Issue","customFields":[` + strings.Join(fields, ",") + `]}`
}

func issueCataloguing(t *testing.T, catalogue string, rest http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, fake.Searching(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == issueCataloguePath {
			fake.JSON(http.StatusOK, catalogue)(w, r)
			return
		}
		rest(w, r)
	}))
}

func issueNamed(name, value string) youtrack.Pair {
	return youtrack.DataPair(name, youtrack.NewString(value))
}

func issueCandidates(field string, names ...string) *youtrack.Node {
	return youtrack.NewMap(
		youtrack.Pair{Key: "field", Value: youtrack.NewString(field)},
		youtrack.Pair{Key: "candidates", Value: texts(names...)})
}

func issueUnresolved(server *fake.Server, fields, key string, entries ...*youtrack.Node) youtrack.Error {
	return youtrack.Error{Code: youtrack.CodeUnknownName, Details: []youtrack.Pair{
		requestTo(http.MethodGet, server, issueCatalogueTarget),
		{Key: "fields", Value: youtrack.NewString(fields)},
		{Key: key, Value: youtrack.NewList(entries...)},
	}}
}

func TestShowIssueResolvesACustomFieldNameAgainstTheCatalogue(t *testing.T) {
	t.Parallel()
	catalogue := issueCatalogue(issueCatalogued("Named", `"Translated"`), issueCatalogued("Other", "null"))
	tests := []struct {
		name       string
		expression string
	}{
		{name: "the name as the catalogue writes it", expression: "customFields(Named)"},
		{name: "the name in another letter case", expression: "customFields(named)"},
		{name: "the localized name", expression: "customFields(Translated)"},
		{name: "the localized name in another letter case", expression: `customFields("translated")`},
		{name: "both names of one field", expression: "customFields(Named,Translated)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := issueCataloguing(t, catalogue, fake.JSON(http.StatusOK, issueWithFields(issueEnum("Named", "Value"))))

			node, err := issueShown(t, server, tc.expression, youtrack.Comments{})

			require.NoError(t, err)
			assert.Equal(t, issueFieldsBlock(issueNamed("Named", "Value")), node)
			assert.Equal(t, []string{issueCataloguePath, issuePath}, server.Paths())
			assert.Equal(t, []string{"Named"}, server.Last(t).URL.Query()["customFields"])
		})
	}
}

func TestShowIssueResolvesANameToTheFieldItNamesBeforeTheOneItTranslates(t *testing.T) {
	t.Parallel()
	catalogue := issueCatalogue(issueCatalogued("Translated", "null"), issueCatalogued("Named", `"Translated"`))
	issue := issueWithFields(issueEnum("Translated", "Own"), issueEnum("Named", "Other"))
	server := issueCataloguing(t, catalogue, fake.JSON(http.StatusOK, issue))

	node, err := issueShown(t, server, "customFields(Translated)", youtrack.Comments{})

	require.NoError(t, err)
	assert.Equal(t, issueFieldsBlock(issueNamed("Translated", "Own")), node)
	assert.Equal(t, []string{"Translated"}, server.Last(t).URL.Query()["customFields"])
}

func TestShowIssueRefusesACustomFieldNameTheCatalogueDoesNotResolve(t *testing.T) {
	t.Parallel()
	named := issueCatalogue(issueCatalogued("Named", `"Translated"`), issueCatalogued("Other", "null"))
	tests := []struct {
		name       string
		catalogue  string
		expression string
		key        string
		entries    []*youtrack.Node
	}{
		{
			name:       "a name near the name of a field",
			catalogue:  named,
			expression: "customFields(Nmed)",
			key:        "unknown",
			entries:    []*youtrack.Node{withNearest("field", "customFields(Nmed)", "Named")},
		},
		{
			name:       "a name near the localized name of a field",
			catalogue:  named,
			expression: "customFields(Translatd)",
			key:        "unknown",
			entries:    []*youtrack.Node{withNearest("field", "customFields(Translatd)", "Named")},
		},
		{
			name:       "a name near no field",
			catalogue:  named,
			expression: "customFields(zzzzzzzz)",
			key:        "unknown",
			entries:    []*youtrack.Node{withNearest("field", "customFields(zzzzzzzz)", "Named", "Other")},
		},
		{
			name:       "two names, each in the order written",
			catalogue:  named,
			expression: `customFields("Nmed",Othr)`,
			key:        "unknown",
			entries: []*youtrack.Node{
				withNearest("field", `customFields("Nmed")`, "Named"),
				withNearest("field", "customFields(Othr)", "Other"),
			},
		},
		{
			name: "a name near more than five fields",
			catalogue: issueCatalogue(issueCatalogued("Name12", "null"), issueCatalogued("Naem", "null"),
				issueCatalogued("Nme", "null"), issueCatalogued("Name2", "null"), issueCatalogued("Nam", "null"),
				issueCatalogued("Name1", "null"), issueCatalogued("Other", "null")),
			expression: "customFields(Name)",
			key:        "unknown",
			entries:    []*youtrack.Node{withNearest("field", "customFields(Name)", "Nam", "Name1", "Name2", "Nme", "Naem")},
		},
		{
			name:       "a name of two fields",
			catalogue:  issueCatalogue(issueCatalogued("twin", "null"), issueCatalogued("Twin", "null")),
			expression: "customFields(Twin)",
			key:        "ambiguous",
			entries:    []*youtrack.Node{issueCandidates("customFields(Twin)", "Twin", "twin")},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := issueCataloguing(t, tc.catalogue, fake.JSON(http.StatusOK, issueWithFields()))

			_, err := issueShown(t, server, tc.expression, youtrack.Comments{})

			assert.Equal(t, issueUnresolved(server, tc.expression, tc.key, tc.entries...), errorOf(t, err))
			assert.Equal(t, []string{issueCataloguePath}, server.Paths())
		})
	}
}

func issueCataloguesInTurn(t *testing.T, issue string, catalogues ...string) *fake.Server {
	t.Helper()
	answers := make([]http.HandlerFunc, 0, len(catalogues))
	for _, catalogue := range catalogues {
		answers = append(answers, fake.JSON(http.StatusOK, catalogue))
	}
	return routes(t, map[string]http.HandlerFunc{
		"GET " + issueCataloguePath: fake.InTurn(answers...),
		"GET " + issuePath:          fake.JSON(http.StatusOK, issue),
	})
}

func TestShowIssueReadsTheCatalogueEachTimeWithoutACache(t *testing.T) {
	t.Parallel()
	server := issueCataloguing(t, issueCatalogue(issueCatalogued("Named", "null")), fake.JSON(http.StatusOK, issueWithFields()))

	_, err := issueShown(t, server, "customFields(Named)", youtrack.Comments{})
	require.NoError(t, err)
	_, err = issueShown(t, server, "customFields(Named)", youtrack.Comments{})

	require.NoError(t, err)
	assert.Equal(t, []string{issueCataloguePath, issuePath, issueCataloguePath, issuePath}, server.Paths())
}

func TestShowIssueResolvesANameByTheCachedCatalogue(t *testing.T) {
	t.Parallel()
	catalogue := issueCatalogue(issueCatalogued("Named", `"Translated"`))
	server, root := issueCataloguesInTurn(t, issueWithFields(issueEnum("Named", "Value")), catalogue), t.TempDir()
	_, err := cached(t, server, root).Issues.Show(t.Context(), "DEV-1", &youtrack.ShowIssueOptions{Fields: "customFields(Named)"})
	require.NoError(t, err)

	node, err := cached(t, server, root).Issues.Show(t.Context(), "DEV-1", &youtrack.ShowIssueOptions{Fields: "customFields(translated)"})

	require.NoError(t, err)
	assert.Equal(t, issueFieldsBlock(issueNamed("Named", "Value")), node)
	assert.Equal(t, []string{issueCataloguePath, issuePath, issuePath}, server.Paths())
	assert.Equal(t, []string{"Named"}, server.Last(t).URL.Query()["customFields"])
}

func TestShowIssueReadsTheCatalogueAgainForANameTheCacheDoesNotResolve(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		cached string
		asked  string
	}{
		{name: "a field created after the cache was written", cached: issueCatalogue(issueCatalogued("Other", "null")), asked: "Named"},
		{
			name:   "a name of two fields in the cache",
			cached: issueCatalogue(issueCatalogued("named", "null"), issueCatalogued("Named", "null"), issueCatalogued("Other", "null")),
			asked:  "Named",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fresh := issueCatalogue(issueCatalogued("Named", "null"), issueCatalogued("Other", "null"))
			server, root := issueCataloguesInTurn(t, issueWithFields(issueEnum("Named", "Value")), tc.cached, fresh), t.TempDir()
			_, err := cached(t, server, root).Issues.Show(t.Context(), "DEV-1", &youtrack.ShowIssueOptions{Fields: "customFields(Other)"})
			require.NoError(t, err)

			node, err := cached(t, server, root).Issues.Show(t.Context(), "DEV-1", &youtrack.ShowIssueOptions{Fields: "customFields(" + tc.asked + ")"})

			require.NoError(t, err)
			assert.Equal(t, issueFieldsBlock(issueNamed("Named", "Value")), node)
			assert.Equal(t, []string{issueCataloguePath, issuePath, issueCataloguePath, issuePath}, server.Paths())
		})
	}
}

func TestShowIssueRefusesANameOnlyByTheCatalogueTheServerHasJustSent(t *testing.T) {
	t.Parallel()
	catalogue := issueCatalogue(issueCatalogued("Named", "null"))
	server, root := issueCataloguesInTurn(t, issueWithFields(), catalogue), t.TempDir()
	_, err := cached(t, server, root).Issues.Show(t.Context(), "DEV-1", &youtrack.ShowIssueOptions{Fields: "customFields(Named)"})
	require.NoError(t, err)

	_, err = cached(t, server, root).Issues.Show(t.Context(), "DEV-1", &youtrack.ShowIssueOptions{Fields: "customFields(Nmed)"})

	want := issueUnresolved(server, "customFields(Nmed)", "unknown", withNearest("field", "customFields(Nmed)", "Named"))
	assert.Equal(t, want, errorOf(t, err))
	assert.Equal(t, []string{issueCataloguePath, issuePath, issueCataloguePath}, server.Paths())
}

func TestShowIssueNamesTheUnknownAndTheAmbiguousNamesOfOneExpressionTogether(t *testing.T) {
	t.Parallel()
	catalogue := issueCatalogue(issueCatalogued("twin", "null"), issueCatalogued("Twin", "null"), issueCatalogued("Named", "null"))
	server := issueCataloguing(t, catalogue, fake.JSON(http.StatusOK, issueWithFields()))

	_, err := issueShown(t, server, "customFields(Twin,Nmed)", youtrack.Comments{})

	want := youtrack.Error{Code: youtrack.CodeUnknownName, Details: []youtrack.Pair{
		requestTo(http.MethodGet, server, issueCatalogueTarget),
		{Key: "fields", Value: youtrack.NewString("customFields(Twin,Nmed)")},
		{Key: "unknown", Value: youtrack.NewList(withNearest("field", "customFields(Nmed)", "Named"))},
		{Key: "ambiguous", Value: youtrack.NewList(issueCandidates("customFields(Twin)", "Twin", "twin"))},
	}}
	assert.Equal(t, want, errorOf(t, err))
	assert.Equal(t, []string{issueCataloguePath}, server.Paths())
}

func TestShowIssueReadsNoCatalogueForTheBlockAskedWhole(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
	}{
		{name: "the block whole", expression: "customFields"},
		{name: "the block whole after a name", expression: "customFields(Named),customFields"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, issueWithFields()))

			_, err := issueShown(t, server, tc.expression, youtrack.Comments{})

			require.NoError(t, err)
			assert.Equal(t, []string{issuePath}, server.Paths())
			assert.Nil(t, server.Last(t).URL.Query()["customFields"])
		})
	}
}

func TestShowIssueSendsNoNamesWhereAnotherIssueCarriesCustomFieldsToo(t *testing.T) {
	t.Parallel()
	target := `[{"$type":"Issue","customFields":[` + issueEnum("Named", "Target") + `]}]`
	issue := `{"$type":"Issue","customFields":[` + issueEnum("Named", "Own") + `,` + issueEnum("Other", "Also") +
		`],"links":[` + issueLinkSlot(target, `"OUTWARD"`, issueDirected()) + `]}`
	server := issueCataloguing(t, issueCatalogue(issueCatalogued("Named", "null")), fake.JSON(http.StatusOK, issue))

	node, err := issueShown(t, server, "customFields(Named),links(issues(customFields))", youtrack.Comments{})

	require.NoError(t, err)
	assert.Equal(t, youtrack.NewMap(
		youtrack.Pair{Key: "customFields", Value: youtrack.NewMap(issueNamed("Named", "Own"))},
		youtrack.Pair{Key: "links", Value: issueLinked("source to target", issueFieldsBlock(issueNamed("Named", "Target")))},
	), node)
	assert.Nil(t, server.Last(t).URL.Query()["customFields"])
}

func TestShowIssuePrintsTheCustomFieldsAsNamed(t *testing.T) {
	t.Parallel()
	catalogue := issueCatalogue(issueCatalogued("First", "null"), issueCatalogued("Second", "null"), issueCatalogued("Many", "null"))
	tests := []struct {
		name       string
		expression string
		received   []string
		printed    *youtrack.Node
	}{
		{
			name:       "in the order named",
			expression: "customFields(Second,First)",
			received:   []string{issueEnum("First", "One"), issueEnum("Second", "Two")},
			printed:    issueFieldsBlock(issueNamed("Second", "Two"), issueNamed("First", "One")),
		},
		{
			name:       "a field of one value that holds none",
			expression: "customFields(First)",
			received:   []string{issueHeld{name: "First", valueType: "enum"}.json()},
			printed:    issueFieldsBlock(youtrack.DataPair("First", youtrack.NewNull())),
		},
		{
			name:       "a field of many values that holds none",
			expression: "customFields(Many)",
			received:   []string{issueHeld{name: "Many", valueType: "enum", multi: true, value: "[]"}.json()},
			printed:    issueFieldsBlock(youtrack.DataPair("Many", youtrack.NewList())),
		},
		{
			name:       "a field the issue does not hold",
			expression: "customFields(First,Second)",
			received:   []string{issueEnum("First", "One")},
			printed:    issueFieldsBlock(issueNamed("First", "One")),
		},
		{
			name:       "not one of the fields named",
			expression: "customFields(First,Second)",
			printed:    issueFieldsBlock(),
		},
		{
			name:       "a field nobody named",
			expression: "customFields(First)",
			received:   []string{issueEnum("First", "One"), issueEnum("Second", "Two")},
			printed:    issueFieldsBlock(issueNamed("First", "One")),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := issueCataloguing(t, catalogue, fake.JSON(http.StatusOK, issueWithFields(tc.received...)))

			node, err := issueShown(t, server, tc.expression, youtrack.Comments{})

			require.NoError(t, err)
			assert.Equal(t, tc.printed, node)
		})
	}
}

func TestListIssuesRefusesACustomFieldNameBeforeTheSearch(t *testing.T) {
	t.Parallel()
	server := issueCataloguing(t, issueCatalogue(issueCatalogued("Named", "null")), fake.JSON(http.StatusOK, `[]`))

	_, _, err := issueSearch(t, server, "field: value", "idReadable,customFields(Bogus)", 50)

	want := issueUnresolved(server, "idReadable,customFields(Bogus)", "unknown",
		withNearest("field", "customFields(Bogus)", "Named"))
	assert.Equal(t, want, errorOf(t, err))
	assert.Equal(t, []string{fake.AssistPath, issueCataloguePath}, server.Paths())
}

func TestListIssuesSendsNoNamesWhereTheBlockIsAskedWhole(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
	}{
		{name: "the block of the issue", expression: "customFields"},
		{name: "the block of an issue it links to", expression: "links(issues(customFields))"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.Searching(t, fake.JSON(http.StatusOK, `[]`)))

			_, _, err := issueSearch(t, server, "field: value", tc.expression, 50)

			require.NoError(t, err)
			assert.Equal(t, []string{fake.AssistPath, issuesPath}, server.Paths())
			assert.Nil(t, server.Last(t).URL.Query()["customFields"])
		})
	}
}
