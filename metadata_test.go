package youtrack_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/youtrack"
	"github.com/hakastein/youtrack/fake"
)

func fourFieldsProject() string {
	return projectJSON(
		metaField{id: "1-3", name: "Third", valueType: "state", ordinal: "3"},
		metaField{id: "1-1", name: "First", localized: "Первое", valueType: "enum", multi: true, required: true, ordinal: "1"},
		metaField{id: "1-2", name: "Second", valueType: "user", ordinal: "2"},
		metaField{id: "1-4", name: "Unplaced", valueType: "string", ordinal: "0"},
	)
}

func fourFields() []youtrack.ProjectField {
	return []youtrack.ProjectField{
		{ID: "1-4", Name: "Unplaced", Type: youtrack.FieldType{ValueType: youtrack.StringType}, CanBeEmpty: true},
		{ID: "1-1", Name: "First", LocalizedName: "Первое", Type: youtrack.FieldType{ValueType: youtrack.EnumType, Multi: true}},
		{ID: "1-2", Name: "Second", Type: youtrack.FieldType{ValueType: youtrack.UserType}, CanBeEmpty: true},
		{ID: "1-3", Name: "Third", Type: youtrack.FieldType{ValueType: youtrack.StateType}, CanBeEmpty: true},
	}
}

func TestReadMetadataReadsTheFieldsOfTheProjectInItsOrder(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, fourFieldsProject()))

	metadata, err := client(t, server).ReadMetadata(t.Context(), "DEV")

	require.NoError(t, err)
	want := &youtrack.Metadata{Fields: fourFields(), Request: requestTo(http.MethodGet, server, projectPath+"?fields="+projectFields)}
	assert.Equal(t, want, metadata)
	assert.Equal(t, []string{projectPath}, server.Paths())
}

func TestMetadataReadsTheServerEachTimeWithoutACache(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, fourFieldsProject()))
	c := client(t, server)

	first, err := c.Metadata(t.Context(), "DEV")
	require.NoError(t, err)
	second, err := c.Metadata(t.Context(), "DEV")

	require.NoError(t, err)
	assert.Equal(t, first, second)
	assert.False(t, second.FromCache)
	assert.Equal(t, []string{projectPath, projectPath}, server.Paths())
}

func TestMetadataAnswersFromTheCacheAfterAReadStoredIt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		first func(c *youtrack.Client) error
	}{
		{name: "a Metadata that missed", first: func(c *youtrack.Client) error { _, err := c.Metadata(t.Context(), "DEV"); return err }},
		{name: "a ReadMetadata", first: func(c *youtrack.Client) error { _, err := c.ReadMetadata(t.Context(), "DEV"); return err }},
		{name: "a Bundle", first: func(c *youtrack.Client) error { _, err := c.Bundle(t.Context(), "DEV", "First"); return err }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			first := metaField{id: "1-1", name: "First", localized: "Первое", valueType: "enum", multi: true, required: true, ordinal: "1"}
			instance := &metaInstance{projects: map[string]string{"DEV": fourFieldsProject()},
				answers: map[string]http.HandlerFunc{"1-1": answering(first.answer(enumBundle()))}}
			server, root := instance.serve(t), t.TempDir()
			require.NoError(t, tc.first(cached(t, server, root)))
			read := len(server.Paths())

			metadata, err := cached(t, server, root).Metadata(t.Context(), "DEV")

			require.NoError(t, err)
			assert.Equal(t, &youtrack.Metadata{Fields: fourFields(), FromCache: true}, metadata)
			assert.Len(t, server.Paths(), read)
		})
	}
}

func TestReadMetadataReadsTheServerOverAWarmCache(t *testing.T) {
	t.Parallel()
	server, root := fake.Serve(t, fake.JSON(http.StatusOK, fourFieldsProject())), t.TempDir()
	_, err := cached(t, server, root).ReadMetadata(t.Context(), "DEV")
	require.NoError(t, err)

	metadata, err := cached(t, server, root).ReadMetadata(t.Context(), "DEV")

	require.NoError(t, err)
	assert.False(t, metadata.FromCache)
	assert.Equal(t, []string{projectPath, projectPath}, server.Paths())
}

func TestMetadataRefusesAProjectWithNoFields(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, projectJSON()))

	_, err := client(t, server).Metadata(t.Context(), "DEV")

	var failed *youtrack.PermissionError
	require.ErrorAs(t, err, &failed)
	assert.Equal(t, youtrack.PermissionError{Request: lastRequest(t, server), Project: "DEV", Permission: "jetbrains.jetpass.project-read"}, *failed)
}

func TestMetadataRefusesAProjectCodeItCannotSend(t *testing.T) {
	t.Parallel()
	for _, code := range []string{"", "1DEV", "DEV-1", "a/b", ".."} {
		t.Run(code, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			_, err := client(t, server).Metadata(t.Context(), code)
			assert.Equal(t, youtrack.ArgumentError{Argument: "project", Value: code}, argumentErrorOf(t, err))

			_, err = client(t, server).ReadMetadata(t.Context(), code)
			assert.Equal(t, youtrack.ArgumentError{Argument: "project", Value: code}, argumentErrorOf(t, err))
		})
	}
}

