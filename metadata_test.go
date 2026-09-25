package youtrack_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/youtrack"
	"github.com/hakastein/youtrack/fake"
)

func TestFieldsReadsTheFieldsOfTheProjectInItsOrder(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, projectJSON(
		metaField{id: "1-3", name: "Third", valueType: "state", ordinal: "3"},
		metaField{id: "1-1", name: "First", localized: "Первое", valueType: "enum", multi: true, required: true, ordinal: "1"},
		metaField{id: "1-2", name: "Second", valueType: "user", ordinal: "2"},
		metaField{id: "1-4", name: "Unplaced", valueType: "string", ordinal: "0"},
	)))

	fields, err := client(t, server).Fields(t.Context(), "DEV")

	require.NoError(t, err)
	assert.Equal(t, []youtrack.ProjectField{
		{ID: "1-4", Name: "Unplaced", Type: youtrack.FieldType{ValueType: youtrack.StringType}, CanBeEmpty: true},
		{ID: "1-1", Name: "First", LocalizedName: "Первое", Type: youtrack.FieldType{ValueType: youtrack.EnumType, Multi: true}},
		{ID: "1-2", Name: "Second", Type: youtrack.FieldType{ValueType: youtrack.UserType}, CanBeEmpty: true},
		{ID: "1-3", Name: "Third", Type: youtrack.FieldType{ValueType: youtrack.StateType}, CanBeEmpty: true},
	}, fields)
	assert.Equal(t, requestTo(http.MethodGet, server, projectPath+"?fields="+projectFields), lastRequest(t, server))
}

func TestFieldsRefusesAProjectWithNoFields(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, projectJSON()))

	_, err := client(t, server).Fields(t.Context(), "DEV")

	var failed *youtrack.PermissionError
	require.ErrorAs(t, err, &failed)
	assert.Equal(t, youtrack.PermissionError{Request: lastRequest(t, server), Project: "DEV", Permission: "jetbrains.jetpass.project-read"}, *failed)
}

func TestFieldsRefusesAProjectCodeItCannotSend(t *testing.T) {
	t.Parallel()
	for _, code := range []string{"", "1DEV", "DEV-1", "a/b", ".."} {
		t.Run(code, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			_, err := client(t, server).Fields(t.Context(), code)

			assert.Equal(t, youtrack.ArgumentError{Argument: "project", Value: code}, argumentErrorOf(t, err))
		})
	}
}

func TestFieldsRefusesMetadataItCannotRead(t *testing.T) {
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

			_, err := client(t, server).Fields(t.Context(), "DEV")

			assert.Equal(t, invalidAnswer(lastRequest(t, server), tc.metadata), responseErrorOf(t, err))
		})
	}
}

func TestFieldsKeepsATypeItDoesNotModel(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, projectJSON(metaField{id: "1-1", name: "Field", valueType: "quantum"})))

	fields, err := client(t, server).Fields(t.Context(), "DEV")

	require.NoError(t, err)
	require.Len(t, fields, 1)
	assert.Equal(t, youtrack.FieldType{ValueType: "quantum"}, fields[0].Type)
	assert.False(t, fields[0].Type.Known())
}
