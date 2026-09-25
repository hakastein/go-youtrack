package youtrack

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
)

// ProjectField is a custom field as it stands on a project: the id of the binding, the names the field
// answers to, its type and whether the project lets an issue leave it empty.
type ProjectField struct {
	ID            string
	Name          string
	LocalizedName string
	Type          FieldType
	CanBeEmpty    bool
}

const (
	projectRead   = "jetbrains.jetpass.project-read"
	canBeEmptyKey = "canBeEmpty"
)

// Metadata is the custom fields of a project in the project's order and where they came from: FromCache when the
// client's metadata cache held them, else the server, and then Request is the read that brought them.
type Metadata struct {
	Fields    []ProjectField
	FromCache bool
	Request   Request
}

// Metadata answers from the metadata cache when the client has one and it holds the project, else reads the
// project from the server and stores it. The cache confirms and never refuses: a caller that finds the cached
// fields disagreeing with the server reads again with ReadMetadata.
func (c *Client) Metadata(ctx context.Context, project string) (*Metadata, error) {
	code, err := parseProjectCode(project)
	if err != nil {
		return nil, err
	}
	if cached, hit := c.cache.load(metadataTarget(code)); hit {
		return &Metadata{Fields: cached, FromCache: true}, nil
	}
	return c.ReadMetadata(ctx, project)
}

// ReadMetadata reads the custom fields of the project from the server and stores them in the metadata cache.
func (c *Client) ReadMetadata(ctx context.Context, project string) (*Metadata, error) {
	code, err := parseProjectCode(project)
	if err != nil {
		return nil, err
	}
	metadata, a, err := c.readProjectMetadata(ctx, code)
	if err != nil {
		return nil, err
	}
	return &Metadata{Fields: metadata.fields, Request: a.request}, nil
}

type projectMetadata struct {
	id     string
	code   string
	fields []ProjectField
}

func fieldNaming() field {
	return field{name: "field", children: []field{
		{name: nameKey},
		{name: localizedNameKey},
		{name: "fieldType", children: []field{{name: "valueType"}, {name: "isMultiValue"}}},
	}}
}

func projectFields() []field {
	return []field{
		{name: idKey},
		{name: "shortName"},
		{name: "customFields", children: []field{{name: idKey}, {name: "ordinal"}, {name: canBeEmptyKey}, fieldNaming()}},
	}
}

func metadataTarget(code string) string {
	return "/api/admin/projects/" + code + "?fields=" + formatFields(projectFields())
}

func (c *Client) readProjectMetadata(ctx context.Context, code string) (projectMetadata, answer, error) {
	a, err := c.read(ctx, func(ctx context.Context) (*http.Response, error) {
		return c.apiGetProject(ctx, code, formatFields(projectFields()))
	})
	if err != nil {
		return projectMetadata{}, answer{}, err
	}
	object, err := a.object()
	if err != nil {
		return projectMetadata{}, answer{}, err
	}
	metadata, err := readProjectMetadata(a, object)
	if err != nil {
		return projectMetadata{}, answer{}, err
	}
	c.cache.store(metadataTarget(code), metadata.fields)
	return metadata, a, nil
}

const (
	brokenProject   = "the id or the short name of the project is not text"
	brokenFields    = "the custom fields of the project are not a JSON array"
	brokenField     = "a custom field of the project is not a JSON object"
	brokenBinding   = "the id, the ordinal or the emptiness of a custom field is not of the shape the specification gives it"
	brokenFieldInfo = "the name or the type of a custom field is not of the shape the specification gives it"
)

type placedField struct {
	field   ProjectField
	ordinal int64
}

// The server sends the fields of a project in the order of their binding ids; the project's own order is ordinal.
func readProjectMetadata(a answer, project map[string]any) (projectMetadata, error) {
	id, isText := project[idKey].(string)
	code, isCode := project["shortName"].(string)
	if !isText || !isCode {
		return projectMetadata{}, a.invalid(brokenProject)
	}
	items, isList := project["customFields"].([]any)
	if !isList {
		return projectMetadata{}, a.invalid(brokenFields)
	}
	placed := make([]placedField, 0, len(items))
	for _, item := range items {
		object, isObject := item.(map[string]any)
		if !isObject {
			return projectMetadata{}, a.invalid(brokenField)
		}
		bindingID, isText := object[idKey].(string)
		ordinal, isWhole := parseInt64(object["ordinal"])
		canBeEmpty, isFlag := object[canBeEmptyKey].(bool)
		if !isText || !isWhole || !isFlag {
			return projectMetadata{}, a.invalid(brokenBinding)
		}
		naming, ok := readFieldNaming(object["field"])
		if !ok {
			return projectMetadata{}, a.invalid(brokenFieldInfo)
		}
		placed = append(placed, placedField{ordinal: ordinal, field: ProjectField{
			ID: bindingID, Name: naming.name, LocalizedName: naming.localizedName, Type: naming.fieldType, CanBeEmpty: canBeEmpty,
		}})
	}
	if len(placed) == 0 {
		return projectMetadata{}, &PermissionError{Request: a.request, Project: code, Permission: projectRead}
	}
	slices.SortStableFunc(placed, func(x, y placedField) int { return cmp.Compare(x.ordinal, y.ordinal) })
	fields := make([]ProjectField, 0, len(placed))
	for _, p := range placed {
		fields = append(fields, p.field)
	}
	return projectMetadata{id: id, code: code, fields: fields}, nil
}

type fieldNamingInfo struct {
	name          string
	localizedName string
	fieldType     FieldType
}

func namingOf(f ProjectField) fieldNamingInfo {
	return fieldNamingInfo{name: f.Name, localizedName: f.LocalizedName, fieldType: f.Type}
}

func readFieldNaming(value any) (fieldNamingInfo, bool) {
	object, isObject := value.(map[string]any)
	if !isObject {
		return fieldNamingInfo{}, false
	}
	name, isText := object[nameKey].(string)
	if !isText {
		return fieldNamingInfo{}, false
	}
	fieldType, ok := readFieldType(object["fieldType"])
	if !ok {
		return fieldNamingInfo{}, false
	}
	localized, ok := readLocalizedName(object[localizedNameKey])
	if !ok {
		return fieldNamingInfo{}, false
	}
	return fieldNamingInfo{name: name, localizedName: localized, fieldType: fieldType}, true
}

func readFieldType(value any) (FieldType, bool) {
	object, isObject := value.(map[string]any)
	if !isObject {
		return FieldType{}, false
	}
	valueType, isText := object["valueType"].(string)
	isMultiValue, isFlag := object["isMultiValue"].(bool)
	if !isText || !isFlag {
		return FieldType{}, false
	}
	return FieldType{ValueType: ValueType(valueType), Multi: isMultiValue}, true
}

func readLocalizedName(value any) (string, bool) {
	switch name := value.(type) {
	case string:
		return name, true
	case nil:
		return "", true
	}
	return "", false
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

func fieldNames(fields []ProjectField, at []int) []string {
	names := make([]string, 0, len(at))
	for _, i := range at {
		names = append(names, fields[i].Name)
	}
	slices.Sort(names)
	return names
}

func allFieldNames(fields []ProjectField) []string {
	at := make([]int, 0, len(fields))
	for i := range fields {
		at = append(at, i)
	}
	return fieldNames(fields, at)
}

func unmodelled(t FieldType) string {
	return fmt.Sprintf("valueType %s with isMultiValue %t is not one of the twenty custom-field types the module models",
		quote(string(t.ValueType)), t.Multi)
}
