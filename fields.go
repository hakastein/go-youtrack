package youtrack

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const idKey = "id"

type field struct {
	name     string
	children []field
}

// parseFields reads a fields= expression of the YouTrack REST API: names, each with an optional list of
// names in parentheses, separated by commas.
func parseFields(text string) ([]field, error) {
	r := &fieldsReader{text: text}
	fields, err := r.list(nil)
	if err != nil {
		return nil, err
	}
	if r.at < len(r.text) {
		return nil, r.unexpected()
	}
	return fields, nil
}

func formatFields(fields []field) string {
	names := make([]string, 0, len(fields))
	for _, f := range fields {
		if f.children == nil {
			names = append(names, f.name)
		} else {
			names = append(names, f.name+"("+formatFields(f.children)+")")
		}
	}
	return strings.Join(names, ",")
}

func merge(fields []field, f field) []field {
	for i := range fields {
		if fields[i].name == f.name {
			for _, child := range f.children {
				fields[i].children = merge(fields[i].children, child)
			}
			return fields
		}
	}
	return append(fields, f)
}

func withFields(fields []field, own ...field) []field {
	merged := cloneFields(fields)
	for _, f := range own {
		merged = merge(merged, f)
	}
	return merged
}

func cloneFields(fields []field) []field {
	if fields == nil {
		return nil
	}
	copied := make([]field, len(fields))
	for i, f := range fields {
		f.children = cloneFields(f.children)
		copied[i] = f
	}
	return copied
}

type fieldsReader struct {
	text string
	at   int
}

func (r *fieldsReader) list(fields []field) ([]field, error) {
	for {
		f, err := r.item()
		if err != nil {
			return nil, err
		}
		fields = merge(fields, f)
		if !r.take(',') {
			return fields, nil
		}
	}
}

func (r *fieldsReader) item() (field, error) {
	r.skipSpace()
	start := r.at
	for r.at < len(r.text) && isNameByte(r.text[r.at]) {
		r.at++
	}
	if r.at == start {
		return field{}, r.unexpected()
	}
	f := field{name: r.text[start:r.at]}
	if !r.take('(') {
		return f, nil
	}
	children, err := r.list(nil)
	if err != nil {
		return field{}, err
	}
	if !r.take(')') {
		return field{}, r.unexpected()
	}
	f.children = children
	return f, nil
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

func (r *fieldsReader) unexpected() error {
	found := "end"
	if r.at < len(r.text) {
		c, _ := utf8.DecodeRuneInString(r.text[r.at:])
		found = quote(string(c))
	}
	column := utf8.RuneCountInString(r.text[:r.at]) + 1
	return &ArgumentError{Argument: "fields", Value: r.text, Reason: fmt.Sprintf("holds an unexpected %s at column %d", found, column)}
}

func isNameByte(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || c == '_' || c == '$'
}
