package youtrack

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
)

const (
	linkSchema              = "IssueLink"
	linksKey                = "links"
	issuesKey               = "issues"
	directionKey            = "direction"
	linkTypeKey             = "linkType"
	issuesSizeKey           = "issuesSize"
	sourceToTarget          = "sourceToTarget"
	targetToSource          = "targetToSource"
	localizedSourceToTarget = "localizedSourceToTarget"
	localizedTargetToSource = "localizedTargetToSource"
	inward                  = "INWARD"
	outward                 = "OUTWARD"
	both                    = "BOTH"
)

const LinkListFields = "idReadable,summary"

// ListLinksOptions: Fields is a fields= expression of each linked issue, empty for LinkListFields and +x for them
// and x.
type ListLinksOptions struct {
	Fields string
}

// List has no page: the links come with the issue, and truncated says the instance sent fewer linked issues than
// its links hold.
func (s *LinksService) List(ctx context.Context, id string, opts *ListLinksOptions) (*Node, error) {
	return result(s.list(ctx, id, optionsOf(opts)))
}

// Add matches the phrase in any letter case and in the translation of the instance; its exact spelling decides
// between two links that answer to it.
func (s *LinksService) Add(ctx context.Context, id, phrase, target string, opts *WriteOptions) (*Node, error) {
	return result(s.add(ctx, id, phrase, target, optionsOf(opts)))
}

// Remove names a link from either end: DEV-1 "depends on" DEV-2 and DEV-2 "is required for" DEV-1 are one link.
func (s *LinksService) Remove(ctx context.Context, id, phrase, target string) (*Node, error) {
	return result(s.remove(ctx, id, phrase, target))
}

func issueLinkFields() []string {
	return []string{linksKey, "parent", "subtasks"}
}

func (s *LinksService) list(ctx context.Context, id string, opts ListLinksOptions) (*Node, *Error) {
	id, fault := parseIssueID(id)
	if fault != nil {
		return nil, fault
	}
	c := s.client
	requested, fault := linkFields(c.spec, opts.Fields)
	if fault != nil {
		return nil, fault
	}
	asked := []requestedField{{name: linksKey, children: linkDocumentFields(linkRequest(c.spec, requested), targetFields(requested))}}
	decoded, fault := c.request(ctx, issueSchema, asked, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetIssue(ctx, id, fields, nil)
	})
	if fault != nil {
		return nil, fault
	}
	return newConverter(decoded, inlineLayout).linkDocument(targetFields(requested), decoded.objects[0])
}

func (s *LinksService) add(ctx context.Context, id, phrase, target string, opts WriteOptions) (*Node, *Error) {
	id, target, fault := parseLinkWrite(id, phrase, target)
	if fault != nil {
		return nil, fault
	}
	c := s.client
	requested, fault := linkFields(c.spec, opts.Fields)
	if fault != nil {
		return nil, fault
	}
	w, fault := c.prepareLinkWrite(ctx, id, phrase, target)
	if fault != nil {
		return nil, fault
	}
	body, _ := json.Marshal(idBody{ID: w.target.id})
	node, fault := writeAs(ctx, c, issueSchema, linkWriteFields(targetOutputFields(linkRequest(c.spec, requested))),
		func(ctx context.Context, fields string) (*http.Response, error) {
			return c.apiAddLinkedIssue(ctx, w.source.readable, w.link.id, body, fields)
		}, w.verify, w.renderResult(targetFields(requested)))
	if fault != nil {
		return nil, w.withLinkDetails(fault)
	}
	return node, nil
}

func (s *LinksService) remove(ctx context.Context, id, phrase, target string) (*Node, *Error) {
	id, target, fault := parseLinkWrite(id, phrase, target)
	if fault != nil {
		return nil, fault
	}
	c := s.client
	w, fault := c.prepareLinkWrite(ctx, id, phrase, target)
	if fault != nil {
		return nil, fault
	}
	if fault := writeEmpty(ctx, func(ctx context.Context) (*http.Response, error) {
		return c.apiRemoveLinkedIssue(ctx, w.source.readable, w.link.id, w.target.id)
	}); fault != nil {
		return nil, w.withLinkDetails(notFoundAs(fault, noLinkToRemove))
	}
	return w.removed(), nil
}

