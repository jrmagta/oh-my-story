package store_test

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jrmagta/oh-my-story/internal/store"
)

func TestSweepAttachments(t *testing.T) {
	f := newFixture(t)
	m, _, act, _ := f.activities()
	st := f.step(m.ID, act.ID, nil, "Sign up")
	file := store.FileUpload{Name: "a.txt", ContentType: "text/plain", Data: []byte("a")}
	count := func() (n int) {
		t.Helper()
		if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM attachments`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	kept := must[store.Attachment](t)(f.s.AddStepAttachment(ctx, f.user, f.ws, m.ID, st.ID, file))
	deleted := must[store.Attachment](t)(f.s.AddStepAttachment(ctx, f.user, f.ws, m.ID, st.ID, file))
	if err := f.s.DeleteAttachment(ctx, f.user, f.ws, m.ID, deleted.ID); err != nil {
		t.Fatal(err)
	}
	draft := must[store.Attachment](t)(f.s.AddDraftAttachment(ctx, f.user, f.ws, m.ID, store.StepOwner(st.ID), file))
	posted := must[store.Attachment](t)(f.s.AddDraftAttachment(ctx, f.user, f.ws, m.ID, store.StepOwner(st.ID), file))
	c := must[store.CommentWrite](t)(f.s.CreateComment(ctx, f.user, f.ws, m.ID, store.StepOwner(st.ID), "see", nil, nil, []uuid.UUID{posted.ID}))
	if len(c.Comment.Attachments) != 1 || c.Comment.Attachments[0].ID != posted.ID {
		t.Fatalf("comment files = %+v", c.Comment.Attachments)
	}

	// Nothing is old enough yet.
	now := time.Now()
	if n := must[int](t)(f.s.SweepAttachments(ctx, now)); n != 0 {
		t.Errorf("fresh sweep removed %d", n)
	}
	// A day on, the unposted draft goes; the deleted file waits out its undo time.
	if n := must[int](t)(f.s.SweepAttachments(ctx, now.Add(store.DraftLifetime+time.Minute))); n != 1 {
		t.Errorf("day-old sweep removed %d", n)
	}
	var left int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM attachments WHERE id = $1`, draft.ID).Scan(&left); err != nil || left != 0 {
		t.Errorf("draft left: %d %v", left, err)
	}
	// After the purge time, the deleted file and the deleted comment's file go, bytes and all.
	if err := f.s.DeleteComment(ctx, f.user, f.ws, m.ID, c.Comment.ID, true); err != nil {
		t.Fatal(err)
	}
	if n := must[int](t)(f.s.SweepAttachments(ctx, now.Add(store.DeletedPurge+time.Minute))); n != 2 {
		t.Errorf("purge removed %d", n)
	}
	var ids []uuid.UUID
	rows, _ := f.pool.Query(ctx, `SELECT attachment_id FROM attachment_data`)
	for rows.Next() {
		var id uuid.UUID
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	if len(ids) != 1 || ids[0] != kept.ID || count() != 1 {
		t.Errorf("left after purge: %v (%d rows)", ids, count())
	}

	// Two sweepers at once remove each of many drafts exactly once.
	const drafts = 450
	if _, err := f.pool.Exec(ctx,
		`INSERT INTO attachments (id, workspace_id, draft_step_id, name, content_type, size_bytes, sha256, uploaded_by, created_at)
		 SELECT gen_random_uuid(), $1, $2, 'x', 'text/plain', 1, '\x00', $3, now() - interval '2 days' FROM generate_series(1, $4)`,
		f.ws, st.ID, f.user, drafts); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	got := make([]int, 2)
	for i := range got {
		wg.Go(func() { got[i] = must[int](t)(f.s.SweepAttachments(ctx, time.Now())) })
	}
	wg.Wait()
	if got[0]+got[1] != drafts || count() != 1 {
		t.Errorf("two sweepers removed %v, %d rows left", got, count())
	}
}
