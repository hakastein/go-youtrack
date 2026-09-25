package youtrack

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

const TagListFields = "name,owner(login),readSharingSettings(permittedGroups(name),permittedUsers(login))"

const TagCreateFields = TagListFields +
	",updateSharingSettings(permittedGroups(name),permittedUsers(login))" +
	",tagSharingSettings(permittedGroups(name),permittedUsers(login))"

const (
	tagsPlural = "tags"
	tagSchema  = "Tag"
	tagKey     = "tag"
	ownerKey   = "owner"
	ownedByKey = "owned_by"
)

const (
	groupSchema        = "UserGroup"
	groupKey           = "group"
	readSharingKey     = "readSharingSettings"
	updateSharingKey   = "updateSharingSettings"
	tagSharingKey      = "tagSharingSettings"
	permittedGroupsKey = "permittedGroups"
	visibleForFlag     = "--visible-for"
	updateableByFlag   = "--updateable-by"
	taggableByFlag     = "--taggable-by"
)

func ListTags(expression string, page Page) (Call, *Error) {
	page, fault := page.parse()
	if fault != nil {
		return nil, fault
	}
	requested, fault := tagFields(expression, TagListFields)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	return func(ctx context.Context, c *Client) (*Node, *Error) {
		return c.listTags(ctx, spec, requested, page)
	}, nil
}

func tagFields(expression string, defaults string) ([]requestedField, *Error) {
	if expression == "" {
		return parseDefault(defaults, false)
	}
	return parseFields(expression, defaults)
}

func (c *Client) listTags(ctx context.Context, spec *schemas, requested []requestedField, page Page) (*Node, *Error) {
	return c.listPage(ctx, spec, tagsPlural, "[]"+tagSchema, requested, page, c.apiGetTags)
}

func CreateTag(name string, shared TagSharing, expression string) (Call, *Error) {
	if fault := rejectNoTagName(name); fault != nil {
		return nil, fault
	}
	if fault := shared.rejectNoGroupName(); fault != nil {
		return nil, fault
	}
	requested, fault := tagFields(expression, TagCreateFields)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	return func(ctx context.Context, c *Client) (*Node, *Error) {
		return c.createTag(ctx, spec, name, shared, requested)
	}, nil
}

type TagSharing struct {
	VisibleFor  VisibleFor
	UpdatableBy UpdatableBy
	TaggableBy  TaggableBy
}

// The tag's visibleFor member holds one group and grants no right, so VisibleFor fills readSharingSettings.
type (
	VisibleFor  []string
	UpdatableBy []string
	TaggableBy  []string
)

func (shared TagSharing) rejectNoGroupName() *Error {
	for _, flag := range []struct {
		name   string
		values []string
	}{
		{name: visibleForFlag, values: shared.VisibleFor},
		{name: updateableByFlag, values: shared.UpdatableBy},
		{name: taggableByFlag, values: shared.TaggableBy},
	} {
		if slices.Contains(flag.values, "") {
			message := flag.name + " is empty, and YouTrack keeps no group under an empty name: it takes the " +
				"name of a group the tag is shared with"
			return &Error{Code: CodeBadUsage, Message: message}
		}
	}
	return nil
}

func (shared TagSharing) any() bool {
	return len(shared.VisibleFor) > 0 || len(shared.UpdatableBy) > 0 || len(shared.TaggableBy) > 0
}

func (c *Client) createTag(ctx context.Context, spec *schemas, name string, shared TagSharing, requested []requestedField) (*Node, *Error) {
	written, fault := c.resolveTagCreate(ctx, spec, name, shared)
	if fault != nil {
		return nil, fault
	}
	body := written.body()
	return c.write(ctx, spec, tagSchema, withFields(requested, written.verifyFields()...), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiCreateTag(ctx, body, fields)
	}, written.verify, writeResultNode(requested))
}

func (c *Client) resolveTagCreate(ctx context.Context, spec *schemas, name string, shared TagSharing) (tagCreate, *Error) {
	if !shared.any() {
		return tagCreate{name: name}, nil
	}
	catalogue, fault := c.listGroups(ctx, spec)
	if fault != nil {
		return tagCreate{}, fault
	}
	groups, fault := catalogue.resolve(shared)
	if fault != nil {
		return tagCreate{}, fault
	}
	return tagCreate{name: name, groups: groups}, nil
}

