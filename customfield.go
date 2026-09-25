package youtrack

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

type fieldInfo struct {
	name          string
	localizedName string
	kind          FieldType
}

func (f fieldInfo) translatedAs(name string) bool {
	return f.localizedName != "" && strings.EqualFold(name, f.localizedName)
}

func (f fieldInfo) translations() []string {
	if f.localizedName == "" {
		return nil
	}
	return []string{f.localizedName}
}

func findMatches(name string, catalogue []fieldInfo) []int {
	var byName, byTranslation []int
	for at, field := range catalogue {
		switch {
		case strings.EqualFold(name, field.name):
			byName = append(byName, at)
		case field.translatedAs(name):
			byTranslation = append(byTranslation, at)
		}
	}
	if len(byName) > 0 {
		return byName
	}
	return byTranslation
}

func catalogueOf[T interface{ info() fieldInfo }](fields []T) []fieldInfo {
	catalogue := make([]fieldInfo, 0, len(fields))
	for _, field := range fields {
		catalogue = append(catalogue, field.info())
	}
	return catalogue
}

func fieldNamed[T interface{ info() fieldInfo }](name string, fields []T) (T, bool) {
	places := findMatches(name, catalogueOf(fields))
	if len(places) == 0 {
		var none T
		return none, false
	}
	return fields[places[0]], true
}

func pick(catalogue []fieldInfo, places []int) []fieldInfo {
	found := make([]fieldInfo, 0, len(places))
	for _, at := range places {
		found = append(found, catalogue[at])
	}
	return found
}

func nearestNamed(name string, catalogue []fieldInfo) []string {
	among := make([]suggestion, 0, len(catalogue))
	for _, field := range catalogue {
		among = append(among, suggestion{name: field.name, also: field.translations()})
	}
	return nearest(name, among, canonical(catalogue))
}

func canonical(catalogue []fieldInfo) []string {
	names := make([]string, 0, len(catalogue))
	for _, field := range catalogue {
		names = append(names, field.name)
	}
	slices.Sort(names)
	return names
}

func (n converter) readValue(kind FieldType, item any) (*Node, bool, string) {
	value, present, reason := kind.read(item)
	if reason != "" || !present {
		return nil, present, reason
	}
	return n.printedValue(kind, item, value), true, ""
}

func (n converter) printedValue(kind FieldType, item any, value Value) *Node {
	number, isNumber := item.(json.Number)
	switch {
	case isNumber && (kind.ValueType == IntegerType || kind.ValueType == FloatType):
		return NewNumber(number)
	case kind.ValueType == TextType:
		return n.textNode(value.Text)
	}
	return NewString(value.Text)
}

func (n converter) recordField(f issueCustomField) (Field, *Error) {
	field := Field{Name: f.name, LocalizedName: f.localizedName, Type: f.kind}
	fault := n.eachValue(f, func(_ any, value Value) { field.Values = append(field.Values, value) })
	return field, fault
}

func (n converter) eachValue(f issueCustomField, read func(item any, value Value)) *Error {
	items, fault := n.valuesOf(f)
	if fault != nil {
		return fault
	}
	for _, item := range items {
		value, present, reason := f.kind.read(item)
		if reason != "" {
			return n.response.invalid(fmt.Sprintf("custom field %s: %s", quote(f.name), reason))
		}
		if present {
			read(item, value)
		}
	}
	return nil
}

const customFieldSchema = "IssueCustomField"

func fieldTypeFields() requestedField {
	return requestedField{name: fieldTypeKey, children: []requestedField{{name: valueTypeKey}, {name: isMultiValueKey}}}
}

func customFieldsAsked(translated bool) []requestedField {
	held := []requestedField{fieldTypeFields()}
	if translated {
		held = append(held, requestedField{name: localizedNameKey})
	}
	return []requestedField{
		{name: nameKey},
		{name: valueKey, children: valueKeyFields()},
		{name: "projectCustomField", children: []requestedField{
			{name: idKey},
			{name: ordinalKey},
			{name: fieldKey, children: held},
		}},
	}
}

func valueKeyFields() []requestedField {
	var members []requestedField
	for _, key := range ValueKeys() {
		members = append(members, requestedField{name: key})
	}
	return members
}

type issueCustomField struct {
	fieldInfo
	value   any
	ordinal int64
	binding string
}

func (f issueCustomField) info() fieldInfo {
	return f.fieldInfo
}

func (n converter) customFields(asked requestedField, value any) (*Node, *Error) {
	fields, fault := n.readCustomFields(value)
	if fault != nil {
		return nil, fault
	}
	if asked.children == nil {
		return n.allFieldsNode(fields)
	}
	return n.selectedFieldsNode(asked.children, fields)
}

func (n converter) readCustomFields(value any) ([]issueCustomField, *Error) {
	received, isList := value.([]any)
	if !isList {
		return nil, n.response.invalid("the custom fields of the issue arrived as something other than an array")
	}
	fields := make([]issueCustomField, 0, len(received))
	named := make(map[string]bool, len(received))
	for _, item := range received {
		field, fault := n.readCustomField(item)
		if fault != nil {
			return nil, fault
		}
		if named[field.name] {
			return nil, n.response.invalid(fmt.Sprintf("two custom fields of the issue are named %s", quote(field.name)))
		}
		named[field.name] = true
		fields = append(fields, field)
	}
	return fields, nil
}

