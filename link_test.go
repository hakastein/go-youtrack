package youtrack_test

import (
	"context"
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
	linkSourceFields = "id,idReadable,links(id,direction,linkType(id,sourceToTarget,targetToSource," +
		"localizedSourceToTarget,localizedTargetToSource))"
	linkTargetFields = "id,idReadable"
	linkWriteFields  = "id,links(direction,linkType(id),issues(id,links(direction,linkType(id,sourceToTarget," +
		"targetToSource),issuesSize,issues(id,idReadable))))"
	linkListFields = "links(issues(idReadable),direction,linkType(sourceToTarget,targetToSource),issuesSize)"
)

const (
	needsLinkType = `{"$type":"IssueLinkType","id":"5-1","sourceToTarget":"is needed by","targetToSource":"needs",` +
		`"localizedSourceToTarget":"нужна для","localizedTargetToSource":"нуждается в"}`
	tiesLinkType = `{"$type":"IssueLinkType","id":"5-2","sourceToTarget":"ties","targetToSource":"",` +
		`"localizedSourceToTarget":"связывает","localizedTargetToSource":""}`
	copiesLinkType = `{"$type":"IssueLinkType","id":"5-3","sourceToTarget":"copied to","targetToSource":"copy of",` +
		`"localizedSourceToTarget":null,"localizedTargetToSource":""}`
	mirrorsLinkType = `{"$type":"IssueLinkType","id":"5-4","sourceToTarget":"mirrors","targetToSource":"mirrored by",` +
		`"localizedSourceToTarget":"","localizedTargetToSource":"отражена"}`
)

const (
	linkTarget        = `{"$type":"Issue","id":"3-2","idReadable":"DEV-2"}`
	linkRemovalTarget = "/api/issues/DEV-1/links/5-1t/issues/3-2"
)

type linkSlot struct {
	id        string
	direction string
	otherEnd  string
	kind      string
}

var (
	needsSlot    = linkSlot{id: "5-1t", direction: "INWARD", otherEnd: "OUTWARD", kind: needsLinkType}
	neededBySlot = linkSlot{id: "5-1s", direction: "OUTWARD", otherEnd: "INWARD", kind: needsLinkType}
	tiesSlot     = linkSlot{id: "5-2", direction: "BOTH", otherEnd: "BOTH", kind: tiesLinkType}
	copiedToSlot = linkSlot{id: "5-3s", direction: "OUTWARD", otherEnd: "INWARD", kind: copiesLinkType}
	copyOfSlot   = linkSlot{id: "5-3t", direction: "INWARD", otherEnd: "OUTWARD", kind: copiesLinkType}
	mirrorsSlot  = linkSlot{id: "5-4", direction: "BOTH", otherEnd: "BOTH", kind: mirrorsLinkType}
)

func (s linkSlot) held() string {
	return `{"$type":"IssueLink","id":"` + s.id + `","direction":"` + s.direction + `","linkType":` + s.kind + `}`
}

func (s linkSlot) listed(issues ...string) string {
	return `{"$type":"IssueLink","direction":"` + s.direction + `","linkType":` + s.kind +
		`,"issuesSize":` + strconv.Itoa(len(issues)) + `,"issues":[` + strings.Join(issues, ",") + `]}`
}

func (s linkSlot) written(sourceLinks ...string) string {
	return `{"$type":"Issue","id":"3-2","links":[{"$type":"IssueLink","direction":"` + s.otherEnd +
		`","linkType":` + s.kind + `,"issues":[{"$type":"Issue","id":"3-1","links":[` +
		strings.Join(sourceLinks, ",") + `]}]}]}`
}

func (s linkSlot) writtenToTheTarget() string {
	return s.written(s.listed(linkTarget))
}

func linkSource(slots ...linkSlot) string {
	held := make([]string, 0, len(slots))
	for _, slot := range slots {
		held = append(held, slot.held())
	}
	return `{"$type":"Issue","id":"3-1","idReadable":"DEV-1","links":[` + strings.Join(held, ",") + `]}`
}

func linkEverySlot() string {
	return linkSource(needsSlot, neededBySlot, tiesSlot, copiedToSlot, copyOfSlot, mirrorsSlot)
}

func linkHeldSlot(id, direction, kind string) string {
	return `{"id":` + id + `,"direction":` + direction + `,"linkType":` + kind + `}`
}

func linkSourceHolding(links ...string) string {
	return `{"$type":"Issue","id":"3-1","idReadable":"DEV-1","links":[` + strings.Join(links, ",") + `]}`
}

func linkSourceWithAnUnnamedEnd(targetToSource string) string {
	return linkSource(tiesSlot, linkSlot{id: "5-7t", direction: "INWARD", kind: `{"id":"5-7",` +
		`"sourceToTarget":"is needed by","targetToSource":` + targetToSource + `,` +
		`"localizedSourceToTarget":"нужна для","localizedTargetToSource":"нуждается в"}`})
}

