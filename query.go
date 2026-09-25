package youtrack

import (
	"context"
	"net/http"
	"strings"
)

const (
	suggestionsSchema = "SearchSuggestions"
	queryKey          = "query"
	styleRangesKey    = "styleRanges"
	startKey          = "start"
	lengthKey         = "length"
	styleKey          = "style"
	freeTextKey       = "free_text"
)

const freeTextStyle = "text"

const freeTextMessage = "part of the search names no field of the instance and is looked for as text"

type styleRange struct {
	utf16Start  int
	utf16Length int
	style       string
}

func markupFields() []requestedField {
	return []requestedField{
		{name: queryKey},
		{name: styleRangesKey, children: []requestedField{{name: startKey}, {name: lengthKey}, {name: styleKey}}},
	}
}

func (c *Client) searchMarkup(ctx context.Context, query string) ([]styleRange, *Error) {
	body := searchBody(query)
	decoded, fault := c.request(ctx, suggestionsSchema, markupFields(), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiAssistSearch(ctx, body, fields)
	})
	if fault != nil {
		return nil, fault
	}
	marked := decoded.objects[0]
	if echoed, isText := marked[queryKey].(string); !isText || echoed != query {
		message := "the search the markup came back with is not the one that was sent"
		return nil, shapeFailure(decoded.httpResponse, decoded.body, message)
	}
	received, isList := marked[styleRangesKey].([]any)
	if !isList {
		message := "the styled ranges of the search arrived as something other than an array"
		return nil, shapeFailure(decoded.httpResponse, decoded.body, message)
	}
	queryUnits := int64(utf16Units(query))
	ranges := make([]styleRange, 0, len(received))
	for _, item := range received {
		styled, isObject := item.(map[string]any)
		if !isObject {
			message := "a styled range of the search arrived as something other than an object"
			return nil, shapeFailure(decoded.httpResponse, decoded.body, message)
		}
		start, startIsWhole := parseInt64(styled[startKey])
		length, lengthIsWhole := parseInt64(styled[lengthKey])
		style, styleIsText := styled[styleKey].(string)
		switch {
		case !startIsWhole || !lengthIsWhole:
			message := "where a styled range of the search begins, or how far it runs on, is no whole number"
			return nil, shapeFailure(decoded.httpResponse, decoded.body, message)
		case !styleIsText:
			message := "the style of a range of the search arrived as something other than text"
			return nil, shapeFailure(decoded.httpResponse, decoded.body, message)
		case !rangeFitsWithoutOverflow(start, length, queryUnits):
			message := "a styled range of the search lies outside the text that was sent"
			return nil, shapeFailure(decoded.httpResponse, decoded.body, message)
		}
		ranges = append(ranges, styleRange{utf16Start: int(start), utf16Length: int(length), style: style})
	}
	return ranges, nil
}

func rangeFitsWithoutOverflow(start, length, total int64) bool {
	return start >= 0 && length >= 0 && start <= total-length
}

func freeTextWarning(query string, ranges []styleRange) *Warning {
	parts := freeText(query, ranges)
	if len(parts) == 0 {
		return nil
	}
	written := make([]*Node, 0, len(parts))
	for _, part := range parts {
		written = append(written, NewString(part))
	}
	details := []Pair{
		{Key: queryKey, Value: NewString(query)},
		{Key: freeTextKey, Value: NewList(written...)},
	}
	return &Warning{Code: CodeUnknownName, Message: freeTextMessage, Details: details}
}

func freeText(query string, ranges []styleRange) []string {
	var parts []string
	previousEnd := -1
	for _, marked := range ranges {
		if marked.style != freeTextStyle || marked.utf16Length == 0 {
			continue
		}
		part := utf16Substring(query, marked.utf16Start, marked.utf16Length)
		if marked.utf16Start == previousEnd {
			parts[len(parts)-1] += part
		} else {
			parts = append(parts, part)
		}
		previousEnd = marked.utf16Start + marked.utf16Length
	}
	return parts
}

func utf16Substring(text string, start, length int) string {
	var cut strings.Builder
	units := 0
	for _, r := range text {
		width := 1
		if r > 0xFFFF {
			width = 2
		}
		if units >= start && units+width <= start+length {
			cut.WriteRune(r)
		}
		units += width
	}
	return cut.String()
}

func utf16Units(text string) int {
	units := 0
	for _, r := range text {
		units++
		if r > 0xFFFF {
			units++
		}
	}
	return units
}