func parseLinkWrite(id, phrase, target string) (string, string, *Error) {
	id, fault := parseIssueID(id)
	if fault != nil {
		return "", "", fault
	}
	target, fault = parseIssueID(target)
	if fault != nil {
		return "", "", fault
	}
	if fault := rejectReplaced("the phrase", phrase, emptyPhrase, nil); fault != nil {
		return "", "", fault
	}
	if strings.EqualFold(id, target) {
		return "", "", &Error{Code: CodeBadUsage, Message: oneIssue}
	}
	return id, target, nil
}

func linkFields(spec *schemas, expression string) ([]requestedField, *Error) {
	written, target, fault := parseFields(expression, LinkListFields, false)
	if fault != nil {
		return nil, fault
	}
	requested := []requestedField{
		{name: linksKey, children: []requestedField{{name: issuesKey, children: target}}},
	}
	if fault := issueCommentTarget().reject(spec, written, requested, issueCommentTarget().commentsOfAList()); fault != nil {
		return nil, fault
	}
	if fault := rejectCustomFieldNames(spec, issueSchema, written, requested); fault != nil {
		return nil, fault
	}
	if fault := rejectLinkParts(spec, issueSchema, written, requested); fault != nil {
		return nil, fault
	}
	return requested, nil
}

const emptyPhrase = "is empty, and a link is named by the phrase it goes by from the issue, such as " +
	`"depends on"`

func (n converter) linkDocument(target []requestedField, issue map[string]any) (*Node, *Error) {
	block, held, printed, fault := n.linkListing(target, issue[linksKey])
	if fault != nil {
		return nil, fault
	}
	pairs := counters(held, held.truncationAt(printed), printed)
	return NewMap(append(pairs, Pair{Key: linksKey, Value: block})...), nil
}

func linkRequest(spec *schemas, requested []requestedField) []requestedField {
	asked := cloneFields(requested)
	issueBlocks(spec, composedIssue(), asked)
	return asked[0].children
}

func targetFields(requested []requestedField) []requestedField {
	for _, field := range requested {
		if field.name == linksKey {
			return targetOutputFields(field.children)
		}
	}
	return nil
}

func (n converter) linkListing(target []requestedField, value any) (*Node, count, int, *Error) {
	links, fault := n.issueLinks(value)
	if fault != nil {
		return nil, count{}, 0, fault
	}
	held := 0
	for _, link := range links {
		size, isCount := parseInt64(link[issuesSizeKey])
		if !isCount || size < 0 {
			return nil, count{}, 0, n.response.invalid("how many issues a link of the issue holds is no whole number of them")
		}
		held += int(size)
	}
	block, received, printed, fault := n.linkBlock(target, links)
	if fault != nil {
		return nil, count{}, 0, fault
	}
	if held < received {
		message := fmt.Sprintf("the issues linked to arrived %d at a time and the links of the issue hold %d of "+
			"them in all", received, held)
		return nil, count{}, 0, n.response.invalid(message)
	}
	return block, counted(held), printed, nil
}

func eachIssueLink(spec *schemas, at string, requested []requestedField, visit func(parents []string, field *requestedField)) {
	for _, name := range issueLinkFields() {
		fieldsNamed(spec, at, issueSchema, name, requested, visit)
	}
}

func rejectLinkParts(spec *schemas, at, expression string, requested []requestedField) *Error {
	var fault *Error
	eachIssueLink(spec, at, requested, func(parents []string, field *requestedField) {
		for _, child := range field.children {
			if child.name == issuesKey || fault != nil {
				continue
			}
			message := fmt.Sprintf("fields %s: %s is printed as the phrase each link goes by against the issues "+
				"it holds, so %s is the only name that stands under it",
				quote(expression), fieldPath(parents, field.name), issuesKey)
			fault = &Error{Code: CodeBadUsage, Message: message}
		}
	})
	return fault
}

func linkRequestFields(callers []requestedField) []requestedField {
	return withFields([]requestedField{{name: issuesKey, children: targetOutputFields(callers)}}, phraseFields()...)
}

