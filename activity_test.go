package youtrack_test

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/hakastein/go-youtrack"
	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	activityLinkTypesPath = "/api/issueLinkTypes"
	activityListPath      = "/api/issues/DEV-1/activities"
)

const (
	activityEarly  = 1000
	activityLate   = 2000
	activityLatest = 3000
)

const activityEveryCategory = "AttachmentsCategory,CommentTextCategory,CommentsCategory,CustomFieldCategory," +
	"DescriptionCategory,IssueCreatedCategory,IssueResolvedCategory,LinksCategory,SummaryCategory," +
	"TagsCategory,VcsChangeCategory,WorkItemCategory"

// "Goes before" is the phrase of two link types, so a link written by it resolves to neither.
const activityLinkTypes = `[` +
	`{"$type":"IssueLinkType","sourceToTarget":"leads to","targetToSource":"follows",` +
	`"localizedSourceToTarget":"Goes before","localizedTargetToSource":"Comes after"},` +
	`{"$type":"IssueLinkType","sourceToTarget":"relates to","targetToSource":"",` +
	`"localizedSourceToTarget":null,"localizedTargetToSource":""},` +
	`{"$type":"IssueLinkType","sourceToTarget":"mirrors","targetToSource":"mirrors",` +
	`"localizedSourceToTarget":"Reflects","localizedTargetToSource":"Reflects"},` +
	`{"$type":"IssueLinkType","sourceToTarget":"precedes","targetToSource":"succeeds",` +
	`"localizedSourceToTarget":"Goes before","localizedTargetToSource":"Comes later"}` +
	`]`

func activityServer(t *testing.T, activities string) *fake.Server {
	t.Helper()
	return activityServerOf(t, fake.JSON(http.StatusOK, activityLinkTypes), fake.JSON(http.StatusOK, activities))
}

func activityServerOf(t *testing.T, linkTypes, activities http.HandlerFunc) *fake.Server {
	t.Helper()
	return routes(t, map[string]http.HandlerFunc{"GET " + activityLinkTypesPath: linkTypes, "GET " + activityListPath: activities})
}

func activityJSON(kind, category string, at int, members string) string {
	return fmt.Sprintf(`{"$type":%q,"timestamp":%d,"category":{"$type":"ActivityCategory","id":%q},%s}`,
		kind, at, category, members)
}

func activityCreated(at int) string {
	return activityJSON("IssueCreatedActivityItem", "IssueCreatedCategory", at, `"field":null`)
}

func activityOfField(valueType, added string) string {
	field := fmt.Sprintf(`{"$type":"CustomFilterField","name":"Label","customField":{"$type":"CustomField",`+
		`"name":"Named","fieldType":{"$type":"FieldType","valueType":%q}}}`, valueType)
	return activityJSON("CustomFieldActivityItem", "CustomFieldCategory", activityEarly,
		`"field":`+field+`,"added":`+added+`,"removed":[]`)
}

func activityOfLink(label string) string {
	return activityJSON("LinksActivityItem", "LinksCategory", activityEarly,
		`"field":{"$type":"LinkTypeFilterField","name":`+strconv.Quote(label)+`},"added":[],"removed":[]`)
}

func activityArray(records ...string) string {
	return "[" + strings.Join(records, ",") + "]"
}

func activityList(t *testing.T, server *fake.Server, fields string, categories ...string) (*youtrack.Node, error) {
	t.Helper()
	opts := &youtrack.ListActivitiesOptions{Fields: fields, Categories: categories}
	return client(t, server).Activities.List(t.Context(), "DEV-1", opts)
}

func activityAt(moment string) *youtrack.Node {
	return youtrack.NewMap(youtrack.Pair{Key: "timestamp", Value: youtrack.NewString(moment)})
}

func activityUnknown(pairs ...youtrack.Pair) youtrack.Error {
	return youtrack.Error{Code: youtrack.CodeUnknownName, Details: pairs}
}