func TestReadMetadataRefusesMetadataItCannotRead(t *testing.T) {
	t.Parallel()
	naming := enumField("1-1", "Field").json()
	tests := []struct {
		name     string
		metadata string
	}{
		{name: "an answer that is no object", metadata: `[` + naming + `]`},
		{name: "a short name that is no text", metadata: `{"$type":"Project","id":"0-1","shortName":7,"customFields":[]}`},
		{name: "custom fields that are no array", metadata: `{"$type":"Project","id":"0-1","shortName":"DEV","customFields":` + naming + `}`},
		{name: "a custom field that is no object", metadata: `{"$type":"Project","id":"0-1","shortName":"DEV","customFields":[[]]}`},
		{name: "an id that is no text", metadata: projectJSON(metaField{id: "", name: "Field", valueType: "enum"})[:0] +
			`{"$type":"Project","id":"0-1","shortName":"DEV","customFields":[{"$type":"ProjectCustomField","id":5,"ordinal":0,"canBeEmpty":true,"field":{"$type":"CustomField","name":"Field","localizedName":null,"fieldType":{"$type":"FieldType","valueType":"enum","isMultiValue":false}}}]}`},
		{name: "an ordinal that is no whole number", metadata: projectJSON(metaField{id: "1-1", name: "Field", valueType: "enum", ordinal: "1.5"})},
		{name: "an emptiness that is no bool", metadata: `{"$type":"Project","id":"0-1","shortName":"DEV","customFields":[{"$type":"ProjectCustomField","id":"1-1","ordinal":0,"canBeEmpty":null,"field":{"$type":"CustomField","name":"Field","localizedName":null,"fieldType":{"$type":"FieldType","valueType":"enum","isMultiValue":false}}}]}`},
		{name: "a field that is no object", metadata: `{"$type":"Project","id":"0-1","shortName":"DEV","customFields":[{"$type":"ProjectCustomField","id":"1-1","ordinal":0,"canBeEmpty":true,"field":null}]}`},
		{name: "a name that is no text", metadata: `{"$type":"Project","id":"0-1","shortName":"DEV","customFields":[{"$type":"ProjectCustomField","id":"1-1","ordinal":0,"canBeEmpty":true,"field":{"$type":"CustomField","name":5,"localizedName":null,"fieldType":{"$type":"FieldType","valueType":"enum","isMultiValue":false}}}]}`},
		{name: "a type that is no object", metadata: `{"$type":"Project","id":"0-1","shortName":"DEV","customFields":[{"$type":"ProjectCustomField","id":"1-1","ordinal":0,"canBeEmpty":true,"field":{"$type":"CustomField","name":"Field","localizedName":null,"fieldType":[]}}]}`},
		{name: "a type of value that is no text", metadata: `{"$type":"Project","id":"0-1","shortName":"DEV","customFields":[{"$type":"ProjectCustomField","id":"1-1","ordinal":0,"canBeEmpty":true,"field":{"$type":"CustomField","name":"Field","localizedName":null,"fieldType":{"$type":"FieldType","valueType":5,"isMultiValue":false}}}]}`},
		{name: "a multiplicity that is no bool", metadata: `{"$type":"Project","id":"0-1","shortName":"DEV","customFields":[{"$type":"ProjectCustomField","id":"1-1","ordinal":0,"canBeEmpty":true,"field":{"$type":"CustomField","name":"Field","localizedName":null,"fieldType":{"$type":"FieldType","valueType":"enum","isMultiValue":"no"}}}]}`},
		{name: "a translation that is neither text nor null", metadata: `{"$type":"Project","id":"0-1","shortName":"DEV","customFields":[{"$type":"ProjectCustomField","id":"1-1","ordinal":0,"canBeEmpty":true,"field":{"$type":"CustomField","name":"Field","localizedName":5,"fieldType":{"$type":"FieldType","valueType":"enum","isMultiValue":false}}}]}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tc.metadata))

			_, err := client(t, server).ReadMetadata(t.Context(), "DEV")

			assert.Equal(t, invalidAnswer(lastRequest(t, server), tc.metadata), responseErrorOf(t, err))
		})
	}
}

func TestReadMetadataKeepsATypeItDoesNotModel(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, projectJSON(metaField{id: "1-1", name: "Field", valueType: "quantum"})))

	metadata, err := client(t, server).ReadMetadata(t.Context(), "DEV")

	require.NoError(t, err)
	require.Len(t, metadata.Fields, 1)
	assert.Equal(t, youtrack.FieldType{ValueType: "quantum"}, metadata.Fields[0].Type)
	assert.False(t, metadata.Fields[0].Type.Known())
}
