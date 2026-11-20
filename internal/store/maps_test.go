package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jrmagta/oh-my-story/internal/store"
)

var ctx = context.Background()

// fixture is a workspace with one user who owns it.
type fixture struct {
	t    *testing.T
	s    *store.Store
	pool *pgxpool.Pool
	user uuid.UUID
	ws   uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	s, pool := newStoreWithPool(t)
	u, err := s.Bootstrap(ctx, "owner@example.com", "Owner", "hash")
	if err != nil {
		t.Fatal(err)
	}
	w, err := s.CreateWorkspace(ctx, u.ID, "Acme", "acme")
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{t: t, s: s, pool: pool, user: u.ID, ws: w.ID}
}

func must[T any](t *testing.T) func(T, error) T {
	return func(v T, err error) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
}

func (f *fixture) newMap(levels int) store.Map {
	return must[store.Map](f.t)(f.s.CreateMap(ctx, f.user, f.ws, store.MapInput{Name: "Onboarding", Levels: levels}))
}

func (f *fixture) node(mapID uuid.UUID, parent *uuid.UUID, level, title string) store.Node {
	f.t.Helper()
	n, err := f.s.CreateNode(ctx, f.user, f.ws, mapID, store.NodeInput{ParentID: parent, Level: level, Title: title}, store.Position{})
	if err != nil {
		f.t.Fatalf("create %s %q: %v", level, title, err)
	}
	return n
}

// steps builds a 3-level map with one activity and two steps.
func (f *fixture) steps() (store.Map, store.Node, store.Node, store.Node) {
	m := f.newMap(3)
	act := f.node(m.ID, nil, "activity", "Sign up")
	return m, act, f.node(m.ID, &act.ID, "step", "Enter email"), f.node(m.ID, &act.ID, "step", "Verify")
}

func (f *fixture) story(mapID, stepID uuid.UUID, release *uuid.UUID, title string) store.Story {
	f.t.Helper()
	st, _, err := f.s.CreateStory(ctx, f.user, f.ws, mapID, store.StoryInput{Title: title, StepID: stepID, ReleaseID: release}, store.Position{})
	if err != nil {
		f.t.Fatal(err)
	}
	return st
}

// cell lists story titles in a cell, in rank order.
func (f *fixture) cell(mapID, stepID uuid.UUID, release *uuid.UUID) []string {
	f.t.Helper()
	rows, err := f.pool.Query(ctx,
		`SELECT s.title FROM placements p JOIN stories s ON s.id = p.story_id
		 WHERE p.map_id = $1 AND p.step_id = $2 AND p.release_id IS NOT DISTINCT FROM $3 AND s.deleted_at IS NULL
		 ORDER BY p.rank`, mapID, stepID, release)
	if err != nil {
		f.t.Fatal(err)
	}
	return must[[]string](f.t)(pgx.CollectRows(rows, pgx.RowTo[string]))
}

type event struct {
	ID            int64
	MapID         *uuid.UUID
	Entity        string
	EntityID      uuid.UUID
	Action        string
	Before, After map[string]any
}

// eventsAfter returns the workspace's events with an id above after.
func (f *fixture) eventsAfter(after int64) []event {
	f.t.Helper()
	rows, err := f.pool.Query(ctx,
		`SELECT id, map_id, entity_type, entity_id, action, before, after FROM events
		 WHERE workspace_id = $1 AND id > $2 ORDER BY id`, f.ws, after)
	if err != nil {
		f.t.Fatal(err)
	}
	return must[[]event](f.t)(pgx.CollectRows(rows, func(r pgx.CollectableRow) (event, error) {
		var e event
		err := r.Scan(&e.ID, &e.MapID, &e.Entity, &e.EntityID, &e.Action, &e.Before, &e.After)
		return e, err
	}))
}

func (f *fixture) lastEventID() int64 {
	f.t.Helper()
	var id int64
	if err := f.pool.QueryRow(ctx, `SELECT coalesce(max(id), 0) FROM events`).Scan(&id); err != nil {
		f.t.Fatal(err)
	}
	return id
}

func wantInvalid(t *testing.T, err error, what string) {
	t.Helper()
	if !errors.Is(err, store.ErrInvalid) {
		t.Errorf("%s: got %v, want ErrInvalid", what, err)
	}
}

