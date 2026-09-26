package youtrack_test

import (
	"context"
	"fmt"
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
	fieldAsked      = fieldNaming + ",canBeEmpty"
	fieldWithValues = fieldAsked + ",bundle(values(name,archived))"
)

func fieldNamed(name string) youtrack.Pair {
	return youtrack.Pair{Key: "field", Value: youtrack.NewMap(youtrack.Pair{Key: "name", Value: youtrack.NewString(name)})}
}

func named(name string) *youtrack.Node {
	return youtrack.NewMap(fieldNamed(name))
}

func unresolvedField(t *testing.T, server *fake.Server, key string, entry *youtrack.Node) youtrack.Error {
	t.Helper()
	return youtrack.Error{Code: youtrack.CodeUnknownName, Details: []youtrack.Pair{
		lastRequest(t, server),
		{Key: "project", Value: youtrack.NewString("DEV")},
		{Key: key, Value: youtrack.NewList(entry)},
	}}
}

func unaddressable(t *testing.T, server *fake.Server) youtrack.Error {
	t.Helper()
	return youtrack.Error{Code: youtrack.CodeUpstreamInvalid, Details: []youtrack.Pair{lastRequest(t, server)}}
}

func placed(ordinal, name string) string {
	return fmt.Sprintf(`{"$type":"ProjectCustomField","ordinal":%s,"field":{"$type":"CustomField","name":%q}}`, ordinal, name)
}

// Go sorts up to twelve elements by insertion, which keeps their order whether the sort is stable or not.
func ofOneOrdinal() (fields []string, printed []*youtrack.Node) {
	for at := range 13 {
		name := fmt.Sprintf("Unplaced %02d", at)
		fields = append(fields, placed("0", name))
		printed = append(printed, named(name))
	}
	return fields, printed
}

func TestListFieldsRefusesACallItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		project string
		fields  string
	}{
		{name: "a project code with a dash", project: "DEV-1"},
		{name: "fields that close nothing", project: "DEV", fields: "field("},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			_, err := client(t, server).Fields.List(t.Context(), tc.project, &youtrack.ListFieldsOptions{Fields: tc.fields})

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

func TestListFieldsRefusesAProjectWithNoFields(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `[]`))

	_, err := client(t, server).Fields.List(t.Context(), "DEV", &youtrack.ListFieldsOptions{Fields: "id"})

	assert.Equal(t, denied(t, server), errorOf(t, err))
}

func TestListFieldsPrintsTheFieldsByOrdinal(t *testing.T) {
	t.Parallel()
	unplaced, unplacedPrinted := ofOneOrdinal()
	tests := []struct {
		name   string
		fields string
		asked  string
		want   *youtrack.Node
	}{
		{
			name:   "fields the server sent out of order",
			fields: `[` + placed("3", "Third") + `,` + placed("1", "First") + `,` + placed("2", "Second") + `]`,
			asked:  "field(name)",
			want:   wholePage("fields", named("First"), named("Second"), named("Third")),
		},
		{
			name:   "fields of one ordinal, in the order the server sent them",
			fields: `[` + placed("2", "Second") + `,` + strings.Join(unplaced, ",") + `,` + placed("1", "First") + `]`,
			asked:  "field(name)",
			want:   wholePage("fields", append(unplacedPrinted, named("First"), named("Second"))...),
		},
		{
			name:   "the ordinal asked for",
			fields: `[` + placed("2", "Second") + `,` + placed("1", "First") + `]`,
			asked:  "field(name),ordinal",
			want: wholePage("fields",
				youtrack.NewMap(fieldNamed("First"), youtrack.Pair{Key: "ordinal", Value: number(1)}),
				youtrack.NewMap(fieldNamed("Second"), youtrack.Pair{Key: "ordinal", Value: number(2)})),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tc.fields))

			got, err := client(t, server).Fields.List(t.Context(), "DEV", &youtrack.ListFieldsOptions{Fields: tc.asked})

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestListFieldsPrintsTheFieldsAskedForAndAsksForTheOrdinalBesides(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `[`+enumField("1-1", "First").json()+`]`))

	got, err := client(t, server).Fields.List(t.Context(), "DEV", &youtrack.ListFieldsOptions{Fields: fieldAsked})

	require.NoError(t, err)
	want := wholePage("fields", youtrack.NewMap(
		youtrack.Pair{Key: "field", Value: youtrack.NewMap(
			youtrack.Pair{Key: "name", Value: youtrack.NewString("First")},
			youtrack.Pair{Key: "localizedName", Value: youtrack.NewNull()},
			youtrack.Pair{Key: "fieldType", Value: youtrack.NewMap(
				youtrack.Pair{Key: "valueType", Value: youtrack.NewString("enum")},
				youtrack.Pair{Key: "isMultiValue", Value: youtrack.NewBool(false)})})},
		youtrack.Pair{Key: "canBeEmpty", Value: youtrack.NewBool(true)}))
	assert.Equal(t, want, got)
	assert.Equal(t, []string{fieldAsked + ",ordinal"}, server.Fields())
}