type tagCreate struct {
	name   string
	groups resolvedSharing
}

// The specification marks the three sharing members read-only, but the server writes them.
type createTagBody struct {
	Name                  string       `json:"name"`
	ReadSharingSettings   *sharingBody `json:"readSharingSettings,omitempty"`
	UpdateSharingSettings *sharingBody `json:"updateSharingSettings,omitempty"`
	TagSharingSettings    *sharingBody `json:"tagSharingSettings,omitempty"`
}

type sharingBody struct {
	PermittedGroups []groupIDBody `json:"permittedGroups"`
}

type groupIDBody struct {
	ID string `json:"id"`
}

func (w tagCreate) body() []byte {
	body, _ := json.Marshal(createTagBody{
		Name:                  w.name,
		ReadSharingSettings:   sharingOf(w.groups.readSharing),
		UpdateSharingSettings: sharingOf(w.groups.updateSharing),
		TagSharingSettings:    sharingOf(w.groups.tagSharing),
	})
	return body
}

func sharingOf(groups []groupID) *sharingBody {
	if groups == nil {
		return nil
	}
	permitted := make([]groupIDBody, 0, len(groups))
	for _, group := range groups {
		permitted = append(permitted, groupIDBody{ID: group.id})
	}
	return &sharingBody{PermittedGroups: permitted}
}

func (w tagCreate) verifyFields() []requestedField {
	own := []requestedField{{name: nameKey}}
	if w.groups.readSharing != nil {
		own = append(own, sharingChecked(readSharingKey))
	}
	if w.groups.updateSharing != nil {
		own = append(own, sharingChecked(updateSharingKey))
	}
	if w.groups.tagSharing != nil {
		own = append(own, sharingChecked(tagSharingKey))
	}
	return own
}

func sharingChecked(set string) requestedField {
	return requestedField{name: set, children: []requestedField{
		{name: permittedGroupsKey, children: []requestedField{{name: idKey}}},
	}}
}

func (w tagCreate) verify(a decodedResponse) *Error {
	tag := a.objects[0]
	wrong := textMismatch(nil, nameKey, w.name, tag[nameKey])
	wrong = sharingMismatch(wrong, readSharingKey, w.groups.readSharing, tag[readSharingKey])
	wrong = sharingMismatch(wrong, updateSharingKey, w.groups.updateSharing, tag[updateSharingKey])
	wrong = sharingMismatch(wrong, tagSharingKey, w.groups.tagSharing, tag[tagSharingKey])
	if len(wrong) == 0 {
		return nil
	}
	return mismatchFault(a, knownAs(tagKey, NewString(w.name)), wrong)
}

func sharingMismatch(wrong []mismatch, set string, written []groupID, value any) []mismatch {
	if written == nil {
		return wrong
	}
	sent := make([]string, 0, len(written))
	for _, group := range written {
		sent = append(sent, group.id)
	}
	permitted := memberOf(value, permittedGroupsKey)
	received, isGroups := groupIDsOf(permitted)
	if isGroups && sameIDsInAnyOrder(sent, received) {
		return wrong
	}
	held := rawValueNode(permitted)
	if isGroups {
		held = textList(received)
	}
	return append(wrong, mismatch{field: set + "." + permittedGroupsKey, expected: textList(sent), actual: held})
}

func groupIDsOf(value any) ([]string, bool) {
	items, isList := value.([]any)
	if !isList {
		return nil, false
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		id, isText := memberOf(item, idKey).(string)
		if !isText {
			return nil, false
		}
		ids = append(ids, id)
	}
	return ids, true
}

func sameIDsInAnyOrder(sent, received []string) bool {
	return slices.Equal(slices.Sorted(slices.Values(sent)), slices.Sorted(slices.Values(received)))
}

func DeleteTag(name string, ownedBy *string) (Call, *Error) {
	sought, fault := parseTagRef(name, ownedBy)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	return func(ctx context.Context, c *Client) (*Node, *Error) {
		return c.deleteTag(ctx, spec, sought)
	}, nil
}

type tagRef struct {
	name  string
	owner string
}

