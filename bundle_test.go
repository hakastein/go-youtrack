package youtrack_test

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/youtrack"
	"github.com/hakastein/youtrack/fake"
)

const (
	fieldPath       = projectPath + "/customFields/"
	firstFieldPath  = fieldPath + "1-1"
	secondFieldPath = fieldPath + "1-2"
	bareField       = fieldNaming + ",canBeEmpty"
	bundleFields    = bareField + ",bundle(values(id,name,archived))"
)

// An instance whose projects and fields a test changes between two reads.
type metaInstance struct {
	mu       sync.Mutex
	projects map[string]string
	answers  map[string]http.HandlerFunc
}

func answering(body string) http.HandlerFunc {
	return fake.JSON(http.StatusOK, body)
}

func oneEnumField() *metaInstance {
	f := metaField{id: "1-1", name: "Field", localized: "Поле", valueType: "enum"}
	return &metaInstance{
		projects: map[string]string{"DEV": projectJSON(f)},
		answers:  map[string]http.HandlerFunc{"1-1": answering(f.answer(enumBundle(bundleValue("3-1", "First", false))))},
	}
}

func (i *metaInstance) change(projects map[string]string, answers map[string]http.HandlerFunc) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.projects, i.answers = projects, answers
}

func (i *metaInstance) serve(t *testing.T) *fake.Server {
	t.Helper()
	routes := http.NewServeMux()
	routes.HandleFunc("GET /api/admin/projects/{code}", func(w http.ResponseWriter, r *http.Request) {
		i.mu.Lock()
		metadata := i.projects[r.PathValue("code")]
		i.mu.Unlock()
		fake.JSON(http.StatusOK, metadata)(w, r)
	})
	routes.HandleFunc("GET /api/admin/projects/{code}/customFields/{id}", func(w http.ResponseWriter, r *http.Request) {
		i.mu.Lock()
		answer, held := i.answers[r.PathValue("id")]
		i.mu.Unlock()
		if !held {
			answer = fake.JSON(http.StatusNotFound, `{"error":"Not Found"}`)
		}
		answer(w, r)
	})
	return fake.Serve(t, routes.ServeHTTP)
}

func cached(t *testing.T, server *fake.Server, root string) *youtrack.Client {
	t.Helper()
	return client(t, server, youtrack.WithMetadataCache(root))
}

func bundleOf(t *testing.T, c *youtrack.Client, project, name string) (*youtrack.Bundle, error) {
	t.Helper()
	return c.Bundle(t.Context(), project, name)
}

func firstBundle() *youtrack.Bundle {
	return &youtrack.Bundle{
		Field:  youtrack.ProjectField{ID: "1-1", Name: "Field", LocalizedName: "Поле", Type: fieldType("enum", false), CanBeEmpty: true},
		Values: []youtrack.BundleValue{{ID: "3-1", Name: "First"}},
	}
}