func TestListFieldsRefusesAnOrdinalItCannotOrderBy(t *testing.T) {
	t.Parallel()
	for _, ordinal := range []string{`"1"`, `null`, `1.5`} {
		t.Run(ordinal, func(t *testing.T) {
			t.Parallel()
			fields := `[` + placed(ordinal, "First") + `]`
			server := fake.Serve(t, fake.JSON(http.StatusOK, fields))

			_, err := client(t, server).Fields.List(t.Context(), "DEV", &youtrack.ListFieldsOptions{Fields: "field(name)"})

			assert.Equal(t, unreadable(lastRequest(t, server), fields), errorOf(t, err))
		})
	}
}

func TestShowFieldRefusesACallItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		project string
		field   string
		fields  string
	}{
		{name: "a project code with a dash", project: "DEV-1", field: "First", fields: "canBeEmpty"},
		{name: "a field of no name", project: "DEV", field: "", fields: "canBeEmpty"},
		{name: "fields that close nothing", project: "DEV", field: "First", fields: "field("},
		{name: "no fields", project: "DEV", field: "First"},
		{name: "fields added to others", project: "DEV", field: "First", fields: "+canBeEmpty"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			_, err := client(t, server).Fields.Show(t.Context(), tc.project, tc.field, &youtrack.ShowFieldOptions{Fields: tc.fields})

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

func TestShowFieldAddressesTheFieldItsNameResolvesTo(t *testing.T) {
	t.Parallel()
	fields := []metaField{
		{id: "1-1", name: "First", valueType: "enum"},
		{id: "1-2", name: "Second", translation: `"Translated"`, valueType: "enum", required: true},
		{id: "1-3", name: "Shared", valueType: "enum"},
		{id: "1-4", name: "Other", translation: `"Shared"`, valueType: "enum"},
	}
	instance := &metaInstance{projects: map[string]string{"DEV": projectJSON(fields...)}, answers: map[string]http.HandlerFunc{}}
	for _, f := range fields {
		instance.answers[f.id] = answering(f.answer(""))
	}
	tests := []struct {
		name       string
		asked      string
		id         string
		canBeEmpty bool
	}{
		{name: "a name", asked: "First", id: "1-1", canBeEmpty: true},
		{name: "a name in another letter case", asked: "FIRST", id: "1-1", canBeEmpty: true},
		{name: "a translation in another letter case", asked: "translated", id: "1-2"},
		{name: "a name that is also the translation of another field", asked: "shared", id: "1-3", canBeEmpty: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := instance.serve(t)

			got, err := client(t, server).Fields.Show(t.Context(), "DEV", tc.asked, &youtrack.ShowFieldOptions{Fields: "canBeEmpty"})

			require.NoError(t, err)
			assert.Equal(t, youtrack.NewMap(youtrack.Pair{Key: "canBeEmpty", Value: youtrack.NewBool(tc.canBeEmpty)}), got)
			assert.Equal(t, []string{projectPath, fieldPath + tc.id}, server.Paths())
		})
	}
}

func TestShowFieldPrintsTheFieldWithTheValuesItAccepts(t *testing.T) {
	t.Parallel()
	f := metaField{id: "1-1", name: "Field", translation: `"Поле"`, valueType: "enum", multi: true}
	values := enumBundle(bundleValue("3-1", "Open", false), bundleValue("3-2", "Legacy", true))
	server := (&metaInstance{
		projects: map[string]string{"DEV": projectJSON(f)},
		answers:  map[string]http.HandlerFunc{"1-1": answering(f.answer(values))},
	}).serve(t)

	got, err := client(t, server).Fields.Show(t.Context(), "DEV", "Field", &youtrack.ShowFieldOptions{Fields: fieldWithValues})

	require.NoError(t, err)
	value := func(name string, archived bool) *youtrack.Node {
		return youtrack.NewMap(
			youtrack.Pair{Key: "name", Value: youtrack.NewString(name)},
			youtrack.Pair{Key: "archived", Value: youtrack.NewBool(archived)})
	}
	want := youtrack.NewMap(
		youtrack.Pair{Key: "field", Value: youtrack.NewMap(
			youtrack.Pair{Key: "name", Value: youtrack.NewString("Field")},
			youtrack.Pair{Key: "localizedName", Value: youtrack.NewString("Поле")},
			youtrack.Pair{Key: "fieldType", Value: youtrack.NewMap(
				youtrack.Pair{Key: "valueType", Value: youtrack.NewString("enum")},
				youtrack.Pair{Key: "isMultiValue", Value: youtrack.NewBool(true)})})},
		youtrack.Pair{Key: "canBeEmpty", Value: youtrack.NewBool(true)},
		youtrack.Pair{Key: "bundle", Value: youtrack.NewMap(
			youtrack.Pair{Key: "values", Value: youtrack.NewList(value("Open", false), value("Legacy", true))})})
	assert.Equal(t, want, got)
}

func typedAnswer(schema string, f metaField, bundle string) string {
	return strings.Replace(f.answer(bundle), `"ProjectCustomField"`, strconv.Quote(schema), 1)
}

func TestShowFieldLeavesOutANameOnlyAnotherTypeOfFieldHolds(t *testing.T) {
	t.Parallel()
	const asked = "canBeEmpty,bundle(values(name,archived),aggregatedUsers(login))"
	canBeEmpty := youtrack.Pair{Key: "canBeEmpty", Value: youtrack.NewBool(true)}
	named := func(name string, rest ...youtrack.Pair) *youtrack.Node {
		return youtrack.NewMap(append([]youtrack.Pair{{Key: "name", Value: youtrack.NewString(name)}}, rest...)...)
	}
	tests := []struct {
		name      string
		valueType string
		schema    string
		bundle    string
		want      *youtrack.Node
	}{
		{
			name:      "a field with a bundle of values",
			valueType: "enum",
			schema:    "EnumProjectCustomField",
			bundle:    enumBundle(bundleValue("3-1", "Open", false)),
			want: youtrack.NewMap(canBeEmpty, youtrack.Pair{Key: "bundle", Value: youtrack.NewMap(youtrack.Pair{
				Key: "values", Value: youtrack.NewList(named("Open", youtrack.Pair{Key: "archived", Value: youtrack.NewBool(false)})),
			})}),
		},
		{
			name:      "a field of users, whose values are the users and the groups of its bundle",
			valueType: "user",
			schema:    "UserProjectCustomField",
			bundle: `{"$type":"UserBundle","aggregatedUsers":[{"$type":"User","login":"first"}],` +
				`"values":[{"$type":"User","name":"First"},{"$type":"ProjectTeam","name":"Team"}]}`,
			want: youtrack.NewMap(canBeEmpty, youtrack.Pair{Key: "bundle", Value: youtrack.NewMap(
				youtrack.Pair{Key: "values", Value: youtrack.NewList(named("First"), named("Team"))},
				youtrack.Pair{Key: "aggregatedUsers", Value: youtrack.NewList(youtrack.NewMap(
					youtrack.Pair{Key: "login", Value: youtrack.NewString("first")}))},
			)}),
		},
		{name: "a field of text", valueType: "string", schema: "SimpleProjectCustomField", want: youtrack.NewMap(canBeEmpty)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := metaField{id: "1-1", name: "Field", valueType: tc.valueType}
			server := (&metaInstance{
				projects: map[string]string{"DEV": projectJSON(f)},
				answers:  map[string]http.HandlerFunc{"1-1": answering(typedAnswer(tc.schema, f, tc.bundle))},
			}).serve(t)

			got, err := client(t, server).Fields.Show(t.Context(), "DEV", "Field", &youtrack.ShowFieldOptions{Fields: asked})

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// Show and Bundle find a field of the project by one rule and read it again over a stale cache by one rule, so
// what they share is tested over both.
type fieldReader struct {
	name string
	read func(ctx context.Context, c *youtrack.Client, project, field string) (any, error)
}

func fieldReaders() []fieldReader {
	return []fieldReader{
		{name: "show", read: func(ctx context.Context, c *youtrack.Client, project, field string) (any, error) {
			return c.Fields.Show(ctx, project, field, &youtrack.ShowFieldOptions{Fields: "canBeEmpty"})
		}},
		{name: "bundle", read: func(ctx context.Context, c *youtrack.Client, project, field string) (any, error) {
			return c.Fields.Bundle(ctx, project, field)
		}},
	}
}

func TestShowAndBundleRefuseAProjectWithNoFields(t *testing.T) {
	t.Parallel()
	for _, reader := range fieldReaders() {
		t.Run(reader.name, func(t *testing.T) {
			t.Parallel()
			server := (&metaInstance{projects: map[string]string{"DEV": projectJSON()}}).serve(t)

			_, err := reader.read(t.Context(), client(t, server), "DEV", "First")

			assert.Equal(t, denied(t, server), errorOf(t, err))
			assert.Equal(t, []string{projectPath}, server.Paths())
		})
	}
}

func TestShowAndBundleRefuseANameNoSingleFieldAnswersTo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		asked    string
		metadata string
		key      string
		entry    *youtrack.Node
	}{
		{
			name:     "a name of two fields alike but for letter case",
			asked:    "upper",
			metadata: projectJSON(enumField("1-1", "Upper"), enumField("1-2", "UPPER"), enumField("1-3", "Uppe")),
			key:      "ambiguous",
			entry:    issueCandidates("upper", "UPPER", "Upper"),
		},
		{
			name:  "a translation of two fields",
			asked: "shared",
			metadata: projectJSON(
				metaField{id: "1-1", name: "First", translation: `"Shared"`, valueType: "enum"},
				metaField{id: "1-2", name: "Second", translation: `"Shared"`, valueType: "enum"},
				enumField("1-3", "Share")),
			key:   "ambiguous",
			entry: issueCandidates("shared", "First", "Second"),
		},
		{
			name:  "a name of no field, beside five names nearer than the rest",
			asked: "Type",
			metadata: projectJSON(
				enumField("1-1", "Typl"), enumField("1-2", "Typi"), enumField("1-3", "Typf"), enumField("1-4", "Ty"),
				enumField("1-5", "Typk"), enumField("1-6", "Typg"), enumField("1-7", "Typj"), enumField("1-8", "Typh")),
			key:   "unknown",
			entry: withNearest("field", "Type", "Typf", "Typg", "Typh", "Typi", "Typj"),
		},
		{
			name:     "a name of no field, beside one name nearer than another",
			asked:    "Type",
			metadata: projectJSON(enumField("1-1", "Typf"), enumField("1-2", "Typxyz")),
			key:      "unknown",
			entry:    withNearest("field", "Type", "Typf"),
		},
	}
	for _, reader := range fieldReaders() {
		for _, tc := range tests {
			t.Run(reader.name+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				server := (&metaInstance{projects: map[string]string{"DEV": tc.metadata}}).serve(t)

				_, err := reader.read(t.Context(), client(t, server), "DEV", tc.asked)

				assert.Equal(t, unresolvedField(t, server, tc.key, tc.entry), errorOf(t, err))
				assert.Equal(t, []string{projectPath}, server.Paths())
			})
		}
	}
}

func TestShowAndBundleRefuseAFieldOfTheMetadataTheyCannotAddress(t *testing.T) {
	t.Parallel()
	for _, reader := range fieldReaders() {
		t.Run(reader.name, func(t *testing.T) {
			t.Parallel()
			server := (&metaInstance{projects: map[string]string{"DEV": projectJSON(enumField("..", "Field"))}}).serve(t)

			_, err := reader.read(t.Context(), client(t, server), "DEV", "Field")

			assert.Equal(t, unaddressable(t, server), errorOf(t, err))
			assert.Equal(t, []string{projectPath}, server.Paths())
		})
	}
}

func TestShowFieldReadsAFieldOfATypeItDoesNotModel(t *testing.T) {
	t.Parallel()
	f := metaField{id: "1-1", name: "Field", valueType: "state", multi: true}
	server := (&metaInstance{
		projects: map[string]string{"DEV": projectJSON(f)},
		answers:  map[string]http.HandlerFunc{"1-1": answering(f.answer(""))},
	}).serve(t)

	got, err := client(t, server).Fields.Show(t.Context(), "DEV", "Field", &youtrack.ShowFieldOptions{Fields: "canBeEmpty"})

	require.NoError(t, err)
	assert.Equal(t, youtrack.NewMap(youtrack.Pair{Key: "canBeEmpty", Value: youtrack.NewBool(true)}), got)
}

func TestShowAndBundleRefuseAFieldTheyCannotCompare(t *testing.T) {
	t.Parallel()
	answer := `{"$type":"ProjectCustomField","canBeEmpty":true,"field":[],"bundle":` + enumBundle() + `}`
	for _, reader := range fieldReaders() {
		t.Run(reader.name, func(t *testing.T) {
			t.Parallel()
			server := (&metaInstance{
				projects: map[string]string{"DEV": projectJSON(enumField("1-1", "Field"))},
				answers:  map[string]http.HandlerFunc{"1-1": answering(answer)},
			}).serve(t)

			_, err := reader.read(t.Context(), client(t, server), "DEV", "Field")

			assert.Equal(t, unreadable(lastRequest(t, server), answer), errorOf(t, err))
		})
	}
}

func TestShowAndBundleRefuseAFieldThatChangedBetweenTheTwoRequests(t *testing.T) {
	t.Parallel()
	held := metaField{id: "1-1", name: "Field", translation: `"Translated"`, valueType: "enum"}
	tests := []struct {
		name   string
		answer metaField
	}{
		{name: "another name", answer: metaField{id: "1-1", name: "Renamed", translation: `"Translated"`, valueType: "enum"}},
		{name: "another translation", answer: metaField{id: "1-1", name: "Field", translation: `"Retranslated"`, valueType: "enum"}},
		{name: "no translation", answer: metaField{id: "1-1", name: "Field", valueType: "enum"}},
		{name: "another type of value", answer: metaField{id: "1-1", name: "Field", translation: `"Translated"`, valueType: "state"}},
		{name: "many values where one was", answer: metaField{id: "1-1", name: "Field", translation: `"Translated"`, valueType: "enum", multi: true}},
	}
	for _, reader := range fieldReaders() {
		for _, tc := range tests {
			t.Run(reader.name+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				answer := tc.answer.answer(enumBundle())
				server := (&metaInstance{
					projects: map[string]string{"DEV": projectJSON(held)},
					answers:  map[string]http.HandlerFunc{"1-1": answering(answer)},
				}).serve(t)

				_, err := reader.read(t.Context(), client(t, server), "DEV", "Field")

				want := youtrack.Error{Code: youtrack.CodeUpstreamFailed, Details: []youtrack.Pair{
					lastRequest(t, server),
					{Key: "project", Value: youtrack.NewString("DEV")},
					{Key: "field", Value: youtrack.NewString("Field")},
					{Key: "upstream_status", Value: number(http.StatusOK)},
					{Key: "upstream_body", Value: youtrack.NewString(answer)},
				}}
				assert.Equal(t, want, errorOf(t, err))
			})
		}
	}
}