func TestNodeParentLevels(t *testing.T) {
	f := newFixture(t)
	create := func(m uuid.UUID, parent *uuid.UUID, level string) error {
		_, err := f.s.CreateNode(ctx, f.user, f.ws, m, store.NodeInput{ParentID: parent, Level: level, Title: level}, store.Position{})
		return err
	}

	four := f.newMap(4)
	obj := f.node(four.ID, nil, "objective", "Grow")
	act := f.node(four.ID, &obj.ID, "activity", "Sign up")
	step := f.node(four.ID, &act.ID, "step", "Enter email")
	wantInvalid(t, create(four.ID, nil, "activity"), "activity without objective on a 4-level map")
	wantInvalid(t, create(four.ID, &obj.ID, "step"), "step under objective")
	wantInvalid(t, create(four.ID, &act.ID, "objective"), "objective under activity")
	wantInvalid(t, create(four.ID, &step.ID, "step"), "step under step")
	wantInvalid(t, create(four.ID, nil, "step"), "step without parent")

	three := f.newMap(3)
	wantInvalid(t, create(three.ID, nil, "objective"), "objective on a 3-level map")
	f.node(three.ID, nil, "activity", "Browse")
	wantInvalid(t, create(three.ID, &act.ID, "step"), "parent from another map")

	_, err := f.s.MoveNode(ctx, f.user, f.ws, four.ID, step.ID, &obj.ID, store.Position{})
	wantInvalid(t, err, "moving a step under an objective")
	act2 := f.node(four.ID, &obj.ID, "activity", "Pay")
	moved := must[store.Node](t)(f.s.MoveNode(ctx, f.user, f.ws, four.ID, step.ID, &act2.ID, store.Position{}))
	if *moved.ParentID != act2.ID || moved.Version != 1 {
		t.Errorf("moved step = %+v", moved)
	}
}

func TestMapLevelsFixedOnceBackboneExists(t *testing.T) {
	f := newFixture(t)
	m := f.newMap(4)
	three, four := 3, 4
	if got := must[store.Map](t)(f.s.UpdateMap(ctx, f.user, f.ws, m.ID, store.MapPatch{Levels: &three})); got.Levels != 3 {
		t.Fatalf("levels = %d", got.Levels)
	}
	f.node(m.ID, nil, "activity", "Sign up")
	_, err := f.s.UpdateMap(ctx, f.user, f.ws, m.ID, store.MapPatch{Levels: &four})
	wantInvalid(t, err, "changing levels with nodes")
}

// Stories on different maps of one workspace draw keys from one counter.
func TestStoryKeysConcurrent(t *testing.T) {
	f := newFixture(t)
	m1, _, s1, _ := f.steps()
	m2, _, s2, _ := f.steps()
	const n = 20
	keys := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m, step := m1.ID, s1.ID
			if i%2 == 1 {
				m, step = m2.ID, s2.ID
			}
			var st store.Story
			st, _, errs[i] = f.s.CreateStory(ctx, f.user, f.ws, m, store.StoryInput{Title: "s", StepID: step}, store.Position{})
			keys[i] = st.Key
		}()
	}
	wg.Wait()
	want := make([]string, n)
	for i := range n {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		want[i] = fmt.Sprintf("STORY-%d", i+1)
	}
	slices.SortFunc(keys, func(a, b string) int {
		ai, _ := strconv.Atoi(a[len("STORY-"):])
		bi, _ := strconv.Atoi(b[len("STORY-"):])
		return ai - bi
	})
	if !slices.Equal(keys, want) {
		t.Errorf("keys = %v", keys)
	}
}

func TestEventsRecordChangedFields(t *testing.T) {
	f := newFixture(t)
	m, _, step, _ := f.steps()

	listener := must[*pgxpool.Conn](t)(f.pool.Acquire(ctx))
	defer listener.Release()
	if _, err := listener.Exec(ctx, `LISTEN events`); err != nil {
		t.Fatal(err)
	}

	start := f.lastEventID()
	st, p, err := f.s.CreateStory(ctx, f.user, f.ws, m.ID, store.StoryInput{Title: "Old", StepID: step.ID}, store.Position{})
	if err != nil {
		t.Fatal(err)
	}
	evs := f.eventsAfter(start)
	if len(evs) != 2 || evs[0].Entity != "story" || evs[1].Entity != "placement" || evs[0].Action != "create" {
		t.Fatalf("create events = %+v", evs)
	}
	if evs[0].After["key"] != st.Key || evs[0].Before != nil || *evs[0].MapID != m.ID || evs[1].EntityID != p.ID {
		t.Errorf("story create event = %+v", evs[0])
	}

	// The notification carries the id of the transaction's last event.
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	note, err := listener.Conn().WaitForNotification(wctx)
	if err != nil {
		t.Fatal(err)
	}
	if note.Payload != strconv.FormatInt(evs[1].ID, 10) {
		t.Errorf("notify payload %q, want %d", note.Payload, evs[1].ID)
	}

	start = f.lastEventID()
	title := "New"
	if _, err := f.s.UpdateStory(ctx, f.user, f.ws, m.ID, st.ID, store.StoryPatch{Title: &title}); err != nil {
		t.Fatal(err)
	}
	evs = f.eventsAfter(start)
	if len(evs) != 1 {
		t.Fatalf("update events = %+v", evs)
	}
	if !reflect.DeepEqual(evs[0].Before, map[string]any{"title": "Old"}) || !reflect.DeepEqual(evs[0].After, map[string]any{"title": "New"}) {
		t.Errorf("update event before=%v after=%v", evs[0].Before, evs[0].After)
	}

	start = f.lastEventID()
	if _, err := f.s.UpdateStory(ctx, f.user, f.ws, m.ID, st.ID, store.StoryPatch{Title: &title}); err != nil {
		t.Fatal(err)
	}
	if evs := f.eventsAfter(start); len(evs) != 0 {
		t.Errorf("a no-op update wrote %+v", evs)
	}
}

