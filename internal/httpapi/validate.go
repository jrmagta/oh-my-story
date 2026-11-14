package httpapi

import (
	"net/mail"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Parsing at the boundary: handlers get clean values or a bad_request error.

func parseEmail(s string) (string, error) {
	s = strings.TrimSpace(s)
	a, err := mail.ParseAddress(s)
	if err != nil || a.Address != s || strings.Count(s, "@") != 1 || len(s) > 254 {
		return "", errBadRequest("email is not a valid address")
	}
	return s, nil
}

func parseDisplayName(s string) (string, error) {
	s = strings.TrimSpace(s)
	if n := utf8.RuneCountInString(s); n < 1 || n > 100 {
		return "", errBadRequest("display name must be 1 to 100 characters")
	}
	return s, nil
}

func parseNewPassword(s string) (string, error) {
	if n := utf8.RuneCountInString(s); n < 12 || n > 256 {
		return "", errBadRequest("password must be 12 to 256 characters")
	}
	return s, nil
}

var (
	slugRE      = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,38}[a-z0-9])?$`)
	keyPrefixRE = regexp.MustCompile(`^[A-Z][A-Z0-9]{0,9}$`)
)

func parseSlug(s string) (string, error) {
	if !slugRE.MatchString(s) {
		return "", errBadRequest("slug must be 1 to 40 lowercase letters, digits or hyphens, not starting or ending with a hyphen")
	}
	return s, nil
}

func parseWorkspaceName(s string) (string, error) {
	s = strings.TrimSpace(s)
	if n := utf8.RuneCountInString(s); n < 1 || n > 100 {
		return "", errBadRequest("workspace name must be 1 to 100 characters")
	}
	return s, nil
}

func parseKeyPrefix(s string) (string, error) {
	if !keyPrefixRE.MatchString(s) {
		return "", errBadRequest("key prefix must be 1 to 10 uppercase letters or digits, starting with a letter")
	}
	return s, nil
}

func parseEstimateUnit(s string) (string, error) {
	s = strings.TrimSpace(s)
	if n := utf8.RuneCountInString(s); n < 1 || n > 20 {
		return "", errBadRequest("estimate unit must be 1 to 20 characters")
	}
	return s, nil
}