func TestBundleReadsTheValuesOfTheFieldItsNameResolvesTo(t *testing.T) {
	t.Parallel()
	fields := []metaField{
		{id: "1-1", name: "First", valueType: "enum"},
		{id: "1-2", name: "Second", localized: "Translated", valueType: "state", multi: false, required: true},
		{id: "1-3", name: "Shared", valueType: "version"},
		{id: "1-4", name: "Other", localized: "Shared", valueType: "build"},
	}
	values := enumBundle(bundleValue("3-1", "Open", false), bundleValue("3-2", "Legacy", true), bundleValue("3-3", "Инфраструктура. DevOps", false))
	instance := &metaInstance{projects: map[string]string{"DEV": projectJSON(fields...)}, answers: map[string]http.HandlerFunc{}}
	for _, f := range fields {
		instance.answers[f.id] = answering(f.answer(values))
	}
	tests := []struct {
		name  string
		asked string
		id    string
		field youtrack.ProjectField
	}{
		{name: "a name", asked: "First", id: "1-1", field: youtrack.ProjectField{ID: "1-1", Name: "First", Type: fieldType("enum", false), CanBeEmpty: true}},
		{name: "a name in another letter case", asked: "FIRST", id: "1-1", field: youtrack.ProjectField{ID: "1-1", Name: "First", Type: fieldType("enum", false), CanBeEmpty: true}},
		{name: "a translation in another letter case", asked: "translated", id: "1-2",
			field: youtrack.ProjectField{ID: "1-2", Name: "Second", LocalizedName: "Translated", Type: fieldType("state", false)}},
		{name: "a name that is also the translation of another field", asked: "shared", id: "1-3",
			field: youtrack.ProjectField{ID: "1-3", Name: "Shared", Type: fieldType("version", false), CanBeEmpty: true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := instance.serve(t)

			bundle, err := bundleOf(t, client(t, server), "DEV", tc.asked)

			require.NoError(t, err)
			assert.Equal(t, &youtrack.Bundle{Field: tc.field, Values: []youtrack.BundleValue{
				{ID: "3-1", Name: "Open"}, {ID: "3-2", Name: "Legacy", Archived: true}, {ID: "3-3", Name: "Инфраструктура. DevOps"},
			}}, bundle)
			assert.Equal(t, []string{projectPath, fieldPath + tc.id}, server.Paths())
			assert.Equal(t, bundleFields, server.Last(t).URL.Query().Get("fields"))
		})
	}
}

func TestBundleAsksForTheBundleOfATypeThatHasOneAndRefusesTheRest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		valueType string
		multi     bool
		bundle    bool
	}{
		{name: "enum of one value", valueType: "enum", bundle: true},
		{name: "enum of many values", valueType: "enum", multi: true, bundle: true},
		{name: "state", valueType: "state", bundle: true},
		{name: "version of one value", valueType: "version", bundle: true},
		{name: "version of many values", valueType: "version", multi: true, bundle: true},
		{name: "build of one value", valueType: "build", bundle: true},
		{name: "build of many values", valueType: "build", multi: true, bundle: true},
		{name: "ownedField of one value", valueType: "ownedField", bundle: true},
		{name: "ownedField of many values", valueType: "ownedField", multi: true, bundle: true},
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
			bundle := ""
			if tc.bundle {
				bundle = enumBundle(bundleValue("3-1", "First", false))
			}
			server := (&metaInstance{projects: map[string]string{"DEV": projectJSON(f)}, answers: map[string]http.HandlerFunc{"1-1": answering(f.answer(bundle))}}).serve(t)

			got, err := bundleOf(t, client(t, server), "DEV", "Field")

			if tc.bundle {
				require.NoError(t, err)
				assert.Equal(t, []youtrack.BundleValue{{ID: "3-1", Name: "First"}}, got.Values)
				assert.Equal(t, bundleFields, server.Last(t).URL.Query().Get("fields"))
				return
			}
			assert.Equal(t, youtrack.ArgumentError{Argument: "field", Value: "Field"}, argumentErrorOf(t, err))
			assert.Equal(t, bareField, server.Last(t).URL.Query().Get("fields"))
		})
	}
}

func TestBundleRefusesANameNoSingleFieldAnswersTo(t *testing.T) {
	t.Parallel()
	project := projectJSON(
		metaField{id: "1-1", name: "First", localized: "Shared", valueType: "enum"},
		metaField{id: "1-2", name: "Second", localized: "Shared", valueType: "enum"},
		metaField{id: "1-3", name: "Third", valueType: "enum"},
	)
	tests := []struct {
		name      string
		asked     string
		unknown   []string
		ambiguous []youtrack.AmbiguousName
	}{
		{name: "a name of no field", asked: "Thrid", unknown: []string{"Thrid"}},
		{name: "a translation of two fields", asked: "shared", ambiguous: []youtrack.AmbiguousName{{Name: "shared", Candidates: []string{"First", "Second"}}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := (&metaInstance{projects: map[string]string{"DEV": project}}).serve(t)

			_, err := bundleOf(t, client(t, server), "DEV", tc.asked)

			var failed *youtrack.FieldNameError
			require.ErrorAs(t, err, &failed)
			want := youtrack.FieldNameError{Request: lastRequest(t, server), Project: "DEV", Unknown: tc.unknown, Ambiguous: tc.ambiguous,
				Known: []string{"First", "Second", "Third"}}
			assert.Equal(t, want, *failed)
			assert.Equal(t, []string{projectPath}, server.Paths())
		})
	}
}

func TestBundleRefusesACallItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		project string
		field   string
		want    youtrack.ArgumentError
	}{
		{name: "a project code with a dash", project: "DEV-1", field: "Field", want: youtrack.ArgumentError{Argument: "project", Value: "DEV-1"}},
		{name: "a field of no name", project: "DEV", field: "", want: youtrack.ArgumentError{Argument: "field"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			_, err := bundleOf(t, client(t, server), tc.project, tc.field)

			assert.Equal(t, tc.want, argumentErrorOf(t, err))
		})
	}
}

func TestBundleRefusesAProjectWithNoFields(t *testing.T) {
	t.Parallel()
	server := (&metaInstance{projects: map[string]string{"DEV": projectJSON()}}).serve(t)

	_, err := bundleOf(t, client(t, server), "DEV", "Field")

	var failed *youtrack.PermissionError
	require.ErrorAs(t, err, &failed)
	assert.Equal(t, youtrack.PermissionError{Request: lastRequest(t, server), Project: "DEV", Permission: "jetbrains.jetpass.project-read"}, *failed)
}

func TestBundleRefusesMetadataItCannotAddress(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		metadata string
	}{
		{name: "an id no path can hold", metadata: projectJSON(enumField("..", "Field"))},
		{name: "a type the module does not model", metadata: projectJSON(metaField{id: "1-1", name: "Field", valueType: "state", multi: true})},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := (&metaInstance{projects: map[string]string{"DEV": tc.metadata}}).serve(t)

			_, err := bundleOf(t, client(t, server), "DEV", "Field")

			assert.Equal(t, invalidAnswer(lastRequest(t, server), tc.metadata), responseErrorOf(t, err))
			assert.Equal(t, []string{projectPath}, server.Paths())
		})
	}
}

func TestBundleRefusesAFieldThatChangedBetweenTheTwoRequests(t *testing.T) {
	t.Parallel()
	held := metaField{id: "1-1", name: "Field", localized: "Translated", valueType: "enum"}
	tests := []struct {
		name   string
		answer metaField
	}{
		{name: "another name", answer: metaField{id: "1-1", name: "Renamed", localized: "Translated", valueType: "enum"}},
		{name: "another translation", answer: metaField{id: "1-1", name: "Field", localized: "Retranslated", valueType: "enum"}},
		{name: "no translation", answer: metaField{id: "1-1", name: "Field", valueType: "enum"}},
		{name: "another type of value", answer: metaField{id: "1-1", name: "Field", localized: "Translated", valueType: "state"}},
		{name: "many values where one was", answer: metaField{id: "1-1", name: "Field", localized: "Translated", valueType: "enum", multi: true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			answer := tc.answer.answer(enumBundle())
			server := (&metaInstance{projects: map[string]string{"DEV": projectJSON(held)}, answers: map[string]http.HandlerFunc{"1-1": answering(answer)}}).serve(t)

			_, err := bundleOf(t, client(t, server), "DEV", "Field")

			var failed *youtrack.ChangedFieldError
			require.ErrorAs(t, err, &failed)
			assert.Equal(t, youtrack.ChangedFieldError{Request: lastRequest(t, server), Project: "DEV", Field: "Field", Body: []byte(answer)}, *failed)
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
		{name: "a field it cannot compare", answer: `{"$type":"ProjectCustomField","canBeEmpty":true,"field":[]}`},
		{name: "an emptiness that is no bool", answer: `{"$type":"ProjectCustomField","canBeEmpty":null,"field":` + f.naming() + `}`},
		{name: "a bundle that is null", answer: f.answer(`null`)},
		{name: "values that are no array", answer: f.answer(`{"$type":"EnumBundle","values":null}`)},
		{name: "a value that is no object", answer: f.answer(enumBundle(`5`))},
		{name: "a value without an id", answer: f.answer(enumBundle(`{"$type":"EnumBundleElement","name":"First","archived":false}`))},
		{name: "a value whose archived is no bool", answer: f.answer(enumBundle(`{"$type":"EnumBundleElement","id":"3-1","name":"First","archived":"no"}`))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := (&metaInstance{projects: map[string]string{"DEV": projectJSON(f)}, answers: map[string]http.HandlerFunc{"1-1": answering(tc.answer)}}).serve(t)

			_, err := bundleOf(t, client(t, server), "DEV", "Field")

			assert.Equal(t, invalidAnswer(lastRequest(t, server), tc.answer), responseErrorOf(t, err))
		})
	}
}

func TestBundleAnswersFromTheMetadataItCached(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		field metaField
	}{
		{name: "a field with a translation", field: metaField{id: "1-1", name: "Field", localized: "Translated", valueType: "enum"}},
		{name: "a field with no translation", field: metaField{id: "1-1", name: "Field", valueType: "enum"}},
		{name: "a field of many values", field: metaField{id: "1-1", name: "Field", valueType: "enum", multi: true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			instance := &metaInstance{
				projects: map[string]string{"DEV": projectJSON(tc.field)},
				answers:  map[string]http.HandlerFunc{"1-1": answering(tc.field.answer(enumBundle(bundleValue("3-1", "First", false))))},
			}
			server, root := instance.serve(t), t.TempDir()
			first, err := bundleOf(t, cached(t, server, root), "DEV", "Field")
			require.NoError(t, err)

			second, err := bundleOf(t, cached(t, server, root), "DEV", "Field")

			require.NoError(t, err)
			assert.Equal(t, first, second)
			assert.Equal(t, []youtrack.BundleValue{{ID: "3-1", Name: "First"}}, second.Values)
			assert.Equal(t, []string{projectPath, firstFieldPath, firstFieldPath}, server.Paths())
		})
	}
}

