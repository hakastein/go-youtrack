package youtrack_test

import (
	"cmp"
	"context"
	"math"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/hakastein/youtrack"
	"github.com/hakastein/youtrack/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	workItemsOfTheIssue = "/api/issues/DEV-1/timeTracking/workItems"
	workItemOfTheIssue  = workItemsOfTheIssue + "/7-1"
)

const (
	workItemDayMidnight     = "1788220800000"
	workItemNextDayMidnight = "1788307200000"
)

const (
	workItemTypeFirst     = `{"$type":"WorkItemType","id":"8-1","name":"First"}`
	workItemTypeSecond    = `{"$type":"WorkItemType","id":"8-2","name":"Second"}`
	workItemTypeLowerTwin = `{"$type":"WorkItemType","id":"8-4","name":"Twin"}`
	workItemTypeUpperTwin = `{"$type":"WorkItemType","id":"8-3","name":"TWIN"}`
	workItemModeAttribute = `{"$type":"WorkItemProjectAttribute","id":"9-1","name":"Mode","values":[` +
		`{"$type":"WorkItemAttributeValue","id":"9-2","name":"Solo"},` +
		`{"$type":"WorkItemAttributeValue","id":"9-3","name":"Pair"}]}`
)

const (
	workItemModeEmpty = `[{"$type":"WorkItemAttribute","id":"9-1","name":"Mode","value":null}]`
	workItemModeSolo  = `[{"$type":"WorkItemAttribute","id":"9-1","name":"Mode",` +
		`"value":{"$type":"WorkItemAttributeValue","id":"9-2","name":"Solo"}}]`
	workItemModePair = `[{"$type":"WorkItemAttribute","id":"9-1","name":"Mode",` +
		`"value":{"$type":"WorkItemAttributeValue","id":"9-3","name":"Pair"}}]`
)

type workItemCall func(ctx context.Context, items *youtrack.WorkItemsService) (*youtrack.Node, error)

func workItemSettings(types, attributes string) string {
	return `{"$type":"Issue","idReadable":"DEV-1","project":{"$type":"Project","shortName":"DEV",` +
		`"plugins":{"$type":"ProjectPlugins","timeTrackingSettings":{"$type":"ProjectTimeTrackingSettings",` +
		`"workItemTypes":[` + types + `],"attributes":[` + attributes + `]}}}}`
}

func workItemProjectSettings() string {
	return workItemSettings(workItemTypeFirst+","+workItemTypeSecond, workItemModeAttribute)
}

type workItemAnswer struct {
	duration   string
	date       string
	text       string
	workType   string
	attributes string
}

func (a workItemAnswer) json() string {
	return `{"$type":"IssueWorkItem","id":"7-1","duration":` + cmp.Or(a.duration, workItemMinutes("90")) +
		`,"date":` + cmp.Or(a.date, "null") + `,"text":` + cmp.Or(a.text, "null") +
		`,"type":` + cmp.Or(a.workType, "null") + `,"attributes":` + cmp.Or(a.attributes, "[]") +
		`,"author":{"$type":"User","login":"author"},"issue":{"$type":"Issue","idReadable":"DEV-1"}}`
}

func workItemMinutes(minutes string) string {
	return `{"$type":"DurationValue","minutes":` + minutes + `}`
}

func workItemWriting(t *testing.T, settings string, answer workItemAnswer) *fake.Server {
	t.Helper()
	return fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			fake.JSON(http.StatusOK, settings)(w, r)
			return
		}
		fake.JSON(http.StatusOK, answer.json())(w, r)
	})
}

func workItemProjectOf(project string) string {
	return `{"$type":"Issue","idReadable":"DEV-1","project":` + project + `}`
}

func workItemTimeTracking(settings string) string {
	return workItemProjectOf(`{"$type":"Project","shortName":"DEV","plugins":{"$type":"ProjectPlugins",` +
		`"timeTrackingSettings":` + settings + `}}`)
}

func workItemMismatch(t *testing.T, server *fake.Server, field string, expected, actual *youtrack.Node) youtrack.Error {
	t.Helper()
	return youtrack.Error{
		Code:       youtrack.CodeUpstreamInvalid,
		AfterWrite: true,
		Details: []youtrack.Pair{
			lastRequest(t, server),
			{Key: "issue", Value: youtrack.NewString("DEV-1")},
			{Key: "id", Value: youtrack.NewString("7-1")},
			{Key: "mismatch", Value: youtrack.NewList(mismatch(field, expected, actual))},
		},
	}
}

func TestListWorkItemsAsksAPageOfTheDefaultFields(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, "[]"))

	node, err := client(t, server).WorkItems.List(t.Context(), "DEV-1", nil)

	require.NoError(t, err)
	assert.Equal(t, wholePage("workItems"), node)
	assert.Equal(t, []string{workItemsOfTheIssue}, server.Paths())
	assert.Equal(t, []url.Values{{
		"fields": {"id,duration(minutes),type(name),attributes(id,name,value(id,name)),author(login),date,text"},
		"$top":   {"50"},
	}}, server.Queries())
}