func TestShowAndBundleAnswerFromTheMetadataTheyCached(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		field metaField
	}{
		{name: "a field with a translation", field: metaField{id: "1-1", name: "Field", translation: `"Translated"`, valueType: "enum"}},
		{name: "a field with no translation", field: metaField{id: "1-1", name: "Field", valueType: "enum"}},
		{name: "a field with an empty translation", field: metaField{id: "1-1", name: "Field", translation: `""`, valueType: "enum"}},
		{name: "a field of many values", field: metaField{id: "1-1", name: "Field", valueType: "enum", multi: true}},
	}
	for _, reader := range fieldReaders() {
		for _, tc := range tests {
			t.Run(reader.name+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				instance := &metaInstance{
					projects: map[string]string{"DEV": projectJSON(tc.field)},
					answers:  map[string]http.HandlerFunc{"1-1": answering(tc.field.answer(enumBundle(bundleValue("3-1", "First", false))))},
				}
				server, root := instance.serve(t), t.TempDir()
				first, err := reader.read(t.Context(), cached(t, server, root), "DEV", "Field")
				require.NoError(t, err)

				second, err := reader.read(t.Context(), cached(t, server, root), "DEV", "Field")

				require.NoError(t, err)
				assert.Equal(t, first, second)
				assert.Equal(t, []string{projectPath, firstFieldPath, firstFieldPath}, server.Paths())
			})
		}
	}
}