func TestBundleReadsTheMetadataAgainWhenTheCacheMisses(t *testing.T) {
	t.Parallel()
	field := metaField{id: "1-1", name: "Field", localized: "Поле", valueType: "enum"}
	added := metaField{id: "1-2", name: "Added", valueType: "enum"}
	user := metaField{id: "1-1", name: "Field", localized: "Поле", valueType: "user"}
	rebound := metaField{id: "1-2", name: "Field", localized: "Поле", valueType: "enum"}
	values := enumBundle(bundleValue("3-1", "First", false))
	tests := []struct {
		name     string
		projects map[string]string
		answers  map[string]http.HandlerFunc
		asked    string
		want     *youtrack.Bundle
		paths    []string
	}{
		{
			name:     "a field added since",
			projects: map[string]string{"DEV": projectJSON(field, added)},
			answers:  map[string]http.HandlerFunc{"1-1": answering(field.answer(values)), "1-2": answering(added.answer(values))},
			asked:    "Added",
			want:     &youtrack.Bundle{Field: youtrack.ProjectField{ID: "1-2", Name: "Added", Type: fieldType("enum", false), CanBeEmpty: true}, Values: firstBundle().Values},
			paths:    []string{projectPath, firstFieldPath, projectPath, secondFieldPath},
		},
		{
			name:     "a field that became one of a type with no bundle",
			projects: map[string]string{"DEV": projectJSON(user)},
			answers:  map[string]http.HandlerFunc{"1-1": answering(user.answer(""))},
			asked:    "Field",
			paths:    []string{projectPath, firstFieldPath, firstFieldPath, projectPath, firstFieldPath},
		},
		{
			name:     "a field bound again under another id since",
			projects: map[string]string{"DEV": projectJSON(rebound)},
			answers:  map[string]http.HandlerFunc{"1-2": answering(rebound.answer(values))},
			asked:    "Field",
			want:     &youtrack.Bundle{Field: youtrack.ProjectField{ID: "1-2", Name: "Field", LocalizedName: "Поле", Type: fieldType("enum", false), CanBeEmpty: true}, Values: firstBundle().Values},
			paths:    []string{projectPath, firstFieldPath, firstFieldPath, projectPath, secondFieldPath},
		},
		{
			name:     "an answer to the request of the cached field that cannot be read",
			projects: map[string]string{"DEV": projectJSON(field)},
			answers:  map[string]http.HandlerFunc{"1-1": fake.InTurn(fake.JSON(http.StatusOK, `[]`), answering(field.answer(values)))},
			asked:    "Field",
			want:     firstBundle(),
			paths:    []string{projectPath, firstFieldPath, firstFieldPath, projectPath, firstFieldPath},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			instance := oneEnumField()
			server, root := instance.serve(t), t.TempDir()
			_, err := bundleOf(t, cached(t, server, root), "DEV", "Field")
			require.NoError(t, err)
			instance.change(tc.projects, tc.answers)

			got, err := bundleOf(t, cached(t, server, root), "DEV", tc.asked)

			if tc.want == nil {
				assert.Equal(t, youtrack.ArgumentError{Argument: "field", Value: tc.asked}, argumentErrorOf(t, err))
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.want, got)
			}
			assert.Equal(t, tc.paths, server.Paths())
		})
	}
}

