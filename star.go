package youtrack

import (
	"context"
	"fmt"
	"net/http"
	"slices"
)

const (
	profileSchema = "GeneralUserProfile"
	starKey       = "star"
	watchersKey   = "watchers"
	hasStarKey    = "hasStar"
)

func starTarget() string {
	return "/api/users/me/profiles/general?fields=" + formatFields(starFields())
}

func starFields() []requestedField {
	return []requestedField{{name: starKey, children: []requestedField{{name: idKey}}}}
}

// starID is the id of the tag that is the star of the token, empty where the user has none, and whether the cache
// gave it. The cache answers first unless fresh is asked for.
func (c *Client) starID(ctx context.Context, fresh bool) (string, bool, *Error) {
	if !fresh {
		if id, hit := c.cache.loadStar(starTarget()); hit {
			return id, true, nil
		}
	}
	a, fault := c.request(ctx, profileSchema, starFields(), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetCurrentUserProfile(ctx, fields)
	})
	if fault != nil {
		return "", false, fault
	}
	id := ""
	if star := a.objects[0][starKey]; star != nil {
		text, isText := memberOf(star, idKey).(string)
		if !isText {
			return "", false, a.invalid("the star of the user arrived with an id other than a string")
		}
		id = text
	}
	c.cache.storeStar(starTarget(), id)
	return id, false, nil
}

// rejectStar refuses the star of the token among tags hung on an issue: the server keeps it apart from the tags of
// the issue, and a write of it is not seen there. A cached star that matches is read again before the refusal.
func (c *Client) rejectStar(ctx context.Context, tags []hungTag) *Error {
	if len(tags) == 0 {
		return nil
	}
	for _, fresh := range []bool{false, true} {
		star, cached, fault := c.starID(ctx, fresh)
		if fault != nil {
			return fault
		}
		found := slices.IndexFunc(tags, func(tag hungTag) bool { return star != "" && tag.id == star })
		if found < 0 {
			return nil
		}
		if !cached {
			return &Error{Code: CodeBadUsage, Message: fmt.Sprintf(starAsTag, quote(tags[found].name))}
		}
	}
	return nil
}

const starAsTag = "the tag %s is the star of the user, which YouTrack keeps apart from the tags of an issue: the " +
	"star of an issue is written as its star"

type watchersBody struct {
	HasStar bool `json:"hasStar"`
}

func starBody(star *bool) *watchersBody {
	if star == nil {
		return nil
	}
	return &watchersBody{HasStar: *star}
}

func starFieldsToVerify() requestedField {
	return requestedField{name: watchersKey, children: []requestedField{{name: hasStarKey}}}
}

func hasStar(issue map[string]any) (bool, bool) {
	held, isBool := memberOf(issue[watchersKey], hasStarKey).(bool)
	return held, isBool
}

// verifyStar: a new issue the call files without a star may come back starred, as the profile of the user stars
// every issue the user files; the star is then taken off by a second write.
func (w issueWrite) verifyStar(issue map[string]any, wrong []mismatch) []mismatch {
	if w.text.star == nil {
		return wrong
	}
	held, isBool := hasStar(issue)
	if isBool && (held == *w.text.star || (w.creating && !*w.text.star)) {
		return wrong
	}
	return append(wrong, mismatch{field: watchersKey + "." + hasStarKey, expected: NewBool(*w.text.star),
		actual: rawValueNode(memberOf(issue[watchersKey], hasStarKey))})
}

// unstarCreated takes off the star the server put on an issue the call filed without one. The issue stands: a
// fault here carries the issue it filed, and the write that filed it marks the fault as after the write.
func (c *Client) unstarCreated(ctx context.Context, filed decodedResponse, asked []requestedField,
	output func(decodedResponse) (*Node, *Error),
) (*Node, *Error) {
	issue := responseID(filed, idReadableKey)
	at, fault := readableIDOf(filed, issueOwner, "the write of its star")
	if fault != nil {
		return nil, fault
	}
	off := false
	unstar := issueWrite{text: issueInput{star: &off}}
	body := unstar.updateBody()
	node, fault := writeAs(ctx, c, issueSchema, asked, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiUpdateIssue(ctx, at, body, fields)
	}, unstar.verify, output)
	if fault != nil {
		if !hasDetail(fault, issueKey) {
			fault.Details = append(fault.Details, Pair{Key: issueKey, Value: issue})
		}
	}
	return node, fault
}

func hasDetail(fault *Error, key string) bool {
	for _, pair := range fault.Details {
		if pair.Key == key {
			return true
		}
	}
	return false
}