func TestShowAndBundleReadTheMetadataAgainWhenTheCacheMisses(t *testing.T) {
	t.Parallel()
	field := metaField{id: "1-1", name: "Field", translation: `"Поле"`, valueType: "enum"}
	added := enumField("1-2", "Added")
	state := metaField{id: "1-1", name: "Field", translation: `"Поле"`, valueType: "state"}
	rebound := metaField{id: "1-2", name: "Field", translation: `"Поле"`, valueType: "enum"}
	values := enumBundle(bundleValue("3-1", "First", false))
	for _, reader := range fieldReaders() {
		// A table per reader: InTurn counts its answers across every subtest that holds it.
		tests := []struct {
			name     string
			projects map[string]string
			answers  map[string]http.HandlerFunc
			asked    string
			paths    []string
		}{
			{
				name:     "a field added since",
				projects: map[string]string{"DEV": projectJSON(field, added)},
				answers:  map[string]http.HandlerFunc{"1-1": answering(field.answer(values)), "1-2": answering(added.answer(values))},
				asked:    "Added",
				paths:    []string{projectPath, firstFieldPath, projectPath, secondFieldPath},
			},
			{
				name:     "a field that changed its type since",
				projects: map[string]string{"DEV": projectJSON(state)},
				answers:  map[string]http.HandlerFunc{"1-1": answering(state.answer(values))},
				asked:    "Field",
				paths:    []string{projectPath, firstFieldPath, firstFieldPath, projectPath, firstFieldPath},
			},
			{
				name:     "a field bound again under another id since",
				projects: map[string]string{"DEV": projectJSON(rebound)},
				answers:  map[string]http.HandlerFunc{"1-2": answering(rebound.answer(values))},
				asked:    "Field",
				paths:    []string{projectPath, firstFieldPath, firstFieldPath, projectPath, secondFieldPath},
			},
			{
				name:     "an answer to the request of the cached field that cannot be read",
				projects: map[string]string{"DEV": projectJSON(field)},
				answers:  map[string]http.HandlerFunc{"1-1": fake.InTurn(fake.JSON(http.StatusOK, `[]`), answering(field.answer(values)))},
				asked:    "Field",
				paths:    []string{projectPath, firstFieldPath, firstFieldPath, projectPath, firstFieldPath},
			},
		}
		for _, tc := range tests {
			t.Run(reader.name+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				instance := oneEnumField()
				server, root := instance.serve(t), t.TempDir()
				_, err := reader.read(t.Context(), cached(t, server, root), "DEV", "Field")
				require.NoError(t, err)
				instance.change(tc.projects, tc.answers)

				got, err := reader.read(t.Context(), cached(t, server, root), "DEV", tc.asked)

				require.NoError(t, err)
				assert.Equal(t, tc.paths, server.Paths())
				fresh, err := reader.read(t.Context(), client(t, instance.serve(t)), "DEV", tc.asked)
				require.NoError(t, err)
				assert.Equal(t, fresh, got)
			})
		}
	}
}

