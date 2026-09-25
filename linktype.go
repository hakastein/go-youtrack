package youtrack

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
)

const linkTypeCatalogue = "[]IssueLinkType"

const topAllCutOff = 1000

// An activity names a link end by its translated, capitalized phrase, whatever Accept-Language asks for.
type linkPhrases map[string][]string

func linkTypeFields() []requestedField {
	return []requestedField{
		{name: sourceToTarget},
		{name: targetToSource},
		{name: localizedSourceToTarget},
		{name: localizedTargetToSource},
	}
}

func (c *Client) linkPhrases(ctx context.Context) (linkPhrases, *Error) {
	a, fault := c.request(ctx, linkTypeCatalogue, linkTypeFields(), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetIssueLinkTypes(ctx, fields, topAllCutOff)
	})
	if fault != nil {
		return nil, fault
	}
	if len(a.objects) >= topAllCutOff {
		message := fmt.Sprintf("the instance answered with as many link types as were asked for, %d, so the "+
			"phrases of any past them are missing", topAllCutOff)
		return nil, a.invalid(message)
	}
	phrases := linkPhrases{}
	for _, kind := range a.objects {
		for _, end := range [][2]string{
			{sourceToTarget, localizedSourceToTarget},
			{targetToSource, localizedTargetToSource},
		} {
			label, phrase, held, reason := linkEnd(kind, end[0], end[1])
			if reason != "" {
				return nil, a.invalid(reason)
			}
			if held {
				phrases.add(label, phrase)
			}
		}
	}
	return phrases, nil
}

func linkEnd(kind map[string]any, plain, translated string) (label, phrase string, held bool, reason string) {
	phrase, reason = linkText(kind, plain)
	if reason != "" || phrase == "" {
		return "", "", false, reason
	}
	label, reason = linkText(kind, translated)
	if reason != "" {
		return "", "", false, reason
	}
	if label == "" {
		label = phrase
	}
	return label, phrase, true, ""
}

func linkText(kind map[string]any, name string) (string, string) {
	switch value := kind[name].(type) {
	case nil:
		return "", ""
	case string:
		return value, ""
	}
	return "", fmt.Sprintf("the %s of a link type of the instance arrived as something other than text", name)
}

func (p linkPhrases) add(label, phrase string) {
	folded := strings.ToLower(label)
	if !slices.Contains(p[folded], phrase) {
		p[folded] = append(p[folded], phrase)
	}
}

func (p linkPhrases) phrase(label string) (string, string) {
	found := p[strings.ToLower(label)]
	switch len(found) {
	case 1:
		return found[0], ""
	case 0:
		return "", fmt.Sprintf("an activity arrived for a link the instance writes %s, and no link type of it "+
			"goes by that phrase", quote(label))
	}
	return "", fmt.Sprintf("an activity arrived for a link the instance writes %s, and %d link types of it go "+
		"by that phrase", quote(label), len(found))
}