func linkListedSlot(direction, kind, size, issues string) string {
	return `{"direction":` + direction + `,"linkType":` + kind + `,"issuesSize":` + size + `,"issues":` + issues + `}`
}

func linkListed(issues ...string) string {
	return `{"$type":"Issue","links":[` + strings.Join(issues, ",") + `]}`
}

func linkedIssue(readable string) string {
	return `{"$type":"Issue","idReadable":"` + readable + `"}`
}

func linkServer(t *testing.T, source, target string, write http.HandlerFunc) *fake.Server {
	t.Helper()
	return routes(t, map[string]http.HandlerFunc{
		"GET /api/issues/DEV-1":                                 fake.JSON(http.StatusOK, source),
		"GET /api/issues/DEV-2":                                 fake.JSON(http.StatusOK, target),
		"POST /api/issues/DEV-1/links/{slot}/issues":            write,
		"DELETE /api/issues/DEV-1/links/{slot}/issues/{target}": write,
	})
}

func linkDocument(total, returned int, truncated bool, phrases ...youtrack.Pair) *youtrack.Node {
	return youtrack.NewMap(
		youtrack.Pair{Key: "total", Value: number(total)},
		youtrack.Pair{Key: "returned", Value: number(returned)},
		youtrack.Pair{Key: "truncated", Value: youtrack.NewBool(truncated)},
		youtrack.Pair{Key: "links", Value: youtrack.NewMap(phrases...)})
}

func linkRecords(phrase string, readable ...string) youtrack.Pair {
	records := make([]*youtrack.Node, 0, len(readable))
	for _, id := range readable {
		records = append(records, youtrack.NewMap(youtrack.Pair{Key: "idReadable", Value: youtrack.NewString(id)}))
	}
	return youtrack.DataPair(phrase, youtrack.NewList(records...))
}

func linkNames(pairs ...youtrack.Pair) []youtrack.Pair {
	return append([]youtrack.Pair{
		{Key: "issue", Value: youtrack.NewString("DEV-1")},
		{Key: "phrase", Value: youtrack.NewString("needs")},
		{Key: "target", Value: youtrack.NewString("DEV-2")},
	}, pairs...)
}

type linkCall func(ctx context.Context, links *youtrack.LinksService) (*youtrack.Node, error)

func TestLinkWriteRefusesAPhraseItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		write linkCall
	}{
		{
			name: "an addition under an empty phrase",
			write: func(ctx context.Context, links *youtrack.LinksService) (*youtrack.Node, error) {
				return links.Add(ctx, "DEV-1", "", "DEV-2", nil)
			},
		},
		{
			name: "an addition under a phrase that is no UTF-8",
			write: func(ctx context.Context, links *youtrack.LinksService) (*youtrack.Node, error) {
				return links.Add(ctx, "DEV-1", "\xff", "DEV-2", nil)
			},
		},
		{
			name: "a removal under an empty phrase",
			write: func(ctx context.Context, links *youtrack.LinksService) (*youtrack.Node, error) {
				return links.Remove(ctx, "DEV-1", "", "DEV-2")
			},
		},
		{
			name: "a removal under a phrase that is no UTF-8",
			write: func(ctx context.Context, links *youtrack.LinksService) (*youtrack.Node, error) {
				return links.Remove(ctx, "DEV-1", "\xff", "DEV-2")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := tc.write(t.Context(), client(t, fake.ServeNothing(t)).Links)

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

func TestLinkRefusesAnExpressionOfTheTargetItCannotSend(t *testing.T) {
	t.Parallel()
	list := func(fields string) linkCall {
		return func(ctx context.Context, links *youtrack.LinksService) (*youtrack.Node, error) {
			return links.List(ctx, "DEV-1", &youtrack.ListLinksOptions{Fields: fields})
		}
	}
	tests := []struct {
		name string
		call linkCall
	}{
		{name: "the comments of a target issue", call: list("comments(text)")},
		{name: "a custom field of a target issue named bare", call: list("+customFields(State)")},
		{name: "a custom field of a target issue named in double quotes", call: list(`+customFields("State")`)},
		{name: "a name under a slot of a target issue in a listing", call: list("+links(id)")},
		{
			name: "a name under a slot of a target issue in an addition",
			call: func(ctx context.Context, links *youtrack.LinksService) (*youtrack.Node, error) {
				return links.Add(ctx, "DEV-1", "needs", "DEV-2", answeredWith("+subtasks(direction)"))
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := tc.call(t.Context(), client(t, fake.ServeNothing(t)).Links)

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

func TestListLinksPrintsThePhrasesOfTheIssueInTheOrderReceived(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, linkListed(
		needsSlot.listed(linkedIssue("DEV-3")),
		copiedToSlot.listed(),
		tiesSlot.listed(linkedIssue("DEV-9"), linkedIssue("DEV-2")),
		neededBySlot.listed(linkedIssue("DEV-4")),
		mirrorsSlot.listed(linkedIssue("DEV-5")),
		copyOfSlot.listed(),
	)))

	node, err := client(t, server).Links.List(t.Context(), "DEV-1", &youtrack.ListLinksOptions{Fields: "idReadable"})

	require.NoError(t, err)
	assert.Equal(t, linkDocument(5, 5, false,
		linkRecords("needs", "DEV-3"),
		linkRecords("ties", "DEV-9", "DEV-2"),
		linkRecords("is needed by", "DEV-4"),
		linkRecords("mirrors", "DEV-5"),
	), node)
}

func TestListLinksCountsWhatTheServerSaysALinkHolds(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, linkListed(
		linkListedSlot(`"BOTH"`, tiesLinkType, "5", "["+linkedIssue("DEV-2")+","+linkedIssue("DEV-3")+"]"),
		linkListedSlot(`"INWARD"`, needsLinkType, "0", "[]"),
	)))

	node, err := client(t, server).Links.List(t.Context(), "DEV-1", &youtrack.ListLinksOptions{Fields: "idReadable"})

	require.NoError(t, err)
	assert.Equal(t, linkDocument(5, 2, true, linkRecords("ties", "DEV-2", "DEV-3")), node)
}

func TestListLinksPrintsTheTextOfATargetOnTheLineOfItsRecord(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, linkListed(
		tiesSlot.listed(`{"$type":"Issue","idReadable":"DEV-2","description":"first\nsecond"}`))))

	node, err := client(t, server).Links.List(t.Context(), "DEV-1", &youtrack.ListLinksOptions{Fields: "description"})

	require.NoError(t, err)
	assert.Equal(t, linkDocument(1, 1, false, youtrack.DataPair("ties", youtrack.NewList(youtrack.NewMap(
		youtrack.Pair{Key: "description", Value: youtrack.NewString("first\nsecond")})))), node)
}

func TestListLinksRefusesLinksOfAnotherShape(t *testing.T) {
	t.Parallel()
	one := "[" + linkedIssue("DEV-2") + "]"
	tests := []struct {
		name  string
		links []string
	}{
		{
			name: "a negative count beside a link holding more than arrived",
			links: []string{
				linkListedSlot(`"INWARD"`, needsLinkType, "-1", "[]"),
				linkListedSlot(`"BOTH"`, tiesLinkType, "2", one),
			},
		},
		{name: "a count that is no whole number", links: []string{linkListedSlot(`"BOTH"`, tiesLinkType, "1.5", one)}},
		{
			name: "a count short of the issues that arrived",
			links: []string{linkListedSlot(`"BOTH"`, tiesLinkType, "1",
				"["+linkedIssue("DEV-2")+","+linkedIssue("DEV-3")+"]")},
		},
		{
			name: "two links of one phrase",
			links: []string{
				linkListedSlot(`"OUTWARD"`, `{"sourceToTarget":"X","targetToSource":"Y"}`, "1", one),
				linkListedSlot(`"BOTH"`, `{"sourceToTarget":"X","targetToSource":""}`, "1", one),
			},
		},
		{
			name:  "a link holding issues under an empty phrase",
			links: []string{linkListedSlot(`"INWARD"`, `{"sourceToTarget":"is needed by","targetToSource":""}`, "1", one)},
		},
		{
			name:  "a link holding issues under a phrase of null",
			links: []string{linkListedSlot(`"INWARD"`, `{"sourceToTarget":"is needed by","targetToSource":null}`, "1", one)},
		},
		{name: "a direction that is no text", links: []string{linkListedSlot("null", tiesLinkType, "1", one)}},
		{name: "a type that is no object", links: []string{linkListedSlot(`"BOTH"`, "null", "1", one)}},
		{name: "issues of a link that are no array", links: []string{linkListedSlot(`"BOTH"`, tiesLinkType, "0", "null")}},
		{
			name:  "an issue at the other end that is no object",
			links: []string{linkListedSlot(`"BOTH"`, tiesLinkType, "1", "[null]")},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := linkListed(tc.links...)
			server := fake.Serve(t, fake.JSON(http.StatusOK, body))

			_, err := client(t, server).Links.List(t.Context(), "DEV-1", &youtrack.ListLinksOptions{Fields: "idReadable"})

			assert.Equal(t, unreadable(requestTo(http.MethodGet, server, "/api/issues/DEV-1?fields="+linkListFields), body),
				errorOf(t, err))
		})
	}
}

