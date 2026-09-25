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

func readLocalized(value any) (string, bool) {
	switch name := value.(type) {
	case string:
		return name, true
	case nil:
		return "", true
	}
	return "", false
}

func (n fieldInfo) translatedAs(name string) bool {
	return n.localizedName != "" && strings.EqualFold(name, n.localizedName)
}

func (n fieldInfo) translations() []string {
	if n.localizedName == "" {
		return nil
	}
	return []string{n.localizedName}
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

type customField struct {
	id   string
	info fieldInfo
}

const (
	brokenField     = "a custom field of the project is not a JSON object"
	brokenFieldInfo = "the name or the type of a custom field is not of the shape the specification gives it"
)

func encodeValue(kind FieldType, text string) (Encoded, string) {
	if text == "" {
		return Encoded{}, emptyValueReason(kind)
	}
	k, known := kind.kind()
	if !known {
		return Encoded{}, unmodelled(kind)
	}
	encoded, reason := k.encode(text)
	return Encoded{Body: encoded.body, Key: encoded.key}, reason
}

func emptyValueReason(kind FieldType) string {
	const leftAlone = "; a field is emptied by clearing it, and a field the call does not name is left as it stands"
	switch {
	case kind.ValueType == StringType || kind.ValueType == TextType:
		return fmt.Sprintf("YouTrack keeps a %s field it is given nothing for as holding nothing at all",
			kind.ValueType) + leftAlone
	case kind.Named():
		return fmt.Sprintf("a value of a %s field is a name, and no value is named by nothing", kind.ValueType) + leftAlone
	}
	return fmt.Sprintf("no value of a %s field is empty", kind.ValueType) + leftAlone
}

func (n converter) readValue(kind FieldType, item any) (*Node, bool, error) {
	value, present, err := kind.ReadValue(item)
	if err != nil || !present {
		return nil, present, err
	}
	number, isNumber := item.(json.Number)
	switch {
	case isNumber && (kind.ValueType == IntegerType || kind.ValueType == FloatType):
		return NewNumber(number), true, nil
	case kind.ValueType == TextType:
		return n.textNode(value.Text), true, nil
	}
	return NewString(value.Text), true, nil
}

func (n converter) valueKeys(f issueCustomField) ([]string, *Error) {
	values, fault := n.fieldValues(f)
	if fault != nil {
		return nil, fault
	}
	texts := make([]string, 0, len(values))
	for _, value := range values {
		texts = append(texts, value.Text)
	}
	return texts, nil
}

func (n converter) fieldValues(f issueCustomField) ([]Value, *Error) {
	items, fault := n.valuesOf(f)
	if fault != nil {
		return nil, fault
	}
	var values []Value
	for _, item := range items {
		value, present, err := f.kind.ReadValue(item)
		if err != nil {
			return nil, n.unreadableValue(f, err)
		}
		if present {
			values = append(values, value)
		}
	}
	return values, nil
}

const customFieldSchema = "IssueCustomField"

func customFieldsAsked(translated bool) []requestedField {
	held := []requestedField{{name: fieldTypeKey, children: []requestedField{{name: valueTypeKey}, {name: "isMultiValue"}}}}
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
	name          string
	value         any
	kind          FieldType
	ordinal       int64
	binding       string
	localizedName string
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
	onIssue := make([]fieldInfo, 0, len(fields))
	for _, field := range fields {
		onIssue = append(onIssue, fieldInfo{name: field.name, localizedName: field.localizedName})
	}
	pairs := make([]Pair, 0, len(asked))
	for _, name := range asked {
		matched := findMatches(name.name, onIssue)
		if len(matched) == 0 {
			continue
		}
		field := fields[matched[0]]
		printed, present, fault := n.valueNode(field)
		if fault != nil {
			return nil, fault
		}
		if !present {
			printed = emptyValue(field.kind)
		}
		pairs = append(pairs, DataPair(field.name, printed))
	}
	return NewMap(pairs...), nil
}

func emptyValue(kind FieldType) *Node {
	if kind.Multi {
		return NewList()
	}
	return NewNull()
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
	return issueCustomField{name: name, value: object[valueKey], kind: named.kind, ordinal: ordinal,
		binding: binding, localizedName: named.localizedName}, nil
}

func brokenBinding(name string) string {
	return fmt.Sprintf("the project's field the custom field %s stands for is not of the shape the "+
		"specification gives it", quote(name))
}

func readBinding(place map[string]any) (binding string, named fieldInfo, ok bool) {
	binding, isText := place[idKey].(string)
	if !isText {
		return "", fieldInfo{}, false
	}
	field, isObject := place[fieldKey].(map[string]any)
	if !isObject {
		return "", fieldInfo{}, false
	}
	kind, isObject := field[fieldTypeKey].(map[string]any)
	if !isObject {
		return "", fieldInfo{}, false
	}
	valueType, isText := kind[valueTypeKey].(string)
	isMultiValue, isFlag := kind["isMultiValue"].(bool)
	if !isText || !isFlag {
		return "", fieldInfo{}, false
	}
	translated, isName := readLocalized(field[localizedNameKey])
	if !isName {
		return "", fieldInfo{}, false
	}
	fieldType := FieldType{ValueType: ValueType(valueType), Multi: isMultiValue}
	return binding, fieldInfo{localizedName: translated, kind: fieldType}, true
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
	values, fault := n.valuesOf(f)
	if fault != nil {
		return nil, false, fault
	}
	items := make([]*Node, 0, len(values))
	for _, value := range values {
		node, present, fault := n.valueKeyNode(f, value)
		if fault != nil {
			return nil, false, fault
		}
		if present {
			items = append(items, node)
		}
	}
	switch {
	case len(items) == 0:
		return nil, false, nil
	case !f.kind.Multi:
		return items[0], true, nil
	}
	return NewList(items...), true, nil
}

func (n converter) valueKeyNode(f issueCustomField, item any) (*Node, bool, *Error) {
	node, present, err := n.readValue(f.kind, item)
	if err != nil {
		return nil, false, n.unreadableValue(f, err)
	}
	return node, present, nil
}

func (n converter) unreadableValue(f issueCustomField, err error) *Error {
	return n.response.invalid(fmt.Sprintf("custom field %s: %v", quote(f.name), err))
}

const customFieldCatalogue = "[]CustomField"

const brokenCatalogue = "a custom field of the instance is named in some shape other than text"

func catalogueFields() []requestedField {
	return []requestedField{{name: nameKey}, {name: localizedNameKey}}
}

func (c *Client) customFieldCatalogue(ctx context.Context) (decodedResponse, []fieldInfo, *Error) {
	a, fault := c.request(ctx, customFieldCatalogue, catalogueFields(), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetCustomFields(ctx, fields, topAll)
	})
	if fault != nil {
		return decodedResponse{}, nil, fault
	}
	catalogue := make([]fieldInfo, 0, len(a.objects))
	for _, object := range a.objects {
		found, ok := readCatalogueEntry(object)
		if !ok {
			return decodedResponse{}, nil, a.invalid(brokenCatalogue)
		}
		catalogue = append(catalogue, found)
	}
	return a, catalogue, nil
}

