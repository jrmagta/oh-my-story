import { useRef, useState, type ClipboardEvent, type DragEvent, type RefObject } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { api, type Attachment, type FileOwner } from '../api'
import { errorMessage } from '../ui'
import { mapKey, useMap } from '../pages/map'
import { imageTypes } from './Attachments'
import { fileMarkdown, Markdown } from './Markdown'

/** Browsers name every pasted screenshot "image.png"; give it a name that tells them apart. */
function named(file: File): File {
  if (!/^image\.\w+$/.test(file.name)) return file
  const d = new Date()
  const p = (n: number) => String(n).padStart(2, '0')
  const stamp = `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}.${p(d.getMinutes())}.${p(d.getSeconds())}`
  return new File([file], `Pasted image ${stamp}${file.name.slice(5)}`, { type: file.type })
}

/**
 * Uploads files pasted or dropped into a Markdown textarea. A placeholder
 * marks the spot, on a line of its own, while each uploads, then turns into
 * `![name](attachment:id)` (or a link, for anything but an image).
 * textarea goes on the textarea; pending counts uploads still going.
 */
export function useInlineFiles({
  area,
  setValue,
  upload,
  onUploaded,
}: {
  area: RefObject<HTMLTextAreaElement | null>
  /** Takes an update, since uploads finish after other typing. */
  setValue: (update: (v: string) => string) => void
  upload: (file: File) => Promise<Attachment>
  onUploaded?: (a: Attachment) => void
}) {
  const [pending, setPending] = useState(0)
  const [errors, setErrors] = useState<string[]>([])

  const attach = (list: FileList) => {
    const files = Array.from(list).map(named)
    if (files.length === 0) return false
    const marks = files.map((f) => ({ file: f, mark: `![Uploading ${f.name.replace(/[[\]\\]/g, '')}…](uploading:${crypto.randomUUID()})` }))
    const el = area.current
    setValue((v) => {
      const at = el?.selectionStart ?? v.length
      const before = v.slice(0, at)
      const after = v.slice(el?.selectionEnd ?? at)
      const open = before && !before.endsWith('\n') ? '\n' : ''
      const close = after && !after.startsWith('\n') ? '\n' : ''
      return before + open + marks.map((m) => m.mark).join('\n') + close + after
    })
    setPending((n) => n + files.length)
    setErrors([])
    for (const { file, mark } of marks) {
      upload(file)
        .then((a) => {
          setValue((v) => v.replace(mark, fileMarkdown(a.name, a.id, imageTypes.has(a.contentType))))
          onUploaded?.(a)
        })
        .catch((e: unknown) => {
          setValue((v) => v.replace(mark, ''))
          setErrors((es) => [...es, `${file.name}: ${errorMessage(e)}`])
        })
        .finally(() => setPending((n) => n - 1))
    }
    return true
  }

  return {
    pending,
    errors,
    /** Uploads picked files too, at the caret. */
    attach,
    textarea: {
      onPaste: (e: ClipboardEvent<HTMLTextAreaElement>) => {
        if (attach(e.clipboardData.files)) e.preventDefault()
      },
      onDragOver: (e: DragEvent<HTMLTextAreaElement>) => {
        if (e.dataTransfer.types.includes('Files')) e.preventDefault()
      },
      onDrop: (e: DragEvent<HTMLTextAreaElement>) => {
        if (e.dataTransfer.files.length === 0) return
        e.preventDefault()
        attach(e.dataTransfer.files)
      },
    },
  }
}

/** Each message from useInlineFiles, as an alert. */
export function FileErrors({ errors }: { errors: string[] }) {
  return errors.map((m) => (
    <p key={m} className="att-error mt-1 text-sm font-medium" role="alert">
      {m}
    </p>
  ))
}

/**
 * A Markdown textarea with Save and Cancel, for a description. Files pasted
 * or dropped into it become files of its step or node.
 */
export function MarkdownEditor({
  owner,
  label,
  value,
  setValue,
  preview,
  onSave,
  onCancel,
}: {
  owner: FileOwner
  label: string
  value: string
  setValue: (update: (v: string) => string) => void
  preview: boolean
  onSave: () => void
  onCancel: () => void
}) {
  const { ws, view } = useMap()
  const qc = useQueryClient()
  const area = useRef<HTMLTextAreaElement>(null)
  const { pending, errors, textarea } = useInlineFiles({
    area,
    setValue,
    upload: (file) => api.uploadAttachment(ws.slug, view.map.id, owner, file),
    onUploaded: () => void qc.invalidateQueries({ queryKey: [...mapKey(ws.slug, view.map.id), 'attachments', owner.id] }),
  })

  return (
    <>
      {preview ? (
        <div className="markdown min-h-40">
          <Markdown>{value}</Markdown>
        </div>
      ) : (
        <textarea
          ref={area}
          autoFocus
          className="textarea min-h-40 w-full font-medium"
          aria-label={label}
          maxLength={50_000}
          value={value}
          onChange={(e) => {
            const next = e.target.value
            setValue(() => next)
          }}
          {...textarea}
        />
      )}
      <FileErrors errors={errors} />
      <div className="mt-3 flex items-center gap-2">
        <button type="button" className="btn btn-sm btn-primary" disabled={pending > 0} onClick={onSave}>
          {pending > 0 ? 'Uploading…' : 'Save'}
        </button>
        <button type="button" className="btn btn-sm btn-ol" onClick={onCancel}>
          Cancel
        </button>
        <span className="text-muted ml-auto text-xs font-medium">Paste or drop files to attach</span>
      </div>
    </>
  )
}