func TestAddLinkWritesToTheSlotThePhraseNames(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		phrase string
		slot   linkSlot
	}{
		{name: "the phrase of an end", phrase: "needs", slot: needsSlot},
		{name: "the phrase of the other end of that type", phrase: "is needed by", slot: neededBySlot},
		{name: "the phrase in title case", phrase: "Needs", slot: needsSlot},
		{name: "the phrase in upper case", phrase: "NEEDS", slot: needsSlot},
		{name: "the translation of an end", phrase: "нуждается в", slot: needsSlot},
		{name: "the translation in upper case", phrase: "НУЖДАЕТСЯ В", slot: needsSlot},
		{name: "a type translated at neither end", phrase: "copied to", slot: copiedToSlot},
		{name: "the other end of that type in upper case", phrase: "COPY OF", slot: copyOfSlot},
		{name: "an undirected type by its phrase", phrase: "ties", slot: tiesSlot},
		{name: "an undirected type by its translation", phrase: "связывает", slot: tiesSlot},
		{name: "an undirected type by the phrase of its target end", phrase: "mirrored by", slot: mirrorsSlot},
		{name: "an undirected type by the translation of its target end", phrase: "ОТРАЖЕНА", slot: mirrorsSlot},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := linkServer(t, linkEverySlot(), linkTarget, fake.JSON(http.StatusOK, tc.slot.writtenToTheTarget()))

			_, err := client(t, server).Links.Add(t.Context(), "DEV-1", tc.phrase, "DEV-2", answeredWith("idReadable"))

			require.NoError(t, err)
			assert.Equal(t, "/api/issues/DEV-1/links/"+tc.slot.id+"/issues", server.Last(t).URL.Path)
			assert.Equal(t, `{"id":"3-2"}`, server.Last(t).Body)
		})
	}
}

func TestAddLinkAsksForTheIssuesAndTheTargetAsAskedOfIt(t *testing.T) {
	t.Parallel()
	server := routes(t, map[string]http.HandlerFunc{
		"GET /api/issues/dev-1":                    fake.JSON(http.StatusOK, linkEverySlot()),
		"GET /api/issues/DEV-2":                    fake.JSON(http.StatusOK, linkTarget),
		"POST /api/issues/DEV-1/links/5-1t/issues": fake.JSON(http.StatusOK, needsSlot.writtenToTheTarget()),
	})

	_, err := client(t, server).Links.Add(t.Context(), "dev-1", "needs", "DEV-2", answeredWith("idReadable"))

	require.NoError(t, err)
	assert.Equal(t, []string{
		"/api/issues/dev-1?fields=" + linkSourceFields,
		"/api/issues/DEV-2?fields=" + linkTargetFields,
		"/api/issues/DEV-1/links/5-1t/issues?fields=" + linkWriteFields,
	}, server.Targets(t))
}

func TestAddLinkPrintsTheLinksTheWriteLeftTheIssueWith(t *testing.T) {
	t.Parallel()
	answer := needsSlot.written(needsSlot.listed(linkTarget), tiesSlot.listed(`{"id":"3-4","idReadable":"DEV-4"}`))
	server := linkServer(t, linkEverySlot(), linkTarget, fake.JSON(http.StatusOK, answer))

	node, err := client(t, server).Links.Add(t.Context(), "DEV-1", "needs", "DEV-2", answeredWith("idReadable"))

	require.NoError(t, err)
	assert.Equal(t, linkDocument(2, 2, false, linkRecords("needs", "DEV-2"), linkRecords("ties", "DEV-4")), node)
}

func TestAddLinkResolvesAPhraseTwoSlotsAnswerToByItsSpelling(t *testing.T) {
	t.Parallel()
	leads := linkSlot{id: "5-6s", direction: "OUTWARD", otherEnd: "INWARD", kind: `{"id":"5-6","sourceToTarget":"leads",` +
		`"targetToSource":"follows","localizedSourceToTarget":"Needs","localizedTargetToSource":null}`}
	source := linkSource(leads, needsSlot)

	t.Run("the phrase of the slot it is the phrase of", func(t *testing.T) {
		t.Parallel()
		server := linkServer(t, source, linkTarget, fake.JSON(http.StatusOK, needsSlot.writtenToTheTarget()))

		_, err := client(t, server).Links.Add(t.Context(), "DEV-1", "needs", "DEV-2", answeredWith("idReadable"))

		require.NoError(t, err)
		assert.Equal(t, "/api/issues/DEV-1/links/5-1t/issues", server.Last(t).URL.Path)
	})

	t.Run("the same phrase in another letter case", func(t *testing.T) {
		t.Parallel()
		server := linkServer(t, source, linkTarget, fake.JSON(http.StatusOK, needsSlot.writtenToTheTarget()))

		_, err := client(t, server).Links.Add(t.Context(), "DEV-1", "NEEDS", "DEV-2", answeredWith("idReadable"))

		assert.Equal(t, youtrack.Error{Code: youtrack.CodeUnknownName, Details: []youtrack.Pair{
			requestTo(http.MethodGet, server, "/api/issues/DEV-1?fields="+linkSourceFields),
			{Key: "issue", Value: youtrack.NewString("DEV-1")},
			{Key: "unknown", Value: youtrack.NewList(withNearest("phrase", "NEEDS", "leads", "needs"))},
		}}, errorOf(t, err))
		assert.Equal(t, []string{"/api/issues/DEV-1"}, server.Paths())
	})
}