func TestMoveStory(t *testing.T) {
	f := newFixture(t)
	m, _, a, b := f.steps()
	s1 := f.story(m.ID, a.ID, nil, "one")
	f.story(m.ID, a.ID, nil, "two")
	s3 := f.story(m.ID, a.ID, nil, "three")

	_, stale, err := f.s.MoveStory(ctx, f.user, f.ws, m.ID, s3.ID, store.StoryMove{StepID: a.ID, Position: store.Position{Before: &s1.ID}, SeenVersion: 0})
	if err != nil || stale {
		t.Fatalf("move: stale=%v err=%v", stale, err)
	}
	if got := f.cell(m.ID, a.ID, nil); !slices.Equal(got, []string{"three", "one", "two"}) {
		t.Errorf("cell = %v", got)
	}

	rel := must[store.Release](t)(f.s.CreateRelease(ctx, f.user, f.ws, m.ID, store.ReleaseInput{Name: "MVP"}, store.Position{}))
	// s3 is now at version 1, so a client that saw version 0 is stale.
	p, stale, err := f.s.MoveStory(ctx, f.user, f.ws, m.ID, s3.ID, store.StoryMove{StepID: b.ID, ReleaseID: &rel.ID, SeenVersion: 0})
	if err != nil || !stale || p.StepID != b.ID || *p.ReleaseID != rel.ID {
		t.Fatalf("cross-cell move: %+v stale=%v err=%v", p, stale, err)
	}
	if got := f.cell(m.ID, b.ID, &rel.ID); !slices.Equal(got, []string{"three"}) {
		t.Errorf("target cell = %v", got)
	}

	_, _, err = f.s.MoveStory(ctx, f.user, f.ws, m.ID, s1.ID, store.StoryMove{StepID: a.ID, Position: store.Position{After: &s3.ID}, SeenVersion: 0})
	wantInvalid(t, err, "anchor in another cell")
	_, _, err = f.s.MoveStory(ctx, f.user, f.ws, m.ID, s1.ID, store.StoryMove{StepID: m.ID})
	wantInvalid(t, err, "moving onto a non-step")
}

func TestDeleteStatusNeedsReplacement(t *testing.T) {
	f := newFixture(t)
	m, _, step, _ := f.steps()
	st := f.story(m.ID, step.ID, nil, "s")
	wfs := must[[]store.Workflow](t)(f.s.Workflows(ctx, f.ws))
	if len(wfs) != 1 || len(wfs[0].Statuses) != 4 {
		t.Fatalf("workflows = %+v", wfs)
	}
	todo, done := wfs[0].Statuses[0], wfs[0].Statuses[3]
	if st.StatusID != todo.ID || done.Name != "Done" {
		t.Fatalf("story status %v, statuses %+v", st.StatusID, wfs[0].Statuses)
	}
	other := must[store.Workflow](t)(f.s.CreateWorkflow(ctx, f.user, f.ws, "Design"))

	wantInvalid(t, f.s.DeleteStatus(ctx, f.user, f.ws, todo.ID, nil), "deleting a used status without a replacement")
	wantInvalid(t, f.s.DeleteStatus(ctx, f.user, f.ws, todo.ID, &other.Statuses[0].ID), "replacement from another workflow")
	wantInvalid(t, f.s.DeleteStatus(ctx, f.user, f.ws, todo.ID, &todo.ID), "replacing a status with itself")

	start := f.lastEventID()
	if err := f.s.DeleteStatus(ctx, f.user, f.ws, todo.ID, &done.ID); err != nil {
		t.Fatal(err)
	}
	var status uuid.UUID
	if err := f.pool.QueryRow(ctx, `SELECT status_id FROM stories WHERE id = $1`, st.ID).Scan(&status); err != nil || status != done.ID {
		t.Errorf("story status = %v, %v", status, err)
	}
	evs := f.eventsAfter(start)
	if len(evs) != 2 || evs[0].EntityID != st.ID || evs[0].After["statusId"] != done.ID.String() || *evs[0].MapID != m.ID ||
		evs[1].Entity != "status" || evs[1].Action != "delete" {
		t.Errorf("events = %+v", evs)
	}

	// Unused statuses delete without a replacement, down to the last one.
	for _, s := range wfs[0].Statuses[1:3] {
		if err := f.s.DeleteStatus(ctx, f.user, f.ws, s.ID, nil); err != nil {
			t.Fatal(err)
		}
	}
	wantInvalid(t, f.s.DeleteStatus(ctx, f.user, f.ws, done.ID, &done.ID), "deleting the last status")
}

