import { useLayoutEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Markdown } from './Markdown'
import { api, atLeast, type Attachment, type Comment, type Member } from '../api'
import { mapKey, useMap } from '../pages/map'
import { useSession } from '../session'
import { ago } from './history'
import { FileList } from './Attachments'
import { FileErrors, useInlineFiles } from './MarkdownEditor'

/**
 * A step's comments: threads (a first comment and its replies), oldest
 * first. Commenters and up write; authors edit and delete their own; admins
 * delete any; a thread's author or an editor resolves it.
 */
export function Comments({ stepId }: { stepId: string }) {
  const { ws, view, run } = useMap()
  const { user } = useSession()
  const members = useQuery({ queryKey: ['ws', ws.slug, 'members'], queryFn: () => api.members(ws.slug) })
  const comments = useQuery({
    queryKey: [...mapKey(ws.slug, view.map.id), 'comments', stepId],
    queryFn: () => api.comments(ws.slug, view.map.id, stepId),
  })
  const canComment = atLeast(ws.role, 'commenter')
  const all = comments.data ?? []
  const threads = all.filter((c) => !c.parentId)
  const repliesTo = (id: string) => all.filter((c) => c.parentId === id)
  const post = (bodyMd: string, mentionIds: string[], attachmentIds: string[], parentId?: string) =>
    run(() => api.createComment(ws.slug, view.map.id, stepId, { bodyMd, mentionIds, attachmentIds, ...(parentId ? { parentId } : {}) }))

  return (
    <section className="comments" aria-label="Comments">
      {comments.isPending && <p className="text-muted px-5 py-4 font-medium">Loading…</p>}
      {comments.isSuccess && threads.length === 0 && <p className="text-muted px-5 py-4 font-medium">No comments yet.</p>}
      <ol className="threads">
        {threads.map((t) => (
          <li key={t.id} className={`thread ${t.resolved ? 'resolved' : ''}`} aria-label={`Thread by ${t.authorName}`}>
            <CommentView c={t} me={user.id} members={members.data ?? []} />
            {repliesTo(t.id).length > 0 && (
              <ol className="replies">
                {repliesTo(t.id).map((r) => (
                  <li key={r.id}>
                    <CommentView c={r} me={user.id} members={members.data ?? []} />
                  </li>
                ))}
              </ol>
            )}
            {canComment && !t.deleted && (
              <Composer
                stepId={stepId}
                members={members.data ?? []}
                label={`Reply to ${t.authorName}`}
                action="Reply"
                compact
                onPost={(b, m, f) => post(b, m, f, t.id)}
              />
            )}
          </li>
        ))}
      </ol>
      {canComment && <Composer stepId={stepId} members={members.data ?? []} label="New comment" action="Comment" onPost={(b, m, f) => post(b, m, f)} />}
    </section>
  )
}

function CommentView({ c, me, members }: { c: Comment; me: string; members: Member[] }) {
  const { ws, view, run } = useMap()
  const [editing, setEditing] = useState(false)
  const mine = c.authorId === me
  const canResolve = !c.parentId && !c.deleted && (mine || atLeast(ws.role, 'editor'))
  if (c.deleted) return <p className="comment deleted">Comment deleted</p>
  return (
    <article className="comment" aria-label={`Comment by ${c.authorName}`}>
      <header>
        <strong>{c.authorName || 'A former member'}</strong>
        <span className="text-muted">
          {ago(c.createdAt)}
          {c.edited && ' · edited'}
        </span>
        {c.resolved && <span className="pill resolved-pill">Resolved</span>}
      </header>
      {editing ? (
        <Composer
          stepId={c.stepId}
          members={members}
          label="Edit comment"
          action="Save"
          initial={c.bodyMd}
          initialFiles={c.attachments}
          onCancel={() => setEditing(false)}
          onPost={async (bodyMd, mentionIds, attachmentIds) => {
            const ok = await run(() => api.patchComment(ws.slug, view.map.id, c.id, { bodyMd, mentionIds, attachmentIds }))
            if (ok) setEditing(false)
            return ok
          }}
        />
      ) : (
        <>
          <div className="markdown">
            <Markdown>{c.bodyMd}</Markdown>
          </div>
          {/* Files shown in the text are not listed again. */}
          <FileList files={c.attachments.filter((a) => !c.bodyMd.includes(`attachment:${a.id}`))} label={`Files of the comment by ${c.authorName}`} />
        </>
      )}
      {!editing && (
        <footer>
          {canResolve && (
            <button type="button" onClick={() => run(() => api.patchComment(ws.slug, view.map.id, c.id, { resolved: !c.resolved }))}>
              {c.resolved ? 'Reopen' : 'Resolve'}
            </button>
          )}
          {mine && (
            <button type="button" onClick={() => setEditing(true)}>
              Edit
            </button>
          )}
          {(mine || atLeast(ws.role, 'admin')) && (
            <button type="button" className="danger" onClick={() => run(() => api.deleteComment(ws.slug, view.map.id, c.id))}>
              Delete
            </button>
          )}
        </footer>
      )}
    </article>
  )
}

/** The @word being typed just before the caret, if any. */
function mentionAt(text: string, caret: number): { start: number; query: string } | null {
  const m = /(^|\s)@([^\s@]*)$/.exec(text.slice(0, caret))
  return m ? { start: caret - m[2]!.length - 1, query: m[2]! } : null
}

/**
 * A Markdown textarea that offers workspace members after "@". Picking one
 * writes @Their Name; on posting, the members still named that way in the
 * text are sent as mentionIds.
 */