func TestBundleRefusesANameOnlyAfterReadingTheMetadataAgain(t *testing.T) {
	t.Parallel()
	kept := metaField{id: "1-2", name: "Kept", valueType: "enum"}
	tests := []struct {
		name     string
		projects map[string]string
		answers  map[string]http.HandlerFunc
		asked    string
		known    []string
		paths    []string
	}{
		{
			name:     "a name the cache does not hold",
			projects: oneEnumField().projects,
			answers:  oneEnumField().answers,
			asked:    "Other",
			known:    []string{"Field"},
			paths:    []string{projectPath, firstFieldPath, projectPath},
		},
		{
			name:     "a field removed since",
			projects: map[string]string{"DEV": projectJSON(kept)},
			answers:  map[string]http.HandlerFunc{"1-2": answering(kept.answer(enumBundle()))},
			asked:    "Field",
			known:    []string{"Kept"},
			paths:    []string{projectPath, firstFieldPath, firstFieldPath, projectPath},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			instance := oneEnumField()
			server, root := instance.serve(t), t.TempDir()
			_, err := bundleOf(t, cached(t, server, root), "DEV", "Field")
			require.NoError(t, err)
			instance.change(tc.projects, tc.answers)

			_, err = bundleOf(t, cached(t, server, root), "DEV", tc.asked)

			var failed *youtrack.FieldNameError
			require.ErrorAs(t, err, &failed)
			assert.Equal(t, youtrack.FieldNameError{Request: lastRequest(t, server), Project: "DEV", Unknown: []string{tc.asked}, Known: tc.known}, *failed)
			assert.Equal(t, tc.paths, server.Paths())
		})
	}
}

func TestBundleReadsTheMetadataAgainForACachedTypeItDoesNotModel(t *testing.T) {
	t.Parallel()
	stateOfMany := metaField{id: "1-1", name: "State", valueType: "state", multi: true}
	stateOfOne := metaField{id: "1-1", name: "State", valueType: "state"}
	instance := &metaInstance{projects: map[string]string{"DEV": projectJSON(stateOfMany)}}
	server, root := instance.serve(t), t.TempDir()
	_, err := bundleOf(t, cached(t, server, root), "DEV", "State")
	require.Error(t, err)
	instance.change(map[string]string{"DEV": projectJSON(stateOfOne)},
		map[string]http.HandlerFunc{"1-1": answering(stateOfOne.answer(enumBundle()))})

	_, err = bundleOf(t, cached(t, server, root), "DEV", "State")

	require.NoError(t, err)
	assert.Equal(t, []string{projectPath, projectPath, firstFieldPath}, server.Paths())
}