func TestDeleteAndRestoreNode(t *testing.T) {
	f := newFixture(t)
	m, act, a, b := f.steps()
	f.story(m.ID, a.ID, nil, "a1")
	gone := f.story(m.ID, a.ID, nil, "a2")
	f.story(m.ID, b.ID, nil, "b1")
	if err := f.s.DeleteStory(ctx, f.user, f.ws, m.ID, gone.ID); err != nil {
		t.Fatal(err)
	}

	start := f.lastEventID()
	if err := f.s.DeleteNode(ctx, f.user, f.ws, m.ID, act.ID); err != nil {
		t.Fatal(err)
	}
	if n := len(f.eventsAfter(start)); n != 5 { // activity, two steps, two live stories
		t.Errorf("delete wrote %d events, want 5", n)
	}
	if got := f.cell(m.ID, a.ID, nil); len(got) != 0 {
		t.Errorf("stories survive their step: %v", got)
	}
	_, err := f.s.CreateNode(ctx, f.user, f.ws, m.ID, store.NodeInput{ParentID: &act.ID, Level: "step", Title: "x"}, store.Position{})
	wantInvalid(t, err, "adding under a deleted node")

	if _, err := f.s.RestoreNode(ctx, f.user, f.ws, m.ID, act.ID); err != nil {
		t.Fatal(err)
	}
	// The story deleted on its own before stays deleted.
	if got := f.cell(m.ID, a.ID, nil); !slices.Equal(got, []string{"a1"}) {
		t.Errorf("cell a after restore = %v", got)
	}
	if got := f.cell(m.ID, b.ID, nil); !slices.Equal(got, []string{"b1"}) {
		t.Errorf("cell b after restore = %v", got)
	}
}

func TestDeleteReleaseUnschedulesItsStories(t *testing.T) {
	f := newFixture(t)
	m, _, a, _ := f.steps()
	rel := must[store.Release](t)(f.s.CreateRelease(ctx, f.user, f.ws, m.ID, store.ReleaseInput{Name: "MVP"}, store.Position{}))
	f.story(m.ID, a.ID, nil, "u1")
	f.story(m.ID, a.ID, &rel.ID, "r1")
	f.story(m.ID, a.ID, &rel.ID, "r2")
	if err := f.s.DeleteRelease(ctx, f.user, f.ws, m.ID, rel.ID); err != nil {
		t.Fatal(err)
	}
	if got := f.cell(m.ID, a.ID, nil); !slices.Equal(got, []string{"u1", "r1", "r2"}) {
		t.Errorf("unscheduled = %v", got)
	}
}

func TestOtherWorkspaceIsNotFound(t *testing.T) {
	f := newFixture(t)
	m, _, step, _ := f.steps()
	st := f.story(m.ID, step.ID, nil, "s")
	other := must[store.Workspace](t)(f.s.CreateWorkspace(ctx, f.user, "Other", "other"))
	title := "x"
	if _, err := f.s.UpdateStory(ctx, f.user, other.ID, m.ID, st.ID, store.StoryPatch{Title: &title}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("update through another workspace: %v", err)
	}
	if _, err := f.s.CreateNode(ctx, f.user, other.ID, m.ID, store.NodeInput{Level: "activity", Title: "x"}, store.Position{}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("create node through another workspace: %v", err)
	}
}

// Event payloads use the API's JSON field names.
func TestEventFieldNames(t *testing.T) {
	f := newFixture(t)
	m, _, step, _ := f.steps()
	start := f.lastEventID()
	f.story(m.ID, step.ID, nil, "s")
	after := f.eventsAfter(start)[0].After
	b, _ := json.Marshal(after)
	for _, k := range []string{"id", "key", "title", "descriptionMd", "statusId", "assigneeId"} {
		if _, ok := after[k]; !ok {
			t.Errorf("story create event lacks %q: %s", k, b)
		}
	}
	if _, ok := after["version"]; ok {
		t.Errorf("events should not carry version: %s", b)
	}
}