func parseTagRef(name string, ownedBy *string) (tagRef, *Error) {
	if fault := rejectNoTagName(name); fault != nil {
		return tagRef{}, fault
	}
	if ownedBy == nil {
		return tagRef{name: name}, nil
	}
	if *ownedBy == "" {
		return tagRef{}, &Error{Code: CodeBadUsage, Message: emptyTagOwner}
	}
	return tagRef{name: name, owner: *ownedBy}, nil
}

func rejectNoTagName(name string) *Error {
	switch {
	case name == "":
		return &Error{Code: CodeBadUsage, Message: emptyTagName}
	case !utf8.ValidString(name):
		return &Error{Code: CodeBadUsage, Message: tagNameOfNoUTF8}
	}
	if first, _ := utf8.DecodeRuneInString(name); isTagNameSpace(first) {
		return &Error{Code: CodeBadUsage, Message: tagNameEdgeMessage("begins", first)}
	}
	if last, _ := utf8.DecodeLastRuneInString(name); isTagNameSpace(last) {
		return &Error{Code: CodeBadUsage, Message: tagNameEdgeMessage("ends", last)}
	}
	return nil
}

func isTagNameSpace(r rune) bool {
	return r == '\t' || r == '\n' || r == '\v' || r == '\f' || r == '\r' ||
		(r >= 0x1C && r <= 0x1F) || unicode.In(r, unicode.Zs, unicode.Zl, unicode.Zp)
}

const (
	emptyTagName    = "--name is empty, and YouTrack keeps no tag under an empty name"
	tagNameOfNoUTF8 = "--name is no valid UTF-8, and no name YouTrack keeps is: bytes that are none name no tag " +
		"there could be"
	emptyTagOwner = "--owned-by is empty, and YouTrack keeps no user under an empty login: it takes the login of " +
		"the user the tag belongs to"
)

func tagNameEdgeMessage(where string, r rune) string {
	return fmt.Sprintf("--name %s with U+%04X, which YouTrack cuts off the edges of the name of a tag: the tag "+
		"would be kept under a name other than the one written, and no tag it keeps carries one there", where, r)
}

func (c *Client) deleteTag(ctx context.Context, spec *schemas, sought tagRef) (*Node, *Error) {
	found, fault := c.resolveTag(ctx, spec, sought)
	if fault != nil {
		return nil, fault
	}
	tag, fault := found.pathSafeID()
	if fault != nil {
		return nil, fault
	}
	if fault := writeEmpty(ctx, func(ctx context.Context) (*http.Response, error) {
		return c.apiDeleteTag(ctx, tag)
	}); fault != nil {
		return nil, found.withDetails(fault)
	}
	return objectNode(found.response, printedTagFields(), found.object, nil)
}

type tagTarget struct {
	schema string
	kind   ownerKind
}

func issueTagTarget() tagTarget {
	return tagTarget{schema: issueSchema, kind: issueOwner}
}

func articleTagTarget() tagTarget {
	return tagTarget{schema: articleSchema, kind: articleOwner}
}

func tagTargetOf(kind ownerKind) tagTarget {
	if kind == articleOwner {
		return articleTagTarget()
	}
	return issueTagTarget()
}

func AddTag(id, name string, ownedBy *string) (Call, *Error) {
	return tagWrite(id, name, ownedBy, (*Client).addTag)
}

func RemoveTag(id, name string, ownedBy *string) (Call, *Error) {
	return tagWrite(id, name, ownedBy, (*Client).removeTag)
}

func tagWrite(id, name string, ownedBy *string, write func(*Client, context.Context, *schemas, owner, tagRef) (*Node, *Error)) (Call, *Error) {
	at, fault := parseOwner(id)
	if fault != nil {
		return nil, fault
	}
	sought, fault := parseTagRef(name, ownedBy)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	return func(ctx context.Context, c *Client) (*Node, *Error) {
		return write(c, ctx, spec, at, sought)
	}, nil
}

func (c *Client) addTag(ctx context.Context, spec *schemas, at owner, sought tagRef) (*Node, *Error) {
	hung, fault := c.resolveTagging(ctx, spec, at, sought)
	if fault != nil {
		return nil, fault
	}
	body := hung.body()
	node, fault := c.write(ctx, spec, tagSchema, resolvedTagFields(), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiAddTag(ctx, hung.target.kind, hung.on, body, fields)
	}, hung.verify, hung.render(addedKey))
	if fault != nil {
		return nil, hung.withDetails(fault)
	}
	return node, nil
}

