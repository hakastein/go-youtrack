package youtrack

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	idKey            = "id"
	idReadableKey    = "idReadable"
	nameKey          = "name"
	localizedNameKey = "localizedName"
	loginKey         = "login"
	minutesKey       = "minutes"
	textKey          = "text"
	valueKey         = "value"
	fieldKey         = "field"
	typeKey          = "type"
	fieldTypeKey     = "fieldType"
	valueTypeKey     = "valueType"
	customFieldsKey  = "customFields"
	ordinalKey       = "ordinal"
	canBeEmptyKey    = "canBeEmpty"
	summaryKey       = "summary"
	projectKey       = "project"
	shortNameKey     = "shortName"
	addedKey         = "added"
	removedKey       = "removed"
)

type requestedField struct {
	name         string
	children     []requestedField
	quoted       bool
	bare         bool
	fromCaller   bool
	normalized   bool
	extraSchemas []string
}

func parseFields(expression, defaults string, named bool) (string, []requestedField, *Error) {
	given := &fieldsReader{text: expression, named: named, fromCaller: true}
	var tree []requestedField
	if expression == "" || given.take('+') {
		var fault *Error
		tree, fault = (&fieldsReader{text: defaults, named: named}).expression(nil)
		if expression == "" || fault != nil {
			return defaults, tree, fault
		}
	}
	requested, fault := given.expression(tree)
	if fault == nil {
		fault = rejectFileContent(expression, requested)
	}
	return expression, requested, fault
}

const fileContentKey = "base64Content"

const fileContentMessage = "is the file itself, which the module does not download; the url of an attachment is " +
	"a signed link, and whoever holds it fetches the file with any client"

func rejectFileContent(expression string, requested []requestedField) *Error {
	path, written := findField(fileContentKey, requested, nil)
	if !written {
		return nil
	}
	message := fmt.Sprintf("fields %s: %s %s", quote(expression), path, fileContentMessage)
	return &Error{Code: CodeBadUsage, Message: message}
}

func findField(name string, requested []requestedField, parents []string) (string, bool) {
	for _, field := range requested {
		if field.name == name && !field.quoted {
			return fieldPath(parents, field.name), true
		}
		childPath := append(slices.Clip(parents), field.name)
		if path, found := findField(name, field.children, childPath); found {
			return path, true
		}
	}
	return "", false
}

func extendsDefault(expression string) bool {
	return (&fieldsReader{text: expression}).take('+')
}

func formatFields(fields []requestedField) string {
	names := make([]string, 0, len(fields))
	for _, field := range fields {
		if field.children == nil {
			names = append(names, formatName(field))
		} else {
			names = append(names, formatName(field)+"("+formatFields(field.children)+")")
		}
	}
	return strings.Join(names, ",")
}

