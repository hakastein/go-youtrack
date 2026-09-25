package youtrack

import (
	"cmp"
	"context"
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
	return result(metadataOf(ctx, project, s.metadata))
}

// ReadMetadata also stores the result in the metadata cache.
func (s *FieldsService) ReadMetadata(ctx context.Context, project string) (*Metadata, error) {
	return result(metadataOf(ctx, project, s.readMetadata))
}

func metadataOf(ctx context.Context, project string, read func(ctx context.Context, code string) (*Metadata, Pair, *Error)) (*Metadata, *Error) {
	code, fault := parseProjectCode(project)
	if fault != nil {
		return nil, fault
	}
	metadata, _, fault := read(ctx, code)
	return metadata, fault
}

// sent is the read that brought the metadata, and nothing for metadata from the cache.
func (s *FieldsService) metadata(ctx context.Context, code string) (*Metadata, Pair, *Error) {
	if cached, hit := s.client.cache.load(metadataTarget(code)); hit {
		return &Metadata{Fields: cached, FromCache: true}, Pair{}, nil
	}
	return s.readMetadata(ctx, code)
}

func (s *FieldsService) readMetadata(ctx context.Context, code string) (*Metadata, Pair, *Error) {
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

func projectFields() []requestedField {
	return []requestedField{
		{name: customFieldsKey, children: []requestedField{{name: idKey}, {name: ordinalKey}, {name: canBeEmptyKey}, fieldInfoFields()}},
	}
}

func metadataTarget(code string) string {
	return "/api/admin/projects/" + code + "?fields=" + formatFields(projectFields())
}

const (
	brokenField     = "a custom field of the project is not a JSON object"
	brokenFieldInfo = "the name or the type of a custom field is not of the shape the specification gives it"
	brokenPlacement = "the id, the ordinal or the emptiness of a custom field is not of the shape the specification gives it"
)

func readProjectField(item any) (ProjectField, string) {
	object, isObject := item.(map[string]any)
	if !isObject {
		return ProjectField{}, brokenField
	}
	id, isText := object[idKey].(string)
	canBeEmpty, isFlag := object[canBeEmptyKey].(bool)
	if !isText || !isFlag {
		return ProjectField{}, brokenPlacement
	}
	named, ok := readFieldInfo(object)
	if !ok {
		return ProjectField{}, brokenFieldInfo
	}
	return ProjectField{ID: id, Name: named.name, LocalizedName: named.localizedName, Type: named.kind, CanBeEmpty: canBeEmpty}, ""
}

func (f ProjectField) info() fieldInfo {
	return fieldInfo{name: f.Name, localizedName: f.LocalizedName, kind: f.Type}
}

type placed[T any] struct {
	ordinal int64
	item    T
}

func inOrdinalOrder[T any](fields []placed[T]) []T {
	slices.SortStableFunc(fields, func(a, b placed[T]) int { return cmp.Compare(a.ordinal, b.ordinal) })
	ordered := make([]T, 0, len(fields))
	for _, field := range fields {
		ordered = append(ordered, field.item)
	}
	return ordered
}

// The server sends the fields of a project in the order of their binding ids; the project's own order is ordinal.
func readProjectFields(a decodedResponse, code string) ([]ProjectField, *Error) {
	items, isList := a.objects[0][customFieldsKey].([]any)
	if !isList {
		return nil, a.invalid("the custom fields of the project are not a JSON array")
	}
	fields := make([]placed[ProjectField], 0, len(items))
	for _, item := range items {
		field, reason := readProjectField(item)
		if reason != "" {
			return nil, a.invalid(reason)
		}
		ordinal, isWhole := parseInt64(memberOf(item, ordinalKey))
		if !isWhole {
			return nil, a.invalid(brokenPlacement)
		}
		fields = append(fields, placed[ProjectField]{ordinal: ordinal, item: field})
	}
	if len(fields) == 0 {
		return nil, noFields(a, code)
	}
	return inOrdinalOrder(fields), nil
}