func phraseFields() []requestedField {
	return []requestedField{
		{name: directionKey},
		{name: linkTypeKey, children: []requestedField{
			{name: sourceToTarget},
			{name: targetToSource},
		}},
	}
}

func linkDocumentFields(asked, target []requestedField) []requestedField {
	own := append(phraseFields(),
		requestedField{name: issuesSizeKey},
		requestedField{name: issuesKey, children: target})
	return withFields(asked, own...)
}

func targetOutputFields(callers []requestedField) []requestedField {
	for _, field := range callers {
		if field.name == issuesKey && field.children != nil {
			return cloneFields(field.children)
		}
	}
	return []requestedField{{name: idReadableKey}}
}

func (n converter) links(field requestedField, value any) (*Node, *Error) {
	if value == nil {
		return NewNull(), nil
	}
	links, fault := n.issueLinks(value)
	if fault != nil {
		return nil, fault
	}
	block, _, _, fault := n.linkBlock(targetOutputFields(field.children), links)
	return block, fault
}

func (n converter) linkBlock(target []requestedField, links []map[string]any) (*Node, int, int, *Error) {
	received, printed := 0, 0
	printedBy := make(map[string]bool, len(links))
	pairs := make([]Pair, 0, len(links))
	for _, link := range links {
		targets, fault := n.targets(link)
		if fault != nil {
			return nil, 0, 0, fault
		}
		if len(targets) == 0 {
			continue
		}
		phrase, fault := n.phrase(link)
		if fault != nil {
			return nil, 0, 0, fault
		}
		if printedBy[phrase] {
			return nil, 0, 0, n.response.invalid(fmt.Sprintf("two links of the issue go by the phrase %s", quote(phrase)))
		}
		printedBy[phrase] = true
		received += len(targets)
		records, fault := n.objectsAt(issueSchema, target, targets)
		if fault != nil {
			return nil, 0, 0, fault
		}
		printed += len(records)
		pairs = append(pairs, DataPair(phrase, NewList(records...)))
	}
	return NewMap(pairs...), received, printed, nil
}

func (n converter) issueLinks(value any) ([]map[string]any, *Error) {
	received := []any{value}
	if list, isList := value.([]any); isList {
		received = list
	}
	links := make([]map[string]any, 0, len(received))
	for _, item := range received {
		link, isObject := item.(map[string]any)
		if !isObject {
			return nil, n.response.invalid("a link of the issue is not a JSON object")
		}
		links = append(links, link)
	}
	return links, nil
}

func (n converter) targets(link map[string]any) ([]map[string]any, *Error) {
	received, isList := link[issuesKey].([]any)
	if !isList {
		return nil, n.response.invalid("the issues of a link of the issue arrived as something other than an array")
	}
	targets := make([]map[string]any, 0, len(received))
	for _, item := range received {
		target, isObject := item.(map[string]any)
		if !isObject {
			return nil, n.response.invalid("an issue at the other end of a link of the issue is not a JSON object")
		}
		targets = append(targets, target)
	}
	return targets, nil
}

func (n converter) phrase(link map[string]any) (string, *Error) {
	direction, isText := link[directionKey].(string)
	if !isText {
		return "", n.response.invalid("the direction of a link of the issue is not text")
	}
	kind, isObject := link[linkTypeKey].(map[string]any)
	if !isObject {
		return "", n.response.invalid("the type of a link of the issue is not a JSON object")
	}
	read := phraseKeys(direction)[0]
	phrase, isText := kind[read].(string)
	if !isText {
		return "", n.response.invalid(fmt.Sprintf("the %s of a link type of the issue is not text", read))
	}
	if phrase == "" {
		return "", n.response.invalid("a link of the issue holds issues and the phrase it goes by is empty")
	}
	return phrase, nil
}

func (c *Client) prepareLinkWrite(ctx context.Context, id, phrase, target string) (linkWrite, *Error) {
	source, fault := c.readSourceIssue(ctx, id)
	if fault != nil {
		return linkWrite{}, fault
	}
	link, fault := source.linkFor(phrase)
	if fault != nil {
		return linkWrite{}, fault
	}
	other, fault := c.readTargetIssue(ctx, target)
	if fault != nil {
		return linkWrite{}, fault
	}
	if other.id == source.id {
		return linkWrite{}, other.a.fault(CodeBadUsage, oneIssue,
			Pair{Key: "issue", Value: NewString(source.readable)},
			Pair{Key: "target", Value: NewString(other.readable)})
	}
	return linkWrite{source: source, target: other, link: link}, nil
}

