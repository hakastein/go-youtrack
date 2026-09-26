package youtrack_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/go-youtrack"
)

func TestFieldTypeTellsTheClassTheKeyAndTheBundleOfEachTypeItModels(t *testing.T) {
	t.Parallel()
	tests := []struct {
		fieldType youtrack.FieldType
		class     string
		key       string
		bundle    bool
	}{
		{fieldType: youtrack.FieldType{ValueType: youtrack.EnumType}, class: "SingleEnumIssueCustomField", key: "name", bundle: true},
		{fieldType: youtrack.FieldType{ValueType: youtrack.EnumType, Multi: true}, class: "MultiEnumIssueCustomField", key: "name", bundle: true},
		{fieldType: youtrack.FieldType{ValueType: youtrack.StateType}, class: "StateIssueCustomField", key: "name", bundle: true},
		{fieldType: youtrack.FieldType{ValueType: youtrack.VersionType}, class: "SingleVersionIssueCustomField", key: "name", bundle: true},
		{fieldType: youtrack.FieldType{ValueType: youtrack.VersionType, Multi: true}, class: "MultiVersionIssueCustomField", key: "name", bundle: true},
		{fieldType: youtrack.FieldType{ValueType: youtrack.BuildType}, class: "SingleBuildIssueCustomField", key: "name", bundle: true},
		{fieldType: youtrack.FieldType{ValueType: youtrack.BuildType, Multi: true}, class: "MultiBuildIssueCustomField", key: "name", bundle: true},
		{fieldType: youtrack.FieldType{ValueType: youtrack.OwnedFieldType}, class: "SingleOwnedIssueCustomField", key: "name", bundle: true},
		{fieldType: youtrack.FieldType{ValueType: youtrack.OwnedFieldType, Multi: true}, class: "MultiOwnedIssueCustomField", key: "name", bundle: true},
		{fieldType: youtrack.FieldType{ValueType: youtrack.UserType}, class: "SingleUserIssueCustomField", key: "login"},
		{fieldType: youtrack.FieldType{ValueType: youtrack.UserType, Multi: true}, class: "MultiUserIssueCustomField", key: "login"},
		{fieldType: youtrack.FieldType{ValueType: youtrack.GroupType}, class: "SingleGroupIssueCustomField", key: "name"},
		{fieldType: youtrack.FieldType{ValueType: youtrack.GroupType, Multi: true}, class: "MultiGroupIssueCustomField", key: "name"},
		{fieldType: youtrack.FieldType{ValueType: youtrack.PeriodType}, class: "PeriodIssueCustomField", key: "minutes"},
		{fieldType: youtrack.FieldType{ValueType: youtrack.TextType}, class: "TextIssueCustomField", key: "text"},
		{fieldType: youtrack.FieldType{ValueType: youtrack.DateType}, class: "DateIssueCustomField"},
		{fieldType: youtrack.FieldType{ValueType: youtrack.DateTimeType}, class: "SimpleIssueCustomField"},
		{fieldType: youtrack.FieldType{ValueType: youtrack.IntegerType}, class: "SimpleIssueCustomField"},
		{fieldType: youtrack.FieldType{ValueType: youtrack.FloatType}, class: "SimpleIssueCustomField"},
		{fieldType: youtrack.FieldType{ValueType: youtrack.StringType}, class: "SimpleIssueCustomField"},
	}
	for _, tc := range tests {
		t.Run(tc.fieldType.String(), func(t *testing.T) {
			t.Parallel()
			assert.True(t, tc.fieldType.Known())
			assert.Equal(t, tc.class, tc.fieldType.Class())
			assert.Equal(t, tc.key, tc.fieldType.ValueKey())
			assert.Equal(t, tc.bundle, tc.fieldType.HasBundle())
		})
	}
}

func TestFieldTypeKnowsNoPairOutsideTheTable(t *testing.T) {
	t.Parallel()
	for _, fieldType := range []youtrack.FieldType{
		{ValueType: youtrack.StateType, Multi: true},
		{ValueType: youtrack.PeriodType, Multi: true},
		{ValueType: youtrack.StringType, Multi: true},
		{ValueType: "quantum"},
	} {
		t.Run(fieldType.String(), func(t *testing.T) {
			t.Parallel()
			assert.False(t, fieldType.Known())
			assert.Equal(t, "", fieldType.Class())
			assert.False(t, fieldType.HasBundle())
		})
	}
}

func TestFieldTypeEncodesAValueKeyAsTheBodyOfItsType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		fieldType youtrack.FieldType
		text      string
		body      string
		key       string
	}{
		{name: "a name", fieldType: fieldType("enum", true), text: "First", body: `{"name":"First"}`, key: "First"},
		{name: "a login", fieldType: fieldType("user", false), text: "first", body: `{"login":"first"}`, key: "first"},
		{name: "a period", fieldType: fieldType("period", false), text: "PT90M", body: `{"minutes":90}`, key: "PT1H30M"},
		{name: "a text", fieldType: fieldType("text", false), text: "a\nb", body: `{"text":"a\nb"}`, key: "a\nb"},
		{name: "a day", fieldType: fieldType("date", false), text: "2026-09-16", body: `1789560000000`, key: "2026-09-16"},
		{name: "a moment", fieldType: fieldType("date and time", false), text: "2026-08-31T03:00:00.123+03:00", body: `1788134400123`, key: "2026-08-31T00:00:00.123Z"},
		{name: "a whole number", fieldType: fieldType("integer", false), text: "007", body: `7`, key: "7"},
		{name: "a number", fieldType: fieldType("float", false), text: "1e3", body: `1000`, key: "1000"},
		{name: "a string", fieldType: fieldType("string", false), text: "First", body: `"First"`, key: "First"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			encoded, err := tc.fieldType.Encode(tc.text)

			require.NoError(t, err)
			body, err := json.Marshal(encoded.Body)
			require.NoError(t, err)
			assert.JSONEq(t, tc.body, string(body))
			assert.Equal(t, tc.key, encoded.Key)
		})
	}
}