func TestAddLinkRefusesAPhraseNoSlotGoesBy(t *testing.T) {
	t.Parallel()
	every := []string{"copied to", "copy of", "is needed by", "mirrors", "needs", "ties"}
	tests := []struct {
		name    string
		source  string
		phrase  string
		nearest []string
	}{
		{name: "a phrase a letter short in another letter case", source: linkEverySlot(), phrase: "Neds",
			nearest: []string{"needs"}},
		{name: "a translation a letter short", source: linkEverySlot(), phrase: "нуждаетя в", nearest: []string{"needs"}},
		{name: "the phrase with a space after it", source: linkEverySlot(), phrase: "needs ", nearest: []string{"needs"}},
		{name: "a phrase near none of them", source: linkEverySlot(), phrase: "zzzzzzzzzz", nearest: every},
		{name: "a phrase as near to an empty name as two letters are", source: linkEverySlot(), phrase: "zz",
			nearest: every},
		{
			name:    "an end a type leaves empty",
			source:  linkSourceWithAnUnnamedEnd(`""`),
			phrase:  "нуждается в",
			nearest: []string{"ties"},
		},
		{
			name:    "an end a type leaves null",
			source:  linkSourceWithAnUnnamedEnd("null"),
			phrase:  "нуждается в",
			nearest: []string{"ties"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := linkServer(t, tc.source, linkTarget, fake.JSON(http.StatusOK, needsSlot.writtenToTheTarget()))

			_, err := client(t, server).Links.Add(t.Context(), "DEV-1", tc.phrase, "DEV-2", nil)

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeUnknownName, Details: []youtrack.Pair{
				requestTo(http.MethodGet, server, "/api/issues/DEV-1?fields="+linkSourceFields),
				{Key: "issue", Value: youtrack.NewString("DEV-1")},
				{Key: "unknown", Value: youtrack.NewList(withNearest("phrase", tc.phrase, tc.nearest...))},
			}}, errorOf(t, err))
			assert.Equal(t, []string{"/api/issues/DEV-1"}, server.Paths())
		})
	}
}

func TestAddLinkRefusesTwoSlotsUnderOnePhraseAndWritesEveryOther(t *testing.T) {
	t.Parallel()
	source := linkSource(
		linkSlot{id: "5-8s", direction: "OUTWARD", kind: `{"id":"5-8","sourceToTarget":"X","targetToSource":"Y",` +
			`"localizedSourceToTarget":"","localizedTargetToSource":""}`},
		linkSlot{id: "5-9", direction: "BOTH", kind: `{"id":"5-9","sourceToTarget":"X","targetToSource":"",` +
			`"localizedSourceToTarget":"","localizedTargetToSource":""}`},
		needsSlot)

	t.Run("the phrase both of them go by", func(t *testing.T) {
		t.Parallel()
		server := linkServer(t, source, linkTarget, fake.JSON(http.StatusOK, needsSlot.writtenToTheTarget()))

		_, err := client(t, server).Links.Add(t.Context(), "DEV-1", "x", "DEV-2", nil)

		assert.Equal(t, youtrack.Error{Code: youtrack.CodeUpstreamInvalid, Details: []youtrack.Pair{
			requestTo(http.MethodGet, server, "/api/issues/DEV-1?fields="+linkSourceFields),
			{Key: "issue", Value: youtrack.NewString("DEV-1")},
			{Key: "phrase", Value: youtrack.NewString("X")},
		}}, errorOf(t, err))
		assert.Equal(t, []string{"/api/issues/DEV-1"}, server.Paths())
	})

	t.Run("a phrase of that issue one slot goes by", func(t *testing.T) {
		t.Parallel()
		server := linkServer(t, source, linkTarget, fake.JSON(http.StatusOK, needsSlot.writtenToTheTarget()))

		_, err := client(t, server).Links.Add(t.Context(), "DEV-1", "needs", "DEV-2", answeredWith("idReadable"))

		require.NoError(t, err)
		assert.Equal(t, "/api/issues/DEV-1/links/5-1t/issues", server.Last(t).URL.Path)
	})
}

