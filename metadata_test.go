package youtrack_test

import (
	"cmp"
	"context"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/youtrack"
	"github.com/hakastein/youtrack/fake"
)

const (
	projectPath     = "/api/admin/projects/DEV"
	fieldPath       = projectPath + "/customFields/"
	firstFieldPath  = fieldPath + "1-1"
	secondFieldPath = fieldPath + "1-2"
	fieldNaming     = "field(name,localizedName,fieldType(valueType,isMultiValue))"
	projectFields   = "customFields(id,ordinal,canBeEmpty," + fieldNaming + ")"
)

// translation is the JSON of localizedName, null when empty.
type metaField struct {
	id          string
	name        string
	translation string
	valueType   string
	multi       bool
	required    bool
	ordinal     string
}

func (f metaField) naming() string {
	return `{"$type":"CustomField","name":` + strconv.Quote(f.name) + `,"localizedName":` + cmp.Or(f.translation, "null") +
		`,"fieldType":{"$type":"FieldType","valueType":` + strconv.Quote(f.valueType) +
		`,"isMultiValue":` + strconv.FormatBool(f.multi) + `}}`
}

func (f metaField) json() string {
	return rawBinding(strconv.Quote(f.id), cmp.Or(f.ordinal, "0"), strconv.FormatBool(!f.required), f.naming())
}

func (f metaField) answer(bundle string) string {
	held := `{"$type":"ProjectCustomField","id":` + strconv.Quote(f.id) + `,"canBeEmpty":` + strconv.FormatBool(!f.required) +
		`,"field":` + f.naming()
	if bundle == "" {
		return held + `}`
	}
	return held + `,"bundle":` + bundle + `}`
}

func rawBinding(id, ordinal, canBeEmpty, naming string) string {
	return `{"$type":"ProjectCustomField","id":` + id + `,"ordinal":` + ordinal + `,"canBeEmpty":` + canBeEmpty +
		`,"field":` + naming + `}`
}

func enumField(id, name string) metaField {
	return metaField{id: id, name: name, valueType: "enum"}
}

func projectJSON(fields ...metaField) string {
	bindings := make([]string, 0, len(fields))
	for _, f := range fields {
		bindings = append(bindings, f.json())
	}
	return projectOf("[" + strings.Join(bindings, ",") + "]")
}

func projectOf(customFields string) string {
	return `{"$type":"Project","id":"0-1","shortName":"DEV","customFields":` + customFields + `}`
}

func enumBundle(values ...string) string {
	return `{"$type":"EnumBundle","id":"5-1","values":[` + strings.Join(values, ",") + `]}`
}

func bundleValue(id, name string, archived bool) string {
	return `{"$type":"EnumBundleElement","id":` + strconv.Quote(id) + `,"name":` + strconv.Quote(name) +
		`,"archived":` + strconv.FormatBool(archived) + `}`
}

// A field with no entry in answers is a 404.
type metaInstance struct {
	mu       sync.Mutex
	projects map[string]string
	answers  map[string]http.HandlerFunc
}

func answering(body string) http.HandlerFunc {
	return fake.JSON(http.StatusOK, body)
}

func oneEnumField() *metaInstance {
	f := metaField{id: "1-1", name: "Field", translation: `"Поле"`, valueType: "enum"}
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
	return routes(t, map[string]http.HandlerFunc{
		"GET /api/admin/projects/{code}": func(w http.ResponseWriter, r *http.Request) {
			i.mu.Lock()
			metadata := i.projects[r.PathValue("code")]
			i.mu.Unlock()
			fake.JSON(http.StatusOK, metadata)(w, r)
		},
		"GET /api/admin/projects/{code}/customFields/{id}": func(w http.ResponseWriter, r *http.Request) {
			i.mu.Lock()
			answer, held := i.answers[r.PathValue("id")]
			i.mu.Unlock()
			if !held {
				answer = fake.JSON(http.StatusNotFound, `{}`)
			}
			answer(w, r)
		},
	})
}

func cached(t *testing.T, server *fake.Server, root string) *youtrack.Client {
	t.Helper()
	return client(t, server, youtrack.WithMetadataCache(root))
}

func denied(t *testing.T, server *fake.Server) youtrack.Error {
	t.Helper()
	return youtrack.Error{Code: youtrack.CodeDenied, Details: []youtrack.Pair{
		lastRequest(t, server),
		{Key: "project", Value: youtrack.NewString("DEV")},
		{Key: "permission", Value: youtrack.NewString("jetbrains.jetpass.project-read")},
	}}
}