func TestShowAndBundleRefuseANameOnlyAfterReadingTheMetadataAgain(t *testing.T) {
	t.Parallel()
	kept := enumField("1-2", "Kept")
	tests := []struct {
		name     string
		projects map[string]string
		answers  map[string]http.HandlerFunc
		asked    string
		nearest  []string
		paths    []string
	}{
		{
			name:     "a name the cache does not hold",
			projects: oneEnumField().projects,
			answers:  oneEnumField().answers,
			asked:    "Other",
			nearest:  []string{"Field"},
			paths:    []string{projectPath, firstFieldPath, projectPath},
		},
		{
			name:     "a field removed since",
			projects: map[string]string{"DEV": projectJSON(kept)},
			answers:  map[string]http.HandlerFunc{"1-2": answering(kept.answer(enumBundle()))},
			asked:    "Field",
			nearest:  []string{"Kept"},
			paths:    []string{projectPath, firstFieldPath, firstFieldPath, projectPath},
		},
	}
	for _, reader := range fieldReaders() {
		for _, tc := range tests {
			t.Run(reader.name+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				instance := oneEnumField()
				server, root := instance.serve(t), t.TempDir()
				_, err := reader.read(t.Context(), cached(t, server, root), "DEV", "Field")
				require.NoError(t, err)
				instance.change(tc.projects, tc.answers)

				_, err = reader.read(t.Context(), cached(t, server, root), "DEV", tc.asked)

				assert.Equal(t, unresolvedField(t, server, "unknown", withNearest("field", tc.asked, tc.nearest...)), errorOf(t, err))
				assert.Equal(t, tc.paths, server.Paths())
			})
		}
	}
}

