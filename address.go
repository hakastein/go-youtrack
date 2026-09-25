package youtrack

import (
	"strings"
	"unicode"
)

func parseIssueID(arg string) (string, error) {
	code, number, dashed := strings.Cut(arg, "-")
	if !dashed || !isProjectCode(code) || !digits(number) {
		return "", &ArgumentError{Argument: "id", Value: arg,
			Reason: "is not the readable id of an issue, which is a project code, a dash and a number, as in DEV-1"}
	}
	return arg, nil
}

func parseProjectCode(arg string) (string, error) {
	if !isProjectCode(arg) {
		return "", &ArgumentError{Argument: "project", Value: arg,
			Reason: "is not a letter followed by letters, digits or underscores"}
	}
	return arg, nil
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

// An internal id is digits, a dash and digits, as in 7-12: only it goes into a path segment of its own,
// because .. in the segment would address another resource.
func isInternalID(arg string) bool {
	number, rest, dashed := strings.Cut(arg, "-")
	return dashed && digits(number) && digits(rest)
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
