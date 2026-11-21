package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

var (
	// ErrSlugTaken and ErrKeyTaken are conflicts on a workspace's URL slug and project key.
	ErrSlugTaken = fmt.Errorf("%w: that URL slug is taken", ErrConflict)
	ErrKeyTaken  = fmt.Errorf("%w: that project key is taken", ErrConflict)
)

// workspaceConflict names which unique workspace column a write collided on.
func workspaceConflict(err error) error {
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "23505" {
		return err
	}
	if pg.ConstraintName == "workspaces_key_prefix_key" {
		return ErrKeyTaken
	}
	return ErrSlugTaken
}

// keyWords splits a workspace name into upper-case ASCII words.
func keyWords(name string) []string {
	return strings.FieldsFunc(strings.ToUpper(name), func(r rune) bool {
		return !(r >= 'A' && r <= 'Z' || r >= '0' && r <= '9')
	})
}

// KeyCandidates lists project keys for a workspace name, best first. The
// first is three characters: the initials of a name of three or more words,
// two letters of the first word and one of the second for two words
// ("Cheshire Cat" gives CHC), or the start of a single word ("Acme" gives
// ACM). Then other three-letter picks from the name that keep its first
// letter, then the first two with a digit. Every candidate starts with a
// letter.
func KeyCandidates(name string) []string {
	words := keyWords(name)
	for len(words) > 0 && (words[0][0] < 'A' || words[0][0] > 'Z') {
		words[0] = strings.TrimLeft(words[0], "0123456789")
		if words[0] == "" {
			words = words[1:]
		}
	}
	if len(words) == 0 {
		words = []string{"WS"}
	}
	var first string
	switch {
	case len(words) >= 3:
		first = string([]byte{words[0][0], words[1][0], words[2][0]})
	case len(words) == 2 && len(words[0]) >= 2:
		first = words[0][:2] + words[1][:1]
	case len(words) == 2:
		first = words[0] + words[1][:min(2, len(words[1]))]
	default:
		first = words[0][:min(3, len(words[0]))]
	}
	out := []string{first}
	seen := map[string]bool{first: true}
	add := func(k string) {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	letters := strings.Join(words, "")
	for i := 1; i < len(letters); i++ {
		for j := i + 1; j < len(letters); j++ {
			add(string([]byte{letters[0], letters[i], letters[j]}))
		}
	}
	two := first[:min(2, len(first))]
	for d := 2; d <= 99; d++ {
		add(fmt.Sprintf("%s%d", two, d))
	}
	return out
}

// SuggestKey returns the first of KeyCandidates(name) no workspace uses.
func (s *Store) SuggestKey(ctx context.Context, name string) (string, error) {
	cands := KeyCandidates(name)
	rows, err := s.pool.Query(ctx, `SELECT key_prefix FROM workspaces WHERE key_prefix = ANY($1)`, cands)
	if err != nil {
		return "", err
	}
	taken := map[string]bool{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return "", err
		}
		taken[k] = true
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	for _, k := range cands {
		if !taken[k] {
			return k, nil
		}
	}
	return "", fmt.Errorf("no free project key for %q", name)
}

// KeyTaken reports whether a workspace uses the project key.
func (s *Store) KeyTaken(ctx context.Context, key string) (bool, error) {
	var taken bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM workspaces WHERE key_prefix = $1)`, key).Scan(&taken)
	return taken, err
}