func fourFieldsProject() string {
	return projectJSON(
		metaField{id: "1-3", name: "Third", valueType: "state", ordinal: "3"},
		metaField{id: "1-1", name: "First", translation: `"Первое"`, valueType: "enum", multi: true, required: true, ordinal: "1"},
		metaField{id: "1-2", name: "Second", valueType: "user", ordinal: "2"},
		metaField{id: "1-4", name: "Unplaced", valueType: "string", ordinal: "0"},
	)
}

func fourFields() []youtrack.ProjectField {
	return []youtrack.ProjectField{
		{ID: "1-4", Name: "Unplaced", Type: fieldType("string", false), CanBeEmpty: true},
		{ID: "1-1", Name: "First", LocalizedName: "Первое", Type: fieldType("enum", true)},
		{ID: "1-2", Name: "Second", Type: fieldType("user", false), CanBeEmpty: true},
		{ID: "1-3", Name: "Third", Type: fieldType("state", false), CanBeEmpty: true},
	}
}

func TestReadMetadataReadsTheFieldsOfTheProjectInItsOrder(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, fourFieldsProject()))

	metadata, err := client(t, server).Fields.ReadMetadata(t.Context(), "DEV")

	require.NoError(t, err)
	assert.Equal(t, &youtrack.Metadata{Fields: fourFields()}, metadata)
}

func TestReadMetadataAsksForTheFieldsOfTheProject(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, fourFieldsProject()))

	_, err := client(t, server).Fields.ReadMetadata(t.Context(), "DEV")

	require.NoError(t, err)
	assert.Equal(t, []string{projectPath + "?fields=" + projectFields}, server.Targets(t))
}

func TestReadMetadataKeepsATypeItDoesNotModel(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, projectJSON(metaField{id: "1-1", name: "Field", valueType: "quantum"})))

	metadata, err := client(t, server).Fields.ReadMetadata(t.Context(), "DEV")

	require.NoError(t, err)
	want := []youtrack.ProjectField{{ID: "1-1", Name: "Field", Type: fieldType("quantum", false), CanBeEmpty: true}}
	assert.Equal(t, &youtrack.Metadata{Fields: want}, metadata)
}

func TestMetadataReadsTheServerEachTimeWithoutACache(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, fourFieldsProject()))
	c := client(t, server)
	_, err := c.Fields.Metadata(t.Context(), "DEV")
	require.NoError(t, err)

	metadata, err := c.Fields.Metadata(t.Context(), "DEV")

	require.NoError(t, err)
	assert.Equal(t, &youtrack.Metadata{Fields: fourFields()}, metadata)
	assert.Equal(t, []string{projectPath, projectPath}, server.Paths())
}

func TestMetadataAnswersFromTheCacheAfterAReadStoredIt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		first func(ctx context.Context, c *youtrack.Client) error
	}{
		{name: "a metadata that missed", first: func(ctx context.Context, c *youtrack.Client) error {
			_, err := c.Fields.Metadata(ctx, "DEV")
			return err
		}},
		{name: "a read of the metadata", first: func(ctx context.Context, c *youtrack.Client) error {
			_, err := c.Fields.ReadMetadata(ctx, "DEV")
			return err
		}},
		{name: "a show of a field", first: func(ctx context.Context, c *youtrack.Client) error {
			_, err := c.Fields.Show(ctx, "DEV", "First", nil)
			return err
		}},
		{name: "a bundle", first: func(ctx context.Context, c *youtrack.Client) error {
			_, err := c.Fields.Bundle(ctx, "DEV", "First")
			return err
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			first := metaField{id: "1-1", name: "First", translation: `"Первое"`, valueType: "enum", multi: true, required: true}
			instance := &metaInstance{projects: map[string]string{"DEV": fourFieldsProject()},
				answers: map[string]http.HandlerFunc{"1-1": answering(first.answer(enumBundle()))}}
			server, root := instance.serve(t), t.TempDir()
			require.NoError(t, tc.first(t.Context(), cached(t, server, root)))
			read := server.Paths()

			metadata, err := cached(t, server, root).Fields.Metadata(t.Context(), "DEV")

			require.NoError(t, err)
			assert.Equal(t, &youtrack.Metadata{Fields: fourFields(), FromCache: true}, metadata)
			assert.Equal(t, read, server.Paths())
		})
	}
}