func TestBundleLeavesTheCacheWarmAfterAFailure(t *testing.T) {
	t.Parallel()
	field := metaField{id: "1-1", name: "Field", localized: "Translated", valueType: "enum"}
	tests := []struct {
		name     string
		metadata string
		refused  string
	}{
		{name: "a name the project does not have", metadata: projectJSON(field), refused: "Other"},
		{name: "a name whose id no path can hold", metadata: projectJSON(field, enumField("..", "Broken")), refused: "Broken"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			instance := &metaInstance{
				projects: map[string]string{"DEV": tc.metadata},
				answers:  map[string]http.HandlerFunc{"1-1": answering(field.answer(enumBundle()))},
			}
			server, root := instance.serve(t), t.TempDir()
			_, err := bundleOf(t, cached(t, server, root), "DEV", tc.refused)
			require.Error(t, err)

			got, err := bundleOf(t, cached(t, server, root), "DEV", "Field")

			require.NoError(t, err)
			assert.Equal(t, "Field", got.Field.Name)
			assert.Equal(t, []string{projectPath, firstFieldPath}, server.Paths())
		})
	}
}

func TestBundleRefusesAnIdNoPathCanHoldOverTheCacheAsWell(t *testing.T) {
	t.Parallel()
	metadata := projectJSON(enumField("..", "Field"))
	instance := &metaInstance{projects: map[string]string{"DEV": metadata}}
	server, root := instance.serve(t), t.TempDir()
	_, err := bundleOf(t, cached(t, server, root), "DEV", "Field")
	require.Error(t, err)

	_, err = bundleOf(t, cached(t, server, root), "DEV", "Field")

	assert.Equal(t, invalidAnswer(lastRequest(t, server), metadata), responseErrorOf(t, err))
	assert.Equal(t, []string{projectPath, projectPath}, server.Paths())
}

func TestBundlePassesOnAFailureOfTheFieldUnderAWarmCache(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		status int
		paths  []string
	}{
		{name: "401", status: http.StatusUnauthorized, paths: []string{projectPath, firstFieldPath, firstFieldPath}},
		{name: "403", status: http.StatusForbidden, paths: []string{projectPath, firstFieldPath, firstFieldPath}},
		{name: "400", status: http.StatusBadRequest, paths: []string{projectPath, firstFieldPath, firstFieldPath}},
		{name: "500", status: http.StatusInternalServerError, paths: []string{projectPath, firstFieldPath, firstFieldPath}},
		{name: "503", status: http.StatusServiceUnavailable, paths: []string{projectPath, firstFieldPath, firstFieldPath}},
		{name: "404, which is a miss", status: http.StatusNotFound, paths: []string{projectPath, firstFieldPath, firstFieldPath, projectPath, firstFieldPath}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			instance := oneEnumField()
			server, root := instance.serve(t), t.TempDir()
			_, err := bundleOf(t, cached(t, server, root), "DEV", "Field")
			require.NoError(t, err)
			instance.change(oneEnumField().projects, map[string]http.HandlerFunc{"1-1": fake.JSON(tc.status, `{}`)})

			_, err = bundleOf(t, cached(t, server, root), "DEV", "Field")

			assert.Equal(t, youtrack.StatusError{Request: lastRequest(t, server), Status: tc.status, Body: []byte(`{}`)}, statusErrorOf(t, err))
			assert.Equal(t, tc.paths, server.Paths())
		})
	}
}

func TestBundleKeepsTheCacheOfOneTokenFromAnother(t *testing.T) {
	t.Parallel()
	server, root := oneEnumField().serve(t), t.TempDir()
	_, err := bundleOf(t, cached(t, server, root), "DEV", "Field")
	require.NoError(t, err)
	another, err := youtrack.New(server.URL, fake.Token+"-of-another-user", youtrack.WithMetadataCache(root))
	require.NoError(t, err)

	_, err = bundleOf(t, another, "DEV", "Field")

	require.NoError(t, err)
	assert.Equal(t, []string{projectPath, firstFieldPath, projectPath, firstFieldPath}, server.Paths())
}

func TestBundleKeepsTheCacheOfOneAddressFromAnother(t *testing.T) {
	t.Parallel()
	instance, root := oneEnumField(), t.TempDir()
	first, second := instance.serve(t), instance.serve(t)
	_, err := bundleOf(t, cached(t, first, root), "DEV", "Field")
	require.NoError(t, err)

	_, err = bundleOf(t, cached(t, second, root), "DEV", "Field")

	require.NoError(t, err)
	assert.Equal(t, []string{projectPath, firstFieldPath}, second.Paths())
}