func TestShowAndBundleLeaveTheCacheWarmAfterAFailure(t *testing.T) {
	t.Parallel()
	field := metaField{id: "1-1", name: "Field", translation: `"Translated"`, valueType: "enum"}
	tests := []struct {
		name     string
		metadata string
		refused  string
	}{
		{name: "a name the project does not have", metadata: projectJSON(field), refused: "Other"},
		{name: "a name whose id no path can hold", metadata: projectJSON(field, enumField("..", "Broken")), refused: "Broken"},
	}
	for _, reader := range fieldReaders() {
		for _, tc := range tests {
			t.Run(reader.name+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				instance := &metaInstance{
					projects: map[string]string{"DEV": tc.metadata},
					answers:  map[string]http.HandlerFunc{"1-1": answering(field.answer(enumBundle()))},
				}
				server, root := instance.serve(t), t.TempDir()
				_, err := reader.read(t.Context(), cached(t, server, root), "DEV", tc.refused)
				require.Error(t, err)

				_, err = reader.read(t.Context(), cached(t, server, root), "DEV", "Field")

				require.NoError(t, err)
				assert.Equal(t, []string{projectPath, firstFieldPath}, server.Paths())
			})
		}
	}
}

func TestShowAndBundleRefuseAnIdNoPathCanHoldOverTheCacheAsWell(t *testing.T) {
	t.Parallel()
	for _, reader := range fieldReaders() {
		t.Run(reader.name, func(t *testing.T) {
			t.Parallel()
			server := (&metaInstance{projects: map[string]string{"DEV": projectJSON(enumField("..", "Field"))}}).serve(t)
			root := t.TempDir()
			_, err := reader.read(t.Context(), cached(t, server, root), "DEV", "Field")
			require.Error(t, err)

			_, err = reader.read(t.Context(), cached(t, server, root), "DEV", "Field")

			assert.Equal(t, unaddressable(t, server), errorOf(t, err))
			assert.Equal(t, []string{projectPath, projectPath}, server.Paths())
		})
	}
}

