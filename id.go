package youtrack

import (
	"fmt"
	"strings"
	"unicode"
)

const articleMarker = "A"

type idForm int

const (
	noForm idForm = iota
	issueForm
	articleForm
	internalForm
)

func formOf(arg string) idForm {
	code, rest, dashed := strings.Cut(arg, "-")
	if !dashed {
		return noForm
	}
	if !isProjectCode(code) {
		if isInternalID(arg) {
			return internalForm
		}
		return noForm
	}
	if digits(rest) {
		return issueForm
	}
	if marker, written := articleMarkerOf(arg); written && marker == articleMarker {
		return articleForm
	}
	return noForm
}

func isProjectCode(arg string) bool {
	for i, r := range arg {
		if i == 0 && !unicode.IsLetter(r) {
			return false
		}
		if i > 0 && !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '_' {
			return false
		}
	}
	return arg != ""
}

func articleMarkerOf(arg string) (string, bool) {
	code, rest, dashed := strings.Cut(arg, "-")
	if !dashed || !isProjectCode(code) {
		return "", false
	}
	marker, number, dashed := strings.Cut(rest, "-")
	if !dashed || !digits(number) {
		return "", false
	}
	return marker, true
}

func parseProjectCode(arg string) (string, *Error) {
	if !isProjectCode(arg) {
		message := fmt.Sprintf("project code %s is not a letter followed by letters, digits or underscores", quote(arg))
		return "", &Error{Code: CodeBadUsage, Message: message}
	}
	return arg, nil
}

type ownerKind int

const (
	issueOwner ownerKind = iota
	articleOwner
)

func (k ownerKind) String() string {
	if k == articleOwner {
		return "article"
	}
	return "issue"
}

func (k ownerKind) key() string {
	if k == articleOwner {
		return articleKey
	}
	return issueKey
}

const ownerNoun = "the issue or the article"

type owner struct {
	kind ownerKind
	id   string
}

func parseOwner(arg string) (owner, *Error) {
	switch shape := formOf(arg); shape {
	case issueForm:
		return owner{kind: issueOwner, id: arg}, nil
	case articleForm:
		return owner{kind: articleOwner, id: arg}, nil
	default:
		return owner{}, invalidIDFault(arg, shape)
	}
}

func parseIssueID(arg string) (string, *Error) {
	found, fault := parseOwner(arg)
	if fault != nil {
		return "", fault
	}
	if found.kind != issueOwner {
		return "", invalidIDFault(arg, articleForm)
	}
	return found.id, nil
}

func parseArticleID(arg string) (string, *Error) {
	found, fault := parseOwner(arg)
	if fault != nil {
		return "", fault
	}
	if found.kind != articleOwner {
		return "", invalidIDFault(arg, issueForm)
	}
	return found.id, nil
}

type childID struct {
	id string
}

func (c childID) String() string {
	return c.id
}

func parseChildID(noun, ownerNoun, arg string) (childID, *Error) {
	if !isInternalID(arg) {
		message := fmt.Sprintf("%s id %s is not an internal id, which is digits, a dash and digits, as in "+
			"7-12: the %s is addressed by the id its record carries among the %ss of %s it hangs from",
			noun, quote(arg), noun, noun, ownerNoun)
		return childID{}, &Error{Code: CodeBadUsage, Message: message}
	}
	return childID{id: arg}, nil
}

type readableID struct {
	readable string
}

func (a readableID) String() string {
	return a.readable
}

func readableIDOf(a decodedResponse, kind ownerKind, what string) (readableID, *Error) {
	return readableIDAt(a, a.objects[0], kind, what)
}

func readableIDAt(a decodedResponse, holder map[string]any, kind ownerKind, what string) (readableID, *Error) {
	readable, isText := holder[idReadableKey].(string)
	if !isText {
		message := fmt.Sprintf("the readable id of the %s arrived as something other than a string", kind)
		return readableID{}, a.invalid(message)
	}
	if found, fault := parseOwner(readable); fault != nil || found.kind != kind {
		message := fmt.Sprintf("the %s arrived with %s for a readable id, and %s is addressed by the readable "+
			"id the server gave", kind, quote(readable), what)
		return readableID{}, a.invalid(message)
	}
	return readableID{readable: readable}, nil
}

func invalidIDFault(arg string, wrong idForm) *Error {
	reason := notAReadableID
	marker, written := articleMarkerOf(arg)
	switch {
	case wrong == internalForm:
		reason = anInternalID
	case wrong == articleForm:
		reason = anArticleID
	case wrong == issueForm:
		reason = anIssueID
	case written && marker == strings.ToLower(articleMarker):
		reason = notAReadableID + lowerCaseMarker
	}
	return &Error{Code: CodeBadUsage, Message: fmt.Sprintf("id %s %s", quote(arg), reason)}
}

const (
	notAReadableID = "is neither the readable id of an issue, which is a project code, a dash and a number, as " +
		"in DEV-1, nor that of an article, which carries an A between them, as in DEV-A-1"
	lowerCaseMarker = "; the A of an article is upper case, and the server has no article under a lower case one"
	anInternalID    = "is an internal id, which addresses an entity that has no readable id of its own; an " +
		"issue is addressed by its idReadable, as in DEV-1, and an article by DEV-A-1"
	anArticleID = "is the readable id of an article, and an issue is a project code, a dash and a number, as " +
		"in DEV-1"
	anIssueID = "is the readable id of an issue, and an article carries an A between the code and the number, " +
		"as in DEV-A-1"
)

func parseLogin(arg string) (string, *Error) {
	switch {
	case arg == "", arg == ".", arg == "..":
		return "", invalidLoginFault(arg, "would reach an endpoint other than the one user it names")
	case strings.ContainsFunc(arg, unicode.IsSpace):
		return "", invalidLoginFault(arg, "holds a space, which no login does, and "+findByLoginOrName)
	case isInternalID(arg):
		return "", invalidLoginFault(arg, "is the internal id of a user, which the server reads in place of a login")
	case isHubID(arg):
		return "", invalidLoginFault(arg, "is a Hub id, which the server reads in place of a login")
	case arg == "me":
		return "", invalidLoginFault(arg, "is the owner of the token to the server, not a login of its own")
	}
	return arg, nil
}

func invalidLoginFault(arg, reason string) *Error {
	return &Error{Code: CodeBadUsage, Message: fmt.Sprintf("login %s %s", quote(arg), reason)}
}

func isInternalID(arg string) bool {
	number, rest, dashed := strings.Cut(arg, "-")
	return dashed && digits(number) && digits(rest)
}

func isHubID(arg string) bool {
	sizes := [...]int{8, 4, 4, 4, 12}
	groups := strings.Split(arg, "-")
	if len(groups) != len(sizes) {
		return false
	}
	for i, group := range groups {
		if !hexDigits(group, sizes[i]) {
			return false
		}
	}
	return true
}

func digits(text string) bool {
	if text == "" {
		return false
	}
	for i := range len(text) {
		if text[i] < '0' || text[i] > '9' {
			return false
		}
	}
	return true
}

func hexDigits(text string, count int) bool {
	if len(text) != count {
		return false
	}
	for i := range len(text) {
		c := text[i]
		if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F') {
			return false
		}
	}
	return true
}