func TestWorkItemsRefuseAnAddressOfAnotherShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		call workItemCall
	}{
		{
			name: "a list of an article",
			call: func(ctx context.Context, items *youtrack.WorkItemsService) (*youtrack.Node, error) {
				return items.List(ctx, "DEV-A-1", nil)
			},
		},
		{
			name: "a creation under a project code",
			call: func(ctx context.Context, items *youtrack.WorkItemsService) (*youtrack.Node, error) {
				return items.Create(ctx, "DEV", &youtrack.WorkItemInput{Duration: time.Hour}, nil)
			},
		},
		{
			name: "an update under an internal id",
			call: func(ctx context.Context, items *youtrack.WorkItemsService) (*youtrack.Node, error) {
				return items.Update(ctx, "7-1", "7-1", &youtrack.WorkItemUpdate{Text: new("x")}, nil)
			},
		},
		{
			name: "an update of a work item under a readable id",
			call: func(ctx context.Context, items *youtrack.WorkItemsService) (*youtrack.Node, error) {
				return items.Update(ctx, "DEV-1", "DEV-2", &youtrack.WorkItemUpdate{Text: new("x")}, nil)
			},
		},
		{
			name: "a deletion of a work item under a path",
			call: func(ctx context.Context, items *youtrack.WorkItemsService) (*youtrack.Node, error) {
				return items.Delete(ctx, "DEV-1", "..")
			},
		},
		{
			name: "a deletion under an article",
			call: func(ctx context.Context, items *youtrack.WorkItemsService) (*youtrack.Node, error) {
				return items.Delete(ctx, "DEV-A-1", "7-1")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := tc.call(t.Context(), client(t, fake.ServeNothing(t)).WorkItems)

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

func TestCreateWorkItemRefusesADurationItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		spent time.Duration
	}{
		{name: "a negative one", spent: -time.Hour},
		{name: "seconds", spent: 30 * time.Second},
		{name: "minutes and seconds", spent: 90 * time.Second},
		{name: "an hour and a nanosecond", spent: time.Hour + time.Nanosecond},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := client(t, fake.ServeNothing(t)).WorkItems.Create(t.Context(), "DEV-1",
				&youtrack.WorkItemInput{Duration: tc.spent}, nil)

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

func TestCreateWorkItemRefusesADayItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		day  string
	}{
		{name: "the day first", day: "01.09.2026"},
		{name: "the month first", day: "09/01/2026"},
		{name: "a word for today", day: "today"},
		{name: "a month and a day of one digit", day: "2026-9-1"},
		{name: "a day the month has none of", day: "2026-02-30"},
		{name: "a time of day", day: "2026-09-01T15:30:00Z"},
		{name: "midnight of another time zone", day: "2026-09-01T00:00:00+03:00"},
		{name: "midnight UTC carried in an offset", day: "2026-08-31T21:00:00-03:00"},
		{name: "a moment with no offset", day: "2026-09-01T00:00:00"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := client(t, fake.ServeNothing(t)).WorkItems.Create(t.Context(), "DEV-1",
				&youtrack.WorkItemInput{Duration: time.Hour, Date: tc.day}, nil)

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

func TestCreateWorkItemRefusesWhatItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		in     *youtrack.WorkItemInput
		fields string
	}{
		{name: "a text that is no UTF-8", in: &youtrack.WorkItemInput{Duration: time.Hour, Text: "bad\xffbyte"}},
		{
			name: "an attribute with no name",
			in:   &youtrack.WorkItemInput{Duration: time.Hour, Attributes: []youtrack.AttributeWrite{{Value: "Pair"}}},
		},
		{
			name: "an attribute with no value",
			in:   &youtrack.WorkItemInput{Duration: time.Hour, Attributes: []youtrack.AttributeWrite{{Name: "Mode"}}},
		},
		{
			name: "an attribute set and emptied at once",
			in: &youtrack.WorkItemInput{Duration: time.Hour, Attributes: []youtrack.AttributeWrite{
				{Name: "Mode", Value: "Solo", Clear: true},
			}},
		},
		{
			name: "an attribute set twice in another letter case",
			in: &youtrack.WorkItemInput{Duration: time.Hour, Attributes: []youtrack.AttributeWrite{
				{Name: "Mode", Value: "Solo"},
				{Name: "MODE", Value: "Pair"},
			}},
		},
		{
			name:   "a name under the duration",
			in:     &youtrack.WorkItemInput{Duration: time.Hour},
			fields: "duration(minutes)",
		},
		{
			name:   "a custom field of the issue named",
			in:     &youtrack.WorkItemInput{Duration: time.Hour},
			fields: "+issue(customFields(State))",
		},
		{
			name:   "the comments of the issue",
			in:     &youtrack.WorkItemInput{Duration: time.Hour},
			fields: "+issue(comments)",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := client(t, fake.ServeNothing(t)).WorkItems.Create(t.Context(), "DEV-1", tc.in, answeredWith(tc.fields))

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

func TestUpdateWorkItemRefusesWhatItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		in     *youtrack.WorkItemUpdate
		fields string
	}{
		{name: "no update at all"},
		{name: "nothing to write at all", in: &youtrack.WorkItemUpdate{}},
		{name: "the type written and taken away", in: &youtrack.WorkItemUpdate{Type: new("First"), ClearType: true}},
		{name: "the text written and emptied", in: &youtrack.WorkItemUpdate{Text: new("x"), ClearText: true}},
		{
			name: "an attribute set and taken away in another letter case",
			in: &youtrack.WorkItemUpdate{Attributes: []youtrack.AttributeWrite{
				{Name: "Mode", Value: "Solo"},
				{Name: "MODE", Clear: true},
			}},
		},
		{
			name: "an attribute of no name taken away",
			in:   &youtrack.WorkItemUpdate{Attributes: []youtrack.AttributeWrite{{Clear: true}}},
		},
		{
			name: "an attribute with no value",
			in:   &youtrack.WorkItemUpdate{Attributes: []youtrack.AttributeWrite{{Name: "Mode"}}},
		},
		{name: "a duration of seconds", in: &youtrack.WorkItemUpdate{Duration: new(90 * time.Second)}},
		{name: "a day of nothing", in: &youtrack.WorkItemUpdate{Date: new("")}},
		{name: "a time of day", in: &youtrack.WorkItemUpdate{Date: new("2026-09-01T15:00:00Z")}},
		{name: "a type of no name", in: &youtrack.WorkItemUpdate{Type: new("")}},
		{name: "a text that is no UTF-8", in: &youtrack.WorkItemUpdate{Text: new("bad\xffbyte")}},
		{name: "a name under the duration", in: &youtrack.WorkItemUpdate{Text: new("x")}, fields: "+duration(minutes)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := client(t, fake.ServeNothing(t)).WorkItems.Update(t.Context(), "DEV-1", "7-1", tc.in,
				answeredWith(tc.fields))

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

func TestListWorkItemsRefusesAnExpressionItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
	}{
		{name: "the minutes of the duration", expression: "duration(minutes)"},
		{name: "the form the server writes a duration in for a person", expression: "duration(presentation)"},
		{name: "the id of the duration", expression: "+duration(id)"},
		{name: "a name under the attributes", expression: "id,attributes(id)"},
		{name: "a part of a link slot of the issue", expression: "issue(links(direction))"},
		{name: "a part of the parent slot of the issue", expression: "issue(parent(id))"},
		{name: "a custom field of the issue named", expression: "issue(customFields(State))"},
		{name: "the comments of the issue", expression: "issue(comments(text))"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := client(t, fake.ServeNothing(t)).WorkItems.List(t.Context(), "DEV-1",
				&youtrack.ListWorkItemsOptions{Fields: tc.expression})

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

func TestCreateWorkItemWritesWhatTheCallGives(t *testing.T) {
	t.Parallel()
	longest := time.Duration(math.MaxInt64).Truncate(time.Minute)
	tests := []struct {
		name   string
		in     youtrack.WorkItemInput
		answer workItemAnswer
		sent   string
	}{
		{name: "hours and minutes", in: youtrack.WorkItemInput{Duration: 90 * time.Minute}, sent: `{"duration":{"minutes":90}}`},
		{
			name:   "no time, which the server judges",
			in:     youtrack.WorkItemInput{},
			answer: workItemAnswer{duration: workItemMinutes("0")},
			sent:   `{"duration":{"minutes":0}}`,
		},
		{
			name:   "hours alone",
			in:     youtrack.WorkItemInput{Duration: 24 * time.Hour},
			answer: workItemAnswer{duration: workItemMinutes("1440")},
			sent:   `{"duration":{"minutes":1440}}`,
		},
		{
			name:   "the longest whole minutes a duration holds",
			in:     youtrack.WorkItemInput{Duration: longest},
			answer: workItemAnswer{duration: workItemMinutes("153722867")},
			sent:   `{"duration":{"minutes":153722867}}`,
		},
		{
			name:   "a calendar day, as its noon UTC",
			in:     youtrack.WorkItemInput{Duration: 90 * time.Minute, Date: "2026-09-01"},
			answer: workItemAnswer{date: workItemDayMidnight},
			sent:   `{"duration":{"minutes":90},"date":1788264000000}`,
		},
		{
			name:   "midnight UTC as a work item reads it back",
			in:     youtrack.WorkItemInput{Duration: 90 * time.Minute, Date: "2026-09-01T00:00:00Z"},
			answer: workItemAnswer{date: workItemDayMidnight},
			sent:   `{"duration":{"minutes":90},"date":1788264000000}`,
		},
		{
			name:   "midnight UTC to the millisecond",
			in:     youtrack.WorkItemInput{Duration: 90 * time.Minute, Date: "2026-09-01T00:00:00.000Z"},
			answer: workItemAnswer{date: workItemDayMidnight},
			sent:   `{"duration":{"minutes":90},"date":1788264000000}`,
		},
		{
			name:   "midnight UTC with an offset of none",
			in:     youtrack.WorkItemInput{Duration: 90 * time.Minute, Date: "2026-09-01T00:00:00+00:00"},
			answer: workItemAnswer{date: workItemDayMidnight},
			sent:   `{"duration":{"minutes":90},"date":1788264000000}`,
		},
		{
			name:   "a text byte for byte",
			in:     youtrack.WorkItemInput{Duration: 90 * time.Minute, Text: " a\r\nb\x00 "},
			answer: workItemAnswer{text: `" a\r\nb\u0000 "`},
			sent:   `{"duration":{"minutes":90},"text":" a\r\nb\u0000 "}`,
		},
		{
			name: "an empty text, as no text",
			in:   youtrack.WorkItemInput{Duration: 90 * time.Minute, Text: ""},
			sent: `{"duration":{"minutes":90}}`,
		},
		{
			name: "an attribute by the ids of the project, named in another letter case",
			in: youtrack.WorkItemInput{Duration: 90 * time.Minute, Attributes: []youtrack.AttributeWrite{
				{Name: "mode", Value: "pair"},
			}},
			answer: workItemAnswer{attributes: workItemModePair},
			sent:   `{"duration":{"minutes":90},"attributes":[{"id":"9-1","value":{"id":"9-3"}}]}`,
		},
		{
			name: "an attribute written empty",
			in: youtrack.WorkItemInput{Duration: 90 * time.Minute, Attributes: []youtrack.AttributeWrite{
				{Name: "Mode", Clear: true},
			}},
			answer: workItemAnswer{attributes: workItemModeEmpty},
			sent:   `{"duration":{"minutes":90},"attributes":[{"id":"9-1","value":null}]}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := workItemWriting(t, workItemProjectSettings(), tc.answer)

			_, err := client(t, server).WorkItems.Create(t.Context(), "DEV-1", &tc.in, answeredWith("id"))

			require.NoError(t, err)
			assert.Equal(t, tc.sent, server.Last(t).Body)
		})
	}
}

func TestUpdateWorkItemWritesTheNamedPartsAlone(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		in     youtrack.WorkItemUpdate
		answer workItemAnswer
		sent   string
	}{
		{
			name:   "the text alone",
			in:     youtrack.WorkItemUpdate{Text: new("x")},
			answer: workItemAnswer{text: `"x"`},
			sent:   `{"text":"x"}`,
		},
		{
			name:   "the text written empty",
			in:     youtrack.WorkItemUpdate{Text: new("")},
			answer: workItemAnswer{text: `""`},
			sent:   `{"text":""}`,
		},
		{name: "the text emptied", in: youtrack.WorkItemUpdate{ClearText: true}, sent: `{"text":null}`},
		{name: "the type taken away", in: youtrack.WorkItemUpdate{ClearType: true}, sent: `{"type":null}`},
		{
			name:   "how long it is and the day it is written against",
			in:     youtrack.WorkItemUpdate{Duration: new(2 * time.Hour), Date: new("2026-09-02")},
			answer: workItemAnswer{duration: workItemMinutes("120"), date: workItemNextDayMidnight},
			sent:   `{"duration":{"minutes":120},"date":1788350400000}`,
		},
		{
			name:   "the type by the id of the project",
			in:     youtrack.WorkItemUpdate{Type: new("Second")},
			answer: workItemAnswer{workType: workItemTypeSecond},
			sent:   `{"type":{"id":"8-2"}}`,
		},
		{
			name:   "an attribute set",
			in:     youtrack.WorkItemUpdate{Attributes: []youtrack.AttributeWrite{{Name: "Mode", Value: "Solo"}}},
			answer: workItemAnswer{attributes: workItemModeSolo},
			sent:   `{"attributes":[{"id":"9-1","value":{"id":"9-2"}}]}`,
		},
		{
			name:   "an attribute taken away, named in another letter case",
			in:     youtrack.WorkItemUpdate{Attributes: []youtrack.AttributeWrite{{Name: "MODE", Clear: true}}},
			answer: workItemAnswer{attributes: workItemModeEmpty},
			sent:   `{"attributes":[{"id":"9-1","value":null}]}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := workItemWriting(t, workItemProjectSettings(), tc.answer)

			_, err := client(t, server).WorkItems.Update(t.Context(), "DEV-1", "7-1", &tc.in, answeredWith("id"))

			require.NoError(t, err)
			assert.Equal(t, tc.sent, server.Last(t).Body)
		})
	}
}

func TestWorkItemWriteAsksForWhatItChecksWhateverWasAskedToPrint(t *testing.T) {
	t.Parallel()
	const settingsFields = "idReadable,project(shortName,plugins(timeTrackingSettings(workItemTypes(id,name))))"
	const settingsWithAttributes = "idReadable,project(shortName,plugins(timeTrackingSettings(workItemTypes(id,name)," +
		"attributes(id,name,values(id,name)))))"
	const attributesAsked = "attributes(id,name,value(id,name))"
	tests := []struct {
		name    string
		write   workItemCall
		answer  workItemAnswer
		targets []string
	}{
		{
			name: "a creation that names no type and no attribute",
			write: func(ctx context.Context, items *youtrack.WorkItemsService) (*youtrack.Node, error) {
				return items.Create(ctx, "dev-1", &youtrack.WorkItemInput{Duration: 90 * time.Minute}, answeredWith("id"))
			},
			targets: []string{"/api/issues/dev-1/timeTracking/workItems?fields=id,duration(minutes),date,text,issue(idReadable)"},
		},
		{
			name: "a creation that names a type",
			write: func(ctx context.Context, items *youtrack.WorkItemsService) (*youtrack.Node, error) {
				return items.Create(ctx, "dev-1", &youtrack.WorkItemInput{Duration: 90 * time.Minute, Type: "First"},
					answeredWith("id"))
			},
			answer: workItemAnswer{workType: workItemTypeFirst},
			targets: []string{
				"/api/issues/dev-1?fields=" + settingsFields,
				workItemsOfTheIssue + "?fields=id,duration(minutes),date,text,issue(idReadable),type(id,name)",
			},
		},
		{
			name: "a creation that names a type and an attribute",
			write: func(ctx context.Context, items *youtrack.WorkItemsService) (*youtrack.Node, error) {
				return items.Create(ctx, "dev-1", &youtrack.WorkItemInput{Duration: 90 * time.Minute, Type: "First",
					Attributes: []youtrack.AttributeWrite{{Name: "Mode", Value: "Solo"}}}, answeredWith("id"))
			},
			answer: workItemAnswer{workType: workItemTypeFirst, attributes: workItemModeSolo},
			targets: []string{
				"/api/issues/dev-1?fields=" + settingsWithAttributes,
				workItemsOfTheIssue + "?fields=id,duration(minutes),date,text,issue(idReadable),type(id,name)," +
					attributesAsked,
			},
		},
		{
			name: "an update of the text",
			write: func(ctx context.Context, items *youtrack.WorkItemsService) (*youtrack.Node, error) {
				return items.Update(ctx, "dev-1", "7-1", &youtrack.WorkItemUpdate{Text: new("x")}, answeredWith("id"))
			},
			answer:  workItemAnswer{text: `"x"`},
			targets: []string{"/api/issues/dev-1/timeTracking/workItems/7-1?fields=id,text"},
		},
		{
			name: "an update of how long it is and the day",
			write: func(ctx context.Context, items *youtrack.WorkItemsService) (*youtrack.Node, error) {
				return items.Update(ctx, "DEV-1", "7-1", &youtrack.WorkItemUpdate{Duration: new(90 * time.Minute),
					Date: new("2026-09-01")}, answeredWith("id"))
			},
			answer:  workItemAnswer{date: workItemDayMidnight},
			targets: []string{workItemOfTheIssue + "?fields=id,duration(minutes),date"},
		},
		{
			name: "an update emptying the text",
			write: func(ctx context.Context, items *youtrack.WorkItemsService) (*youtrack.Node, error) {
				return items.Update(ctx, "DEV-1", "7-1", &youtrack.WorkItemUpdate{ClearText: true}, answeredWith("id"))
			},
			targets: []string{workItemOfTheIssue + "?fields=id,text"},
		},
		{
			name: "an update taking the type away",
			write: func(ctx context.Context, items *youtrack.WorkItemsService) (*youtrack.Node, error) {
				return items.Update(ctx, "DEV-1", "7-1", &youtrack.WorkItemUpdate{ClearType: true}, answeredWith("id"))
			},
			targets: []string{workItemOfTheIssue + "?fields=id,type(name)"},
		},
		{
			name: "an update of the type",
			write: func(ctx context.Context, items *youtrack.WorkItemsService) (*youtrack.Node, error) {
				return items.Update(ctx, "dev-1", "7-1", &youtrack.WorkItemUpdate{Type: new("First")}, answeredWith("id"))
			},
			answer: workItemAnswer{workType: workItemTypeFirst},
			targets: []string{
				"/api/issues/dev-1?fields=" + settingsFields,
				workItemOfTheIssue + "?fields=id,type(id,name)",
			},
		},
		{
			name: "an update taking an attribute away",
			write: func(ctx context.Context, items *youtrack.WorkItemsService) (*youtrack.Node, error) {
				return items.Update(ctx, "dev-1", "7-1", &youtrack.WorkItemUpdate{Attributes: []youtrack.AttributeWrite{
					{Name: "Mode", Clear: true},
				}}, answeredWith("id"))
			},
			answer: workItemAnswer{attributes: workItemModeEmpty},
			targets: []string{
				"/api/issues/dev-1?fields=" + settingsWithAttributes,
				workItemOfTheIssue + "?fields=id," + attributesAsked,
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := workItemWriting(t, workItemProjectSettings(), tc.answer)

			node, err := tc.write(t.Context(), client(t, server).WorkItems)

			require.NoError(t, err)
			assert.Equal(t, youtrack.NewMap(youtrack.Pair{Key: "id", Value: youtrack.NewString("7-1")}), node)
			assert.Equal(t, tc.targets, server.Targets(t))
		})
	}
}

func TestCreateWorkItemRefusesWhatTheServerKeptOtherwise(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		day        string
		text       string
		workType   string
		attributes []youtrack.AttributeWrite
		answer     workItemAnswer
		field      string
		expected   *youtrack.Node
		actual     *youtrack.Node
	}{
		{
			name:     "a duration of another length",
			answer:   workItemAnswer{duration: workItemMinutes("60")},
			field:    "duration",
			expected: youtrack.NewString("PT1H30M"),
			actual:   youtrack.NewString("PT1H"),
		},
		{
			name:     "no duration at all",
			answer:   workItemAnswer{duration: "null"},
			field:    "duration",
			expected: youtrack.NewString("PT1H30M"),
			actual:   youtrack.NewNull(),
		},
		{
			name:     "a day after the one written",
			day:      "2026-09-01",
			answer:   workItemAnswer{date: workItemNextDayMidnight},
			field:    "date",
			expected: youtrack.NewString("2026-09-01"),
			actual:   youtrack.NewString("2026-09-02T00:00:00Z"),
		},
		{
			name:     "a day that is no number of milliseconds",
			day:      "2026-09-01",
			answer:   workItemAnswer{date: `"2026-09-01"`},
			field:    "date",
			expected: youtrack.NewString("2026-09-01"),
			actual:   youtrack.NewNull(),
		},
		{
			name:     "no day at all",
			day:      "2026-09-01",
			field:    "date",
			expected: youtrack.NewString("2026-09-01"),
			actual:   youtrack.NewNull(),
		},
		{
			name:     "a text the server rewrote",
			text:     "a\rb",
			answer:   workItemAnswer{text: `"a\nb"`},
			field:    "text",
			expected: youtrack.NewString("a\rb"),
			actual:   youtrack.NewString("a\nb"),
		},
		{
			name:     "a text in another letter case",
			text:     "Upper",
			answer:   workItemAnswer{text: `"upper"`},
			field:    "text",
			expected: youtrack.NewString("Upper"),
			actual:   youtrack.NewString("upper"),
		},
		{
			name:     "another type of the project",
			workType: "first",
			answer:   workItemAnswer{workType: workItemTypeSecond},
			field:    "type",
			expected: youtrack.NewString("first"),
			actual:   youtrack.NewString("Second"),
		},
		{
			name:     "no type at all",
			workType: "First",
			field:    "type",
			expected: youtrack.NewString("First"),
			actual:   youtrack.NewNull(),
		},
		{
			name:       "an attribute kept empty",
			attributes: []youtrack.AttributeWrite{{Name: "Mode", Value: "Pair"}},
			answer:     workItemAnswer{attributes: workItemModeEmpty},
			field:      "Mode",
			expected:   youtrack.NewString("Pair"),
			actual:     youtrack.NewNull(),
		},
		{
			name:       "an attribute kept with another value",
			attributes: []youtrack.AttributeWrite{{Name: "Mode", Value: "Pair"}},
			answer:     workItemAnswer{attributes: workItemModeSolo},
			field:      "Mode",
			expected:   youtrack.NewString("Pair"),
			actual:     youtrack.NewString("Solo"),
		},
		{
			name:       "an attribute left after it was written empty",
			attributes: []youtrack.AttributeWrite{{Name: "Mode", Clear: true}},
			answer:     workItemAnswer{attributes: workItemModeSolo},
			field:      "Mode",
			expected:   youtrack.NewNull(),
			actual:     youtrack.NewString("Solo"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := workItemWriting(t, workItemProjectSettings(), tc.answer)

			_, err := client(t, server).WorkItems.Create(t.Context(), "DEV-1", &youtrack.WorkItemInput{
				Duration:   90 * time.Minute,
				Date:       tc.day,
				Text:       tc.text,
				Type:       tc.workType,
				Attributes: tc.attributes,
			}, answeredWith("id"))

			assert.Equal(t, workItemMismatch(t, server, tc.field, tc.expected, tc.actual), errorOf(t, err))
		})
	}
}

func TestUpdateWorkItemRefusesWhatTheServerKeptOtherwise(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		in       youtrack.WorkItemUpdate
		answer   workItemAnswer
		field    string
		expected *youtrack.Node
		actual   *youtrack.Node
	}{
		{
			name:     "a duration of another length",
			in:       youtrack.WorkItemUpdate{Duration: new(2 * time.Hour)},
			field:    "duration",
			expected: youtrack.NewString("PT2H"),
			actual:   youtrack.NewString("PT1H30M"),
		},
		{
			name:     "no duration at all",
			in:       youtrack.WorkItemUpdate{Duration: new(2 * time.Hour)},
			answer:   workItemAnswer{duration: "null"},
			field:    "duration",
			expected: youtrack.NewString("PT2H"),
			actual:   youtrack.NewNull(),
		},
		{
			name:     "a day after the one written",
			in:       youtrack.WorkItemUpdate{Date: new("2026-09-01")},
			answer:   workItemAnswer{date: workItemNextDayMidnight},
			field:    "date",
			expected: youtrack.NewString("2026-09-01"),
			actual:   youtrack.NewString("2026-09-02T00:00:00Z"),
		},
		{
			name:     "another text",
			in:       youtrack.WorkItemUpdate{Text: new("x")},
			answer:   workItemAnswer{text: `"y"`},
			field:    "text",
			expected: youtrack.NewString("x"),
			actual:   youtrack.NewString("y"),
		},
		{
			name:     "a text in another letter case",
			in:       youtrack.WorkItemUpdate{Text: new("Upper")},
			answer:   workItemAnswer{text: `"upper"`},
			field:    "text",
			expected: youtrack.NewString("Upper"),
			actual:   youtrack.NewString("upper"),
		},
		{
			name:     "a text left after it was emptied",
			in:       youtrack.WorkItemUpdate{ClearText: true},
			answer:   workItemAnswer{text: `"x"`},
			field:    "text",
			expected: youtrack.NewNull(),
			actual:   youtrack.NewString("x"),
		},
		{
			name:     "a text emptied to an empty string",
			in:       youtrack.WorkItemUpdate{ClearText: true},
			answer:   workItemAnswer{text: `""`},
			field:    "text",
			expected: youtrack.NewNull(),
			actual:   youtrack.NewString(""),
		},
		{
			name:     "a type left after it was taken away",
			in:       youtrack.WorkItemUpdate{ClearType: true},
			answer:   workItemAnswer{workType: workItemTypeFirst},
			field:    "type",
			expected: youtrack.NewNull(),
			actual:   youtrack.NewString("First"),
		},
		{
			name:     "another type of the project",
			in:       youtrack.WorkItemUpdate{Type: new("First")},
			answer:   workItemAnswer{workType: workItemTypeSecond},
			field:    "type",
			expected: youtrack.NewString("First"),
			actual:   youtrack.NewString("Second"),
		},
		{
			name:     "an attribute left after it was taken away",
			in:       youtrack.WorkItemUpdate{Attributes: []youtrack.AttributeWrite{{Name: "Mode", Clear: true}}},
			answer:   workItemAnswer{attributes: workItemModePair},
			field:    "Mode",
			expected: youtrack.NewNull(),
			actual:   youtrack.NewString("Pair"),
		},
		{
			name:     "an attribute kept with another value",
			in:       youtrack.WorkItemUpdate{Attributes: []youtrack.AttributeWrite{{Name: "Mode", Value: "Pair"}}},
			answer:   workItemAnswer{attributes: workItemModeSolo},
			field:    "Mode",
			expected: youtrack.NewString("Pair"),
			actual:   youtrack.NewString("Solo"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := workItemWriting(t, workItemProjectSettings(), tc.answer)

			_, err := client(t, server).WorkItems.Update(t.Context(), "DEV-1", "7-1", &tc.in, answeredWith("id"))

			assert.Equal(t, workItemMismatch(t, server, tc.field, tc.expected, tc.actual), errorOf(t, err))
		})
	}
}

func TestCreateWorkItemTellsAKeptTypeByItsIDFromOneOfTheSameName(t *testing.T) {
	t.Parallel()
	server := workItemWriting(t, workItemSettings(workItemTypeLowerTwin+","+workItemTypeUpperTwin, ""),
		workItemAnswer{workType: workItemTypeLowerTwin})

	_, err := client(t, server).WorkItems.Create(t.Context(), "DEV-1",
		&youtrack.WorkItemInput{Duration: 90 * time.Minute, Type: "TWIN"}, answeredWith("id"))

	assert.Equal(t, workItemMismatch(t, server, "type", youtrack.NewString("TWIN"), youtrack.NewString("Twin")),
		errorOf(t, err))
}

func TestWorkItemWriteChecksOnlyWhatItWrote(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		write   workItemCall
		answer  workItemAnswer
		printed *youtrack.Node
	}{
		{
			name: "a day kept at midnight UTC of the day written",
			write: func(ctx context.Context, items *youtrack.WorkItemsService) (*youtrack.Node, error) {
				return items.Create(ctx, "DEV-1", &youtrack.WorkItemInput{Duration: 90 * time.Minute, Date: "2026-09-01"},
					answeredWith("date"))
			},
			answer:  workItemAnswer{date: workItemDayMidnight},
			printed: youtrack.NewMap(youtrack.Pair{Key: "date", Value: youtrack.NewString("2026-09-01T00:00:00Z")}),
		},
		{
			name: "a creation that wrote no day and no text",
			write: func(ctx context.Context, items *youtrack.WorkItemsService) (*youtrack.Node, error) {
				return items.Create(ctx, "DEV-1", &youtrack.WorkItemInput{Duration: 90 * time.Minute}, answeredWith("date,text"))
			},
			answer: workItemAnswer{date: workItemNextDayMidnight, text: `"first\nsecond"`},
			printed: youtrack.NewMap(
				youtrack.Pair{Key: "date", Value: youtrack.NewString("2026-09-02T00:00:00Z")},
				youtrack.Pair{Key: "text", Value: youtrack.NewText("first\nsecond")}),
		},
		{
			name: "an update that wrote the text alone",
			write: func(ctx context.Context, items *youtrack.WorkItemsService) (*youtrack.Node, error) {
				return items.Update(ctx, "DEV-1", "7-1", &youtrack.WorkItemUpdate{Text: new("x")},
					answeredWith("duration,type(name),date"))
			},
			answer: workItemAnswer{duration: workItemMinutes("45"), workType: workItemTypeSecond,
				date: workItemNextDayMidnight, text: `"x"`},
			printed: youtrack.NewMap(
				youtrack.Pair{Key: "duration", Value: youtrack.NewString("PT45M")},
				youtrack.Pair{Key: "type", Value: youtrack.NewMap(youtrack.Pair{Key: "name", Value: youtrack.NewString("Second")})},
				youtrack.Pair{Key: "date", Value: youtrack.NewString("2026-09-02T00:00:00Z")}),
		},
		{
			name: "an attribute taken away that the answer does not carry",
			write: func(ctx context.Context, items *youtrack.WorkItemsService) (*youtrack.Node, error) {
				return items.Update(ctx, "DEV-1", "7-1", &youtrack.WorkItemUpdate{Attributes: []youtrack.AttributeWrite{
					{Name: "Mode", Clear: true},
				}}, answeredWith("id"))
			},
			printed: youtrack.NewMap(youtrack.Pair{Key: "id", Value: youtrack.NewString("7-1")}),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := workItemWriting(t, workItemProjectSettings(), tc.answer)

			node, err := tc.write(t.Context(), client(t, server).WorkItems)

			require.NoError(t, err)
			assert.Equal(t, tc.printed, node)
		})
	}
}

func TestCreateWorkItemResolvesATypeOfTheProjectByName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		types string
		named string
		kept  string
		sent  string
	}{
		{
			name:  "in lower case",
			types: workItemTypeFirst + "," + workItemTypeSecond,
			named: "second",
			kept:  workItemTypeSecond,
			sent:  `{"duration":{"minutes":90},"type":{"id":"8-2"}}`,
		},
		{
			name:  "in upper case",
			types: workItemTypeFirst + "," + workItemTypeSecond,
			named: "FIRST",
			kept:  workItemTypeFirst,
			sent:  `{"duration":{"minutes":90},"type":{"id":"8-1"}}`,
		},
		{
			name:  "the spelling of one of two types that differ in letter case alone",
			types: workItemTypeLowerTwin + "," + workItemTypeUpperTwin,
			named: "TWIN",
			kept:  workItemTypeUpperTwin,
			sent:  `{"duration":{"minutes":90},"type":{"id":"8-3"}}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := workItemWriting(t, workItemSettings(tc.types, ""), workItemAnswer{workType: tc.kept})

			_, err := client(t, server).WorkItems.Create(t.Context(), "DEV-1",
				&youtrack.WorkItemInput{Duration: 90 * time.Minute, Type: tc.named}, answeredWith("id"))

			require.NoError(t, err)
			assert.Equal(t, tc.sent, server.Last(t).Body)
		})
	}
}

func TestWorkItemWriteRefusesANameTheProjectDoesNotHave(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		settings string
		write    workItemCall
		unknown  []*youtrack.Node
	}{
		{
			name:     "a type a letter short",
			settings: workItemProjectSettings(),
			write: func(ctx context.Context, items *youtrack.WorkItemsService) (*youtrack.Node, error) {
				return items.Create(ctx, "DEV-1", &youtrack.WorkItemInput{Duration: time.Hour, Type: "Secnd"}, nil)
			},
			unknown: []*youtrack.Node{withNearest("type", "Secnd", "Second")},
		},
		{
			name:     "a type near none of the project",
			settings: workItemProjectSettings(),
			write: func(ctx context.Context, items *youtrack.WorkItemsService) (*youtrack.Node, error) {
				return items.Create(ctx, "DEV-1", &youtrack.WorkItemInput{Duration: time.Hour, Type: "zzzzzzzz"}, nil)
			},
			unknown: []*youtrack.Node{withNearest("type", "zzzzzzzz", "First", "Second")},
		},
		{
			name:     "a type two types answer to in another letter case",
			settings: workItemSettings(workItemTypeLowerTwin+","+workItemTypeUpperTwin, ""),
			write: func(ctx context.Context, items *youtrack.WorkItemsService) (*youtrack.Node, error) {
				return items.Update(ctx, "DEV-1", "7-1", &youtrack.WorkItemUpdate{Type: new("twin")}, nil)
			},
			unknown: []*youtrack.Node{withNearest("type", "twin", "TWIN", "Twin")},
		},
		{
			name:     "an attribute the project has none of",
			settings: workItemProjectSettings(),
			write: func(ctx context.Context, items *youtrack.WorkItemsService) (*youtrack.Node, error) {
				return items.Create(ctx, "DEV-1", &youtrack.WorkItemInput{Duration: time.Hour,
					Attributes: []youtrack.AttributeWrite{{Name: "Mood", Value: "Pair"}}}, nil)
			},
			unknown: []*youtrack.Node{withNearest("attribute", "Mood", "Mode")},
		},
		{
			name:     "a value the attribute does not take",
			settings: workItemProjectSettings(),
			write: func(ctx context.Context, items *youtrack.WorkItemsService) (*youtrack.Node, error) {
				return items.Create(ctx, "DEV-1", &youtrack.WorkItemInput{Duration: time.Hour,
					Attributes: []youtrack.AttributeWrite{{Name: "Mode", Value: "Trio"}}}, nil)
			},
			unknown: []*youtrack.Node{youtrack.NewMap(
				youtrack.Pair{Key: "attribute", Value: youtrack.NewString("Mode")},
				youtrack.Pair{Key: "value", Value: youtrack.NewString("Trio")},
				youtrack.Pair{Key: "nearest", Value: texts("Pair", "Solo")})},
		},
		{
			name:     "a value set and an attribute taken away, both unknown",
			settings: workItemProjectSettings(),
			write: func(ctx context.Context, items *youtrack.WorkItemsService) (*youtrack.Node, error) {
				return items.Update(ctx, "DEV-1", "7-1", &youtrack.WorkItemUpdate{Attributes: []youtrack.AttributeWrite{
					{Name: "Mode", Value: "Sol"},
					{Name: "Mood", Clear: true},
				}}, nil)
			},
			unknown: []*youtrack.Node{
				youtrack.NewMap(
					youtrack.Pair{Key: "attribute", Value: youtrack.NewString("Mode")},
					youtrack.Pair{Key: "value", Value: youtrack.NewString("Sol")},
					youtrack.Pair{Key: "nearest", Value: texts("Solo")}),
				withNearest("attribute", "Mood", "Mode"),
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := workItemWriting(t, tc.settings, workItemAnswer{})

			_, err := tc.write(t.Context(), client(t, server).WorkItems)

			assert.Equal(t, youtrack.Error{
				Code: youtrack.CodeUnknownName,
				Details: []youtrack.Pair{
					lastRequest(t, server),
					{Key: "project", Value: youtrack.NewString("DEV")},
					{Key: "unknown", Value: youtrack.NewList(tc.unknown...)},
				},
			}, errorOf(t, err))
			assert.Equal(t, []string{"/api/issues/DEV-1"}, server.Paths())
		})
	}
}

func TestWorkItemWriteRefusesSettingsOfAnotherShape(t *testing.T) {
	t.Parallel()
	mode := []youtrack.AttributeWrite{{Name: "Mode", Value: "Solo"}}
	tests := []struct {
		name       string
		read       string
		attributes []youtrack.AttributeWrite
	}{
		{name: "an issue under the readable id of an article", read: `{"$type":"Issue","idReadable":"DEV-A-1",` +
			`"project":{"$type":"Project","shortName":"DEV","plugins":null}}`},
		{name: "a project that is no object", read: workItemProjectOf("null")},
		{
			name: "a project with no text for a short name",
			read: workItemProjectOf(`{"$type":"Project","shortName":null,"plugins":null}`),
		},
		{name: "no plugins", read: workItemProjectOf(`{"$type":"Project","shortName":"DEV","plugins":null}`)},
		{name: "no settings of time tracking", read: workItemTimeTracking("null")},
		{name: "no types of work", read: workItemTimeTracking(`{"workItemTypes":null,"attributes":[]}`)},
		{name: "a type that is null", read: workItemTimeTracking(`{"workItemTypes":[null],"attributes":[]}`)},
		{
			name: "a type with no text for an id",
			read: workItemTimeTracking(`{"workItemTypes":[{"id":8,"name":"First"}],"attributes":[]}`),
		},
		{name: "a type with no name", read: workItemTimeTracking(`{"workItemTypes":[{"id":"8-1","name":null}],"attributes":[]}`)},
		{
			name:       "no attributes",
			read:       workItemTimeTracking(`{"workItemTypes":[],"attributes":null}`),
			attributes: mode,
		},
		{
			name:       "an attribute with no id",
			read:       workItemTimeTracking(`{"workItemTypes":[],"attributes":[{"id":null,"name":"Mode","values":[]}]}`),
			attributes: mode,
		},
		{
			name:       "an attribute with no values",
			read:       workItemTimeTracking(`{"workItemTypes":[],"attributes":[{"id":"9-1","name":"Mode","values":null}]}`),
			attributes: mode,
		},
		{
			name: "a value of an attribute with no name",
			read: workItemTimeTracking(`{"workItemTypes":[],"attributes":[{"id":"9-1","name":"Mode",` +
				`"values":[{"id":"9-2","name":null}]}]}`),
			attributes: mode,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := workItemWriting(t, tc.read, workItemAnswer{})

			_, err := client(t, server).WorkItems.Create(t.Context(), "DEV-1",
				&youtrack.WorkItemInput{Duration: time.Hour, Type: "First", Attributes: tc.attributes}, nil)

			assert.Equal(t, unreadable(lastRequest(t, server), tc.read), errorOf(t, err))
			assert.Equal(t, []string{"/api/issues/DEV-1"}, server.Paths())
		})
	}
}

func TestDeleteWorkItemRemovesTheWorkItemUnderTheIssueTheReadNamed(t *testing.T) {
	t.Parallel()
	server := routes(t, map[string]http.HandlerFunc{
		"GET /api/issues/dev-1/timeTracking/workItems/7-1": fake.JSON(http.StatusOK,
			`{"$type":"IssueWorkItem","id":"7-1","issue":{"$type":"Issue","idReadable":"DEV-1"}}`),
		"DELETE " + workItemOfTheIssue: fake.JSON(http.StatusOK, ""),
	})

	node, err := client(t, server).WorkItems.Delete(t.Context(), "dev-1", "7-1")

	require.NoError(t, err)
	assert.Equal(t, youtrack.NewMap(
		youtrack.Pair{Key: "id", Value: youtrack.NewString("7-1")},
		youtrack.Pair{Key: "issue", Value: youtrack.NewMap(youtrack.Pair{Key: "idReadable", Value: youtrack.NewString("DEV-1")})},
	), node)
	assert.Equal(t, []string{"GET /api/issues/dev-1/timeTracking/workItems/7-1", "DELETE " + workItemOfTheIssue},
		server.Routes())
}

func TestDeleteWorkItemRemovesNothingByAReadOfAnotherShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		read string
	}{
		{name: "another work item than the one asked for", read: `{"$type":"IssueWorkItem","id":"7-2","issue":{"idReadable":"DEV-1"}}`},
		{name: "an id that is no internal id", read: `{"$type":"IssueWorkItem","id":"..","issue":{"idReadable":"DEV-1"}}`},
		{name: "an id that is no text", read: `{"$type":"IssueWorkItem","id":7,"issue":{"idReadable":"DEV-1"}}`},
		{name: "no issue at all", read: `{"$type":"IssueWorkItem","id":"7-1","issue":null}`},
		{name: "the readable id of an article", read: `{"$type":"IssueWorkItem","id":"7-1","issue":{"idReadable":"DEV-A-1"}}`},
		{name: "a readable id that is a path", read: `{"$type":"IssueWorkItem","id":"7-1","issue":{"idReadable":".."}}`},
		{name: "a readable id that is no text", read: `{"$type":"IssueWorkItem","id":"7-1","issue":{"idReadable":1}}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tc.read))

			_, err := client(t, server).WorkItems.Delete(t.Context(), "DEV-1", "7-1")

			assert.Equal(t, unreadable(lastRequest(t, server), tc.read), errorOf(t, err))
			assert.Equal(t, []string{workItemOfTheIssue}, server.Paths())
		})
	}
}

