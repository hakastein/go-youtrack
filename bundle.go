package youtrack

import (
	"context"
	"fmt"
)

// Field.CanBeEmpty comes from the read of the field, not from the metadata.
type Bundle struct {
	Field  ProjectField
	Values []BundleValue
}

type BundleValue struct {
	ID       string
	Name     string
	Archived bool
}

// Bundle finds the field as Show does. Only a field whose type HasBundle has one, and the type is taken from the
// server before a field is refused for it.
func (s *FieldsService) Bundle(ctx context.Context, project, field string) (*Bundle, error) {
	return result(s.bundle(ctx, project, field))
}

func (s *FieldsService) bundle(ctx context.Context, project, name string) (*Bundle, *Error) {
	code, fault := parseProjectCode(project)
	if fault != nil {
		return nil, fault
	}
	if fault := checkFieldName(name); fault != nil {
		return nil, fault
	}
	read, fault := s.readField(ctx, code, name, bundleFields)
	if fault != nil {
		return nil, fault
	}
	a, field := read.answer, read.field
	canBeEmpty, isFlag := a.objects[0][canBeEmptyKey].(bool)
	if !isFlag {
		return nil, a.invalid(brokenPlacement)
	}
	field.CanBeEmpty = canBeEmpty
	if !field.Type.HasBundle() {
		message := fmt.Sprintf("the custom field %s is a %s field, and a %s field holds no bundle of values",
			quote(field.Name), field.Type, field.Type.ValueType)
		return nil, &Error{Code: CodeBadUsage, Message: message}
	}
	values, fault := readBundleValues(a)
	if fault != nil {
		return nil, fault
	}
	return &Bundle{Field: field, Values: values}, nil
}

func bundleFields(found ProjectField) ([]requestedField, bool, *Error) {
	if !found.Type.Known() {
		return nil, false, nil
	}
	asked := []requestedField{fieldInfoFields(), {name: canBeEmptyKey}}
	if found.Type.HasBundle() {
		values := requestedField{name: "values", children: []requestedField{{name: idKey}, {name: nameKey}, {name: "archived"}}}
		asked = append(asked, requestedField{name: "bundle", children: []requestedField{values}})
	}
	return asked, true, nil
}

func readBundleValues(a decodedResponse) ([]BundleValue, *Error) {
	bundle, isObject := a.objects[0]["bundle"].(map[string]any)
	if !isObject {
		return nil, a.invalid("the bundle of the custom field is not a JSON object")
	}
	items, isList := bundle["values"].([]any)
	if !isList {
		return nil, a.invalid("the values of the bundle are not a JSON array")
	}
	values := make([]BundleValue, 0, len(items))
	for _, item := range items {
		object, isObject := item.(map[string]any)
		if !isObject {
			return nil, a.invalid("a value of the bundle is not a JSON object")
		}
		id, isID := object[idKey].(string)
		name, isName := object[nameKey].(string)
		archived, isFlag := object["archived"].(bool)
		if !isID || !isName || !isFlag {
			return nil, a.invalid("a value of the bundle is not of the shape the specification gives it")
		}
		values = append(values, BundleValue{ID: id, Name: name, Archived: archived})
	}
	return values, nil
}
