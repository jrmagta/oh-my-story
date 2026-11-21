package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/jrmagta/oh-my-story/internal/rank"
)

// ErrInvalid wraps a rejected change: a wrong parent level, a reference to a
// row the change may not use, or a rule the schema enforces.
var ErrInvalid = errors.New("invalid")

// change is one row for the event log. before and after hold changed fields
// only, keyed by their JSON names; a create has only after.
type change struct {
	mapID         uuid.UUID // uuid.Nil for workspace-level entities
	entity        string
	id            uuid.UUID
	action        string
	before, after map[string]any
	echo          bool // a copy for another map the step is on (see echoes)
}

// Readers follow the event log by id. Holding this lock from the first event
// insert until commit makes transactions commit in id order, so a reader that
// has seen id n never later finds an uncommitted id below n.
const eventLogLock = 0x6f6d735f65767473 // "oms_evts"

// write runs fn in a transaction and appends the changes it returns to the
// event log in that transaction, then wakes listeners. With mapID set it first
// locks the map, so writes to one map are serialised and rank keys computed
// from neighbours cannot collide.
func (s *Store) write(ctx context.Context, actor, workspaceID, mapID uuid.UUID, fn func(tx pgx.Tx) ([]change, error)) error {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if mapID != uuid.Nil {
			var ok bool
			err := tx.QueryRow(ctx,
				`SELECT true FROM maps WHERE id = $1 AND workspace_id = $2 AND deleted_at IS NULL FOR UPDATE`,
				mapID, workspaceID).Scan(&ok)
			if err != nil {
				return notFound(err)
			}
		}
		changes, err := fn(tx)
		if err != nil {
			return err
		}
		return appendEvents(ctx, tx, actor, workspaceID, changes)
	})
	return dbError(err)
}

func appendEvents(ctx context.Context, tx pgx.Tx, actor, workspaceID uuid.UUID, changes []change) error {
	if len(changes) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(eventLogLock)); err != nil {
		return err
	}
	var last int64
	for _, c := range changes {
		var mapID *uuid.UUID
		if c.mapID != uuid.Nil {
			mapID = &c.mapID
		}
		err := tx.QueryRow(ctx,
			`INSERT INTO events (workspace_id, map_id, actor_id, actor_kind, entity_type, entity_id, action, before, after, echo)
			 VALUES ($1, $2, $3, 'user', $4, $5, $6, $7, $8, $9) RETURNING id`,
			workspaceID, mapID, actor, c.entity, c.id, c.action, jsonOrNull(c.before), jsonOrNull(c.after), c.echo).Scan(&last)
		if err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, `SELECT pg_notify('events', $1)`, strconv.FormatInt(last, 10))
	return err
}

func jsonOrNull(m map[string]any) any {
	if m == nil {
		return nil
	}
	return m
}

// fields is v as a JSON object, the shape events record.
func fields(v any) map[string]any {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		panic(err)
	}
	return m
}

// diff returns the fields that differ between two values of the same type.
// Both maps are nil when nothing changed. A version bump alone is not a
// change, but a real change carries the new version, so a live client always
// knows the version it is showing.
func diff(before, after any) (map[string]any, map[string]any) {
	b, a := fields(before), fields(after)
	var db, da map[string]any
	for k, av := range a {
		if k == "version" {
			continue
		}
		if bv := b[k]; !reflect.DeepEqual(bv, av) {
			if db == nil {
				db, da = map[string]any{}, map[string]any{}
			}
			db[k], da[k] = bv, av
		}
	}
	if v, ok := a["version"]; ok && da != nil {
		db["version"], da["version"] = b["version"], v
	}
	return db, da
}

// querier is a pool or a transaction.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func created(mapID uuid.UUID, entity string, id uuid.UUID, v any) change {
	return change{mapID: mapID, entity: entity, id: id, action: "create", after: fields(v)}
}

// gone builds a delete or restore. label holds the fields that name the row
// (a title, a key), so history can say what went or came back: before for a
// delete, after for a restore.
func gone(mapID uuid.UUID, entity string, id uuid.UUID, action string, label map[string]any) change {
	c := change{mapID: mapID, entity: entity, id: id, action: action}
	if action == "restore" {
		c.after = label
	} else {
		c.before = label
	}
	return c
}

// updated returns false when before and after are equal, so no-op writes leave no event.
func updated(mapID uuid.UUID, entity string, id uuid.UUID, action string, before, after any) (change, bool) {
	b, a := diff(before, after)
	return change{mapID: mapID, entity: entity, id: id, action: action, before: b, after: a}, b != nil
}

func dbError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "23514", "23503": // check_violation, foreign_key_violation
			return fmt.Errorf("%w: %s", ErrInvalid, pg.Message)
		case "23505":
			return ErrConflict
		}
	}
	if errors.Is(err, rank.ErrInvalid) {
		return fmt.Errorf("%w: position", ErrInvalid)
	}
	return err
}

// Position places an item among its siblings: right after After, right before
// Before, or last when both are nil.
type Position struct {
	After  *uuid.UUID `json:"afterId,omitempty"`
	Before *uuid.UUID `json:"beforeId,omitempty"`
}

// siblings names the rows an item is ordered among: table plus a WHERE clause
// over $1..$n. Soft-deleted rows count, so a restored row keeps a unique key.
// Position ids match anchor, which defaults to the id column.
type siblings struct {
	table  string
	where  string
	args   []any
	anchor string
}

// rank returns a key for pos among the siblings, ignoring the row self (the
// item being moved, or uuid.Nil for a new one).
func (sb siblings) rank(ctx context.Context, tx pgx.Tx, pos Position, self uuid.UUID) (string, error) {
	n := len(sb.args)
	query := func(sql string, extra ...any) (string, error) {
		var v *string
		args := append(append([]any{}, sb.args...), extra...)
		if err := tx.QueryRow(ctx, sql, args...).Scan(&v); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return "", fmt.Errorf("%w: position refers to an item that is not a sibling", ErrInvalid)
			}
			return "", err
		}
		if v == nil {
			return "", nil
		}
		return *v, nil
	}
	from := fmt.Sprintf(`FROM %s WHERE %s AND id <> $%d`, sb.table, sb.where, n+1)
	anchor := sb.anchor
	if anchor == "" {
		anchor = "id"
	}
	var a, b string
	var err error
	switch {
	case pos.After != nil && pos.Before != nil:
		return "", fmt.Errorf("%w: give afterId or beforeId, not both", ErrInvalid)
	case pos.After != nil:
		if a, err = query(fmt.Sprintf(`SELECT rank %s AND %s = $%d`, from, anchor, n+2), self, *pos.After); err != nil {
			return "", err
		}
		b, err = query(fmt.Sprintf(`SELECT min(rank) %s AND rank > $%d`, from, n+2), self, a)
	case pos.Before != nil:
		if b, err = query(fmt.Sprintf(`SELECT rank %s AND %s = $%d`, from, anchor, n+2), self, *pos.Before); err != nil {
			return "", err
		}
		a, err = query(fmt.Sprintf(`SELECT max(rank) %s AND rank < $%d`, from, n+2), self, b)
	default:
		a, err = query(`SELECT max(rank) `+from, self)
	}
	if err != nil {
		return "", err
	}
	return rank.Between(a, b)
}