func TestShowAndBundlePassOnAFailureOfTheFieldUnderAWarmCache(t *testing.T) {
	t.Parallel()
	passed := []string{projectPath, firstFieldPath, firstFieldPath}
	tests := []struct {
		name   string
		status int
		code   youtrack.Code
		paths  []string
	}{
		{name: "401", status: http.StatusUnauthorized, code: youtrack.CodeDenied, paths: passed},
		{name: "403", status: http.StatusForbidden, code: youtrack.CodeDenied, paths: passed},
		{name: "400", status: http.StatusBadRequest, code: youtrack.CodeRejected, paths: passed},
		{name: "500", status: http.StatusInternalServerError, code: youtrack.CodeUpstreamFailed, paths: passed},
		{name: "503", status: http.StatusServiceUnavailable, code: youtrack.CodeUpstreamFailed, paths: passed},
		{
			name:   "404, which is a miss",
			status: http.StatusNotFound,
			code:   youtrack.CodeNotFound,
			paths:  []string{projectPath, firstFieldPath, firstFieldPath, projectPath, firstFieldPath},
		},
	}
	for _, reader := range fieldReaders() {
		for _, tc := range tests {
			t.Run(reader.name+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				instance := oneEnumField()
				server, root := instance.serve(t), t.TempDir()
				_, err := reader.read(t.Context(), cached(t, server, root), "DEV", "Field")
				require.NoError(t, err)
				instance.change(oneEnumField().projects, map[string]http.HandlerFunc{"1-1": fake.JSON(tc.status, `{}`)})

				_, err = reader.read(t.Context(), cached(t, server, root), "DEV", "Field")

				want := youtrack.Error{Code: tc.code, Details: []youtrack.Pair{
					lastRequest(t, server),
					{Key: "upstream_status", Value: number(tc.status)},
				}}
				assert.Equal(t, want, errorOf(t, err))
				assert.Equal(t, tc.paths, server.Paths())
			})
		}
	}
}
