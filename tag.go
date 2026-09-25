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

const (
	TagListFields   = "name,owner(login),readSharingSettings(permittedGroups(name),permittedUsers(login))"
	TagCreateFields = TagListFields +
		",updateSharingSettings(permittedGroups(name),permittedUsers(login))" +
		",tagSharingSettings(permittedGroups(name),permittedUsers(login))"
)

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
)

// ListTagsOptions: Fields is a fields= expression, empty for TagListFields and +x for them and x.
type ListTagsOptions struct {
	Fields string
	Page   Page
}

// TagSharing names the groups a new tag is shared with, a set with no group leaving the right to the owner alone.
// A group name resolves in any letter case, an exact spelling settling a tie.
type TagSharing struct {
	// VisibleFor fills readSharingSettings: the visibleFor member of a tag holds one group and grants no right.
	VisibleFor  []string
	UpdatableBy []string
	TaggableBy  []string
}

// TagOptions: the name of a tag resolves among the tags the token is shown in any letter case, an exact spelling
// settling a tie; OwnedBy, the login of the user the tag belongs to, narrows it, and empty leaves every owner.
type TagOptions struct {
	OwnedBy string
}

// List is a page of the tags the token owns or that are shared with it; a name may repeat across owners.
func (s *TagsService) List(ctx context.Context, opts *ListTagsOptions) (*Node, error) {
	return result(s.list(ctx, optionsOf(opts)))
}

// Create makes a tag of the owner of the token. YouTrack rejects a name another tag of that owner carries in any
// letter case.
func (s *TagsService) Create(ctx context.Context, name string, sharing TagSharing, opts *WriteOptions) (*Node, error) {
	return result(s.create(ctx, name, sharing, optionsOf(opts)))
}

// Delete deletes the tag everywhere it hangs; the token of an administrator deletes a tag of another user as well.
func (s *TagsService) Delete(ctx context.Context, name string, opts *TagOptions) (*Node, error) {
	return result(s.delete(ctx, name, optionsOf(opts)))
}

// Add hangs the tag on owner, the readable id of an issue or of an article.
func (s *TagsService) Add(ctx context.Context, owner, name string, opts *TagOptions) (*Node, error) {
	return result(s.tagging(ctx, owner, name, optionsOf(opts), (*Client).addTag))
}

// Remove takes the tag off owner, the readable id of an issue or of an article; the tag itself stays.
func (s *TagsService) Remove(ctx context.Context, owner, name string, opts *TagOptions) (*Node, error) {
	return result(s.tagging(ctx, owner, name, optionsOf(opts), (*Client).removeTag))
}

func (s *TagsService) list(ctx context.Context, opts ListTagsOptions) (*Node, *Error) {
	page, fault := opts.Page.parse()
	if fault != nil {
		return nil, fault
	}
	requested, fault := parseFields(opts.Fields, TagListFields)
	if fault != nil {
		return nil, fault
	}
	c := s.client
	return c.listPage(ctx, tagsPlural, "[]"+tagSchema, requested, page, c.apiGetTags)
}

func (s *TagsService) create(ctx context.Context, name string, sharing TagSharing, opts WriteOptions) (*Node, *Error) {
	if fault := rejectNoTagName(name); fault != nil {
		return nil, fault
	}
	if fault := sharing.rejectNoGroupName(); fault != nil {
		return nil, fault
	}
	requested, fault := parseFields(opts.Fields, TagCreateFields)
	if fault != nil {
		return nil, fault
	}
	c := s.client
	written, fault := c.resolveTagCreate(ctx, name, sharing)
	if fault != nil {
		return nil, fault
	}
	body := written.body()
	return writeAs(ctx, c, tagSchema, withFields(requested, written.verifyFields()...), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiCreateTag(ctx, body, fields)
	}, written.verify, writeResultNode(requested))
}

func (sharing TagSharing) rejectNoGroupName() *Error {
	for _, set := range []struct {
		right  string
		groups []string
	}{
		{right: "see the tag", groups: sharing.VisibleFor},
		{right: "update the tag", groups: sharing.UpdatableBy},
		{right: "tag with it", groups: sharing.TaggableBy},
	} {
		if slices.Contains(set.groups, "") {
			message := "a group to " + set.right + " goes by an empty name, and YouTrack keeps no group under an " +
				"empty name: the tag is shared with a group by its name"
			return &Error{Code: CodeBadUsage, Message: message}
		}
	}
	return nil
}