func TestListWorkItemsPrintsAWorkItem(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
		received   string
		printed    *youtrack.Node
	}{
		{
			name:       "a duration of hours and minutes",
			expression: "duration",
			received:   `{"duration":` + workItemMinutes("90") + `}`,
			printed:    youtrack.NewString("PT1H30M"),
		},
		{
			name:       "a duration of a whole hour",
			expression: "duration",
			received:   `{"duration":` + workItemMinutes("60") + `}`,
			printed:    youtrack.NewString("PT1H"),
		},
		{
			name:       "a duration of a minute",
			expression: "duration",
			received:   `{"duration":` + workItemMinutes("1") + `}`,
			printed:    youtrack.NewString("PT1M"),
		},
		{
			name:       "a duration of a day of the clock",
			expression: "duration",
			received:   `{"duration":` + workItemMinutes("1440") + `}`,
			printed:    youtrack.NewString("PT24H"),
		},
		{
			name:       "the largest duration the server keeps",
			expression: "duration",
			received:   `{"duration":` + workItemMinutes("2147483647") + `}`,
			printed:    youtrack.NewString("PT35791394H7M"),
		},
		{
			name:       "a duration of no time",
			expression: "duration",
			received:   `{"duration":` + workItemMinutes("0") + `}`,
			printed:    youtrack.NewString("PT0M"),
		},
		{name: "no duration", expression: "duration", received: `{"duration":null}`, printed: youtrack.NewNull()},
		{
			name:       "the day, at the midnight UTC it is kept at",
			expression: "date",
			received:   `{"date":` + workItemDayMidnight + `}`,
			printed:    youtrack.NewString("2026-09-01T00:00:00Z"),
		},
		{
			name:       "a text of two lines, on the line of its record",
			expression: "text",
			received:   `{"text":"first\nsecond"}`,
			printed:    youtrack.NewString("first\nsecond"),
		},
		{
			name:       "the attributes, as the value each holds under its name",
			expression: "attributes",
			received: `{"attributes":[{"id":"9-1","name":"Mode","value":{"id":"9-3","name":"Pair"}},` +
				`{"id":"9-4","name":"Kind","value":null}]}`,
			printed: youtrack.NewMap(youtrack.DataPair("Mode", youtrack.NewString("Pair")),
				youtrack.DataPair("Kind", youtrack.NewNull())),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, "["+tc.received+"]"))

			node, err := client(t, server).WorkItems.List(t.Context(), "DEV-1",
				&youtrack.ListWorkItemsOptions{Fields: tc.expression})

			require.NoError(t, err)
			assert.Equal(t, wholePage("workItems", youtrack.NewMap(youtrack.Pair{Key: tc.expression, Value: tc.printed})), node)
		})
	}
}