const noLinkToRemove = "the issue holds no link under that phrase to the target issue, and a link is taken away " +
	"from the end the phrase names"

const oneIssue = "the issue and the target issue are one issue, and YouTrack answers a link of an issue to itself " +
	"with a 200 and writes nothing"

type linkIssue struct {
	a        decodedResponse
	id       string
	readable string
	links    []issueLink
}

type issueLink struct {
	id        string
	direction string
	kind      string
	phrase    string
	names     []string
}

func linkCatalogueFields() []requestedField {
	return []requestedField{
		{name: idKey},
		{name: idReadableKey},
		{name: linksKey, children: []requestedField{
			{name: idKey},
			{name: directionKey},
			{name: linkTypeKey, children: []requestedField{
				{name: idKey},
				{name: sourceToTarget},
				{name: targetToSource},
				{name: localizedSourceToTarget},
				{name: localizedTargetToSource},
			}},
		}},
	}
}

func (c *Client) readSourceIssue(ctx context.Context, id string) (linkIssue, *Error) {
	read, fault := c.readLinkedIssue(ctx, id, linkCatalogueFields())
	if fault != nil {
		return linkIssue{}, fault
	}
	links, fault := newConverter(read.a, inlineLayout).parseIssueLinks(read.a.objects[0][linksKey])
	if fault != nil {
		return linkIssue{}, fault
	}
	read.links = links
	return read, nil
}

func (c *Client) readTargetIssue(ctx context.Context, id string) (linkIssue, *Error) {
	return c.readLinkedIssue(ctx, id, []requestedField{{name: idKey}, {name: idReadableKey}})
}

func (c *Client) readLinkedIssue(ctx context.Context, id string, requested []requestedField) (linkIssue, *Error) {
	a, fault := c.request(ctx, issueSchema, requested, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetIssue(ctx, id, fields, nil)
	})
	if fault != nil {
		return linkIssue{}, fault
	}
	readable, fault := readableIDOf(a, issueOwner, "a link")
	if fault != nil {
		return linkIssue{}, fault
	}
	internal, isText := a.objects[0][idKey].(string)
	if !isText || !isInternalID(internal) {
		message := "the issue arrived with something other than an internal id of the instance for an id, and " +
			"that is what YouTrack takes an issue at the other end of a link by"
		return linkIssue{}, a.invalid(message)
	}
	return linkIssue{a: a, id: internal, readable: readable.String()}, nil
}

func (n converter) parseIssueLinks(value any) ([]issueLink, *Error) {
	received, fault := n.parseLinks(value)
	if fault != nil {
		return nil, fault
	}
	links := make([]issueLink, 0, len(received))
	for _, link := range received {
		id, isText := link.raw[idKey].(string)
		if !isText {
			return nil, n.response.invalid("the id of a link of the issue is not text")
		}
		phrase, named, fault := n.linkNames(link.direction, link.kind)
		if fault != nil {
			return nil, fault
		}
		links = append(links, issueLink{id: id, direction: link.direction, kind: link.typeID, phrase: phrase, names: named})
	}
	return links, nil
}

type parsedLink struct {
	raw       map[string]any
	direction string
	kind      map[string]any
	typeID    string
}

func (n converter) parseLinks(value any) ([]parsedLink, *Error) {
	received, fault := n.issueLinks(value)
	if fault != nil {
		return nil, fault
	}
	links := make([]parsedLink, 0, len(received))
	for _, held := range received {
		direction, isEnd := held[directionKey].(string)
		if !isEnd {
			return nil, n.response.invalid("the direction of a link of the issue is not text")
		}
		kind, isObject := held[linkTypeKey].(map[string]any)
		if !isObject {
			return nil, n.response.invalid("the type of a link of the issue is not a JSON object")
		}
		typeID, isText := kind[idKey].(string)
		if !isText {
			return nil, n.response.invalid("the id of a link type of the issue is not text")
		}
		links = append(links, parsedLink{raw: held, direction: direction, kind: kind, typeID: typeID})
	}
	return links, nil
}

