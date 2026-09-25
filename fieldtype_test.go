package youtrack_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/youtrack"
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
