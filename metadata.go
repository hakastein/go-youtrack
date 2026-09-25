package youtrack

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
)

type ProjectField struct {
	ID            string
	Name          string
	LocalizedName string
	Type          FieldType
	CanBeEmpty    bool
}

// Metadata is the custom fields of a project; Request is the read that brought them when FromCache is false.
type Metadata struct {
	Fields    []ProjectField
	FromCache bool
	Request   Request
}

// Metadata answers from the metadata cache when it holds the project, else reads the server and stores the
// result; a caller that finds the cache stale reads again with ReadMetadata.
func (c *Client) Metadata(ctx context.Context, project string) (*Metadata, error) {
	return result(c.metadata(ctx, project))
}

// ReadMetadata reads the custom fields of the project from the server and stores them in the metadata cache.
func (c *Client) ReadMetadata(ctx context.Context, project string) (*Metadata, error) {
	return result(c.readMetadata(ctx, project))
}

func (c *Client) metadata(ctx context.Context, project string) (*Metadata, *Error) {
	code, fault := parseProjectCode(project)
	if fault != nil {
		return nil, fault
	}
	if cached, hit := c.cache.load(metadataTarget(code)); hit {
		return &Metadata{Fields: cached, FromCache: true}, nil
	}
	return c.readMetadata(ctx, code)
}

func (c *Client) readMetadata(ctx context.Context, project string) (*Metadata, *Error) {
	code, fault := parseProjectCode(project)
	if fault != nil {
		return nil, fault
	}
	decoded, fault := c.request(ctx, loadSchemas(), projectSchema, projectFields(), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetProject(ctx, code, fields)
	})
	if fault != nil {
		return nil, fault
	}
	fields, fault := readProjectFields(decoded, code)
	if fault != nil {
		return nil, fault
	}
	c.cache.store(metadataTarget(code), fields)
	return &Metadata{Fields: fields, Request: requestOf(decoded.httpResponse)}, nil
}

const canBeEmptyKey = "canBeEmpty"

func fieldNaming() requestedField {
	return requestedField{name: "field", children: []requestedField{
		{name: nameKey},
		{name: localizedNameKey},
		{name: fieldTypeKey, children: []requestedField{{name: valueTypeKey}, {name: "isMultiValue"}}},
	}}
}

func projectFields() []requestedField {
	return []requestedField{
		{name: idKey},
		{name: "shortName"},
		{name: customFieldsKey, children: []requestedField{{name: idKey}, {name: ordinalKey}, {name: canBeEmptyKey}, fieldNaming()}},
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
		return nil, shapeFailure(a.httpResponse, a.body, "the custom fields of the project are not a JSON array")
	}
	placed := make([]placedField, 0, len(items))
	for _, item := range items {
		object, isObject := item.(map[string]any)
		if !isObject {
			return nil, shapeFailure(a.httpResponse, a.body, brokenField)
		}
		bindingID, isText := object[idKey].(string)
		ordinal, isWhole := parseInt64(object[ordinalKey])
		canBeEmpty, isFlag := object[canBeEmptyKey].(bool)
		if !isText || !isWhole || !isFlag {
			return nil, shapeFailure(a.httpResponse, a.body, brokenPlacement)
		}
		named, ok := readFieldInfo(object)
		if !ok {
			return nil, shapeFailure(a.httpResponse, a.body, brokenFieldInfo)
		}
		placed = append(placed, placedField{ordinal: ordinal, field: ProjectField{
			ID: bindingID, Name: named.name, LocalizedName: named.localizedName, Type: named.kind, CanBeEmpty: canBeEmpty,
		}})
	}
	if len(placed) == 0 {
		return nil, noFields(requestDetail(a.httpResponse.Request.Method, a.httpResponse.Request.URL.Redacted()), code)
	}
	slices.SortStableFunc(placed, func(x, y placedField) int { return cmp.Compare(x.ordinal, y.ordinal) })
	fields := make([]ProjectField, 0, len(placed))
	for _, p := range placed {
		fields = append(fields, p.field)
	}
	return fields, nil
}

// The server resolves a name as matchFields does: by name, then by the translation, without regard to letter case.
func matchFields(name string, fields []ProjectField) []int {
	var byName, byTranslation []int
	for at, f := range fields {
		switch {
		case strings.EqualFold(name, f.Name):
			byName = append(byName, at)
		case f.LocalizedName != "" && strings.EqualFold(name, f.LocalizedName):
			byTranslation = append(byTranslation, at)
		}
	}
	if len(byName) > 0 {
		return byName
	}
	return byTranslation
}

func unmodelled(t FieldType) string {
	return fmt.Sprintf("valueType %s with isMultiValue %t is not one of the twenty custom-field types the module models",
		quote(string(t.ValueType)), t.Multi)
}