func (n converter) linkNames(direction string, kind map[string]any) (string, []string, *Error) {
	read := phraseKeys(direction)
	phrase := ""
	var named []string
	for i, name := range read {
		text, isText := kind[name].(string)
		if !isText && kind[name] != nil {
			return "", nil, n.response.invalid(fmt.Sprintf("the %s of a link type of the issue is neither text nor null", name))
		}
		if i == 0 {
			phrase = text
		}
		if text != "" {
			named = append(named, text)
		}
	}
	return phrase, named, nil
}

func phraseKeys(direction string) []string {
	switch direction {
	case inward:
		return []string{targetToSource, localizedTargetToSource}
	case both:
		return []string{sourceToTarget, localizedSourceToTarget, targetToSource, localizedTargetToSource}
	}
	return []string{sourceToTarget, localizedSourceToTarget}
}

func (s linkIssue) linkFor(phrase string) (issueLink, *Error) {
	named := s.matchLinks(phrase)
	if twin, alike := duplicatePhrase(named); alike {
		message := fmt.Sprintf("two links of the issue go by the phrase %s, and neither of them can be named "+
			"by it", quote(twin))
		return issueLink{}, s.fault(CodeUpstreamInvalid, message,
			Pair{Key: "phrase", Value: NewString(twin)})
	}
	link, found := soleMatch(named, phrase, func(link issueLink) string { return link.phrase })
	if !found {
		return issueLink{}, s.unknownPhrase(phrase, named)
	}
	if !link.hasValidID() {
		return issueLink{}, s.fault(CodeUpstreamInvalid, unreadableLink,
			Pair{Key: "phrase", Value: NewString(link.phrase)})
	}
	return link, nil
}

func (s linkIssue) matchLinks(phrase string) []issueLink {
	var named []issueLink
	for _, link := range s.links {
		if link.phrase == "" {
			continue
		}
		if slices.ContainsFunc(link.names, func(name string) bool { return strings.EqualFold(name, phrase) }) {
			named = append(named, link)
		}
	}
	return named
}

func duplicatePhrase(named []issueLink) (string, bool) {
	seen := make(map[string]bool, len(named))
	for _, link := range named {
		if seen[link.phrase] {
			return link.phrase, true
		}
		seen[link.phrase] = true
	}
	return "", false
}

func (s linkIssue) unknownPhrase(phrase string, named []issueLink) *Error {
	nearby := canonicalPhrases(named)
	if len(named) == 0 {
		among := make([]suggestion, 0, len(s.links))
		for _, link := range s.links {
			if link.phrase != "" {
				among = append(among, suggestion{name: link.phrase, also: link.names[1:]})
			}
		}
		nearby = nearest(phrase, among, canonicalPhrases(s.links))
	}
	entry := nearestEntry("phrase", phrase, nearby)
	return s.fault(CodeUnknownName, unknownPhrase, Pair{Key: "unknown", Value: NewList(entry)})
}

const unknownPhrase = "the phrase under unknown is no phrase a link of the issue goes by"

func canonicalPhrases(links []issueLink) []string {
	phrases := make([]string, 0, len(links))
	for _, link := range links {
		if link.phrase != "" {
			phrases = append(phrases, link.phrase)
		}
	}
	slices.Sort(phrases)
	return phrases
}

func (link issueLink) hasValidID() bool {
	number, rest, dashed := strings.Cut(link.id, "-")
	if !dashed || !digits(number) {
		return false
	}
	switch link.direction {
	case both:
		return digits(rest)
	case outward:
		return strings.HasSuffix(rest, "s") && digits(strings.TrimSuffix(rest, "s"))
	case inward:
		return strings.HasSuffix(rest, "t") && digits(strings.TrimSuffix(rest, "t"))
	}
	return false
}

const unreadableLink = "the server addresses the link by an id whose end cannot be read: a link an issue " +
	"stands at either end of is addressed by digits, a dash and digits, one it stands at the source of by the " +
	"same and an s, and one it stands at the target of by the same and a t"

