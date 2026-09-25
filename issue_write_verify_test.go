package youtrack_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/youtrack"
	"github.com/hakastein/youtrack/fake"
)

func issueWritingAField(t *testing.T, valueType string, multi bool, held string) *fake.Server {
	t.Helper()
	project := issueProject(metaField{id: "1-1", name: "Field", valueType: valueType, multi: multi})
	answer := issueWritten(t, map[string]any{"summary": "First", "customFields": issueHeldFields(
		issueHeld{name: "Field", valueType: valueType, multi: multi, value: held})})
	return issueWriting(t, project, "[]", fake.JSON(http.StatusOK, answer))
}

func TestCreateIssueRefusesAnAnswerThatDisagreesWithACustomField(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		valueType string
		multi     bool
		values    []string
		held      string
		expected  *youtrack.Node
		actual    *youtrack.Node
	}{
		{name: "a name the server resolved to another", valueType: "enum", values: []string{"First"},
			held:     issueElement("Second"),
			expected: youtrack.NewString("First"), actual: youtrack.NewString("Second")},
		{name: "a string in another letter case", valueType: "string", values: []string{"Upper"},
			held:     `"upper"`,
			expected: youtrack.NewString("Upper"), actual: youtrack.NewString("upper")},
		{name: "a text in another letter case", valueType: "text", values: []string{"Upper"},
			held:     `{"$type":"TextFieldValue","text":"upper"}`,
			expected: youtrack.NewString("Upper"), actual: youtrack.NewString("upper")},
		{name: "a set holding a value more", valueType: "enum", multi: true, values: []string{"First", "Second"},
			held:     `[` + issueElement("First") + `,` + issueElement("Second") + `,` + issueElement("Third") + `]`,
			expected: texts("First", "Second"), actual: texts("First", "Second", "Third")},
		{name: "a set holding a value fewer", valueType: "enum", multi: true, values: []string{"First", "Second"},
			held:     `[` + issueElement("First") + `]`,
			expected: texts("First", "Second"), actual: texts("First")},
		{name: "a set held empty", valueType: "enum", multi: true, values: []string{"First"},
			held:     `[]`,
			expected: texts("First"), actual: youtrack.NewList()},
		{name: "a value held as nothing", valueType: "enum", values: []string{"First"},
			held:     `null`,
			expected: youtrack.NewString("First"), actual: youtrack.NewNull()},
		{name: "a day the server keeps as another", valueType: "date", values: []string{"2026-09-16"},
			held:     `1789646400000`,
			expected: youtrack.NewString("2026-09-16"), actual: youtrack.NewString("2026-09-17")},
		{name: "a moment the server keeps a millisecond off", valueType: "date and time",
			values: []string{"2026-08-31T03:00:00.123+03:00"}, held: `1788134400124`,
			expected: youtrack.NewString("2026-08-31T03:00:00.123+03:00"), actual: youtrack.NewString("2026-08-31T00:00:00.124Z")},
		{name: "a number the server keeps as another", valueType: "float", values: []string{"1.5"},
			held:     `1.75`,
			expected: youtrack.NewString("1.5"), actual: youtrack.NewString("1.75")},
		{name: "a period the server rounded to the hour", valueType: "period", values: []string{"PT1H30M"},
			held:     `{"$type":"PeriodValue","minutes":60}`,
			expected: youtrack.NewString("PT1H30M"), actual: youtrack.NewString("PT1H")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := issueWritingAField(t, tc.valueType, tc.multi, tc.held)

			_, err := issueCreate(t, server, issueFilling(issueFill("Field", tc.values...)), "idReadable")

			want := issueMismatch(
				requestTo(http.MethodPost, server, issuesPath+"?fields=idReadable,summary,"+issueCustomFields),
				mismatch("Field", tc.expected, tc.actual))
			assert.Equal(t, want, errorOf(t, err))
		})
	}
}

func TestCreateIssueTakesAnAnswerThatHoldsWhatWasWritten(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		valueType string
		multi     bool
		values    []string
		held      string
	}{
		{name: "a name in another letter case", valueType: "enum", values: []string{"first"},
			held: issueElement("First")},
		{name: "a login in another letter case", valueType: "user", values: []string{"FIRST"},
			held: `{"$type":"User","login":"first"}`},
		{name: "a set in another order and letter case", valueType: "enum", multi: true,
			values: []string{"First", "second", "first"},
			held:   `[` + issueElement("Second") + `,` + issueElement("First") + `]`},
		{name: "a day the server keeps at midnight UTC", valueType: "date", values: []string{"2026-09-16"},
			held: `1789516800000`},
		{name: "a moment the server keeps in UTC", valueType: "date and time",
			values: []string{"2026-08-31T03:00:00.123+03:00"}, held: `1788134400123`},
		{name: "a number the server keeps to the digits a float holds", valueType: "float",
			values: []string{"123456789.123456789"}, held: `123456789.12345679`},
		{name: "a period written in minutes", valueType: "period", values: []string{"PT90M"},
			held: `{"$type":"PeriodValue","minutes":90}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := issueWritingAField(t, tc.valueType, tc.multi, tc.held)

			node, err := issueCreate(t, server, issueFilling(issueFill("Field", tc.values...)), "idReadable")

			require.NoError(t, err)
			assert.Equal(t, issueWrittenID(), node)
		})
	}
}

func TestUpdateIssueTakesAnAnswerWhereAnEmptiedFieldHoldsNothing(t *testing.T) {
	t.Parallel()
	project := issueProject(enumField("1-1", "Field"))
	tests := []struct {
		name string
		held json.RawMessage
	}{
		{name: "the field held with no value", held: issueHeldFields(issueHeld{name: "Field", valueType: "enum"})},
		{name: "the field not held at all", held: issueHeldFields()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			answer := issueWritten(t, map[string]any{"customFields": tc.held})
			server := issueWriting(t, project, "[]", fake.JSON(http.StatusOK, answer))

			node, err := issueUpdate(t, server, youtrack.IssueUpdate{Fields: []youtrack.FieldWrite{issueClear("Field")}}, "idReadable")

			require.NoError(t, err)
			assert.Equal(t, issueWrittenID(), node)
		})
	}
}

func TestUpdateIssueRefusesAnAnswerWhereAnEmptiedFieldHoldsAValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		multi    bool
		held     string
		expected *youtrack.Node
		actual   *youtrack.Node
	}{
		{name: "a field that holds one value", held: issueElement("First"),
			expected: youtrack.NewNull(), actual: youtrack.NewString("First")},
		{name: "a field that holds several", multi: true, held: `[` + issueElement("First") + `]`,
			expected: youtrack.NewList(), actual: texts("First")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := issueWritingAField(t, "enum", tc.multi, tc.held)

			_, err := issueUpdate(t, server, youtrack.IssueUpdate{Fields: []youtrack.FieldWrite{issueClear("Field")}}, "idReadable")

			want := issueMismatch(
				requestTo(http.MethodPost, server, issuePath+"?fields=idReadable,"+issueCustomFields),
				mismatch("Field", tc.expected, tc.actual))
			assert.Equal(t, want, errorOf(t, err))
		})
	}
}