func TestListActivitiesRefusesACallItCannotSend(t *testing.T) {
	t.Parallel()
	every := strings.Split(activityEveryCategory, ",")
	tests := []struct {
		name       string
		fields     string
		categories []string
		want       youtrack.Error
	}{
		{name: "a name under the category", fields: "category(id)", want: youtrack.Error{Code: youtrack.CodeBadUsage}},
		{name: "a name under the field", fields: "field(name)", want: youtrack.Error{Code: youtrack.CodeBadUsage}},
		{name: "a category of nothing at all", fields: "timestamp", categories: []string{""}, want: youtrack.Error{Code: youtrack.CodeBadUsage}},
		{
			name:   "a name under the duration of a work item",
			fields: "target(duration(minutes))",
			want:   youtrack.Error{Code: youtrack.CodeBadUsage},
		},
		{
			name:   "a name no value of an activity declares",
			fields: "added(logn)",
			want: activityUnknown(
				youtrack.Pair{Key: "fields", Value: youtrack.NewString("added(logn)")},
				youtrack.Pair{Key: "unknown", Value: youtrack.NewList(withNearest("field", "added(logn)", "login"))}),
		},
		{
			name:   "a name no value declares at either end",
			fields: "added(verson),removed(urlz)",
			want: activityUnknown(
				youtrack.Pair{Key: "fields", Value: youtrack.NewString("added(verson),removed(urlz)")},
				youtrack.Pair{Key: "unknown", Value: youtrack.NewList(
					withNearest("field", "added(verson)", "version"),
					withNearest("field", "removed(urlz)", "url", "urls"))}),
		},
		{
			name:       "a category a letter short",
			fields:     "timestamp",
			categories: []string{"LinksCategry"},
			want: activityUnknown(youtrack.Pair{Key: "unknown", Value: youtrack.NewList(
				withNearest("category", "LinksCategry", "LinksCategory"))}),
		},
		{
			name:       "a category holding a letter of another alphabet",
			fields:     "timestamp",
			categories: []string{"LinksСategory"},
			want: activityUnknown(youtrack.Pair{Key: "unknown", Value: youtrack.NewList(
				withNearest("category", "LinksСategory", "LinksCategory"))}),
		},
		{
			name:       "two categories written as one name",
			fields:     "timestamp",
			categories: []string{"LinksCategory,CommentsCategory"},
			want: activityUnknown(youtrack.Pair{Key: "unknown", Value: youtrack.NewList(
				withNearest("category", "LinksCategory,CommentsCategory", every...))}),
		},
		{
			name:       "one misspelling written in two letter cases",
			fields:     "timestamp",
			categories: []string{"Bogus", "BOGUS"},
			want: activityUnknown(youtrack.Pair{Key: "unknown", Value: youtrack.NewList(
				withNearest("category", "Bogus", every...))}),
		},
		{
			name:       "two misspellings beside a category that resolves",
			fields:     "timestamp",
			categories: []string{"Bogus", "LinksCategory", "Nope"},
			want: activityUnknown(youtrack.Pair{Key: "unknown", Value: youtrack.NewList(
				withNearest("category", "Bogus", every...),
				withNearest("category", "Nope", every...))}),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := activityList(t, fake.ServeNothing(t), tc.fields, tc.categories...)

			assert.Equal(t, tc.want, errorOf(t, err))
		})
	}
}

func TestListActivitiesRefusesAnIssueOrAPageItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		issue string
		page  youtrack.Page
	}{
		{name: "no issue at all", issue: ""},
		{name: "an article", issue: "DEV-A-1"},
		{name: "the internal id of an issue", issue: "2-1"},
		{name: "a negative limit", issue: "DEV-1", page: youtrack.Page{Limit: -1}},
		{name: "a limit that leaves no room for the one activity past it", issue: "DEV-1", page: youtrack.Page{Limit: math.MaxInt32}},
		{name: "a negative skip", issue: "DEV-1", page: youtrack.Page{Skip: -1}},
		{name: "a skip past the largest int32", issue: "DEV-1", page: youtrack.Page{Skip: math.MaxInt32 + 1}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts := &youtrack.ListActivitiesOptions{Fields: "timestamp", Page: tc.page}
			_, err := client(t, fake.ServeNothing(t)).Activities.List(t.Context(), tc.issue, opts)

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

func TestListActivitiesAsksForTheCategoriesItResolves(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		categories []string
		sent       string
	}{
		{name: "no category, which asks for every one and the commits among them", sent: activityEveryCategory},
		{
			name:       "one category in two letter cases beside another",
			categories: []string{"linkscategory", "LINKSCATEGORY", "CommentsCategory"},
			sent:       "CommentsCategory,LinksCategory",
		},
		{name: "the commits alone", categories: []string{"vcschangecategory"}, sent: "VcsChangeCategory"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := activityServer(t, `[]`)

			_, err := activityList(t, server, "category", tc.categories...)

			require.NoError(t, err)
			assert.Equal(t, []string{tc.sent}, server.Last(t).URL.Query()["categories"])
		})
	}
}

func TestListActivitiesAsksForWhatItReadsBesideWhatItPrints(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		fields string
		sent   string
	}{
		{
			name:   "every part of an activity, which reads the field and the values of every category",
			fields: "timestamp,author(login),category,field,added(id,idReadable,login,name,urls),removed(id,idReadable,login,name,urls)",
			sent: "timestamp,author(login),category(id),field(name,customField(name,fieldType(valueType)))," +
				"added(id,idReadable,login,name,urls,minutes),removed(id,idReadable,login,name,urls,minutes)",
		},
		{name: "the category alone, beside which the moment is checked", fields: "category", sent: "category(id),timestamp"},
		{name: "the moment alone, beside which the category is read", fields: "timestamp", sent: "timestamp,category(id)"},
		{
			name:   "the field, which a custom field prints by its name in the project",
			fields: "field",
			sent:   "field(name,customField(name)),timestamp,category(id)",
		},
		{
			name:   "the values, which a custom field reads by its type and a duration by its minutes",
			fields: "added",
			sent:   "added(minutes),timestamp,category(id),field(customField(fieldType(valueType)))",
		},
		{
			name:   "the duration of a work item the activity stands for, which is read by its minutes",
			fields: "target(duration)",
			sent:   "target(duration(minutes)),timestamp,category(id)",
		},
		{
			name:   "the duration of a work item among the values, which is read by its minutes",
			fields: "added(duration)",
			sent:   "added(duration(minutes),minutes),timestamp,category(id),field(customField(fieldType(valueType)))",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := activityServer(t, `[]`)

			_, err := activityList(t, server, tc.fields, "CommentsCategory")

			require.NoError(t, err)
			assert.Equal(t, []string{tc.sent}, server.Fields())
		})
	}
}