func TestListWorkItemsRefusesAWorkItemOfAnotherShape(t *testing.T) {
	t.Parallel()
	const asked = "?fields=duration(minutes),attributes(id,name,value(id,name))&$top=50"
	tests := []struct {
		name     string
		received string
	}{
		{name: "a duration of no minutes", received: `{"duration":{"minutes":null},"attributes":[]}`},
		{name: "a fraction of a minute", received: `{"duration":{"minutes":1.5},"attributes":[]}`},
		{name: "the minutes as text", received: `{"duration":{"minutes":"90"},"attributes":[]}`},
		{name: "attributes that are null", received: `{"duration":null,"attributes":null}`},
		{name: "an attribute with no name", received: `{"duration":null,"attributes":[{"id":"9-1","name":null,"value":null}]}`},
		{
			name: "two attributes of one name",
			received: `{"duration":null,"attributes":[{"id":"9-1","name":"Mode","value":null},` +
				`{"id":"9-4","name":"Mode","value":null}]}`,
		},
		{
			name:     "a value of an attribute with no name",
			received: `{"duration":null,"attributes":[{"id":"9-1","name":"Mode","value":{"id":"9-3","name":null}}]}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := "[" + tc.received + "]"
			server := fake.Serve(t, fake.JSON(http.StatusOK, body))

			_, err := client(t, server).WorkItems.List(t.Context(), "DEV-1",
				&youtrack.ListWorkItemsOptions{Fields: "duration,attributes"})

			assert.Equal(t, unreadable(requestTo(http.MethodGet, server, workItemsOfTheIssue+asked), body), errorOf(t, err))
		})
	}
}

func TestListWorkItemsAsksTheIssuesOfALinkSlotOfTheIssue(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, "[]"))

	_, err := client(t, server).WorkItems.List(t.Context(), "DEV-1",
		&youtrack.ListWorkItemsOptions{Fields: "issue(links(issues(idReadable)))"})

	require.NoError(t, err)
	assert.Equal(t, []string{"issue(links(issues(idReadable),direction,linkType(sourceToTarget,targetToSource)))"},
		server.Fields())
}