func (c *Client) removeTag(ctx context.Context, spec *schemas, at owner, sought tagRef) (*Node, *Error) {
	off, fault := c.resolveTagging(ctx, spec, at, sought)
	if fault != nil {
		return nil, fault
	}
	if fault := writeEmpty(ctx, func(ctx context.Context) (*http.Response, error) {
		return c.apiRemoveTag(ctx, off.target.kind, off.on, off.tag)
	}); fault != nil {
		return nil, off.withDetails(notOnTheOwner(off.target.kind, fault))
	}
	tag, fault := objectNode(off.found.response, printedTagFields(), off.found.object, nil)
	if fault != nil {
		return nil, fault
	}
	return off.document(removedKey, tag), nil
}

func notOnTheOwner(kind ownerKind, fault *Error) *Error {
	if fault.Code != CodeNotFound {
		return fault
	}
	fault.Message = fmt.Sprintf("the tag is not on the %s, and the tag itself stands: nothing was taken off, and "+
		"which tags the %s carries is read by ytrack %s show --fields tags(name)", kind, kind, kind)
	return fault
}

func (c *Client) resolveTagging(ctx context.Context, spec *schemas, at owner, sought tagRef) (tagOp, *Error) {
	target := tagTargetOf(at.kind)
	on, fault := c.readTagOwner(ctx, spec, target, at)
	if fault != nil {
		return tagOp{}, fault
	}
	found, fault := c.resolveTag(ctx, spec, sought)
	if fault != nil {
		return tagOp{}, fault
	}
	tag, fault := found.pathSafeID()
	if fault != nil {
		return tagOp{}, fault
	}
	return tagOp{on: on, target: target, found: found, tag: tag}, nil
}

func (c *Client) readTagOwner(ctx context.Context, spec *schemas, target tagTarget, at owner) (readableID, *Error) {
	requested := []requestedField{{name: idReadableKey}}
	a, fault := c.request(ctx, spec, target.schema, requested, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.getOwnerToTag(ctx, at, fields)
	})
	if fault != nil {
		return readableID{}, fault
	}
	return readableIDAt(a, a.objects[0], target.kind, "a tagging")
}

func (c *Client) getOwnerToTag(ctx context.Context, at owner, fields string) (*http.Response, error) {
	if at.kind == articleOwner {
		return c.apiGetArticle(ctx, at.id, fields)
	}
	return c.apiGetIssue(ctx, at.id, fields, nil)
}

func (c *Client) apiAddTag(ctx context.Context, kind ownerKind, on readableID, body []byte, fields string) (*http.Response, error) {
	if kind == articleOwner {
		return c.apiAddArticleTag(ctx, on, body, fields)
	}
	return c.apiAddIssueTag(ctx, on, body, fields)
}

func (c *Client) apiRemoveTag(ctx context.Context, kind ownerKind, on readableID, tag tagID) (*http.Response, error) {
	if kind == articleOwner {
		return c.apiRemoveArticleTag(ctx, on, tag)
	}
	return c.apiRemoveIssueTag(ctx, on, tag)
}

type tagOp struct {
	on     readableID
	target tagTarget
	found  resolvedTag
	tag    tagID
}

type tagRefBody struct {
	ID string `json:"id"`
}

func (h tagOp) body() []byte {
	body, _ := json.Marshal(tagRefBody{ID: h.tag.id})
	return body
}

func (h tagOp) verify(a decodedResponse) *Error {
	if received, isText := a.objects[0][idKey].(string); isText && received == h.tag.id {
		return nil
	}
	message := fmt.Sprintf("the tag the %s carries came back under an id other than the one the name resolved to",
		h.target.kind)
	return shapeFailure(a.httpResponse, a.body, message)
}

func (h tagOp) render(key string) func(decodedResponse) (*Node, *Error) {
	return func(a decodedResponse) (*Node, *Error) {
		tag, fault := objectNode(a, printedTagFields(), a.objects[0], nil)
		if fault != nil {
			return nil, fault
		}
		return h.document(key, tag), nil
	}
}