function Composer(props: {
  stepId: string
  members: Member[]
  label: string
  action: string
  initial?: string
  initialFiles?: Attachment[]
  compact?: boolean
  onPost: (bodyMd: string, mentionIds: string[], attachmentIds: string[]) => Promise<boolean>
  onCancel?: () => void
}) {
  const { ws, view } = useMap()
  const [text, setText] = useState(props.initial ?? '')
  // Files go up as drafts at once, so a pasted image has an id to show; posting claims them.
  const [files, setFiles] = useState<Attachment[]>(props.initialFiles ?? [])
  const picker = useRef<HTMLInputElement>(null)
  const [open, setOpen] = useState(!props.compact || !!props.initial)
  const [at, setAt] = useState<{ start: number; query: string } | null>(null)
  const [pick, setPick] = useState(0)
  const area = useRef<HTMLTextAreaElement>(null)
  const inline = useInlineFiles({
    area,
    setValue: setText,
    upload: (file) => api.uploadDraftAttachment(ws.slug, view.map.id, props.stepId, file),
    onUploaded: (a) => setFiles((fs) => [...fs, a]),
  })
  // Taking a file off also takes it out of the text.
  const remove = (a: Attachment) => {
    setFiles((fs) => fs.filter((f) => f.id !== a.id))
    setText((t) => t.replace(new RegExp(`!?\\[[^\\]]*\\]\\(attachment:${a.id}\\)\\n?`, 'g'), ''))
  }
  const matches = at
    ? props.members
        .filter((m) => `${m.displayName} ${m.email}`.toLowerCase().includes(at.query.toLowerCase()))
        .slice(0, 6)
    : []

  // After a mention goes in, the caret goes after it in the same render, before the next key
  // is handled; moving it a frame later could land it mid-word if typing had carried on.
  const caretTo = useRef<number | null>(null)
  useLayoutEffect(() => {
    if (caretTo.current === null) return
    area.current?.setSelectionRange(caretTo.current, caretTo.current)
    caretTo.current = null
  }, [text])
  const choose = (m: Member) => {
    if (!at) return
    const caret = at.start + 1 + at.query.length
    const next = `${text.slice(0, at.start)}@${m.displayName} ${text.slice(caret)}`
    setText(next)
    setAt(null)
    caretTo.current = at.start + m.displayName.length + 2
  }
  const submit = async () => {
    const body = text.trim()
    if (!body) return
    const mentionIds = props.members.filter((m) => body.includes(`@${m.displayName}`)).map((m) => m.userId)
    if (await props.onPost(body, mentionIds, files.map((f) => f.id))) {
      setText('')
      setFiles([])
      if (props.compact) setOpen(false)
    }
  }

  if (!open) {
    return (
      <button type="button" className="reply-btn" onClick={() => setOpen(true)}>
        {props.action}
      </button>
    )
  }
  return (
    <div className="composer">
      <textarea
        ref={area}
        autoFocus={props.compact || !!props.initial}
        className="textarea w-full"
        aria-label={props.label}
        placeholder="Write a comment. Type @ to mention someone."
        maxLength={10_000}
        value={text}
        onChange={(e) => {
          setText(e.target.value)
          setAt(mentionAt(e.target.value, e.target.selectionStart))
          setPick(0)
        }}
        onKeyDown={(e) => {
          if (at && matches.length > 0) {
            if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
              e.preventDefault()
              setPick((p) => (p + (e.key === 'ArrowDown' ? 1 : matches.length - 1)) % matches.length)
              return
            }
            if (e.key === 'Enter' || e.key === 'Tab') {
              e.preventDefault()
              const m = matches[pick]
              if (m) choose(m)
              return
            }
            if (e.key === 'Escape') {
              e.stopPropagation()
              setAt(null)
              return
            }
          }
          if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
            e.preventDefault()
            void submit()
          }
        }}
        {...inline.textarea}
      />
      {at && matches.length > 0 && (
        <ul className="mention-list" role="listbox" aria-label="Mention someone">
          {matches.map((m, i) => (
            <li key={m.userId} role="option" aria-selected={i === pick} onMouseDown={(e) => e.preventDefault()} onClick={() => choose(m)}>
              <strong>{m.displayName}</strong> <span className="text-muted">{m.email}</span>
            </li>
          ))}
        </ul>
      )}
      <FileErrors errors={inline.errors} />
      <FileList files={files} label={`Files for ${props.label}`} onRemove={remove} />
      <div className="flex gap-2">
        <button type="button" className="btn btn-sm btn-primary" disabled={!text.trim() || inline.pending > 0} onClick={() => void submit()}>
          {inline.pending > 0 ? 'Uploading…' : props.action}
        </button>
        <button type="button" className="btn btn-sm btn-ol" onClick={() => picker.current?.click()}>
          Attach
        </button>
        <input
          ref={picker}
          type="file"
          multiple
          hidden
          aria-label={`Files to attach to ${props.label}`}
          onChange={(e) => {
            if (e.target.files) inline.attach(e.target.files)
            e.target.value = ''
          }}
        />
        {(props.onCancel || props.compact) && (
          <button
            type="button"
            className="btn btn-sm btn-ol"
            onClick={() => {
              setText(props.initial ?? '')
              setFiles(props.initialFiles ?? [])
              props.onCancel?.()
              if (props.compact) setOpen(false)
            }}
          >
            Cancel
          </button>
        )}
      </div>
    </div>
  )
}
