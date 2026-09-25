package youtrack_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/youtrack"
	"github.com/hakastein/youtrack/fake"
)

const bundleFields = youtrack.FieldListFields + ",bundle(values(id,name,archived))"

func TestBundleReadsTheValuesOfTheFieldItsNameResolvesTo(t *testing.T) {
	t.Parallel()
	fields := []metaField{
		{id: "1-1", name: "First", valueType: "enum"},
		{id: "1-2", name: "Second", translation: `"Translated"`, valueType: "state", required: true},
		{id: "1-3", name: "Shared", valueType: "version"},
		{id: "1-4", name: "Other", translation: `"Shared"`, valueType: "build"},
	}
	values := enumBundle(bundleValue("3-1", "Open", false), bundleValue("3-2", "Legacy", true), bundleValue("3-3", "Инфраструктура. DevOps", false))
	instance := &metaInstance{projects: map[string]string{"DEV": projectJSON(fields...)}, answers: map[string]http.HandlerFunc{}}
	for _, f := range fields {
		instance.answers[f.id] = answering(f.answer(values))
	}
	tests := []struct {
		name  string
		asked string
		field youtrack.ProjectField
	}{
		{name: "a name", asked: "First", field: youtrack.ProjectField{ID: "1-1", Name: "First", Type: fieldType("enum", false), CanBeEmpty: true}},
		{name: "a name in another letter case", asked: "FIRST", field: youtrack.ProjectField{ID: "1-1", Name: "First", Type: fieldType("enum", false), CanBeEmpty: true}},
		{
			name:  "a translation in another letter case",
			asked: "translated",
			field: youtrack.ProjectField{ID: "1-2", Name: "Second", LocalizedName: "Translated", Type: fieldType("state", false)},
		},
		{
			name:  "a name that is also the translation of another field",
			asked: "shared",
			field: youtrack.ProjectField{ID: "1-3", Name: "Shared", Type: fieldType("version", false), CanBeEmpty: true},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := instance.serve(t)

			bundle, err := client(t, server).Fields.Bundle(t.Context(), "DEV", tc.asked)

			require.NoError(t, err)
			assert.Equal(t, &youtrack.Bundle{Field: tc.field, Values: []youtrack.BundleValue{
				{ID: "3-1", Name: "Open"}, {ID: "3-2", Name: "Legacy", Archived: true}, {ID: "3-3", Name: "Инфраструктура. DevOps"},
			}}, bundle)
			assert.Equal(t, []string{projectPath, fieldPath + tc.field.ID}, server.Paths())
		})
	}
}

func TestBundleAsksForTheValuesOfTheField(t *testing.T) {
	t.Parallel()
	server := oneEnumField().serve(t)

	_, err := client(t, server).Fields.Bundle(t.Context(), "DEV", "Field")

	require.NoError(t, err)
	assert.Equal(t, bundleFields, server.Last(t).URL.Query().Get("fields"))
}

func TestBundleTakesWhetherTheFieldMayStandEmptyFromTheReadOfTheField(t *testing.T) {
	t.Parallel()
	required := metaField{id: "1-1", name: "Field", valueType: "enum", required: true}
	optional := metaField{id: "1-1", name: "Field", valueType: "enum"}
	server := (&metaInstance{
		projects: map[string]string{"DEV": projectJSON(required)},
		answers:  map[string]http.HandlerFunc{"1-1": answering(optional.answer(enumBundle()))},
	}).serve(t)

	got, err := client(t, server).Fields.Bundle(t.Context(), "DEV", "Field")

	require.NoError(t, err)
	want := &youtrack.Bundle{Field: youtrack.ProjectField{ID: "1-1", Name: "Field", Type: fieldType("enum", false), CanBeEmpty: true}, Values: []youtrack.BundleValue{}}
	assert.Equal(t, want, got)
}

