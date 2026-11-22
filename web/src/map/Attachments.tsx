import { useEffect, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api, type Attachment } from '../api'
import { errorMessage } from '../ui'
import { mapKey, useMap } from '../pages/map'
import { ago } from './history'

/** The types the server sends for the browser to show; the rest download. */
export const imageTypes = new Set(['image/png', 'image/jpeg', 'image/gif', 'image/webp'])

/** "512 B", "48 KB", "2.4 MB", "12 MB". */
export function fileSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  const kb = bytes / 1024
  if (kb < 1024) return `${Math.round(kb)} KB`
  const mb = kb / 1024
  return `${mb < 10 ? mb.toFixed(1) : Math.round(mb)} MB`
}

interface Upload {
  id: string
  name: string
  done: number
  error?: string
}

/**
 * A step's files, in the Details tab. Editors drop files anywhere on the
 * section or pick them with Add files; each uploads at once with its own
 * progress bar. Images show a thumbnail that opens full size.
 */
export function AttachmentSection({ stepId }: { stepId: string }) {
  const { ws, view, canEdit, run, shared, files: source } = useMap()
  const qc = useQueryClient()
  const files = useQuery({
    queryKey: [...mapKey(ws.slug, view.map.id), 'attachments', stepId],
    queryFn: () => source.list(stepId),
  })
  const [uploads, setUploads] = useState<Upload[]>([])
  const [over, setOver] = useState(false)
  const [viewing, setViewing] = useState<Attachment | null>(null)
  const input = useRef<HTMLInputElement>(null)

  const change = (id: string, f: (u: Upload) => Upload | null) =>
    setUploads((us) => us.flatMap((u) => (u.id === id ? (f(u) ?? []) : [u])))
  const add = (list: FileList | null) => {
    for (const file of Array.from(list ?? [])) {
      const id = crypto.randomUUID()
      setUploads((us) => [...us, { id, name: file.name, done: 0 }])
      api
        .uploadAttachment(ws.slug, view.map.id, stepId, file, (done) => change(id, (u) => ({ ...u, done })))
        .then(async () => {
          // The list and the card's count both come from the map's queries.
          await qc.invalidateQueries({ queryKey: mapKey(ws.slug, view.map.id) })
          change(id, () => null)
        })
        .catch((e: unknown) => change(id, (u) => ({ ...u, error: errorMessage(e) })))
    }
  }
  const hasFiles = (e: React.DragEvent) => canEdit && e.dataTransfer.types.includes('Files')

  const list = files.data ?? []
  return (
    <section
      className={`border-line border-b px-5 py-4 attachments ${over ? 'drop-over' : ''}`}
      aria-label="Attachments"
      onDragOver={(e) => {
        if (!hasFiles(e)) return
        e.preventDefault()
        setOver(true)
      }}
      onDragLeave={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setOver(false)
      }}
      onDrop={(e) => {
        if (!hasFiles(e)) return
        e.preventDefault()
        setOver(false)
        add(e.dataTransfer.files)
      }}
    >
      <div className="mb-2 flex items-center">
        <h3 className="label-up flex-1">Attachments{list.length > 0 && ` (${list.length})`}</h3>
        {canEdit && (
          <>
            <button type="button" className="btn btn-xs btn-ol" onClick={() => input.current?.click()}>
              + Add files
            </button>
            <input
              ref={input}
              type="file"
              multiple
              hidden
              aria-label="Files to attach"
              onChange={(e) => {
                add(e.target.files)
                e.target.value = ''
              }}
            />
          </>
        )}
      </div>
      {list.length > 0 && (
        <ul className="att-list" aria-label="Files">
          {list.map((a) => {
            const url = source.url(a.id)
            const image = imageTypes.has(a.contentType)
            return (
              <li key={a.id}>
                {image ? (
                  <button type="button" className="att-thumb" aria-label={`View ${a.name}`} onClick={() => setViewing(a)}>
                    <img src={url} alt="" loading="lazy" />
                  </button>
                ) : (
                  <span className="att-icon" aria-hidden>
                    <FileIcon />
                  </span>
                )}
                <span className="min-w-0 flex-1">
                  <a className="att-name" href={url} download={a.name}>
                    {a.name}
                  </a>
                  <span className="att-meta">
                    {[fileSize(a.size), ...(shared ? [] : [a.uploaderName || 'Removed user']), ago(a.createdAt)].join(' · ')}
                  </span>
                </span>
                {canEdit && (
                  <button
                    type="button"
                    className="att-x"
                    aria-label={`Delete ${a.name}`}
                    onClick={() =>
                      run(() => api.deleteAttachment(ws.slug, view.map.id, a.id), {
                        done: `Deleted ${a.name}`,
                        undo: () => api.restoreAttachment(ws.slug, view.map.id, a.id),
                      })
                    }
                  >
                    ✕
                  </button>
                )}
              </li>
            )
          })}
        </ul>
      )}
      {uploads.length > 0 && (
        <ul className="att-list" aria-label="Uploading">
          {uploads.map((u) => (
            <li key={u.id} className={u.error ? 'failed' : ''}>
              <span className="min-w-0 flex-1">
                <span className="att-name">{u.name}</span>
                {u.error ? (
                  <span className="att-meta att-error" role="alert">
                    {u.error}
                  </span>
                ) : (
                  <progress className="att-progress" value={u.done} max={1} aria-label={`Uploading ${u.name}`} />
                )}
              </span>
              {u.error && (
                <button type="button" className="att-x" aria-label={`Dismiss ${u.name}`} onClick={() => change(u.id, () => null)}>
                  ✕
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
      {files.isSuccess && list.length === 0 && uploads.length === 0 && (
        <p className="text-muted font-medium">{canEdit ? 'Drop files here, or use Add files.' : 'No files.'}</p>
      )}
      {viewing && <ImageViewer file={viewing} url={source.url(viewing.id)} onClose={() => setViewing(null)} />}
    </section>
  )
}

/** An image full size, over everything; Escape, Close or a click outside it closes. */
function ImageViewer({ file, url, onClose }: { file: Attachment; url: string; onClose: () => void }) {
  const ref = useRef<HTMLDialogElement>(null)
  useEffect(() => {
    ref.current?.showModal()
  }, [])
  return (
    <dialog
      ref={ref}
      className="att-viewer"
      aria-label={file.name}
      onClose={onClose}
      onClick={(e) => {
        if (e.target === e.currentTarget) ref.current?.close()
      }}
    >
      <img src={url} alt={file.name} />
      <div className="att-viewer-bar">
        <span className="min-w-0 flex-1 truncate font-bold">{file.name}</span>
        <a className="btn btn-xs btn-ol" href={url} download={file.name}>
          Download
        </a>
        <button type="button" className="btn btn-xs btn-primary" onClick={() => ref.current?.close()}>
          Close
        </button>
      </div>
    </dialog>
  )
}

export function FileIcon({ size = 18 }: { size?: number }) {
  return (
    <svg viewBox="0 0 16 16" width={size} height={size}>
      <path d="M4 1.5h5l3.5 3.5v9.5h-8.5z M9 1.5V5h3.5" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinejoin="round" />
    </svg>
  )
}