func (sharing TagSharing) any() bool {
	return len(sharing.VisibleFor) > 0 || len(sharing.UpdatableBy) > 0 || len(sharing.TaggableBy) > 0
}

func (c *Client) resolveTagCreate(ctx context.Context, name string, sharing TagSharing) (tagCreate, *Error) {
	if !sharing.any() {
		return tagCreate{name: name}, nil
	}
	catalogue, fault := c.listGroups(ctx)
	if fault != nil {
		return tagCreate{}, fault
	}
	groups, fault := catalogue.resolve(sharing)
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
	return mismatchFault(a, wrong, Pair{Key: tagKey, Value: NewString(w.name)})
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

func (s *TagsService) delete(ctx context.Context, name string, opts TagOptions) (*Node, *Error) {
	sought, fault := parseTagRef(name, opts)
	if fault != nil {
		return nil, fault
	}
	c := s.client
	found, fault := c.resolveTag(ctx, sought)
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

type tagRef struct {
	name  string
	owner string
}

func parseTagRef(name string, opts TagOptions) (tagRef, *Error) {
	if fault := rejectNoTagName(name); fault != nil {
		return tagRef{}, fault
	}
	return tagRef{name: name, owner: opts.OwnedBy}, nil
}

func rejectNoTagName(name string) *Error {
	if fault := rejectReplaced("the name of the tag", name, emptyTagName, nil); fault != nil {
		return fault
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

const emptyTagName = "is empty, and YouTrack keeps no tag under an empty name"

func tagNameEdgeMessage(where string, r rune) string {
	return fmt.Sprintf("the name of the tag %s with U+%04X, which YouTrack cuts off the edges of the name of a tag: "+
		"the tag would be kept under a name other than the one written, and no tag it keeps carries one there", where, r)
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

func (s *TagsService) tagging(ctx context.Context, id, name string, opts TagOptions, write func(*Client, context.Context, owner, tagRef) (*Node, *Error)) (*Node, *Error) {
	at, fault := parseOwner(id)
	if fault != nil {
		return nil, fault
	}
	sought, fault := parseTagRef(name, opts)
	if fault != nil {
		return nil, fault
	}
	return write(s.client, ctx, at, sought)
}

func (c *Client) addTag(ctx context.Context, at owner, sought tagRef) (*Node, *Error) {
	hung, fault := c.resolveTagging(ctx, at, sought)
	if fault != nil {
		return nil, fault
	}
	body := hung.body()
	node, fault := writeAs(ctx, c, tagSchema, resolvedTagFields(), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiAddTag(ctx, hung.target.kind, hung.on, body, fields)
	}, hung.verify, hung.render(addedKey))
	if fault != nil {
		return nil, hung.withDetails(fault)
	}
	return node, nil
}

func (c *Client) removeTag(ctx context.Context, at owner, sought tagRef) (*Node, *Error) {
	off, fault := c.resolveTagging(ctx, at, sought)
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
		"the tags the %s carries are under its field tags", kind, kind)
	return fault
}

func (c *Client) resolveTagging(ctx context.Context, at owner, sought tagRef) (tagOp, *Error) {
	target := tagTargetOf(at.kind)
	on, fault := c.readTagOwner(ctx, target, at)
	if fault != nil {
		return tagOp{}, fault
	}
	found, fault := c.resolveTag(ctx, sought)
	if fault != nil {
		return tagOp{}, fault
	}
	tag, fault := found.pathSafeID()
	if fault != nil {
		return tagOp{}, fault
	}
	return tagOp{on: on, target: target, found: found, tag: tag}, nil
}

func (c *Client) readTagOwner(ctx context.Context, target tagTarget, at owner) (readableID, *Error) {
	requested := []requestedField{{name: idReadableKey}}
	a, fault := c.request(ctx, target.schema, requested, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.getOwnerToTag(ctx, at, fields)
	})
	if fault != nil {
		return readableID{}, fault
	}
	return readableIDOf(a, target.kind, "a tagging")
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
	return a.invalid(message)
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

func (c *Client) resolveTag(ctx context.Context, sought tagRef) (resolvedTag, *Error) {
	requested := resolvedTagFields()
	a, fault := c.request(ctx, "[]"+tagSchema, requested, func(ctx context.Context, fields string) (*http.Response, error) {
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
			return nil, a.invalid("the name of a tag is not text")
		}
		login, isText := memberOf(object[ownerKey], loginKey).(string)
		if !isText {
			message := fmt.Sprintf("the login of the owner of the tag %s is not text", quote(name))
			return nil, a.invalid(message)
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
	entry := nearestEntry(tagKey, name, nearestNames(name, sortedNames(shown)))
	message := "the name under unknown is no tag this token is shown"
	return a.fault(CodeUnknownName, message, Pair{Key: "unknown", Value: NewList(entry)})
}

func severalTagsNamed(a decodedResponse, name string, shown []tagCandidate, candidates []int) *Error {
	entry := NewMap(
		Pair{Key: tagKey, Value: NewString(name)},
		Pair{Key: "candidates", Value: tagsListed(shown, candidates)})
	message := "the name under ambiguous is the name of more than one tag this token is shown"
	return a.fault(CodeUnknownName, message, Pair{Key: "ambiguous", Value: NewList(entry)})
}

func noTagOfThatOwner(a decodedResponse, sought tagRef, shown []tagCandidate, named []int) *Error {
	entry := NewMap(
		Pair{Key: tagKey, Value: NewString(sought.name)},
		Pair{Key: ownedByKey, Value: NewString(sought.owner)},
		Pair{Key: "candidates", Value: tagsListed(shown, named)})
	message := "no tag this token is shown under the name under unknown belongs to the login beside it"
	return a.fault(CodeUnknownName, message, Pair{Key: "unknown", Value: NewList(entry)})
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

func (r resolvedTag) pathSafeID() (tagID, *Error) {
	id, isText := r.object[idKey].(string)
	switch {
	case !isText:
		message := fmt.Sprintf("the id of the tag named %s is not text", quote(r.name))
		return tagID{}, r.response.invalid(message)
	case !isInternalID(id):
		message := fmt.Sprintf("the tag named %s arrived under the id %s, and a tag is addressed by the internal id "+
			"the server gives every entity, which is digits, a dash and digits", quote(r.name), quote(id))
		return tagID{}, r.response.invalid(message)
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

func (c *Client) listGroups(ctx context.Context) (groupCatalogue, *Error) {
	requested := []requestedField{{name: idKey}, {name: nameKey}}
	a, fault := c.request(ctx, "[]"+groupSchema, requested, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetGroups(ctx, fields, topAll)
	})
	if fault != nil {
		return groupCatalogue{}, fault
	}
	groups := make([]groupCandidate, 0, len(a.objects))
	for _, object := range a.objects {
		name, isText := object[nameKey].(string)
		if !isText {
			return groupCatalogue{}, a.invalid("the name of a group is not text")
		}
		id, isText := object[idKey].(string)
		if !isText {
			message := fmt.Sprintf("the id of the group named %s is not text", quote(name))
			return groupCatalogue{}, a.invalid(message)
		}
		groups = append(groups, groupCandidate{name: name, id: id})
	}
	return groupCatalogue{response: a, groups: groups}, nil
}

type resolvedSharing struct {
	readSharing   []groupID
	updateSharing []groupID
	tagSharing    []groupID
}

func (g groupCatalogue) resolve(sharing TagSharing) (resolvedSharing, *Error) {
	of := &groupResolver{shown: g}
	groups := resolvedSharing{
		readSharing:   of.resolveDistinct(sharing.VisibleFor),
		updateSharing: of.resolveDistinct(sharing.UpdatableBy),
		tagSharing:    of.resolveDistinct(sharing.TaggableBy),
	}
	if fault := of.fault(); fault != nil {
		return resolvedSharing{}, fault
	}
	return groups, nil
}

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
		r.unknown = append(r.unknown, nearestEntry(groupKey, name, nearestNames(name, sortedNames(r.shown.groups))))
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
		r.broken = r.shown.response.invalid(message)
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
	var details []Pair
	if len(r.unknown) > 0 {
		details = append(details, Pair{Key: "unknown", Value: NewList(r.unknown...)})
	}
	if len(r.ambiguous) > 0 {
		details = append(details, Pair{Key: "ambiguous", Value: NewList(r.ambiguous...)})
	}
	return r.shown.response.fault(CodeUnknownName, unresolvedGroups(len(r.unknown), len(r.ambiguous)), details...)
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