func (n converter) allFieldsNode(fields []issueCustomField) (*Node, *Error) {
	slices.SortStableFunc(fields, inProjectOrder)
	pairs := make([]Pair, 0, len(fields))
	for _, field := range fields {
		printed, present, fault := n.valueNode(field)
		if fault != nil {
			return nil, fault
		}
		if present {
			pairs = append(pairs, DataPair(field.name, printed))
		}
	}
	return NewMap(pairs...), nil
}

func (n converter) selectedFieldsNode(asked []requestedField, fields []issueCustomField) (*Node, *Error) {
	pairs := make([]Pair, 0, len(asked))
	for _, name := range asked {
		field, found := fieldNamed(name.name, fields)
		if !found {
			continue
		}
		printed, present, fault := n.valueNode(field)
		if fault != nil {
			return nil, fault
		}
		if !present {
			printed = textsNode(nil, field.kind)
		}
		pairs = append(pairs, DataPair(field.name, printed))
	}
	return NewMap(pairs...), nil
}

func inProjectOrder(a, b issueCustomField) int {
	return cmp.Or(cmp.Compare(a.ordinal, b.ordinal), compareBindings(a.binding, b.binding))
}

func compareBindings(a, b string) int {
	first, isNumbered := bindingNumbers(a)
	second, alsoNumbered := bindingNumbers(b)
	if !isNumbered || !alsoNumbered {
		return strings.Compare(a, b)
	}
	return cmp.Or(cmp.Compare(first[0], second[0]), cmp.Compare(first[1], second[1]))
}

func bindingNumbers(id string) ([2]int, bool) {
	before, after, dashed := strings.Cut(id, "-")
	if !dashed {
		return [2]int{}, false
	}
	first, firstErr := strconv.Atoi(before)
	second, secondErr := strconv.Atoi(after)
	if firstErr != nil || secondErr != nil {
		return [2]int{}, false
	}
	return [2]int{first, second}, true
}

func (n converter) readCustomField(item any) (issueCustomField, *Error) {
	object, isObject := item.(map[string]any)
	if !isObject {
		return issueCustomField{}, n.response.invalid("a custom field of the issue is not a JSON object")
	}
	name, isText := object[nameKey].(string)
	if !isText {
		return issueCustomField{}, n.response.invalid("the name of a custom field of the issue is not text")
	}
	place, _ := object["projectCustomField"].(map[string]any)
	binding, named, whole := readBinding(place)
	if !whole {
		return issueCustomField{}, n.response.invalid(brokenBinding(name))
	}
	ordinal, isWhole := parseInt64(place[ordinalKey])
	if !isWhole {
		message := fmt.Sprintf("the place of the custom field %s among the fields of the project is no whole number", quote(name))
		return issueCustomField{}, n.response.invalid(message)
	}
	if !named.kind.Known() {
		return issueCustomField{}, n.response.invalid(unmodelled(named.kind))
	}
	named.name = name
	return issueCustomField{fieldInfo: named, value: object[valueKey], ordinal: ordinal, binding: binding}, nil
}

func brokenBinding(name string) string {
	return fmt.Sprintf("the project's field the custom field %s stands for is not of the shape the "+
		"specification gives it", quote(name))
}

func readBinding(place map[string]any) (binding string, named fieldInfo, ok bool) {
	binding, isText := place[idKey].(string)
	field, _ := place[fieldKey].(map[string]any)
	kind, typed := readFieldType(field)
	translated, isName := readOptionalText(field[localizedNameKey])
	return binding, fieldInfo{localizedName: translated, kind: kind}, isText && typed && isName
}

func readFieldType(field map[string]any) (FieldType, bool) {
	kind, _ := field[fieldTypeKey].(map[string]any)
	valueType, isText := kind[valueTypeKey].(string)
	isMultiValue, isFlag := kind[isMultiValueKey].(bool)
	return FieldType{ValueType: ValueType(valueType), Multi: isMultiValue}, isText && isFlag
}

func (n converter) valuesOf(f issueCustomField) ([]any, *Error) {
	values, isList := f.value.([]any)
	switch {
	case f.value == nil:
		return nil, nil
	case isList && !f.kind.Multi:
		return nil, n.response.invalid(fmt.Sprintf("the custom field %s holds one value by its type and arrived as a list", quote(f.name)))
	case !isList && f.kind.Multi:
		message := fmt.Sprintf("the custom field %s holds more than one value by its type and arrived as "+
			"something other than a list", quote(f.name))
		return nil, n.response.invalid(message)
	case !isList:
		return []any{f.value}, nil
	}
	return values, nil
}