func TestListActivitiesReadsTheLinkTypesOnlyToPrintTheFieldOfALink(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		fields     string
		categories []string
		sent       []string
	}{
		{name: "the field of every category", fields: "field", sent: []string{activityLinkTypesPath, activityListPath}},
		{name: "activities that print no field", fields: "timestamp,added", sent: []string{activityListPath}},
		{name: "activities of no link", fields: "field", categories: []string{"CommentsCategory"}, sent: []string{activityListPath}},
		{
			name:       "activities of links alone",
			fields:     "field",
			categories: []string{"LinksCategory"},
			sent:       []string{activityLinkTypesPath, activityListPath},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := activityServer(t, `[]`)

			_, err := activityList(t, server, tc.fields, tc.categories...)

			require.NoError(t, err)
			assert.Equal(t, tc.sent, server.Paths())
		})
	}
}

func TestListActivitiesSendsNoActivitiesWhereTheLinkTypesFail(t *testing.T) {
	t.Parallel()
	server := activityServerOf(t, fake.JSON(http.StatusInternalServerError, `{}`), fake.JSON(http.StatusOK, `[]`))

	_, err := activityList(t, server, "field")

	want := youtrack.Error{Code: youtrack.CodeUpstreamFailed, Details: []youtrack.Pair{
		lastRequest(t, server),
		{Key: "upstream_status", Value: number(http.StatusInternalServerError)},
	}}
	assert.Equal(t, want, errorOf(t, err))
	assert.Equal(t, []string{activityLinkTypesPath}, server.Paths())
}

func activityLinkTypesAsManyAsAskedFor() string {
	linkTypes := make([]string, 0, 1000)
	for at := range 1000 {
		linkTypes = append(linkTypes, fmt.Sprintf(`{"$type":"IssueLinkType","sourceToTarget":"goes with %d",`+
			`"targetToSource":"","localizedSourceToTarget":null,"localizedTargetToSource":null}`, at))
	}
	return activityArray(linkTypes...)
}

func TestListActivitiesRefusesLinkTypesItCannotRead(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		linkTypes string
	}{
		{
			name: "a phrase that is a number",
			linkTypes: `[{"$type":"IssueLinkType","sourceToTarget":7,"targetToSource":"",` +
				`"localizedSourceToTarget":null,"localizedTargetToSource":null}]`,
		},
		{
			name: "a translation that is a list",
			linkTypes: `[{"$type":"IssueLinkType","sourceToTarget":"leads to","targetToSource":"",` +
				`"localizedSourceToTarget":["Goes before"],"localizedTargetToSource":null}]`,
		},
		{name: "as many link types as were asked for", linkTypes: activityLinkTypesAsManyAsAskedFor()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := activityServerOf(t, fake.JSON(http.StatusOK, tc.linkTypes), fake.JSON(http.StatusOK, `[]`))

			_, err := activityList(t, server, "field")

			assert.Equal(t, unreadable(lastRequest(t, server), tc.linkTypes), errorOf(t, err))
			assert.Equal(t, []string{activityLinkTypesPath}, server.Paths())
		})
	}
}

