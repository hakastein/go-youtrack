package youtrack

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// Bundle is the set of values a custom field of a project accepts.
type Bundle struct {
	Field  ProjectField
	Values []BundleValue
}

// BundleValue is one value of a bundle: its internal id, which survives a rename, its name and whether it is archived.
type BundleValue struct {
	ID       string
	Name     string
	Archived bool
}

// Bundle reads the values of the custom field of the project the name resolves to, by name and then by
// translation, without regard to letter case. Only a field whose type HasBundle has one. With a metadata
// cache the id of the field comes from the cache, and the project is read again when the cache disagrees
// with the server; the field itself always comes from the server.
func (c *Client) Bundle(ctx context.Context, project, field string) (*Bundle, error) {
	code, err := parseProjectCode(project)
	if err != nil {
		return nil, err
	}
	if field == "" {
		return nil, &ArgumentError{Argument: "field", Value: field, Reason: "names no custom field"}
	}
	target := metadataTarget(code)
	if cached, hit := c.cache.load(target); hit {
		bundle, err, stale := c.bundleFrom(ctx, code, field, cached, answer{}, true)
		if !stale {
			return bundle, err
		}
	}
	metadata, a, err := c.readProjectMetadata(ctx, code)
	if err != nil {
		return nil, err
	}
	c.cache.store(target, metadata.fields)
	bundle, err, _ := c.bundleFrom(ctx, code, field, metadata.fields, a, false)
	return bundle, err
}

func (c *Client) bundleFrom(ctx context.Context, code, name string, fields []ProjectField, a answer, fromCache bool) (*Bundle, error, bool) {
	at := matchFields(name, fields)
	if len(at) != 1 {
		if fromCache {
			return nil, nil, true
		}
		return nil, unresolvedName(a, code, name, fields, at), false
	}
	found := fields[at[0]]
	if !isInternalID(found.ID) {
		if fromCache {
			return nil, nil, true
		}
		return nil, a.invalid(fmt.Sprintf("the id %s of a custom field is not two numbers with a dash between them", quote(found.ID))), false
	}
	kind, known := found.Type.kind()
	if !known {
		if fromCache {
			return nil, nil, true
		}
		return nil, a.invalid(unmodelled(found.Type)), false
	}
	asked := []field{fieldNaming(), {name: canBeEmptyKey}}
	if kind.bundle {
		asked = append(asked, field{name: "bundle", children: []field{{name: "values", children: []field{{name: idKey}, {name: nameKey}, {name: "archived"}}}}})
	}
	read, err := c.read(ctx, func(ctx context.Context) (*http.Response, error) {
		return c.apiGetProjectCustomField(ctx, code, found.ID, formatFields(asked))
	})
	if err != nil {
		return nil, err, fromCache && isStale(err)
	}
	object, err := read.object()
	if err != nil {
		return nil, err, fromCache
	}
	naming, ok := readFieldNaming(object["field"])
	if !ok {
		return nil, read.invalid(brokenFieldInfo), fromCache
	}
	canBeEmpty, isFlag := object[canBeEmptyKey].(bool)
	if !isFlag {
		return nil, read.invalid(brokenBinding), fromCache
	}
	if naming != namingOf(found) {
		if fromCache {
			return nil, nil, true
		}
		return nil, &ChangedFieldError{Request: read.request, Project: code, Field: found.Name, Body: read.body}, false
	}
	if !kind.bundle {
		reason := fmt.Sprintf("is a %s field, and a %s field holds no bundle of values", found.Type, found.Type.ValueType)
		return nil, &ArgumentError{Argument: "field", Value: name, Reason: reason}, false
	}
	values, err := readBundleValues(read, object["bundle"])
	if err != nil {
		return nil, err, false
	}
	found.CanBeEmpty = canBeEmpty
	return &Bundle{Field: found, Values: values}, nil, false
}

// A field the server no longer finds under its cached id and an answer that does not fit are what a stale cache looks like.
func isStale(err error) bool {
	var status *StatusError
	var response *ResponseError
	return errors.As(err, &status) && status.Status == http.StatusNotFound || errors.As(err, &response)
}

func unresolvedName(a answer, code, name string, fields []ProjectField, at []int) error {
	failed := &FieldNameError{Request: a.request, Project: code, Known: allFieldNames(fields)}
	if len(at) > 1 {
		failed.Ambiguous = []AmbiguousName{{Name: name, Candidates: fieldNames(fields, at)}}
	} else {
		failed.Unknown = []string{name}
	}
	return failed
}

func readBundleValues(a answer, value any) ([]BundleValue, error) {
	bundle, isObject := value.(map[string]any)
	if !isObject {
		return nil, a.invalid("the bundle of the custom field is not a JSON object")
	}
	items, isList := bundle["values"].([]any)
	if !isList {
		return nil, a.invalid("the values of the bundle are not a JSON array")
	}
	values := make([]BundleValue, 0, len(items))
	for _, item := range items {
		object, isObject := item.(map[string]any)
		if !isObject {
			return nil, a.invalid("a value of the bundle is not a JSON object")
		}
		id, isID := object[idKey].(string)
		name, isName := object[nameKey].(string)
		archived, isFlag := object["archived"].(bool)
		if !isID || !isName || !isFlag {
			return nil, a.invalid("a value of the bundle is not of the shape the specification gives it")
		}
		values = append(values, BundleValue{ID: id, Name: name, Archived: archived})
	}
	return values, nil
}