func (n converter) valueNode(f issueCustomField) (*Node, bool, *Error) {
	var items []*Node
	if fault := n.eachValue(f, func(item any, value Value) { items = append(items, n.printedValue(f.kind, item, value)) }); fault != nil {
		return nil, false, fault
	}
	switch {
	case len(items) == 0:
		return nil, false, nil
	case !f.kind.Multi:
		return items[0], true, nil
	}
	return NewList(items...), true, nil
}

const customFieldsListing = "[]CustomField"

const brokenCatalogue = "a custom field of the instance is named in some shape other than text"

func catalogueFields() []requestedField {
	return []requestedField{{name: nameKey}, {name: localizedNameKey}}
}

func (c *Client) customFieldCatalogue(ctx context.Context) (decodedResponse, []fieldInfo, *Error) {
	a, fault := c.request(ctx, customFieldsListing, catalogueFields(), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetCustomFields(ctx, fields, topAll)
	})
	if fault != nil {
		return decodedResponse{}, nil, fault
	}
	catalogue := make([]fieldInfo, 0, len(a.objects))
	for _, object := range a.objects {
		found, ok := readFieldNames(object)
		if !ok {
			return decodedResponse{}, nil, a.invalid(brokenCatalogue)
		}
		catalogue = append(catalogue, found)
	}
	return a, catalogue, nil
}

func readFieldNames(field map[string]any) (fieldInfo, bool) {
	name, isText := field[nameKey].(string)
	translated, isName := readOptionalText(field[localizedNameKey])
	return fieldInfo{name: name, localizedName: translated}, isText && isName
}

func (c *Client) resolveCustomFields(ctx context.Context, requested []requestedField) *Error {
	named := namedCustomFields(c.spec, requested)
	if named == nil || !slices.ContainsFunc(named.children, fromCaller) {
		return nil
	}
	a, catalogue, fault := c.customFieldCatalogue(ctx)
	if fault != nil {
		return fault
	}
	resolved, fault := resolveNames(a, requested, named.children, catalogue)
	if fault != nil {
		return fault
	}
	named.children = resolved
	return nil
}

func fromCaller(name requestedField) bool {
	return name.fromCaller
}

func fromDefault(name requestedField) bool {
	return !name.fromCaller
}

func hasDefaultNames(spec *schemas, requested []requestedField) bool {
	named := namedCustomFields(spec, requested)
	return named != nil && slices.ContainsFunc(named.children, fromDefault)
}

func resolveNames(a decodedResponse, requested, asked []requestedField, catalogue []fieldInfo) ([]requestedField, *Error) {
	var resolved []requestedField
	names := resolvingNames(catalogue)
	for _, name := range asked {
		if !fromCaller(name) {
			resolved = merge(resolved, name)
			continue
		}
		if at, found := names.place(name.name, fieldPath([]string{customFieldsKey}, formatName(name))); found {
			resolved = merge(resolved, requestedField{name: catalogue[at].name, fromCaller: true})
		}
	}
	against := Pair{Key: "fields", Value: NewString(formatFields(requested))}
	if fault := names.fault(a.sent(), against, "the instance"); fault != nil {
		return nil, fault
	}
	return resolved, nil
}

type nameResolver struct {
	catalogue []fieldInfo
	reported  map[string]bool
	unknown   []*Node
	ambiguous []*Node
}

func resolvingNames(catalogue []fieldInfo) *nameResolver {
	return &nameResolver{catalogue: catalogue, reported: map[string]bool{}}
}

func (r *nameResolver) place(name, written string) (int, bool) {
	places := findMatches(name, r.catalogue)
	switch {
	case len(places) == 1:
		return places[0], true
	case r.reported[written]:
	case len(places) == 0:
		r.unknown = append(r.unknown, nearestEntry(fieldKey, written, nearestNamed(name, r.catalogue)))
	default:
		r.ambiguous = append(r.ambiguous, NewMap(
			Pair{Key: fieldKey, Value: NewString(written)},
			Pair{Key: "candidates", Value: textList(canonical(pick(r.catalogue, places)))}))
	}
	r.reported[written] = true
	return 0, false
}

func (r *nameResolver) fault(sent, against Pair, among string) *Error {
	unresolved := unresolvedDetails(r.unknown, r.ambiguous)
	if unresolved == nil {
		return nil
	}
	message := "the names under unknown are not custom fields of " + among
	switch {
	case len(r.unknown) == 0:
		message = "the names under ambiguous are the names of more than one custom field of " + among + " each"
	case len(r.ambiguous) > 0:
		message += ", and each name under ambiguous is the name of more than one"
	}
	return &Error{Code: CodeUnknownName, Message: message, Details: append([]Pair{sent, against}, unresolved...)}
}

func fieldInfoFields() requestedField {
	return requestedField{name: fieldKey, children: []requestedField{
		{name: nameKey},
		{name: localizedNameKey},
		fieldTypeFields(),
	}}
}

func readFieldInfo(object map[string]any) (fieldInfo, bool) {
	field, _ := object[fieldKey].(map[string]any)
	named, isNamed := readFieldNames(field)
	kind, typed := readFieldType(field)
	named.kind = kind
	return named, isNamed && typed
}