func TestListActivitiesRefusesAnActivityItCannotRead(t *testing.T) {
	t.Parallel()
	const created = `"field":null,"added":[],"removed":[]`
	tests := []struct {
		name       string
		activities string
	}{
		{
			name: "an activity newer than the one before it",
			activities: activityArray(
				activityJSON("IssueCreatedActivityItem", "IssueCreatedCategory", activityEarly, created),
				activityJSON("IssueCreatedActivityItem", "IssueCreatedCategory", activityLate, created)),
		},
		{
			name: "a moment that is the text of one",
			activities: `[{"$type":"IssueCreatedActivityItem","timestamp":"1970-01-01T00:00:01Z",` +
				`"category":{"$type":"ActivityCategory","id":"IssueCreatedCategory"},` + created + `}]`,
		},
		{
			name:       "a category nobody asked for",
			activities: activityArray(activityJSON("VotersActivityItem", "VotersCategory", activityEarly, created)),
		},
		{
			name: "a category that is a list of one",
			activities: `[{"$type":"IssueCreatedActivityItem","timestamp":1000,` +
				`"category":[{"$type":"ActivityCategory","id":"IssueCreatedCategory"}],` + created + `}]`,
		},
		{
			name: "a category whose identifier is a number",
			activities: `[{"$type":"IssueCreatedActivityItem","timestamp":1000,` +
				`"category":{"$type":"ActivityCategory","id":7},` + created + `}]`,
		},
		{
			name:       "an object where the type of the field holds a bare value",
			activities: activityArray(activityOfField("period", `[{"$type":"DurationValue","id":"90","minutes":90}]`)),
		},
		{
			name:       "a bare value where the type of the field holds values with names of their own",
			activities: activityArray(activityOfField("state", `"Named"`)),
		},
		{
			name:       "a value of a type outside the custom-field types there are",
			activities: activityArray(activityOfField("unmodelled", `"Named"`)),
		},
		{name: "a fraction where the minutes of a period stand", activities: activityArray(activityOfField("period", `1.5`))},
		{name: "a text where the milliseconds of a moment stand", activities: activityArray(activityOfField("date and time", `"1970-01-01"`))},
		{
			name: "a change of a custom field standing for a filter that is no custom field",
			activities: activityArray(activityJSON("CustomFieldActivityItem", "CustomFieldCategory", activityEarly,
				`"field":{"$type":"PredefinedFilterField","name":"Label"},"added":[],"removed":[]`)),
		},
		{
			name: "a change of a custom field standing for no filter at all",
			activities: activityArray(activityJSON("CustomFieldActivityItem", "CustomFieldCategory", activityEarly,
				`"field":"Label","added":[],"removed":[]`)),
		},
		{
			name: "a custom field with no type of value",
			activities: activityArray(activityJSON("CustomFieldActivityItem", "CustomFieldCategory", activityEarly,
				`"field":{"$type":"CustomFilterField","name":"Label","customField":{"$type":"CustomField","name":"Named"}},`+
					`"added":[],"removed":[]`)),
		},
		{
			name: "a custom field with no name in the project",
			activities: activityArray(activityJSON("CustomFieldActivityItem", "CustomFieldCategory", activityEarly,
				`"field":{"$type":"CustomFilterField","name":"Label","customField":{"$type":"CustomField",`+
					`"fieldType":{"$type":"FieldType","valueType":"state"}}},"added":[],"removed":[]`)),
		},
		{
			name: "a link standing for its phrase rather than for a filter",
			activities: activityArray(activityJSON("LinksActivityItem", "LinksCategory", activityEarly,
				`"field":"Comes after","added":[],"removed":[]`)),
		},
		{
			name: "a link standing for a filter with no phrase",
			activities: activityArray(activityJSON("LinksActivityItem", "LinksCategory", activityEarly,
				`"field":{"$type":"LinkTypeFilterField"},"added":[],"removed":[]`)),
		},
		{name: "a link of a phrase no link type goes by", activities: activityArray(activityOfLink("Blocks"))},
		{name: "a link of a phrase two link types go by", activities: activityArray(activityOfLink("Goes before"))},
		{name: "a link of no phrase at all", activities: activityArray(activityOfLink(""))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := activityServer(t, tc.activities)

			_, err := activityList(t, server, "field,added,removed")

			assert.Equal(t, unreadable(lastRequest(t, server), tc.activities), errorOf(t, err))
		})
	}
}

func TestListActivitiesChecksTheOrderOfTheMomentsWhateverItPrints(t *testing.T) {
	t.Parallel()
	outOfOrder := activityArray(activityCreated(activityEarly), activityCreated(activityLate))
	for _, fields := range []string{"timestamp", "category"} {
		t.Run(fields, func(t *testing.T) {
			t.Parallel()
			server := activityServer(t, outOfOrder)

			_, err := activityList(t, server, fields)

			assert.Equal(t, unreadable(lastRequest(t, server), outOfOrder), errorOf(t, err))
		})
	}
}

func TestListActivitiesChecksTheFilterOfAChangedFieldWhateverItPrints(t *testing.T) {
	t.Parallel()
	predefined := activityArray(activityJSON("CustomFieldActivityItem", "CustomFieldCategory", activityEarly,
		`"field":{"$type":"PredefinedFilterField","name":"Label","customField":{"$type":"CustomField",`+
			`"name":"Named","fieldType":{"$type":"FieldType","valueType":"state"}}},"added":[],"removed":[]`))
	for _, fields := range []string{"field", "field,added", "added"} {
		t.Run(fields, func(t *testing.T) {
			t.Parallel()
			server := activityServer(t, predefined)

			_, err := activityList(t, server, fields)

			assert.Equal(t, unreadable(lastRequest(t, server), predefined), errorOf(t, err))
		})
	}
}