func TestFieldTypeRefusesToEncodeAValueItsTypeCannotHold(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		fieldType youtrack.FieldType
		text      string
	}{
		{name: "an empty name", fieldType: fieldType("enum", false), text: ""},
		{name: "a period of days", fieldType: fieldType("period", false), text: "P1D"},
		{name: "a string with a space around it", fieldType: fieldType("string", false), text: " a"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := tc.fieldType.Encode(tc.text)

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

func TestFieldTypeRefusesToEncodeForATypeItDoesNotModel(t *testing.T) {
	t.Parallel()

	_, err := fieldType("quantum", false).Encode("a")

	assert.Equal(t, youtrack.Error{Code: youtrack.CodeUpstreamInvalid}, errorOf(t, err))
}

func TestFieldTypeReadsOneValueByTheKeyOfItsType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		fieldType youtrack.FieldType
		item      string
		value     youtrack.Value
		present   bool
	}{
		{name: "a bundle element", fieldType: fieldType("state", false), item: `{"id":"3-1","name":"In Progress","localizedName":"В работе"}`,
			value: youtrack.Value{ID: "3-1", Text: "In Progress", LocalizedName: "В работе"}, present: true},
		{name: "a user", fieldType: fieldType("user", true), item: `{"id":"1-5","login":"first","fullName":"F"}`,
			value: youtrack.Value{ID: "1-5", Text: "first"}, present: true},
		{name: "a period", fieldType: fieldType("period", false), item: `{"id":"PT1H","minutes":60}`, value: youtrack.Value{Text: "PT1H"}, present: true},
		{name: "a text that is null", fieldType: fieldType("text", false), item: `{"text":null}`},
		{name: "a float", fieldType: fieldType("float", false), item: `1.50`, value: youtrack.Value{Text: "1.5"}, present: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var item any
			decoder := json.NewDecoder(strings.NewReader(tc.item))
			decoder.UseNumber()
			require.NoError(t, decoder.Decode(&item))

			value, present, err := tc.fieldType.ReadValue(item)

			require.NoError(t, err)
			assert.Equal(t, tc.present, present)
			assert.Equal(t, tc.value, value)
		})
	}
}

func TestFieldTypeRefusesToReadAValueOfAnotherShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		fieldType youtrack.FieldType
		item      any
	}{
		{name: "a name that is no object", fieldType: fieldType("enum", false), item: "First"},
		{name: "an object without the member", fieldType: fieldType("user", false), item: map[string]any{"name": "x"}},
		{name: "minutes that are text", fieldType: fieldType("period", false), item: map[string]any{"minutes": "90"}},
		{name: "a string that is a number", fieldType: fieldType("string", false), item: json.Number("5")},
		{name: "a type the module does not model", fieldType: fieldType("quantum", false), item: "x"},
		{name: "an id that is a number", fieldType: fieldType("enum", false), item: map[string]any{"id": json.Number("5"), "name": "First"}},
		{name: "a translation that is a number", fieldType: fieldType("state", false),
			item: map[string]any{"id": "3-1", "name": "First", "localizedName": json.Number("5")}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, _, err := tc.fieldType.ReadValue(tc.item)

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeUpstreamInvalid}, errorOf(t, err))
		})
	}
}

func TestFieldTypeComparesNamedValuesWithoutRegardToCase(t *testing.T) {
	t.Parallel()
	tests := []struct {
		fieldType     youtrack.FieldType
		written, held string
		named, same   bool
	}{
		{fieldType: fieldType("enum", false), written: "first", held: "First", named: true, same: true},
		{fieldType: fieldType("user", true), written: "first", held: "First", named: true, same: true},
		{fieldType: fieldType("string", false), written: "first", held: "First"},
		{fieldType: fieldType("text", false), written: "first", held: "First"},
		{fieldType: fieldType("period", false), written: "PT1H", held: "PT1H", same: true},
	}
	for _, tc := range tests {
		t.Run(tc.fieldType.String(), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.named, tc.fieldType.Named())
			assert.Equal(t, tc.same, tc.fieldType.Same(tc.written, tc.held))
		})
	}
}

func TestFieldTypeNamesTheBundleFieldsOfItsType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		fieldType youtrack.FieldType
		fields    string
	}{
		{fieldType: fieldType("enum", true), fields: "bundle(values(name,archived))"},
		{fieldType: fieldType("ownedField", false), fields: "bundle(values(name,archived))"},
		{fieldType: fieldType("user", false), fields: "bundle(aggregatedUsers(login))"},
		{fieldType: fieldType("group", false)},
		{fieldType: fieldType("period", false)},
	}
	for _, tc := range tests {
		t.Run(tc.fieldType.String(), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.fields, tc.fieldType.BundleFields())
		})
	}
}

func TestValueKeysAreTheMembersValuesOfEveryTypeAreNamedBy(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"name", "login", "minutes", "text"}, youtrack.ValueKeys())
}

func fieldType(valueType string, multi bool) youtrack.FieldType {
	return youtrack.FieldType{ValueType: youtrack.ValueType(valueType), Multi: multi}
}
