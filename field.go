package youtrack

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
)

const FieldListFields = "field(name,localizedName,fieldType(valueType,isMultiValue)),canBeEmpty"

const projectRead = "jetbrains.jetpass.project-read"

// ListFieldsOptions: Fields is a fields= expression, empty for FieldListFields and +x for them and x.
type ListFieldsOptions struct {
	Fields string
}

// ShowFieldOptions: Fields is a fields= expression, empty for the default of the field's type and +x for it and x.
// The default is FieldListFields with the bundle of values of a type that has one or the users of a user field.
type ShowFieldOptions struct {
	Fields string
}

// List is every custom field of the project in the project's order, in one read. An empty list is refused as
// denied: the server answers a token without the right to read the project with one.
func (s *FieldsService) List(ctx context.Context, project string, opts *ListFieldsOptions) (*Node, error) {
	return result(s.list(ctx, project, optionsOf(opts)))
}

// Show reads the project's custom field by name, then by translation, without regard to letter case.
func (s *FieldsService) Show(ctx context.Context, project, name string, opts *ShowFieldOptions) (*Node, error) {
	return result(s.show(ctx, project, name, optionsOf(opts)))
}

func (s *FieldsService) list(ctx context.Context, project string, opts ListFieldsOptions) (*Node, *Error) {
	code, fault := parseProjectCode(project)
	if fault != nil {
		return nil, fault
	}
	_, requested, fault := parseFields(opts.Fields, FieldListFields, false)
	if fault != nil {
		return nil, fault
	}
	c := s.client
	asked := withFields(requested, requestedField{name: ordinalKey})
	decoded, fault := c.request(ctx, "[]ProjectCustomField", asked, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetProjectCustomFields(ctx, code, fields, topAll)
	})
	if fault != nil {
		return nil, fault
	}
	if len(decoded.objects) == 0 {
		return nil, noFields(decoded, code)
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

func (s *FieldsService) show(ctx context.Context, project, name string, opts ShowFieldOptions) (*Node, *Error) {
	code, fault := parseProjectCode(project)
	if fault != nil {
		return nil, fault
	}
	if fault := checkFieldName(name); fault != nil {
		return nil, fault
	}
	if _, _, fault := parseFields(opts.Fields, FieldListFields, false); fault != nil {
		return nil, fault
	}
	read, fault := s.readField(ctx, code, name, func(found fieldInfo) ([]requestedField, bool, *Error) {
		return fieldsToPrint(opts.Fields, found)
	})
	if fault != nil {
		return nil, fault
	}
	return objectNode(read.answer, read.requested, read.answer.objects[0], nil)
}

func checkFieldName(name string) *Error {
	if name == "" {
		return &Error{Code: CodeBadUsage, Message: "the name of a custom field is empty"}
	}
	return nil
}

// modelled is false when what to read depends on a type the module does not model.
type fieldAsk func(found fieldInfo) (requested []requestedField, modelled bool, fault *Error)

type fieldRead struct {
	field     customField
	requested []requestedField
	answer    decodedResponse
}

// The cache confirms and never refuses: whatever of it disagrees with the server sends the project to be read
// again, and only metadata the server has just sent is refused.
func (s *FieldsService) readField(ctx context.Context, code, name string, ask fieldAsk) (fieldRead, *Error) {
	metadata, sent, fault := s.metadata(ctx, code)
	if fault != nil {
		return fieldRead{}, fault
	}
	read, fault, cacheStale := s.readFieldFrom(ctx, code, name, ask, metadata, sent)
	if !cacheStale {
		return read, fault
	}
	if metadata, sent, fault = s.readMetadata(ctx, code); fault != nil {
		return fieldRead{}, fault
	}
	read, fault, _ = s.readFieldFrom(ctx, code, name, ask, metadata, sent)
	return read, fault
}

func (s *FieldsService) readFieldFrom(ctx context.Context, code, name string, ask fieldAsk, metadata *Metadata, sent Pair) (fieldRead, *Error, bool) {
	fields := projectFieldsOf(metadata)
	names := resolvingNames(fieldInfos(fields))
	at, ok := names.place(name, name)
	if !ok {
		return refuseUnlessCached(metadata, names.fault(sent, Pair{Key: projectKey, Value: NewString(code)}, "the project"))
	}
	found := fields[at]
	if !found.hasValidID() {
		return refuseUnlessCached(metadata, metadataInvalid(sent, invalidFieldID(found.id)))
	}
	requested, modelled, fault := ask(found.info)
	if fault != nil {
		return fieldRead{}, fault, false
	}
	if !modelled {
		return refuseUnlessCached(metadata, metadataInvalid(sent, unmodelled(found.info.kind)))
	}
	c := s.client
	asked := withFields(requested, fieldInfoFields())
	answer, fault := c.request(ctx, "ProjectCustomField", asked, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetProjectCustomField(ctx, code, found.id, fields)
	})
	if fault != nil {
		return fieldRead{}, fault, metadata.FromCache && isStale(fault)
	}
	if fault := found.info.verifyUnchanged(answer, code); fault != nil {
		return fieldRead{}, fault, metadata.FromCache
	}
	return fieldRead{field: found, requested: requested, answer: answer}, nil, false
}