func (h tagOp) document(key string, tag *Node) *Node {
	return NewMap(
		Pair{Key: idReadableKey, Value: NewString(h.on.String())},
		Pair{Key: key, Value: tag})
}

func (h tagOp) withDetails(fault *Error) *Error {
	named := h.found.withDetails(fault)
	named.Details = insertAfterRequest(named.Details,
		Pair{Key: h.target.kind.String(), Value: NewString(h.on.String())})
	return named
}

type tagID struct {
	id string
}

func resolvedTagFields() []requestedField {
	return append([]requestedField{{name: idKey}}, printedTagFields()...)
}

func printedTagFields() []requestedField {
	return []requestedField{
		{name: nameKey},
		{name: ownerKey, children: []requestedField{{name: loginKey}}},
	}
}

type resolvedTag struct {
	response decodedResponse
	object   map[string]any
	name     string
}

type tagCandidate struct {
	name  string
	owner string
}

func (c *Client) resolveTag(ctx context.Context, spec *schemas, sought tagRef) (resolvedTag, *Error) {
	requested := resolvedTagFields()
	a, fault := c.request(ctx, spec, "[]"+tagSchema, requested, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetTags(ctx, fields, allRecords)
	})
	if fault != nil {
		return resolvedTag{}, fault
	}
	shown, fault := parseTagCandidates(a)
	if fault != nil {
		return resolvedTag{}, fault
	}
	at, fault := matchTag(a, sought, shown)
	if fault != nil {
		return resolvedTag{}, fault
	}
	return resolvedTag{response: a, object: a.objects[at], name: sought.name}, nil
}

func parseTagCandidates(a decodedResponse) ([]tagCandidate, *Error) {
	shown := make([]tagCandidate, 0, len(a.objects))
	for _, object := range a.objects {
		name, isText := object[nameKey].(string)
		if !isText {
			return nil, shapeFailure(a.httpResponse, a.body, "the name of a tag is not text")
		}
		login, isText := memberOf(object[ownerKey], loginKey).(string)
		if !isText {
			message := fmt.Sprintf("the login of the owner of the tag %s is not text", quote(name))
			return nil, shapeFailure(a.httpResponse, a.body, message)
		}
		shown = append(shown, tagCandidate{name: name, owner: login})
	}
	return shown, nil
}

func matchTag(a decodedResponse, sought tagRef, shown []tagCandidate) (int, *Error) {
	var named []int
	for at, tag := range shown {
		if strings.EqualFold(sought.name, tag.name) {
			named = append(named, at)
		}
	}
	candidates := named
	if sought.owner != "" {
		candidates = nil
		for _, at := range named {
			if strings.EqualFold(sought.owner, shown[at].owner) {
				candidates = append(candidates, at)
			}
		}
	}
	switch {
	case len(candidates) == 0 && len(named) > 0:
		return 0, noTagOfThatOwner(a, sought, shown, named)
	case len(candidates) == 0:
		return 0, noTagNamed(a, sought.name, shown)
	case len(candidates) == 1:
		return candidates[0], nil
	}
	if tieBrokenByCase := exactlyNamed(shown, candidates, sought.name); len(tieBrokenByCase) == 1 {
		return tieBrokenByCase[0], nil
	}
	return 0, severalTagsNamed(a, sought.name, shown, candidates)
}

func exactlyNamed(shown []tagCandidate, candidates []int, name string) []int {
	var exact []int
	for _, at := range candidates {
		if shown[at].name == name {
			exact = append(exact, at)
		}
	}
	return exact
}

type named interface {
	displayName() string
}

func (t tagCandidate) displayName() string   { return t.name }
func (g groupCandidate) displayName() string { return g.name }

func sortedNames[E named](shown []E) []string {
	names := make([]string, 0, len(shown))
	for _, entry := range shown {
		names = append(names, entry.displayName())
	}
	slices.Sort(names)
	return names
}

func noTagNamed(a decodedResponse, name string, shown []tagCandidate) *Error {
	entry := NewMap(
		Pair{Key: tagKey, Value: NewString(name)},
		Pair{Key: "nearest", Value: textList(nearestNames(name, sortedNames(shown)))})
	message := "the name under unknown is no tag this token is shown"
	return unresolvedTag(a, "unknown", message, entry)
}

