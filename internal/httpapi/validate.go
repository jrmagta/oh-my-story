package httpapi

import (
	"encoding/json"
	"fmt"
	"math"
	"net/mail"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
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

// parseEstimate accepts what steps.estimate (numeric(6,2)) holds exactly:
// 0 to 9999.99 with at most two decimals.
func parseEstimate(v float64) error {
	if v < 0 || v > 9999.99 || math.Abs(v*100-math.Round(v*100)) > 1e-6 {
		return errBadRequest("estimate must be a number from 0 to 9999.99 with at most two decimals")
	}
	return nil
}

// text trims s and requires 1 to max characters, or 0 to max when optional.
func text(field, s string, max int, optional bool) (string, error) {
	s = strings.TrimSpace(s)
	n := utf8.RuneCountInString(s)
	if n > max || (n == 0 && !optional) {
		if optional {
			return "", errBadRequest(fmt.Sprintf("%s must be at most %d characters", field, max))
		}
		return "", errBadRequest(fmt.Sprintf("%s must be 1 to %d characters", field, max))
	}
	return s, nil
}

// textPtr applies text to an optional field.
func textPtr(field string, s *string, max int, optional bool) (*string, error) {
	if s == nil {
		return nil, nil
	}
	v, err := text(field, *s, max, optional)
	return &v, err
}

const (
	maxName        = 100
	maxTitle       = 500
	maxDescription = 50_000
)

var colourRE = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

func parseColour(s *string) (*string, error) {
	if s == nil {
		return nil, nil
	}
	if !colourRE.MatchString(*s) {
		return nil, errBadRequest("colour must be a hex colour such as #FCB131")
	}
	return s, nil
}

func parseDate(s *string) (*string, error) {
	if s == nil {
		return nil, nil
	}
	if _, err := time.Parse(time.DateOnly, *s); err != nil {
		return nil, errBadRequest("targetDate must be a date such as 2026-12-31")
	}
	return s, nil
}

func oneOf(field, s string, allowed ...string) (string, error) {
	if !slices.Contains(allowed, s) {
		return "", errBadRequest(fmt.Sprintf("%s must be one of %s", field, strings.Join(allowed, ", ")))
	}
	return s, nil
}

// optional is a JSON field whose absence differs from null: Set is false when
// the field is missing, and Value is nil when it is null.
type optional[T any] struct {
	Set   bool
	Value *T
}

func (o *optional[T]) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Value = nil
		return nil
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	o.Value = &v
	return nil
}

// idSet checks a list of ids that replaces a set: no repeats, at most max.
// A nil list is a field left out.
func idSet(field string, ids *[]uuid.UUID, max int) error {
	if ids == nil {
		return nil
	}
	seen := map[uuid.UUID]bool{}
	for _, id := range *ids {
		if seen[id] {
			return errBadRequest(fmt.Sprintf("%s must not repeat an id", field))
		}
		seen[id] = true
	}
	if len(seen) > max {
		return errBadRequest(fmt.Sprintf("%s takes at most %d ids", field, max))
	}
	return nil
}
