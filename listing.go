package youtrack

import (
	"context"
	"fmt"
	"math"
	"net/http"
)

const topAll = -1

// DefaultLimit is how many records a page holds when Page.Limit is zero.
const DefaultLimit = 50

// Page is a window over a list: Skip records passed over, then at most Limit records.
type Page struct {
	Skip  int
	Limit int
}

func (p Page) parse() (Page, *Error) {
	return p.validate(math.MaxInt32)
}

func (p Page) validate(most int) (Page, *Error) {
	if p.Limit == 0 {
		p.Limit = min(DefaultLimit, most)
	}
	if p.Limit < 1 || p.Limit > most {
		message := fmt.Sprintf("limit %d is not between 1 and %d", p.Limit, most)
		return Page{}, &Error{Code: CodeBadUsage, Message: message}
	}
	if p.Skip < 0 || p.Skip > math.MaxInt32 {
		message := fmt.Sprintf("skip %d is not between 0 and %d", p.Skip, math.MaxInt32)
		return Page{}, &Error{Code: CodeBadUsage, Message: message}
	}
	return p, nil
}

func (p Page) mayHaveSkippedPastTheEnd(returned int) bool {
	return returned == 0 && p.Skip > 0
}

func (p Page) window() window {
	return window{top: int32(p.Limit), skip: int32(p.Skip)}
}

type window struct {
	top  int32
	skip int32
}

var allRecords = window{top: topAll}

func (w window) skipped() *int32 {
	if w.skip == 0 {
		return nil
	}
	return &w.skip
}

type pageFetcher func(ctx context.Context, fields string, w window) (*http.Response, error)

type count struct {
	total int
	known bool
}

func counted(total int) count {
	return count{total: total, known: true}
}

type truncation struct {
	left  bool
	known bool
}

func truncated(left bool) truncation {
	return truncation{left: left, known: true}
}

func (c count) truncationAt(shown int) truncation {
	return truncation{left: c.total > shown, known: c.known}
}

type list struct {
	client     *Client
	spec       *schemas
	plural     string
	schema     string
	requested  []requestedField
	page       Page
	fetchPage  pageFetcher
	sentFields []requestedField
	countTotal func(ctx context.Context) (count, *Error)
}

func (l list) requestFields() requestFields {
	if l.sentFields != nil {
		return requestFields{sent: l.sentFields, output: l.requested}
	}
	return requestFields{sent: l.requested, output: l.requested}
}

func (c *Client) listPage(ctx context.Context, spec *schemas, plural, schema string, requested []requestedField, page Page, fetchPage pageFetcher) (*Node, *Error) {
	return c.newList(spec, plural, schema, requested, nil, page, fetchPage).fetch(ctx)
}

func (c *Client) newList(spec *schemas, plural, schema string, requested, sentFields []requestedField, page Page, fetchPage pageFetcher) list {
	l := list{client: c, spec: spec, plural: plural, schema: schema, requested: requested, sentFields: sentFields, page: page, fetchPage: fetchPage}
	l.countTotal = func(ctx context.Context) (count, *Error) {
		ids, fault := c.read(ctx, spec, schema, []requestedField{{name: idKey}}, func(ctx context.Context, fields string) (*http.Response, error) {
			return fetchPage(ctx, fields, allRecords)
		})
		if fault != nil {
			return count{}, fault
		}
		return counted(len(ids)), nil
	}
	return l
}

func (l list) fetch(ctx context.Context) (*Node, *Error) {
	page, fault := l.client.readList(ctx, l.spec, l.schema, l.requestFields(), func(ctx context.Context, fields string) (*http.Response, error) {
		return l.fetchPage(ctx, fields, l.page.window())
	})
	if fault != nil {
		return nil, fault
	}
	returned := len(page)
	if returned > l.page.Limit {
		details := []Pair{{Key: "limit", Value: intNode(l.page.Limit)}, {Key: "returned", Value: intNode(returned)}}
		message := "more " + l.plural + " arrived than the limit asked for"
		return nil, &Error{Code: CodeUpstreamInvalid, Message: message, Details: details}
	}
	found := counted(l.page.Skip + returned)
	pageIsFull := returned == l.page.Limit
	if pageIsFull || l.page.mayHaveSkippedPastTheEnd(returned) {
		if found, fault = l.countTotal(ctx); fault != nil {
			return nil, fault
		}
		if found.known && returned > 0 && found.total < l.page.Skip+returned {
			details := []Pair{{Key: "total", Value: intNode(found.total)}, {Key: "returned", Value: intNode(returned)}}
			message := "fewer " + l.plural + " were counted than arrived: they changed between the requests"
			return nil, &Error{Code: CodeUpstreamFailed, Message: message, Details: details}
		}
	}
	return listDocument(l.plural, found, found.truncationAt(l.page.Skip+returned), page), nil
}

func countedListDocument(plural string, found count, records []*Node) *Node {
	return listDocument(plural, found, found.truncationAt(len(records)), records)
}

func truncatedListDocument(plural string, found count, left bool, records []*Node) *Node {
	return listDocument(plural, found, truncated(left), records)
}

func listDocument(plural string, found count, left truncation, records []*Node) *Node {
	return NewMap(append(counters(found, left, len(records)),
		Pair{Key: plural, Value: NewList(records...)})...)
}

func counters(found count, left truncation, returned int) []Pair {
	total, truncated := NewNull(), NewNull()
	if found.known {
		total = intNode(found.total)
	}
	if left.known {
		truncated = NewBool(left.left)
	}
	return []Pair{
		{Key: "total", Value: total},
		{Key: "returned", Value: intNode(returned)},
		{Key: "truncated", Value: truncated},
	}
}