func TestAddLinkRefusesASlotAddressedAgainstItsOwnEnd(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		slot   linkSlot
		phrase string
	}{
		{
			name:   "the source end addressed with no suffix",
			slot:   linkSlot{id: "5-1", direction: "OUTWARD", kind: needsLinkType},
			phrase: "is needed by",
		},
		{
			name:   "the target end addressed as the source end",
			slot:   linkSlot{id: "5-1s", direction: "INWARD", kind: needsLinkType},
			phrase: "needs",
		},
		{
			name:   "an undirected type addressed as the target end of a directed one",
			slot:   linkSlot{id: "5-2t", direction: "BOTH", kind: tiesLinkType},
			phrase: "ties",
		},
		{
			name:   "an id that would reach an endpoint other than the slot",
			slot:   linkSlot{id: "..", direction: "BOTH", kind: tiesLinkType},
			phrase: "ties",
		},
		{
			name:   "the suffix of the right end on a path that climbs out of the slots",
			slot:   linkSlot{id: "../../5-1t", direction: "INWARD", kind: needsLinkType},
			phrase: "needs",
		},
		{
			name:   "an id whose number before the dash is no number",
			slot:   linkSlot{id: "hub-1t", direction: "INWARD", kind: needsLinkType},
			phrase: "needs",
		},
		{
			name:   "the suffix of the source end in upper case",
			slot:   linkSlot{id: "5-1S", direction: "OUTWARD", kind: needsLinkType},
			phrase: "is needed by",
		},
		{
			name:   "a direction the server has no slot for",
			slot:   linkSlot{id: "5-1t", direction: "SIDEWAYS", kind: needsLinkType},
			phrase: "is needed by",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := linkServer(t, linkSource(tc.slot), linkTarget, fake.JSON(http.StatusOK, tc.slot.writtenToTheTarget()))

			_, err := client(t, server).Links.Add(t.Context(), "DEV-1", tc.phrase, "DEV-2", nil)

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeUpstreamInvalid, Details: []youtrack.Pair{
				requestTo(http.MethodGet, server, "/api/issues/DEV-1?fields="+linkSourceFields),
				{Key: "issue", Value: youtrack.NewString("DEV-1")},
				{Key: "phrase", Value: youtrack.NewString(tc.phrase)},
			}}, errorOf(t, err))
			assert.Equal(t, []string{"/api/issues/DEV-1"}, server.Paths())
		})
	}
}