func TestListActivitiesPrintsThePageTheLimitAndTheSkipAskFor(t *testing.T) {
	t.Parallel()
	latest, late, early := activityCreated(activityLatest), activityCreated(activityLate), activityCreated(activityEarly)
	tests := []struct {
		name       string
		opts       *youtrack.ListActivitiesOptions
		activities string
		want       *youtrack.Node
		top, skip  string
	}{
		{
			name:       "no limit, which is the default one",
			opts:       &youtrack.ListActivitiesOptions{Fields: "timestamp"},
			activities: activityArray(early),
			want:       wholePage("activities", activityAt("1970-01-01T00:00:01Z")),
			top:        "51",
		},
		{
			name:       "a page the history goes past",
			opts:       &youtrack.ListActivitiesOptions{Fields: "timestamp", Page: youtrack.Page{Limit: 2}},
			activities: activityArray(latest, late, early),
			want: page("activities", youtrack.NewNull(), youtrack.NewBool(true),
				activityAt("1970-01-01T00:00:03Z"), activityAt("1970-01-01T00:00:02Z")),
			top: "3",
		},
		{
			name:       "a page the history fills to its end",
			opts:       &youtrack.ListActivitiesOptions{Fields: "timestamp", Page: youtrack.Page{Limit: 2}},
			activities: activityArray(late, early),
			want:       wholePage("activities", activityAt("1970-01-01T00:00:02Z"), activityAt("1970-01-01T00:00:01Z")),
			top:        "3",
		},
		{
			name:       "a last page short of the limit",
			opts:       &youtrack.ListActivitiesOptions{Fields: "timestamp", Page: youtrack.Page{Limit: 2, Skip: 2}},
			activities: activityArray(early),
			want:       page("activities", number(3), youtrack.NewBool(false), activityAt("1970-01-01T00:00:01Z")),
			top:        "3",
			skip:       "2",
		},
		{
			name:       "an empty page past the end, where the skip may have passed over the history",
			opts:       &youtrack.ListActivitiesOptions{Fields: "timestamp", Page: youtrack.Page{Limit: 2, Skip: 9}},
			activities: `[]`,
			want:       page("activities", youtrack.NewNull(), youtrack.NewBool(false)),
			top:        "3",
			skip:       "9",
		},
		{
			name:       "the largest limit, and the one activity past it the largest int32",
			opts:       &youtrack.ListActivitiesOptions{Fields: "timestamp", Page: youtrack.Page{Limit: math.MaxInt32 - 1}},
			activities: activityArray(early),
			want:       wholePage("activities", activityAt("1970-01-01T00:00:01Z")),
			top:        "2147483647",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := activityServer(t, tc.activities)

			got, err := client(t, server).Activities.List(t.Context(), "DEV-1", tc.opts)

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
			sent := server.Last(t).URL.Query()
			assert.Equal(t, []string{tc.top, tc.skip}, []string{sent.Get("$top"), sent.Get("$skip")})
		})
	}
}

func TestListActivitiesChecksTheActivityPastTheLimitWithTheRest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		past string
	}{
		{name: "an activity past the limit newer than the one before it", past: activityCreated(activityLatest)},
		{
			name: "an activity past the limit of a category nobody asked for",
			past: activityJSON("VotersActivityItem", "VotersCategory", activityEarly, `"field":null`),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			activities := activityArray(activityCreated(activityLate), tc.past)
			server := activityServer(t, activities)
			opts := &youtrack.ListActivitiesOptions{Fields: "timestamp", Page: youtrack.Page{Limit: 1}}

			_, err := client(t, server).Activities.List(t.Context(), "DEV-1", opts)

			assert.Equal(t, unreadable(lastRequest(t, server), activities), errorOf(t, err))
		})
	}
}

func TestListActivitiesRefusesMoreActivitiesThanTheLimitAndTheOnePastIt(t *testing.T) {
	t.Parallel()
	server := activityServer(t, activityArray(
		activityCreated(activityLatest), activityCreated(activityLate), activityCreated(activityEarly)))
	opts := &youtrack.ListActivitiesOptions{Fields: "timestamp", Page: youtrack.Page{Limit: 1}}

	_, err := client(t, server).Activities.List(t.Context(), "DEV-1", opts)

	want := youtrack.Error{Code: youtrack.CodeUpstreamInvalid, Details: []youtrack.Pair{
		lastRequest(t, server),
		{Key: "limit", Value: number(1)},
		{Key: "returned", Value: number(3)},
	}}
	assert.Equal(t, want, errorOf(t, err))
}