func formatName(field requestedField) string {
	if !field.quoted {
		return field.name
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(field.name) + `"`
}

func merge(fields []requestedField, field requestedField) []requestedField {
	for i := range fields {
		if fields[i].name == field.name {
			for _, child := range field.children {
				fields[i].children = merge(fields[i].children, child)
			}
			fields[i].bare = field.bare
			fields[i].fromCaller = fields[i].fromCaller || field.fromCaller
			fields[i].normalized = fields[i].normalized || field.normalized
			for _, schema := range field.extraSchemas {
				if !slices.Contains(fields[i].extraSchemas, schema) {
					fields[i].extraSchemas = append(fields[i].extraSchemas, schema)
				}
			}
			return fields
		}
	}
	return append(fields, field)
}

func withFields(requested []requestedField, own ...requestedField) []requestedField {
	asked := cloneFields(requested)
	for _, field := range own {
		asked = merge(asked, field)
	}
	return asked
}

func cloneFields(fields []requestedField) []requestedField {
	if fields == nil {
		return nil
	}
	copied := make([]requestedField, len(fields))
	for i, field := range fields {
		field.children = cloneFields(field.children)
		copied[i] = field
	}
	return copied
}

func walkFields(c *schemas, at string, requested []requestedField, parents []string, visit func(declaringSchema string, decl typeRef, path []string, field *requestedField)) {
	for i := range requested {
		field := &requested[i]
		decl, _ := c.declaration(at, field.name)
		visit(at, decl, parents, field)
		if decl.schema == "" {
			continue
		}
		childPath := append(slices.Clip(parents), field.name)
		walkFields(c, decl.schema, field.children, childPath, visit)
	}
}

func fieldsNamed(c *schemas, at, schema, name string, requested []requestedField, visit func(parents []string, field *requestedField)) {
	walkFields(c, at, requested, nil, func(declaringSchema string, _ typeRef, path []string, field *requestedField) {
		if declaringSchema == schema && field.name == name {
			visit(path, field)
		}
	})
}

func fieldsOfType(c *schemas, at, schema string, requested []requestedField, visit func(parents []string, field *requestedField)) {
	walkFields(c, at, requested, nil, func(_ string, decl typeRef, path []string, field *requestedField) {
		if decl.schema == schema {
			visit(path, field)
		}
	})
}

func firstFieldNamed(c *schemas, at, schema, name string, requested []requestedField) (string, bool) {
	first, found := "", false
	fieldsNamed(c, at, schema, name, requested, func(path []string, field *requestedField) {
		if !found {
			first, found = fieldPath(path, field.name), true
		}
	})
	return first, found
}

type fieldsReader struct {
	text       string
	at         int
	named      bool
	fromCaller bool
}

func (r *fieldsReader) expression(tree []requestedField) ([]requestedField, *Error) {
	tree, fault := r.list(tree)
	if fault != nil {
		return nil, fault
	}
	if r.at < len(r.text) {
		return nil, r.unexpected()
	}
	return tree, nil
}

func (r *fieldsReader) list(fields []requestedField) ([]requestedField, *Error) {
	for {
		field, fault := r.item()
		if fault != nil {
			return nil, fault
		}
		fields = merge(fields, field)
		if !r.take(',') {
			return fields, nil
		}
	}
}

func (r *fieldsReader) item() (requestedField, *Error) {
	field, fault := r.itemName()
	if fault != nil {
		return requestedField{}, fault
	}
	if !r.take('(') {
		field.bare = true
		return field, nil
	}
	children, fault := r.list(nil)
	if fault != nil {
		return requestedField{}, fault
	}
	if !r.take(')') {
		return requestedField{}, r.unexpected()
	}
	field.children = children
	return field, nil
}

func (r *fieldsReader) itemName() (requestedField, *Error) {
	r.skipSpace()
	if r.named && r.at < len(r.text) && r.text[r.at] == '"' {
		return r.quotedName()
	}
	start := r.at
	for r.at < len(r.text) && isNameByte(r.text[r.at]) {
		r.at++
	}
	if r.at == start {
		return requestedField{}, r.unexpected()
	}
	field := requestedField{name: r.text[start:r.at], fromCaller: r.fromCaller}
	if err := CheckKey(field.name); err != nil {
		message := fmt.Sprintf("fields %s: the name at column %d cannot be printed: %v", quote(r.text), r.column(start), err)
		return requestedField{}, &Error{Code: CodeBadUsage, Message: message}
	}
	return field, nil
}

func (r *fieldsReader) quotedName() (requestedField, *Error) {
	r.at++
	var name strings.Builder
	for r.at < len(r.text) {
		switch c := r.text[r.at]; c {
		case '"':
			if name.Len() == 0 {
				return requestedField{}, r.unexpected()
			}
			r.at++
			return requestedField{name: name.String(), quoted: true, fromCaller: r.fromCaller}, nil
		case '\\':
			r.at++
			if r.at >= len(r.text) || (r.text[r.at] != '"' && r.text[r.at] != '\\') {
				return requestedField{}, r.unexpected()
			}
			name.WriteByte(r.text[r.at])
		default:
			name.WriteByte(c)
		}
		r.at++
	}
	return requestedField{}, r.unexpected()
}

func (r *fieldsReader) take(c byte) bool {
	r.skipSpace()
	if r.at < len(r.text) && r.text[r.at] == c {
		r.at++
		return true
	}
	return false
}

func (r *fieldsReader) skipSpace() {
	for r.at < len(r.text) && (r.text[r.at] == ' ' || r.text[r.at] == '\t') {
		r.at++
	}
}

func (r *fieldsReader) unexpected() *Error {
	found := "end"
	if r.at < len(r.text) {
		c, _ := utf8.DecodeRuneInString(r.text[r.at:])
		found = quote(string(c))
	}
	message := fmt.Sprintf("fields %s: unexpected %s at column %d", quote(r.text), found, r.column(r.at))
	return &Error{Code: CodeBadUsage, Message: message}
}

func (r *fieldsReader) column(at int) int {
	return utf8.RuneCountInString(r.text[:at]) + 1
}

func isNameByte(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || c == '_' || c == '$'
}