func TestAddLinkRefusesAnIssueReadOfAnotherShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		source string
	}{
		{
			name: "the phrase of an end as a number",
			source: linkSourceHolding(linkHeldSlot(`"5-1s"`, `"OUTWARD"`, `{"id":"5-1","sourceToTarget":42,`+
				`"targetToSource":"needs","localizedSourceToTarget":null,"localizedTargetToSource":null}`)),
		},
		{
			name: "the translation of an end as an object",
			source: linkSourceHolding(linkHeldSlot(`"5-1s"`, `"OUTWARD"`, `{"id":"5-1","sourceToTarget":"is needed by",`+
				`"targetToSource":"needs","localizedSourceToTarget":{"ru":"нужна для"},"localizedTargetToSource":null}`)),
		},
		{name: "a link whose id is no text", source: linkSourceHolding(linkHeldSlot("5", `"BOTH"`, tiesLinkType))},
		{name: "a link whose direction is no text", source: linkSourceHolding(linkHeldSlot(`"5-2"`, "null", tiesLinkType))},
		{name: "a link whose type is no object", source: linkSourceHolding(linkHeldSlot(`"5-2"`, `"BOTH"`, "null"))},
		{
			name: "a type whose id is no text",
			source: linkSourceHolding(linkHeldSlot(`"5-2"`, `"BOTH"`, `{"id":null,"sourceToTarget":"ties",`+
				`"targetToSource":"","localizedSourceToTarget":null,"localizedTargetToSource":null}`)),
		},
		{
			name:   "a readable id that would reach another endpoint",
			source: `{"$type":"Issue","id":"3-1","idReadable":"..","links":[]}`,
		},
		{name: "an id that is no internal id", source: `{"$type":"Issue","id":"x","idReadable":"DEV-1","links":[]}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := linkServer(t, tc.source, linkTarget, fake.JSON(http.StatusOK, needsSlot.writtenToTheTarget()))

			_, err := client(t, server).Links.Add(t.Context(), "DEV-1", "needs", "DEV-2", nil)

			assert.Equal(t, unreadable(requestTo(http.MethodGet, server, "/api/issues/DEV-1?fields="+linkSourceFields),
				tc.source), errorOf(t, err))
			assert.Equal(t, []string{"/api/issues/DEV-1"}, server.Paths())
		})
	}
}

func TestAddLinkRefusesATargetUnderTheIDItIsAddressedByRatherThanTheInternalOne(t *testing.T) {
	t.Parallel()
	const target = `{"$type":"Issue","id":"DEV-2","idReadable":"DEV-2"}`
	server := linkServer(t, linkEverySlot(), target, fake.JSON(http.StatusOK, needsSlot.writtenToTheTarget()))

	_, err := client(t, server).Links.Add(t.Context(), "DEV-1", "needs", "DEV-2", nil)

	assert.Equal(t, unreadable(requestTo(http.MethodGet, server, "/api/issues/DEV-2?fields="+linkTargetFields), target),
		errorOf(t, err))
	assert.Equal(t, []string{"/api/issues/DEV-1", "/api/issues/DEV-2"}, server.Paths())
}

func TestLinkWriteRefusesAnIssueNamedAsItsOwnTarget(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		write linkCall
	}{
		{
			name: "an addition to the same id",
			write: func(ctx context.Context, links *youtrack.LinksService) (*youtrack.Node, error) {
				return links.Add(ctx, "DEV-1", "ties", "DEV-1", nil)
			},
		},
		{
			name: "an addition to the same id in another letter case",
			write: func(ctx context.Context, links *youtrack.LinksService) (*youtrack.Node, error) {
				return links.Add(ctx, "DEV-1", "ties", "dev-1", nil)
			},
		},
		{
			name: "a removal from the same id",
			write: func(ctx context.Context, links *youtrack.LinksService) (*youtrack.Node, error) {
				return links.Remove(ctx, "DEV-1", "ties", "DEV-1")
			},
		},
		{
			name: "a removal from the same id in another letter case",
			write: func(ctx context.Context, links *youtrack.LinksService) (*youtrack.Node, error) {
				return links.Remove(ctx, "dev-1", "ties", "DEV-1")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := tc.write(t.Context(), client(t, fake.ServeNothing(t)).Links)

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

func TestAddLinkRefusesATargetThatIsTheIssueUnderAnotherID(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, linkEverySlot()))

	_, err := client(t, server).Links.Add(t.Context(), "DEV-1", "ties", "OLD-1", nil)

	assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage, Details: []youtrack.Pair{
		requestTo(http.MethodGet, server, "/api/issues/OLD-1?fields="+linkTargetFields),
		{Key: "issue", Value: youtrack.NewString("DEV-1")},
		{Key: "target", Value: youtrack.NewString("DEV-1")},
	}}, errorOf(t, err))
	assert.Equal(t, []string{"/api/issues/DEV-1", "/api/issues/OLD-1"}, server.Paths())
}

func TestAddLinkRefusesAnIssueTheServerDoesNotHave(t *testing.T) {
	t.Parallel()
	missing := fake.JSON(http.StatusNotFound, `{"error":"Not Found","error_description":"Entity not found"}`)
	tests := []struct {
		name   string
		source http.HandlerFunc
		target http.HandlerFunc
		read   string
		paths  []string
	}{
		{
			name:   "the issue the link is written on",
			source: missing,
			target: fake.JSON(http.StatusOK, linkTarget),
			read:   "/api/issues/DEV-1?fields=" + linkSourceFields,
			paths:  []string{"/api/issues/DEV-1"},
		},
		{
			name:   "the issue at the other end",
			source: fake.JSON(http.StatusOK, linkEverySlot()),
			target: missing,
			read:   "/api/issues/DEV-2?fields=" + linkTargetFields,
			paths:  []string{"/api/issues/DEV-1", "/api/issues/DEV-2"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := routes(t, map[string]http.HandlerFunc{"GET /api/issues/DEV-1": tc.source, "GET /api/issues/DEV-2": tc.target})

			_, err := client(t, server).Links.Add(t.Context(), "DEV-1", "needs", "DEV-2", nil)

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeNotFound, Details: []youtrack.Pair{
				requestTo(http.MethodGet, server, tc.read),
				{Key: "upstream_status", Value: number(404)},
				{Key: "upstream_error", Value: youtrack.NewString("Not Found")},
				{Key: "upstream_message", Value: youtrack.NewString("Entity not found")},
			}}, errorOf(t, err))
			assert.Equal(t, tc.paths, server.Paths())
		})
	}
}

func TestAddLinkRefusesAnAnswerThatDoesNotHoldTheLink(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		written string
	}{
		{
			name:    "the issue holding the target issue at the end opposite the one the phrase names",
			written: needsSlot.written(neededBySlot.listed(linkTarget)),
		},
		{
			name:    "the issue holding the target issue under another type",
			written: needsSlot.written(copyOfSlot.listed(linkTarget)),
		},
		{
			name:    "the issue holding the link and not the target issue",
			written: needsSlot.written(needsSlot.listed(`{"$type":"Issue","id":"3-9","idReadable":"DEV-9"}`)),
		},
		{
			name: "the target issue holding the issue at the end the phrase names",
			written: linkSlot{direction: "INWARD", otherEnd: "INWARD", kind: needsLinkType}.written(
				needsSlot.listed(linkTarget)),
		},
		{
			name: "an issue other than the target issue",
			written: `{"$type":"Issue","id":"3-9","links":[{"direction":"OUTWARD","linkType":` + needsLinkType +
				`,"issues":[{"id":"3-1","links":[` + needsSlot.listed(linkTarget) + `]}]}]}`,
		},
		{
			name:    "the issue with no links at all",
			written: needsSlot.written(),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := linkServer(t, linkEverySlot(), linkTarget, fake.JSON(http.StatusOK, tc.written))

			_, err := client(t, server).Links.Add(t.Context(), "DEV-1", "needs", "DEV-2", answeredWith("idReadable"))

			assert.Equal(t, youtrack.Error{
				Code:       youtrack.CodeUpstreamInvalid,
				AfterWrite: true,
				Details: append([]youtrack.Pair{
					requestTo(http.MethodPost, server, "/api/issues/DEV-1/links/5-1t/issues?fields="+linkWriteFields),
				}, linkNames()...),
			}, errorOf(t, err))
		})
	}
}

func TestAddLinkNamesTheLinkInWhatTheServerSaidAboutTheWrite(t *testing.T) {
	t.Parallel()
	server := linkServer(t, linkEverySlot(), linkTarget,
		fake.JSON(http.StatusBadRequest, `{"error":"invalid_properties","error_description":"A cycle"}`))

	_, err := client(t, server).Links.Add(t.Context(), "DEV-1", "NEEDS", "DEV-2", answeredWith("idReadable"))

	assert.Equal(t, youtrack.Error{
		Code: youtrack.CodeRejected,
		Details: append([]youtrack.Pair{
			requestTo(http.MethodPost, server, "/api/issues/DEV-1/links/5-1t/issues?fields="+linkWriteFields),
		}, linkNames(
			youtrack.Pair{Key: "upstream_status", Value: number(400)},
			youtrack.Pair{Key: "upstream_error", Value: youtrack.NewString("invalid_properties")},
			youtrack.Pair{Key: "upstream_message", Value: youtrack.NewString("A cycle")},
		)...),
	}, errorOf(t, err))
}

func TestRemoveLinkTakesTheLinkAwayBySlotAndInternalID(t *testing.T) {
	t.Parallel()
	server := linkServer(t, linkEverySlot(), linkTarget, fake.JSON(http.StatusOK, ""))

	node, err := client(t, server).Links.Remove(t.Context(), "DEV-1", "NEEDS", "DEV-2")

	require.NoError(t, err)
	assert.Equal(t, youtrack.NewMap(
		youtrack.Pair{Key: "idReadable", Value: youtrack.NewString("DEV-1")},
		youtrack.Pair{Key: "removed", Value: youtrack.NewMap(linkRecords("needs", "DEV-2"))},
	), node)
	assert.Equal(t, []string{"/api/issues/DEV-1", "/api/issues/DEV-2", linkRemovalTarget}, server.Paths())
	assert.Equal(t, http.MethodDelete, server.Last(t).Method)
}

func TestRemoveLinkRefusesALinkTheIssueDoesNotHold(t *testing.T) {
	t.Parallel()
	server := linkServer(t, linkEverySlot(), linkTarget,
		fake.JSON(http.StatusNotFound, `{"error":"Not Found","error_description":"Entity not found"}`))

	_, err := client(t, server).Links.Remove(t.Context(), "DEV-1", "needs", "DEV-2")

	assert.Equal(t, youtrack.Error{
		Code: youtrack.CodeNotFound,
		Details: append([]youtrack.Pair{requestTo(http.MethodDelete, server, linkRemovalTarget)}, linkNames(
			youtrack.Pair{Key: "upstream_status", Value: number(404)},
			youtrack.Pair{Key: "upstream_error", Value: youtrack.NewString("Not Found")},
			youtrack.Pair{Key: "upstream_message", Value: youtrack.NewString("Entity not found")},
		)...),
	}, errorOf(t, err))
}

func TestRemoveLinkRefusesBeforeTheRemovalTheWayAddDoes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		phrase  string
		target  string
		code    youtrack.Code
		read    string
		details []youtrack.Pair
		paths   []string
	}{
		{
			name:   "a phrase no slot goes by",
			phrase: "Neds",
			target: linkTarget,
			code:   youtrack.CodeUnknownName,
			read:   "/api/issues/DEV-1?fields=" + linkSourceFields,
			details: []youtrack.Pair{
				{Key: "issue", Value: youtrack.NewString("DEV-1")},
				{Key: "unknown", Value: youtrack.NewList(withNearest("phrase", "Neds", "needs"))},
			},
			paths: []string{"/api/issues/DEV-1"},
		},
		{
			name:   "a target that is the issue under another id",
			phrase: "ties",
			target: `{"$type":"Issue","id":"3-1","idReadable":"DEV-1"}`,
			code:   youtrack.CodeBadUsage,
			read:   "/api/issues/DEV-2?fields=" + linkTargetFields,
			details: []youtrack.Pair{
				{Key: "issue", Value: youtrack.NewString("DEV-1")},
				{Key: "target", Value: youtrack.NewString("DEV-1")},
			},
			paths: []string{"/api/issues/DEV-1", "/api/issues/DEV-2"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := linkServer(t, linkEverySlot(), tc.target, fake.JSON(http.StatusOK, ""))

			_, err := client(t, server).Links.Remove(t.Context(), "DEV-1", tc.phrase, "DEV-2")

			assert.Equal(t, youtrack.Error{
				Code:    tc.code,
				Details: append([]youtrack.Pair{requestTo(http.MethodGet, server, tc.read)}, tc.details...),
			}, errorOf(t, err))
			assert.Equal(t, tc.paths, server.Paths())
		})
	}
}