func TestListActivitiesPrintsTheActivitiesAsTheyArrive(t *testing.T) {
	t.Parallel()
	early, late := activityCreated(activityEarly), activityCreated(activityLate)
	tests := []struct {
		name       string
		activities string
		want       *youtrack.Node
	}{
		{name: "none at all", activities: `[]`, want: wholePage("activities")},
		{
			name:       "the newest first",
			activities: activityArray(late, early),
			want:       wholePage("activities", activityAt("1970-01-01T00:00:02Z"), activityAt("1970-01-01T00:00:01Z")),
		},
		{
			name:       "two of one moment",
			activities: activityArray(early, early),
			want:       wholePage("activities", activityAt("1970-01-01T00:00:01Z"), activityAt("1970-01-01T00:00:01Z")),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := activityServer(t, tc.activities)

			got, err := activityList(t, server, "timestamp")

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestListActivitiesPrintsTheFieldOfAChangeByItsCategory(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		activity string
		want     *youtrack.Node
	}{
		{
			name:     "a custom field, by its name in the project and not by the label of the change",
			activity: activityOfField("state", `[]`),
			want:     youtrack.NewString("Named"),
		},
		{
			name: "a filing, which changes no one field",
			activity: activityJSON("IssueCreatedActivityItem", "IssueCreatedCategory", activityEarly,
				`"field":{"$type":"PredefinedFilterField","name":"Label"}`),
			want: youtrack.NewNull(),
		},
		{name: "a link, by the phrase its label translates", activity: activityOfLink("Comes after"), want: youtrack.NewString("follows")},
		{name: "a link, whatever the letter case of its label", activity: activityOfLink("comes after"), want: youtrack.NewString("follows")},
		{name: "a link of a phrase with no translation", activity: activityOfLink("Relates to"), want: youtrack.NewString("relates to")},
		{name: "a link of a type written alike at both ends", activity: activityOfLink("Reflects"), want: youtrack.NewString("mirrors")},
		{
			name:     "a link of a type whose other end shares its phrase",
			activity: activityOfLink("Comes later"),
			want:     youtrack.NewString("succeeds"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := activityServer(t, activityArray(tc.activity))

			got, err := activityList(t, server, "field")

			require.NoError(t, err)
			assert.Equal(t, wholePage("activities", youtrack.NewMap(youtrack.Pair{Key: "field", Value: tc.want})), got)
		})
	}
}

func TestListActivitiesPrintsTheValuesOfAChangeAsAList(t *testing.T) {
	t.Parallel()
	issue := youtrack.NewMap(
		youtrack.Pair{Key: "id", Value: youtrack.NewString("1-2")},
		youtrack.Pair{Key: "idReadable", Value: youtrack.NewString("DEV-2")})
	const issueJSON = `{"$type":"Issue","id":"1-2","idReadable":"DEV-2"}`
	tests := []struct {
		name     string
		activity string
		added    *youtrack.Node
		removed  *youtrack.Node
	}{
		{
			name: "a moment put there and null taken away",
			activity: activityJSON("IssueResolvedActivityItem", "IssueResolvedCategory", activityEarly,
				`"field":null,"added":2000,"removed":null`),
			added:   texts("1970-01-01T00:00:02Z"),
			removed: youtrack.NewList(),
		},
		{
			name: "one text at either end",
			activity: activityJSON("SimpleValueActivityItem", "SummaryCategory", activityEarly,
				`"field":null,"added":"Late","removed":"Early"`),
			added:   texts("Late"),
			removed: texts("Early"),
		},
		{
			name: "a text of lines, which stays on the line of the record",
			activity: activityJSON("TextMarkupActivityItem", "DescriptionCategory", activityEarly,
				`"field":null,"added":"First\nSecond","removed":null`),
			added:   texts("First\nSecond"),
			removed: youtrack.NewList(),
		},
		{
			name: "a list of one issue and an empty list",
			activity: activityJSON("LinksActivityItem", "LinksCategory", activityEarly,
				`"field":null,"added":[`+issueJSON+`],"removed":[]`),
			added:   youtrack.NewList(issue),
			removed: youtrack.NewList(),
		},
		{
			name: "an issue on its own rather than in a list",
			activity: activityJSON("IssueCreatedActivityItem", "IssueCreatedCategory", activityEarly,
				`"field":null,"added":`+issueJSON+`,"removed":null`),
			added:   youtrack.NewList(issue),
			removed: youtrack.NewList(),
		},
		{
			name: "the duration of a work item, which is a period of its minutes",
			activity: activityJSON("WorkItemDurationActivityItem", "WorkItemCategory", activityEarly,
				`"field":null,"added":{"$type":"DurationValue","id":"120","minutes":120},`+
					`"removed":{"$type":"DurationValue","id":"90","minutes":90}`),
			added:   texts("PT2H"),
			removed: texts("PT1H30M"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := activityServer(t, activityArray(tc.activity))

			got, err := activityList(t, server, "added(id,idReadable),removed(id,idReadable)")

			require.NoError(t, err)
			want := youtrack.NewMap(youtrack.Pair{Key: "added", Value: tc.added}, youtrack.Pair{Key: "removed", Value: tc.removed})
			assert.Equal(t, wholePage("activities", want), got)
		})
	}
}

func TestListActivitiesPrintsTheValuesOfACustomFieldByTheTypeOfTheField(t *testing.T) {
	t.Parallel()
	named := func(id, name string) *youtrack.Node {
		return youtrack.NewMap(
			youtrack.Pair{Key: "id", Value: youtrack.NewString(id)},
			youtrack.Pair{Key: "name", Value: youtrack.NewString(name)})
	}
	tests := []struct {
		name      string
		valueType string
		added     string
		want      *youtrack.Node
	}{
		{
			name: "a state", valueType: "state",
			added: `[{"$type":"StateBundleElement","id":"1-1","name":"First"}]`,
			want:  youtrack.NewList(named("1-1", "First")),
		},
		{
			name: "an enum of two values", valueType: "enum",
			added: `[{"$type":"EnumBundleElement","id":"1-1","name":"First"},{"$type":"EnumBundleElement","id":"1-2","name":"Second"}]`,
			want:  youtrack.NewList(named("1-1", "First"), named("1-2", "Second")),
		},
		{
			name: "a group", valueType: "group",
			added: `[{"$type":"UserGroup","id":"1-1","name":"First"}]`,
			want:  youtrack.NewList(named("1-1", "First")),
		},
		{
			name: "a user", valueType: "user",
			added: `[{"$type":"User","id":"1-1","login":"first","name":"First"}]`,
			want: youtrack.NewList(youtrack.NewMap(
				youtrack.Pair{Key: "id", Value: youtrack.NewString("1-1")},
				youtrack.Pair{Key: "login", Value: youtrack.NewString("first")},
				youtrack.Pair{Key: "name", Value: youtrack.NewString("First")})),
		},
		{name: "a period", valueType: "period", added: `6755`, want: texts("PT112H35M")},
		{name: "a period of no minutes", valueType: "period", added: `0`, want: texts("PT0M")},
		{name: "a date", valueType: "date", added: `129600000`, want: texts("1970-01-02")},
		{name: "a moment", valueType: "date and time", added: `0`, want: texts("1970-01-01T00:00:00Z")},
		{name: "a whole number", valueType: "integer", added: `10`, want: youtrack.NewList(youtrack.NewNumber("10"))},
		{name: "a fraction", valueType: "float", added: `1.5`, want: youtrack.NewList(youtrack.NewNumber("1.5"))},
		{name: "a line of text", valueType: "string", added: `"Line"`, want: texts("Line")},
		{name: "a text of lines", valueType: "text", added: `"First\nSecond"`, want: texts("First\nSecond")},
		{name: "a field emptied outright", valueType: "state", added: `null`, want: youtrack.NewList()},
		{name: "a field nothing was put into", valueType: "state", added: `[]`, want: youtrack.NewList()},
		{name: "a field of bare values emptied outright", valueType: "period", added: `null`, want: youtrack.NewList()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := activityServer(t, activityArray(activityOfField(tc.valueType, tc.added)))

			got, err := activityList(t, server, "added(id,login,name)")

			require.NoError(t, err)
			assert.Equal(t, wholePage("activities", youtrack.NewMap(youtrack.Pair{Key: "added", Value: tc.want})), got)
		})
	}
}

func TestListActivitiesPrintsOfAValueTheNamesItsTypeDeclares(t *testing.T) {
	t.Parallel()
	comment := activityJSON("CommentActivityItem", "CommentsCategory", activityEarly,
		`"field":null,"added":[{"$type":"IssueComment","id":"1-1","text":"First","author":{"$type":"User","login":"first"}}],"removed":[]`)
	tests := []struct {
		name       string
		fields     string
		activities string
		want       []*youtrack.Node
	}{
		{
			name:       "names of the caller's own",
			fields:     "added(text,author(login))",
			activities: activityArray(comment),
			want: []*youtrack.Node{youtrack.NewMap(youtrack.Pair{Key: "added", Value: youtrack.NewList(youtrack.NewMap(
				youtrack.Pair{Key: "text", Value: youtrack.NewString("First")},
				youtrack.Pair{Key: "author", Value: youtrack.NewMap(youtrack.Pair{Key: "login", Value: youtrack.NewString("first")})}))})},
		},
		{
			name:       "the value with no names under it",
			fields:     "added",
			activities: activityArray(comment),
			want:       []*youtrack.Node{youtrack.NewMap(youtrack.Pair{Key: "added", Value: youtrack.NewList(youtrack.NewMap())})},
		},
		{
			name:   "a name only another type declares",
			fields: "category,added(login,idReadable)",
			activities: activityArray(
				activityJSON("CustomFieldActivityItem", "CustomFieldCategory", activityLate,
					`"field":{"$type":"CustomFilterField","name":"Label","customField":{"$type":"CustomField",`+
						`"name":"Named","fieldType":{"$type":"FieldType","valueType":"user"}}},`+
						`"added":[{"$type":"User","login":"first"}],"removed":[]`),
				activityJSON("LinksActivityItem", "LinksCategory", activityEarly,
					`"field":null,"added":[{"$type":"Issue","idReadable":"DEV-2"}],"removed":[]`),
				comment),
			want: []*youtrack.Node{
				youtrack.NewMap(
					youtrack.Pair{Key: "category", Value: youtrack.NewString("CustomFieldCategory")},
					youtrack.Pair{Key: "added", Value: youtrack.NewList(youtrack.NewMap(youtrack.Pair{Key: "login", Value: youtrack.NewString("first")}))}),
				youtrack.NewMap(
					youtrack.Pair{Key: "category", Value: youtrack.NewString("LinksCategory")},
					youtrack.Pair{Key: "added", Value: youtrack.NewList(youtrack.NewMap(youtrack.Pair{Key: "idReadable", Value: youtrack.NewString("DEV-2")}))}),
				youtrack.NewMap(
					youtrack.Pair{Key: "category", Value: youtrack.NewString("CommentsCategory")},
					youtrack.Pair{Key: "added", Value: youtrack.NewList(youtrack.NewMap())}),
			},
		},
		{
			name:   "names declared by no value that arrived",
			fields: "added(idReadable,localizedName,version),removed(text)",
			activities: activityArray(activityJSON("LinksActivityItem", "LinksCategory", activityEarly,
				`"field":null,"added":[{"$type":"Issue","idReadable":"DEV-2"}],"removed":[]`)),
			want: []*youtrack.Node{youtrack.NewMap(
				youtrack.Pair{Key: "added", Value: youtrack.NewList(youtrack.NewMap(youtrack.Pair{Key: "idReadable", Value: youtrack.NewString("DEV-2")}))},
				youtrack.Pair{Key: "removed", Value: youtrack.NewList()})},
		},
		{
			name:   "a commit by its link, its hash, its message and its moment",
			fields: "added(urls,version,text,date)",
			activities: activityArray(activityJSON("VcsChangeActivityItem", "VcsChangeCategory", activityEarly,
				`"field":null,"added":[{"$type":"VcsChange","urls":["https://vcs.example/commit/1"],"version":"1",`+
					`"text":"First\nSecond","date":1000}],"removed":[]`)),
			want: []*youtrack.Node{youtrack.NewMap(youtrack.Pair{Key: "added", Value: youtrack.NewList(youtrack.NewMap(
				youtrack.Pair{Key: "urls", Value: texts("https://vcs.example/commit/1")},
				youtrack.Pair{Key: "version", Value: youtrack.NewString("1")},
				youtrack.Pair{Key: "text", Value: youtrack.NewString("First\nSecond")},
				youtrack.Pair{Key: "date", Value: youtrack.NewString("1970-01-01T00:00:01Z")}))})},
		},
		{
			name:   "a field below the record, which is no field of a change",
			fields: "author(savedQueries(issues(customFields(projectCustomField(field(name))))))",
			activities: activityArray(activityJSON("IssueCreatedActivityItem", "IssueCreatedCategory", activityEarly,
				`"field":null,"author":{"$type":"User","savedQueries":[{"$type":"SavedQuery","issues":[{"$type":"Issue",`+
					`"customFields":[{"$type":"IssueCustomField","projectCustomField":{"$type":"ProjectCustomField",`+
					`"field":{"$type":"CustomField","name":"Named"}}}]}]}]}`)),
			want: []*youtrack.Node{youtrack.NewMap(youtrack.Pair{Key: "author", Value: youtrack.NewMap(
				youtrack.Pair{Key: "savedQueries", Value: youtrack.NewList(youtrack.NewMap(
					youtrack.Pair{Key: "issues", Value: youtrack.NewList(youtrack.NewMap(
						youtrack.Pair{Key: "customFields", Value: youtrack.NewList(youtrack.NewMap(
							youtrack.Pair{Key: "projectCustomField", Value: youtrack.NewMap(
								youtrack.Pair{Key: "field", Value: youtrack.NewMap(
									youtrack.Pair{Key: "name", Value: youtrack.NewString("Named")})})}))}))}))})})},
		},
		{
			name:   "the removed of an attachment below the record, which is no value of a change",
			fields: "author(savedQueries(issues(attachments(removed))))",
			activities: activityArray(activityJSON("IssueCreatedActivityItem", "IssueCreatedCategory", activityEarly,
				`"field":null,"author":{"$type":"User","savedQueries":[{"$type":"SavedQuery","issues":[{"$type":"Issue",`+
					`"attachments":[{"$type":"IssueAttachment","removed":false}]}]}]}`)),
			want: []*youtrack.Node{youtrack.NewMap(youtrack.Pair{Key: "author", Value: youtrack.NewMap(
				youtrack.Pair{Key: "savedQueries", Value: youtrack.NewList(youtrack.NewMap(
					youtrack.Pair{Key: "issues", Value: youtrack.NewList(youtrack.NewMap(
						youtrack.Pair{Key: "attachments", Value: youtrack.NewList(youtrack.NewMap(
							youtrack.Pair{Key: "removed", Value: youtrack.NewBool(false)}))}))}))})})},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := activityServer(t, tc.activities)

			got, err := activityList(t, server, tc.fields)

			require.NoError(t, err)
			assert.Equal(t, wholePage("activities", tc.want...), got)
		})
	}
}