func TestBundleReadsTheValuesOfATypeThatHasThem(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		valueType string
		multi     bool
	}{
		{name: "enum of one value", valueType: "enum"},
		{name: "enum of many values", valueType: "enum", multi: true},
		{name: "state", valueType: "state"},
		{name: "version of one value", valueType: "version"},
		{name: "version of many values", valueType: "version", multi: true},
		{name: "build of one value", valueType: "build"},
		{name: "build of many values", valueType: "build", multi: true},
		{name: "ownedField of one value", valueType: "ownedField"},
		{name: "ownedField of many values", valueType: "ownedField", multi: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := metaField{id: "1-1", name: "Field", valueType: tc.valueType, multi: tc.multi}
			server := (&metaInstance{
				projects: map[string]string{"DEV": projectJSON(f)},
				answers:  map[string]http.HandlerFunc{"1-1": answering(f.answer(enumBundle(bundleValue("3-1", "First", false))))},
			}).serve(t)

			got, err := client(t, server).Fields.Bundle(t.Context(), "DEV", "Field")

			require.NoError(t, err)
			want := &youtrack.Bundle{
				Field:  youtrack.ProjectField{ID: "1-1", Name: "Field", Type: fieldType(tc.valueType, tc.multi), CanBeEmpty: true},
				Values: []youtrack.BundleValue{{ID: "3-1", Name: "First"}},
			}
			assert.Equal(t, want, got)
		})
	}
}

func TestBundleRefusesAFieldOfATypeWithNoBundle(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		valueType string
		multi     bool
	}{
		{name: "user of one value", valueType: "user"},
		{name: "user of many values", valueType: "user", multi: true},
		{name: "group of one value", valueType: "group"},
		{name: "group of many values", valueType: "group", multi: true},
		{name: "period", valueType: "period"},
		{name: "text", valueType: "text"},
		{name: "date", valueType: "date"},
		{name: "date and time", valueType: "date and time"},
		{name: "integer", valueType: "integer"},
		{name: "float", valueType: "float"},
		{name: "string", valueType: "string"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := metaField{id: "1-1", name: "Field", valueType: tc.valueType, multi: tc.multi}
			server := (&metaInstance{
				projects: map[string]string{"DEV": projectJSON(f)},
				answers:  map[string]http.HandlerFunc{"1-1": answering(f.answer(""))},
			}).serve(t)

			_, err := client(t, server).Fields.Bundle(t.Context(), "DEV", "Field")

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
			assert.Equal(t, []string{projectPath, firstFieldPath}, server.Paths())
		})
	}
}

func TestBundleRefusesACallItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		project string
		field   string
	}{
		{name: "a project code with a dash", project: "DEV-1", field: "Field"},
		{name: "a field of no name", project: "DEV", field: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			_, err := client(t, server).Fields.Bundle(t.Context(), tc.project, tc.field)

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

func TestBundleRefusesAnAnswerOfAnotherShape(t *testing.T) {
	t.Parallel()
	f := enumField("1-1", "Field")
	tests := []struct {
		name   string
		answer string
	}{
		{name: "an emptiness that is no bool", answer: `{"$type":"ProjectCustomField","canBeEmpty":null,"field":` + f.naming() + `,"bundle":` + enumBundle() + `}`},
		{name: "a bundle that is null", answer: f.answer(`null`)},
		{name: "values that are no array", answer: f.answer(`{"$type":"EnumBundle","values":null}`)},
		{name: "a value whose id is no text", answer: f.answer(enumBundle(`{"$type":"EnumBundleElement","id":null,"name":"First","archived":false}`))},
		{name: "a value whose archived is no bool", answer: f.answer(enumBundle(`{"$type":"EnumBundleElement","id":"3-1","name":"First","archived":"no"}`))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := (&metaInstance{
				projects: map[string]string{"DEV": projectJSON(f)},
				answers:  map[string]http.HandlerFunc{"1-1": answering(tc.answer)},
			}).serve(t)

			_, err := client(t, server).Fields.Bundle(t.Context(), "DEV", "Field")

			assert.Equal(t, unreadable(lastRequest(t, server), tc.answer), errorOf(t, err))
		})
	}
}

func TestBundleRefusesAFieldThatLostItsBundleOnlyAfterReadingTheMetadataAgain(t *testing.T) {
	t.Parallel()
	user := metaField{id: "1-1", name: "Field", translation: `"Поле"`, valueType: "user"}
	instance := oneEnumField()
	server, root := instance.serve(t), t.TempDir()
	_, err := cached(t, server, root).Fields.Bundle(t.Context(), "DEV", "Field")
	require.NoError(t, err)
	instance.change(map[string]string{"DEV": projectJSON(user)}, map[string]http.HandlerFunc{"1-1": answering(user.answer(""))})

	_, err = cached(t, server, root).Fields.Bundle(t.Context(), "DEV", "Field")

	assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
	assert.Equal(t, []string{projectPath, firstFieldPath, firstFieldPath, projectPath, firstFieldPath}, server.Paths())
}