func severalTagsNamed(a decodedResponse, name string, shown []tagCandidate, candidates []int) *Error {
	entry := NewMap(
		Pair{Key: tagKey, Value: NewString(name)},
		Pair{Key: "candidates", Value: tagsListed(shown, candidates)})
	message := "the name under ambiguous is the name of more than one tag this token is shown"
	return unresolvedTag(a, "ambiguous", message, entry)
}

func noTagOfThatOwner(a decodedResponse, sought tagRef, shown []tagCandidate, named []int) *Error {
	entry := NewMap(
		Pair{Key: tagKey, Value: NewString(sought.name)},
		Pair{Key: ownedByKey, Value: NewString(sought.owner)},
		Pair{Key: "candidates", Value: tagsListed(shown, named)})
	message := "no tag this token is shown under the name under unknown belongs to the login beside it"
	return unresolvedTag(a, "unknown", message, entry)
}

func tagsListed(shown []tagCandidate, at []int) *Node {
	found := make([]tagCandidate, 0, len(at))
	for _, where := range at {
		found = append(found, shown[where])
	}
	slices.SortFunc(found, func(one, other tagCandidate) int {
		return cmp.Or(strings.Compare(one.name, other.name), strings.Compare(one.owner, other.owner))
	})
	entries := make([]*Node, 0, len(found))
	for _, tag := range found {
		entries = append(entries, NewMap(
			Pair{Key: nameKey, Value: NewString(tag.name)},
			Pair{Key: ownerKey, Value: NewString(tag.owner)}))
	}
	return NewList(entries...)
}

func unresolvedTag(a decodedResponse, key, message string, entry *Node) *Error {
	details := []Pair{
		requestDetail(a.httpResponse.Request.Method, a.httpResponse.Request.URL.Redacted()),
		{Key: key, Value: NewList(entry)},
	}
	return &Error{Code: CodeUnknownName, Message: message, Details: details}
}

func textList(texts []string) *Node {
	items := make([]*Node, 0, len(texts))
	for _, text := range texts {
		items = append(items, NewString(text))
	}
	return NewList(items...)
}

func (r resolvedTag) pathSafeID() (tagID, *Error) {
	id, isText := r.object[idKey].(string)
	switch {
	case !isText:
		message := fmt.Sprintf("the id of the tag named %s is not text", quote(r.name))
		return tagID{}, shapeFailure(r.response.httpResponse, r.response.body, message)
	case !isInternalID(id):
		message := fmt.Sprintf("the tag named %s arrived under the id %s, and a deletion is addressed by the "+
			"internal id the server gives every entity, which is digits, a dash and digits", quote(r.name), quote(id))
		return tagID{}, shapeFailure(r.response.httpResponse, r.response.body, message)
	}
	return tagID{id: id}, nil
}

func (r resolvedTag) withDetails(fault *Error) *Error {
	fault.Details = insertAfterRequest(fault.Details, Pair{Key: tagKey, Value: NewString(r.name)})
	return fault
}

type groupID struct {
	id string
}

type groupCandidate struct {
	name string
	id   string
}

type groupCatalogue struct {
	response decodedResponse
	groups   []groupCandidate
}

func (c *Client) listGroups(ctx context.Context, spec *schemas) (groupCatalogue, *Error) {
	requested := []requestedField{{name: idKey}, {name: nameKey}}
	a, fault := c.request(ctx, spec, "[]"+groupSchema, requested, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetGroups(ctx, fields, topAll)
	})
	if fault != nil {
		return groupCatalogue{}, fault
	}
	groups := make([]groupCandidate, 0, len(a.objects))
	for _, object := range a.objects {
		name, isText := object[nameKey].(string)
		if !isText {
			return groupCatalogue{}, shapeFailure(a.httpResponse, a.body, "the name of a group is not text")
		}
		id, isText := object[idKey].(string)
		if !isText {
			message := fmt.Sprintf("the id of the group named %s is not text", quote(name))
			return groupCatalogue{}, shapeFailure(a.httpResponse, a.body, message)
		}
		groups = append(groups, groupCandidate{name: name, id: id})
	}
	return groupCatalogue{response: a, groups: groups}, nil
}