func readCatalogueEntry(object map[string]any) (fieldInfo, bool) {
	name, isText := object[nameKey].(string)
	if !isText {
		return fieldInfo{}, false
	}
	translated, isName := readLocalized(object[localizedNameKey])
	if !isName {
		return fieldInfo{}, false
	}
	return fieldInfo{name: name, localizedName: translated}, true
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
	key, entries, message := "unknown", r.unknown, "the names under unknown are not custom fields of "+among
	if len(r.unknown) == 0 {
		key, entries = "ambiguous", r.ambiguous
		message = "the names under ambiguous are the names of more than one custom field of " + among + " each"
	}
	if len(entries) == 0 {
		return nil
	}
	details := []Pair{sent, against, {Key: key, Value: NewList(entries...)}}
	return &Error{Code: CodeUnknownName, Message: message, Details: details}
}

func fieldInfoFields() requestedField {
	return requestedField{name: fieldKey, children: []requestedField{
		{name: nameKey},
		{name: localizedNameKey},
		{name: fieldTypeKey, children: []requestedField{{name: valueTypeKey}, {name: "isMultiValue"}}},
	}}
}

func readFieldInfo(object map[string]any) (fieldInfo, bool) {
	field, isObject := object[fieldKey].(map[string]any)
	if !isObject {
		return fieldInfo{}, false
	}
	name, isText := field[nameKey].(string)
	if !isText {
		return fieldInfo{}, false
	}
	kind, isObject := field[fieldTypeKey].(map[string]any)
	if !isObject {
		return fieldInfo{}, false
	}
	valueType, isText := kind[valueTypeKey].(string)
	if !isText {
		return fieldInfo{}, false
	}
	isMultiValue, isBool := kind["isMultiValue"].(bool)
	if !isBool {
		return fieldInfo{}, false
	}
	translated, isName := readLocalized(field[localizedNameKey])
	if !isName {
		return fieldInfo{}, false
	}
	fieldType := FieldType{ValueType: ValueType(valueType), Multi: isMultiValue}
	return fieldInfo{name: name, localizedName: translated, kind: fieldType}, true
}