func TestReadMetadataReadsTheServerOverAWarmCache(t *testing.T) {
	t.Parallel()
	server, root := fake.Serve(t, fake.JSON(http.StatusOK, fourFieldsProject())), t.TempDir()
	_, err := cached(t, server, root).Fields.ReadMetadata(t.Context(), "DEV")
	require.NoError(t, err)

	metadata, err := cached(t, server, root).Fields.ReadMetadata(t.Context(), "DEV")

	require.NoError(t, err)
	assert.Equal(t, &youtrack.Metadata{Fields: fourFields()}, metadata)
	assert.Equal(t, []string{projectPath, projectPath}, server.Paths())
}

func TestMetadataRefusesAProjectWithNoFields(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, projectJSON()))

	_, err := client(t, server).Fields.Metadata(t.Context(), "DEV")

	assert.Equal(t, denied(t, server), errorOf(t, err))
}

func TestMetadataRefusesAProjectCodeItCannotSend(t *testing.T) {
	t.Parallel()
	for _, code := range []string{"", "1DEV", "DEV-1", "a/b", ".."} {
		t.Run(code, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			_, err := client(t, server).Fields.Metadata(t.Context(), code)
			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))

			_, err = client(t, server).Fields.ReadMetadata(t.Context(), code)
			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

func TestReadMetadataRefusesMetadataItCannotRead(t *testing.T) {
	t.Parallel()
	field := enumField("1-1", "Field")
	tests := []struct {
		name     string
		metadata string
	}{
		{name: "an answer that is no object", metadata: `[` + projectJSON(field) + `]`},
		{name: "custom fields that are no array", metadata: projectOf(field.json())},
		{name: "a custom field that is no object", metadata: projectOf(`[[]]`)},
		{name: "an id that is no text", metadata: projectOf(`[` + rawBinding(`5`, `0`, `true`, field.naming()) + `]`)},
		{name: "an ordinal that is no whole number", metadata: projectJSON(metaField{id: "1-1", name: "Field", valueType: "enum", ordinal: "1.5"})},
		{name: "an emptiness that is no bool", metadata: projectOf(`[` + rawBinding(`"1-1"`, `0`, `null`, field.naming()) + `]`)},
		{name: "a field that is no object", metadata: projectOf(`[` + rawBinding(`"1-1"`, `0`, `true`, `null`) + `]`)},
		{
			name: "a name that is no text",
			metadata: projectOf(`[` + rawBinding(`"1-1"`, `0`, `true`, `{"$type":"CustomField","name":5,"localizedName":null,`+
				`"fieldType":{"$type":"FieldType","valueType":"enum","isMultiValue":false}}`) + `]`),
		},
		{
			name: "a type that is no object",
			metadata: projectOf(`[` + rawBinding(`"1-1"`, `0`, `true`,
				`{"$type":"CustomField","name":"Field","localizedName":null,"fieldType":[]}`) + `]`),
		},
		{
			name: "a type of value that is no text",
			metadata: projectOf(`[` + rawBinding(`"1-1"`, `0`, `true`, `{"$type":"CustomField","name":"Field","localizedName":null,`+
				`"fieldType":{"$type":"FieldType","valueType":5,"isMultiValue":false}}`) + `]`),
		},
		{
			name: "a multiplicity that is no bool",
			metadata: projectOf(`[` + rawBinding(`"1-1"`, `0`, `true`, `{"$type":"CustomField","name":"Field","localizedName":null,`+
				`"fieldType":{"$type":"FieldType","valueType":"enum","isMultiValue":"no"}}`) + `]`),
		},
		{
			name:     "a translation that is neither text nor null",
			metadata: projectJSON(metaField{id: "1-1", name: "Field", translation: `5`, valueType: "enum"}),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tc.metadata))

			_, err := client(t, server).Fields.ReadMetadata(t.Context(), "DEV")

			assert.Equal(t, unreadable(lastRequest(t, server), tc.metadata), errorOf(t, err))
		})
	}
}

