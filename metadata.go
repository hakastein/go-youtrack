package youtrack

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"slices"
)

// ID is the id of the binding to the project, not of the field.
type ProjectField struct {
	ID            string
	Name          string
	LocalizedName string
	Type          FieldType
	CanBeEmpty    bool
}

// Fields are in the project's order.
type Metadata struct {
	Fields    []ProjectField
	FromCache bool
}

// Metadata answers from the metadata cache when it holds the project, else reads the server and stores the
// result; a caller that finds the cache stale reads again with ReadMetadata.
func (s *FieldsService) Metadata(ctx context.Context, project string) (*Metadata, error) {
	metadata, _, fault := s.metadata(ctx, project)
	return result(metadata, fault)
}

// ReadMetadata also stores the result in the metadata cache.
func (s *FieldsService) ReadMetadata(ctx context.Context, project string) (*Metadata, error) {
	metadata, _, fault := s.readMetadata(ctx, project)
	return result(metadata, fault)
}

// sent is the read that brought the metadata, and nothing for metadata from the cache.
func (s *FieldsService) metadata(ctx context.Context, project string) (*Metadata, Pair, *Error) {
	code, fault := parseProjectCode(project)
	if fault != nil {
		return nil, Pair{}, fault
	}
	if cached, hit := s.client.cache.load(metadataTarget(code)); hit {
		return &Metadata{Fields: cached, FromCache: true}, Pair{}, nil
	}
	return s.readMetadata(ctx, code)
}

func (s *FieldsService) readMetadata(ctx context.Context, project string) (*Metadata, Pair, *Error) {
	code, fault := parseProjectCode(project)
	if fault != nil {
		return nil, Pair{}, fault
	}
	c := s.client
	decoded, fault := c.request(ctx, projectSchema, projectFields(), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetProject(ctx, code, fields)
	})
	if fault != nil {
		return nil, Pair{}, fault
	}
	fields, fault := readProjectFields(decoded, code)
	if fault != nil {
		return nil, Pair{}, fault
	}
	c.cache.store(metadataTarget(code), fields)
	return &Metadata{Fields: fields}, decoded.sent(), nil
}

const canBeEmptyKey = "canBeEmpty"

func projectFields() []requestedField {
	return []requestedField{
		{name: idKey},
		{name: "shortName"},
		{name: customFieldsKey, children: []requestedField{{name: idKey}, {name: ordinalKey}, {name: canBeEmptyKey}, fieldInfoFields()}},
	}
}

func metadataTarget(code string) string {
	return "/api/admin/projects/" + code + "?fields=" + formatFields(projectFields())
}

const brokenPlacement = "the id, the ordinal or the emptiness of a custom field is not of the shape the specification gives it"

type placedField struct {
	field   ProjectField
	ordinal int64
}

// The server sends the fields of a project in the order of their binding ids; the project's own order is ordinal.
func readProjectFields(a decodedResponse, code string) ([]ProjectField, *Error) {
	items, isList := a.objects[0][customFieldsKey].([]any)
	if !isList {
		return nil, a.invalid("the custom fields of the project are not a JSON array")
	}
	placed := make([]placedField, 0, len(items))
	for _, item := range items {
		object, isObject := item.(map[string]any)
		if !isObject {
			return nil, a.invalid(brokenField)
		}
		bindingID, isText := object[idKey].(string)
		ordinal, isWhole := parseInt64(object[ordinalKey])
		canBeEmpty, isFlag := object[canBeEmptyKey].(bool)
		if !isText || !isWhole || !isFlag {
			return nil, a.invalid(brokenPlacement)
		}
		named, ok := readFieldInfo(object)
		if !ok {
			return nil, a.invalid(brokenFieldInfo)
		}
		placed = append(placed, placedField{ordinal: ordinal, field: ProjectField{
			ID: bindingID, Name: named.name, LocalizedName: named.localizedName, Type: named.kind, CanBeEmpty: canBeEmpty,
		}})
	}
	if len(placed) == 0 {
		return nil, noFields(a, code)
	}
	slices.SortStableFunc(placed, func(x, y placedField) int { return cmp.Compare(x.ordinal, y.ordinal) })
	fields := make([]ProjectField, 0, len(placed))
	for _, p := range placed {
		fields = append(fields, p.field)
	}
	return fields, nil
}

func unmodelled(t FieldType) string {
	return fmt.Sprintf("valueType %s with isMultiValue %t is not one of the twenty custom-field types the module models",
		quote(string(t.ValueType)), t.Multi)
}