func refuseUnlessCached(metadata *Metadata, fault *Error) (fieldRead, *Error, bool) {
	if metadata.FromCache {
		return fieldRead{}, nil, true
	}
	return fieldRead{}, fault, false
}

func projectFieldsOf(metadata *Metadata) []customField {
	fields := make([]customField, 0, len(metadata.Fields))
	for _, field := range metadata.Fields {
		named := fieldInfo{name: field.Name, localizedName: field.LocalizedName, kind: field.Type}
		fields = append(fields, customField{id: field.ID, info: named})
	}
	return fields
}

func metadataInvalid(sent Pair, message string) *Error {
	return &Error{Code: CodeUpstreamInvalid, Message: message, Details: []Pair{sent}}
}

// A field the server no longer finds under its cached id and an answer that does not fit are what a stale cache
// looks like.
func isStale(fault *Error) bool {
	return fault.Code == CodeNotFound || fault.Code == CodeUpstreamInvalid
}

func fieldInfos(fields []customField) []fieldInfo {
	catalogue := make([]fieldInfo, 0, len(fields))
	for _, field := range fields {
		catalogue = append(catalogue, field.info)
	}
	return catalogue
}

func (f customField) hasValidID() bool {
	return isInternalID(f.id)
}

func invalidFieldID(id string) string {
	return fmt.Sprintf("the id %s of a custom field is not two numbers with a dash between them", quote(id))
}

func (n fieldInfo) verifyUnchanged(a decodedResponse, code string) *Error {
	answered, ok := readFieldInfo(a.objects[0])
	if !ok {
		return a.invalid(brokenFieldInfo)
	}
	if answered == n {
		return nil
	}
	details := append(responseDetails(a.httpResponse),
		Pair{Key: projectKey, Value: NewString(code)},
		Pair{Key: fieldKey, Value: NewString(n.name)},
		bodyDetail(a.body))
	message := "the custom field the id addresses is no longer the one the name resolved to"
	return &Error{Code: CodeUpstreamFailed, Message: message, Details: details}
}

func defaultFields(n fieldInfo) (string, bool) {
	switch {
	case !n.kind.Known():
		return "", false
	case n.kind.BundleFields() == "":
		return FieldListFields, true
	}
	return FieldListFields + "," + n.kind.BundleFields(), true
}

func fieldsToPrint(expression string, n fieldInfo) (requested []requestedField, modelled bool, fault *Error) {
	defaults := ""
	if expression == "" || extendsDefault(expression) {
		if defaults, modelled = defaultFields(n); !modelled {
			return nil, false, nil
		}
	}
	_, requested, fault = parseFields(expression, defaults, false)
	return requested, true, fault
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
			return nil, decoded.invalid("the ordinal of a custom field is not a number")
		}
		position, err := number.Int64()
		if err != nil {
			return nil, decoded.invalid("the ordinal of a custom field is not a whole number")
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

func noFields(a decodedResponse, code string) *Error {
	message := "not one custom field of the project arrived, and a token without the right under permission is sent an empty list"
	return a.fault(CodeDenied, message,
		Pair{Key: projectKey, Value: NewString(code)},
		Pair{Key: "permission", Value: NewString(projectRead)})
}
