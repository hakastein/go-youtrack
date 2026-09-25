package youtrack

import (
	"cmp"
	"context"
	"encoding/json"
	"net/http"
	"slices"
)

const FieldListFields = "field(name,localizedName,fieldType(valueType,isMultiValue)),canBeEmpty"

const projectRead = "jetbrains.jetpass.project-read"

const ordinalKey = "ordinal"

func ListFields(code, expression string) (Call, *Error) {
	code, fault := parseProjectCode(code)
	if fault != nil {
		return nil, fault
	}
	requested, fault := parseFields(expression, FieldListFields)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	return func(ctx context.Context, c *Client) (*Node, *Error) {
		return c.listFields(ctx, spec, code, requested)
	}, nil
}

func ShowField(code, name string, expression string) (Call, *Error) {
	code, fault := parseProjectCode(code)
	if fault != nil {
		return nil, fault
	}
	if name == "" {
		return nil, &Error{Code: CodeBadUsage, Message: "the name of a custom field is empty"}
	}
	if fault := checkFieldsSyntax(expression); fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	return func(ctx context.Context, c *Client) (*Node, *Error) {
		return c.showField(ctx, spec, code, name, expression)
	}, nil
}

func checkFieldsSyntax(expression string) *Error {
	if expression == "" {
		return nil
	}
	_, fault := parseFields(expression, FieldListFields)
	return fault
}

func (c *Client) showField(ctx context.Context, spec *schemas, code, name string, expression string) (*Node, *Error) {
	metadata, fault := c.metadata(ctx, code)
	if fault != nil {
		return nil, fault
	}
	node, fault, cacheStale := c.showFieldFrom(ctx, spec, code, name, expression, metadata)
	if !cacheStale {
		return node, fault
	}
	if metadata, fault = c.readMetadata(ctx, code); fault != nil {
		return nil, fault
	}
	node, fault, _ = c.showFieldFrom(ctx, spec, code, name, expression, metadata)
	return node, fault
}

func (c *Client) showFieldFrom(ctx context.Context, spec *schemas, code, name string, expression string, metadata *Metadata) (*Node, *Error, bool) {
	fields := projectFieldsOf(metadata)
	found, ok := lookUp(name, fields)
	if !ok {
		return refuseUnlessCached(metadata, unresolved(metadata.Request, code, name, fields))
	}
	if !found.hasValidID() {
		return refuseUnlessCached(metadata, metadataInvalid(metadata, invalidFieldID(found.id)))
	}
	requested, modelled, fault := fieldsToPrint(expression, found.info)
	if fault != nil {
		return nil, fault, false
	}
	if !modelled {
		return refuseUnlessCached(metadata, metadataInvalid(metadata, unmodelledType(found.info)))
	}
	decoded, fault := c.getField(ctx, spec, code, found.id, requested)
	if fault != nil {
		return nil, fault, metadata.FromCache && isStale(fault)
	}
	if fault := found.info.verifyUnchanged(decoded, code); fault != nil {
		return nil, fault, metadata.FromCache
	}
	node, fault := objectNode(decoded, requested, decoded.objects[0], nil)
	return node, fault, false
}

func refuseUnlessCached(metadata *Metadata, fault *Error) (*Node, *Error, bool) {
	if metadata.FromCache {
		return nil, nil, true
	}
	return nil, fault, false
}

func projectFieldsOf(metadata *Metadata) []customField {
	fields := make([]customField, 0, len(metadata.Fields))
	for _, field := range metadata.Fields {
		named := fieldInfo{name: field.Name, localizedName: field.LocalizedName, kind: field.Type}
		fields = append(fields, customField{id: field.ID, info: named})
	}
	return fields
}

func metadataInvalid(metadata *Metadata, message string) *Error {
	return &Error{Code: CodeUpstreamInvalid, Message: message, Details: []Pair{sentRequest(metadata.Request)}}
}

func isStale(fault *Error) bool {
	return fault.Code == CodeNotFound || fault.Code == CodeUpstreamInvalid
}

func (c *Client) getField(ctx context.Context, spec *schemas, code, id string, requested []requestedField) (decodedResponse, *Error) {
	asked := withFields(requested, fieldInfoFields())
	return c.request(ctx, spec, "ProjectCustomField", asked, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetProjectCustomField(ctx, code, id, fields)
	})
}

func (c *Client) listFields(ctx context.Context, spec *schemas, code string, requested []requestedField) (*Node, *Error) {
	asked := withFields(requested, requestedField{name: ordinalKey})
	decoded, fault := c.request(ctx, spec, "[]ProjectCustomField", asked, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetProjectCustomFields(ctx, code, fields, topAll)
	})
	if fault != nil {
		return nil, fault
	}
	if len(decoded.objects) == 0 {
		sent := requestDetail(decoded.httpResponse.Request.Method, decoded.httpResponse.Request.URL.Redacted())
		return nil, noFields(sent, code)
	}
	ordered, fault := sortedByOrdinal(decoded)
	if fault != nil {
		return nil, fault
	}
	records, fault := newConverter(decoded, blockLayout).objectsAt(decoded.schema, requested, ordered)
	if fault != nil {
		return nil, fault
	}
	return countedListDocument("fields", counted(len(ordered)), records), nil
}

type orderedField struct {
	position int64
	field    map[string]any
}

func sortedByOrdinal(decoded decodedResponse) ([]map[string]any, *Error) {
	placed := make([]orderedField, 0, len(decoded.objects))
	for _, field := range decoded.objects {
		number, isNumber := field[ordinalKey].(json.Number)
		if !isNumber {
			return nil, shapeFailure(decoded.httpResponse, decoded.body, "the ordinal of a custom field is not a number")
		}
		position, err := number.Int64()
		if err != nil {
			return nil, shapeFailure(decoded.httpResponse, decoded.body, "the ordinal of a custom field is not a whole number")
		}
		placed = append(placed, orderedField{position: position, field: field})
	}
	slices.SortStableFunc(placed, func(a, b orderedField) int { return cmp.Compare(a.position, b.position) })
	ordered := make([]map[string]any, 0, len(placed))
	for _, p := range placed {
		ordered = append(ordered, p.field)
	}
	return ordered, nil
}

func noFields(sent Pair, code string) *Error {
	details := []Pair{
		sent,
		{Key: "project", Value: NewString(code)},
		{Key: "permission", Value: NewString(projectRead)},
	}
	message := "not one custom field of the project arrived, and a token without the right under permission is sent an empty list"
	return &Error{Code: CodeDenied, Message: message, Details: details}
}
