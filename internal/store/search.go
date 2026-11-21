package store

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// SearchHit is a step found by Search, with where it sits.
type SearchHit struct {
	StepID     uuid.UUID `json:"stepId"`
	Key        string    `json:"key"`
	Title      string    `json:"title"`
	StatusID   uuid.UUID `json:"statusId"`
	MapID      uuid.UUID `json:"mapId"`
	MapName    string    `json:"mapName"`
	ActivityID uuid.UUID `json:"activityId"`
	Activity   string    `json:"activity"`
}

// SearchLimit is how many hits Search returns at most.
const SearchLimit = 20

// searchWords splits a query into words of letters and digits, the way the
// steps.search tsvector ('simple') splits titles and descriptions.
func searchWords(q string) []string {
	return strings.FieldsFunc(strings.ToLower(q), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r > 127)
	})
}

// Search finds live steps on live maps of the workspace. A step whose key is
// the query (ignoring case) comes first; then steps where every word of the
// query starts a word of the title or description, best match first.
func (s *Store) Search(ctx context.Context, workspaceID uuid.UUID, q string) ([]SearchHit, error) {
	words := searchWords(q)
	if len(words) == 0 {
		return []SearchHit{}, nil
	}
	// Each word becomes a prefix term ('word:*'); the words are letters and
	// digits only, so they cannot carry tsquery syntax.
	terms := make([]string, len(words))
	for i, w := range words {
		terms[i] = w + ":*"
	}
	return collect(ctx, s.pool, func(row pgx.Row) (SearchHit, error) {
		var h SearchHit
		err := row.Scan(&h.StepID, &h.Key, &h.Title, &h.StatusID, &h.MapID, &h.MapName, &h.ActivityID, &h.Activity)
		return h, err
	},
		`SELECT s.id, s.key, s.title, s.status_id, m.id, m.name, n.id, n.title
		 FROM steps s
		 JOIN placements p ON p.step_id = s.id
		 JOIN maps m ON m.id = p.map_id AND m.deleted_at IS NULL
		 JOIN nodes n ON n.id = p.activity_id
		 WHERE s.workspace_id = $1 AND s.deleted_at IS NULL
		   AND (upper(s.key) = upper($2) OR s.search @@ to_tsquery('simple', $3))
		 ORDER BY upper(s.key) = upper($2) DESC, ts_rank(s.search, to_tsquery('simple', $3)) DESC, s.key
		 LIMIT $4`,
		workspaceID, strings.TrimSpace(q), strings.Join(terms, " & "), SearchLimit)
}