func (s linkIssue) fault(code Code, message string, own ...Pair) *Error {
	return s.a.fault(code, message, append([]Pair{{Key: "issue", Value: NewString(s.readable)}}, own...)...)
}

type linkWrite struct {
	source linkIssue
	target linkIssue
	link   issueLink
}

func linkWriteFields(target []requestedField) []requestedField {
	source := []requestedField{
		{name: idKey},
		{name: linksKey, children: linkDocumentFields(responseLinkFields(),
			withFields([]requestedField{{name: idKey}}, target...))},
	}
	return []requestedField{
		{name: idKey},
		{name: linksKey, children: append(responseLinkFields(),
			requestedField{name: issuesKey, children: source})},
	}
}

func responseLinkFields() []requestedField {
	return []requestedField{
		{name: directionKey},
		{name: linkTypeKey, children: []requestedField{{name: idKey}}},
	}
}

func (w linkWrite) verify(a decodedResponse) *Error {
	source, fault := w.findSource(a)
	if fault != nil {
		return fault
	}
	links, fault := newConverter(a, inlineLayout).responseLinks(source[linksKey])
	if fault != nil {
		return fault
	}
	if _, held := findIssue(links, w.link.kind, w.link.direction, w.target.id); !held {
		return a.fault(CodeUpstreamInvalid, "the issue does not hold the target issue at the end of the link the phrase names")
	}
	return nil
}

func (w linkWrite) findSource(a decodedResponse) (map[string]any, *Error) {
	if id, isText := a.objects[0][idKey].(string); !isText || id != w.target.id {
		return nil, a.fault(CodeUpstreamInvalid, "the write was answered with an issue other than the target issue it named")
	}
	links, fault := newConverter(a, inlineLayout).responseLinks(a.objects[0][linksKey])
	if fault != nil {
		return nil, fault
	}
	source, held := findIssue(links, w.link.kind, opposite(w.link.direction), w.source.id)
	if !held {
		return nil, a.fault(CodeUpstreamInvalid, "the target issue does not hold the issue at the end the other side of the link is read from")
	}
	return source, nil
}

func (w linkWrite) removed() *Node {
	record := NewMap(Pair{Key: idReadableKey, Value: NewString(w.target.readable)})
	return NewMap(
		Pair{Key: idReadableKey, Value: NewString(w.source.readable)},
		Pair{Key: removedKey, Value: NewMap(
			DataPair(w.link.phrase, NewList(record)))})
}

func (w linkWrite) renderResult(printed []requestedField) func(decodedResponse) (*Node, *Error) {
	return func(a decodedResponse) (*Node, *Error) {
		source, fault := w.findSource(a)
		if fault != nil {
			return nil, fault
		}
		return newConverter(a, inlineLayout).linkDocument(printed, source)
	}
}

type responseLink struct {
	direction string
	kind      string
	issues    []map[string]any
}

func (n converter) responseLinks(value any) ([]responseLink, *Error) {
	received, fault := n.parseLinks(value)
	if fault != nil {
		return nil, fault
	}
	links := make([]responseLink, 0, len(received))
	for _, link := range received {
		issues, fault := n.targets(link.raw)
		if fault != nil {
			return nil, fault
		}
		links = append(links, responseLink{direction: link.direction, kind: link.typeID, issues: issues})
	}
	return links, nil
}

func findIssue(links []responseLink, kind, direction, id string) (map[string]any, bool) {
	for _, link := range links {
		if link.kind != kind || link.direction != direction {
			continue
		}
		for _, issue := range link.issues {
			if held, isText := issue[idKey].(string); isText && held == id {
				return issue, true
			}
		}
	}
	return nil, false
}

func opposite(direction string) string {
	switch direction {
	case inward:
		return outward
	case outward:
		return inward
	}
	return direction
}

func (w linkWrite) withLinkDetails(fault *Error) *Error {
	fault.Details = insertAfterRequest(fault.Details,
		Pair{Key: "issue", Value: NewString(w.source.readable)},
		Pair{Key: "phrase", Value: NewString(w.link.phrase)},
		Pair{Key: "target", Value: NewString(w.target.readable)})
	return fault
}