func TestBundleKeepsTheCacheOfOneProjectFromAnother(t *testing.T) {
	t.Parallel()
	field, other := enumField("1-1", "Field"), enumField("2-1", "Other")
	server := (&metaInstance{
		projects: map[string]string{"DEV": projectJSON(field), "DOCS": projectJSON(other)},
		answers:  map[string]http.HandlerFunc{"1-1": answering(field.answer(enumBundle())), "2-1": answering(other.answer(enumBundle()))},
	}).serve(t)
	root := t.TempDir()
	_, err := bundleOf(t, cached(t, server, root), "DEV", "Field")
	require.NoError(t, err)
	_, err = bundleOf(t, cached(t, server, root), "DOCS", "Other")
	require.NoError(t, err)

	got, err := bundleOf(t, cached(t, server, root), "DEV", "Field")

	require.NoError(t, err)
	assert.Equal(t, "Field", got.Field.Name)
	assert.Equal(t, []string{projectPath, firstFieldPath, "/api/admin/projects/DOCS", "/api/admin/projects/DOCS/customFields/2-1", firstFieldPath}, server.Paths())
}

type cacheEntry struct {
	path string
	dir  bool
	mode fs.FileMode
}

func cacheEntries(t *testing.T, root string) (entries []cacheEntry, content string) {
	t.Helper()
	digest := regexp.MustCompile(`[0-9a-f]{64}`)
	require.NoError(t, filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		require.NoError(t, err)
		relative, err := filepath.Rel(root, path)
		require.NoError(t, err)
		info, err := entry.Info()
		require.NoError(t, err)
		entries = append(entries, cacheEntry{
			path: digest.ReplaceAllString(filepath.ToSlash(relative), "<sha256>"),
			dir:  entry.IsDir(),
			mode: info.Mode().Perm(),
		})
		if !entry.IsDir() {
			held, err := os.ReadFile(path)
			require.NoError(t, err)
			content += string(held)
		}
		return nil
	}))
	return entries, content
}

func TestBundleCachesTheMetadataForTheLoginAlone(t *testing.T) {
	t.Parallel()
	server, root := oneEnumField().serve(t), filepath.Join(t.TempDir(), "cache")

	_, err := bundleOf(t, cached(t, server, root), "DEV", "Field")

	require.NoError(t, err)
	entries, content := cacheEntries(t, root)
	assert.Equal(t, []cacheEntry{
		{path: ".", dir: true, mode: 0o700},
		{path: "<sha256>", dir: true, mode: 0o700},
		{path: "<sha256>/<sha256>", mode: 0o600},
	}, entries)
	assert.NotContains(t, content, fake.Token)
}

func TestBundleSaysNothingOfACacheItCannotWrite(t *testing.T) {
	t.Parallel()
	server, home := oneEnumField().serve(t), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(home, "file"), nil, 0o600))
	root := filepath.Join(home, "file", "cache")
	_, err := bundleOf(t, cached(t, server, root), "DEV", "Field")
	require.NoError(t, err)

	got, err := bundleOf(t, cached(t, server, root), "DEV", "Field")

	require.NoError(t, err)
	assert.Equal(t, firstBundle(), got)
	assert.Equal(t, []string{projectPath, firstFieldPath, projectPath, firstFieldPath}, server.Paths())
}

func TestBundleReadsTheMetadataAgainWhenTheCacheDoesNotReadBack(t *testing.T) {
	t.Parallel()
	server, root := oneEnumField().serve(t), t.TempDir()
	_, err := bundleOf(t, cached(t, server, root), "DEV", "Field")
	require.NoError(t, err)
	require.NoError(t, filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		return os.Truncate(path, 0)
	}))

	got, err := bundleOf(t, cached(t, server, root), "DEV", "Field")

	require.NoError(t, err)
	assert.Equal(t, firstBundle(), got)
	assert.Equal(t, []string{projectPath, firstFieldPath, projectPath, firstFieldPath}, server.Paths())
}