type resolvedSharing struct {
	readSharing   visibleForIDs
	updateSharing updatableByIDs
	tagSharing    taggableByIDs
}

type (
	visibleForIDs  []groupID
	updatableByIDs []groupID
	taggableByIDs  []groupID
)

func (g groupCatalogue) resolve(shared TagSharing) (resolvedSharing, *Error) {
	of := &groupResolver{shown: g}
	groups := resolvedSharing{
		readSharing:   shared.VisibleFor.resolve(of),
		updateSharing: shared.UpdatableBy.resolve(of),
		tagSharing:    shared.TaggableBy.resolve(of),
	}
	if fault := of.fault(); fault != nil {
		return resolvedSharing{}, fault
	}
	return groups, nil
}

func (names VisibleFor) resolve(r *groupResolver) visibleForIDs   { return r.resolveDistinct(names) }
func (names UpdatableBy) resolve(r *groupResolver) updatableByIDs { return r.resolveDistinct(names) }
func (names TaggableBy) resolve(r *groupResolver) taggableByIDs   { return r.resolveDistinct(names) }

type groupResolver struct {
	shown     groupCatalogue
	unknown   []*Node
	ambiguous []*Node
	broken    *Error
}

func (r *groupResolver) resolveDistinct(names []string) []groupID {
	if len(names) == 0 {
		return nil
	}
	ids := make([]groupID, 0, len(names))
	for _, name := range names {
		group, found := r.resolveOne(name)
		if found && !slices.Contains(ids, group) {
			ids = append(ids, group)
		}
	}
	return ids
}

func (r *groupResolver) resolveOne(name string) (groupID, bool) {
	var candidates []groupCandidate
	for _, group := range r.shown.groups {
		if strings.EqualFold(name, group.name) {
			candidates = append(candidates, group)
		}
	}
	if len(candidates) == 0 {
		r.unknown = append(r.unknown, NewMap(
			Pair{Key: groupKey, Value: NewString(name)},
			Pair{Key: "nearest", Value: textList(nearestNames(name, sortedNames(r.shown.groups)))}))
		return groupID{}, false
	}
	if len(candidates) > 1 {
		var exact []groupCandidate
		for _, group := range candidates {
			if group.name == name {
				exact = append(exact, group)
			}
		}
		if len(exact) != 1 {
			r.ambiguous = append(r.ambiguous, NewMap(
				Pair{Key: groupKey, Value: NewString(name)},
				Pair{Key: "candidates", Value: textList(sortedNames(candidates))}))
			return groupID{}, false
		}
		candidates = exact
	}
	return r.validID(candidates[0])
}

func (r *groupResolver) validID(group groupCandidate) (groupID, bool) {
	if isInternalID(group.id) {
		return groupID{id: group.id}, true
	}
	if r.broken == nil {
		message := fmt.Sprintf("the group named %s arrived under the id %s, and a tag is shared with the internal "+
			"id the server gives every entity, which is digits, a dash and digits", quote(group.name), quote(group.id))
		r.broken = shapeFailure(r.shown.response.httpResponse, r.shown.response.body, message)
	}
	return groupID{}, false
}

func (r *groupResolver) fault() *Error {
	if r.broken != nil {
		return r.broken
	}
	if len(r.unknown) == 0 && len(r.ambiguous) == 0 {
		return nil
	}
	a := r.shown.response
	details := []Pair{requestDetail(a.httpResponse.Request.Method, a.httpResponse.Request.URL.Redacted())}
	if len(r.unknown) > 0 {
		details = append(details, Pair{Key: "unknown", Value: NewList(r.unknown...)})
	}
	if len(r.ambiguous) > 0 {
		details = append(details, Pair{Key: "ambiguous", Value: NewList(r.ambiguous...)})
	}
	return &Error{Code: CodeUnknownName, Message: unresolvedGroups(len(r.unknown), len(r.ambiguous)), Details: details}
}

func unresolvedGroups(unknown, ambiguous int) string {
	switch {
	case ambiguous == 0:
		return "the names under unknown are no groups this token is shown"
	case unknown == 0:
		return "each name under ambiguous is the name of more than one group this token is shown"
	}
	return "the names under unknown are no groups this token is shown, and each name under ambiguous is the " +
		"name of more than one"
}