func TestMetadataKeepsTheCacheOfOneTokenFromAnother(t *testing.T) {
	t.Parallel()
	server, root := fake.Serve(t, fake.JSON(http.StatusOK, fourFieldsProject())), t.TempDir()
	_, err := cached(t, server, root).Fields.ReadMetadata(t.Context(), "DEV")
	require.NoError(t, err)
	another, err := youtrack.NewClient(server.URL, fake.Token+"-of-another-user", youtrack.WithMetadataCache(root))
	require.NoError(t, err)

	metadata, err := another.Fields.Metadata(t.Context(), "DEV")

	require.NoError(t, err)
	assert.Equal(t, &youtrack.Metadata{Fields: fourFields()}, metadata)
	assert.Equal(t, []string{projectPath, projectPath}, server.Paths())
}

func TestMetadataKeepsTheCacheOfOneAddressFromAnother(t *testing.T) {
	t.Parallel()
	answer, root := fake.JSON(http.StatusOK, fourFieldsProject()), t.TempDir()
	first, second := fake.Serve(t, answer), fake.Serve(t, answer)
	_, err := cached(t, first, root).Fields.ReadMetadata(t.Context(), "DEV")
	require.NoError(t, err)

	metadata, err := cached(t, second, root).Fields.Metadata(t.Context(), "DEV")

	require.NoError(t, err)
	assert.Equal(t, &youtrack.Metadata{Fields: fourFields()}, metadata)
	assert.Equal(t, []string{projectPath}, second.Paths())
}

func TestMetadataKeepsTheCacheOfOneProjectFromAnother(t *testing.T) {
	t.Parallel()
	other := metaField{id: "2-1", name: "Other", valueType: "enum"}
	server := (&metaInstance{projects: map[string]string{"DEV": fourFieldsProject(), "DOCS": projectJSON(other)}}).serve(t)
	root := t.TempDir()
	_, err := cached(t, server, root).Fields.ReadMetadata(t.Context(), "DEV")
	require.NoError(t, err)
	_, err = cached(t, server, root).Fields.ReadMetadata(t.Context(), "DOCS")
	require.NoError(t, err)

	metadata, err := cached(t, server, root).Fields.Metadata(t.Context(), "DEV")

	require.NoError(t, err)
	assert.Equal(t, &youtrack.Metadata{Fields: fourFields(), FromCache: true}, metadata)
	assert.Equal(t, []string{projectPath, "/api/admin/projects/DOCS"}, server.Paths())
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

func TestMetadataCachesTheProjectForTheLoginAlone(t *testing.T) {
	t.Parallel()
	server, root := fake.Serve(t, fake.JSON(http.StatusOK, fourFieldsProject())), filepath.Join(t.TempDir(), "cache")

	_, err := cached(t, server, root).Fields.Metadata(t.Context(), "DEV")

	require.NoError(t, err)
	entries, content := cacheEntries(t, root)
	assert.Equal(t, []cacheEntry{
		{path: ".", dir: true, mode: 0o700},
		{path: "<sha256>", dir: true, mode: 0o700},
		{path: "<sha256>/<sha256>", mode: 0o600},
	}, entries)
	assert.NotContains(t, content, fake.Token)
}

func TestMetadataSaysNothingOfACacheItCannotWrite(t *testing.T) {
	t.Parallel()
	server, home := fake.Serve(t, fake.JSON(http.StatusOK, fourFieldsProject())), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(home, "file"), nil, 0o600))
	root := filepath.Join(home, "file", "cache")
	_, err := cached(t, server, root).Fields.Metadata(t.Context(), "DEV")
	require.NoError(t, err)

	metadata, err := cached(t, server, root).Fields.Metadata(t.Context(), "DEV")

	require.NoError(t, err)
	assert.Equal(t, &youtrack.Metadata{Fields: fourFields()}, metadata)
	assert.Equal(t, []string{projectPath, projectPath}, server.Paths())
}

func TestMetadataReadsTheServerAgainWhenTheCacheDoesNotReadBack(t *testing.T) {
	t.Parallel()
	server, root := fake.Serve(t, fake.JSON(http.StatusOK, fourFieldsProject())), t.TempDir()
	_, err := cached(t, server, root).Fields.Metadata(t.Context(), "DEV")
	require.NoError(t, err)
	require.NoError(t, filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		return os.Truncate(path, 0)
	}))

	metadata, err := cached(t, server, root).Fields.Metadata(t.Context(), "DEV")

	require.NoError(t, err)
	assert.Equal(t, &youtrack.Metadata{Fields: fourFields()}, metadata)
	assert.Equal(t, []string{projectPath, projectPath}, server.Paths())
}
