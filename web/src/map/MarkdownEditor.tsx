import { useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { api } from '../api'
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
 * A Markdown textarea with Save and Cancel. A file pasted or dropped into it
 * becomes a file of the step: a placeholder marks the spot while it uploads,
 * then turns into `![name](attachment:id)` (or a link, for anything but an image).
 */
export function MarkdownEditor({
  stepId,
  label,
  value,
  setValue,
  preview,
  onSave,
  onCancel,
}: {
  stepId: string
  label: string
  value: string
  /** Takes an update, since uploads finish after other typing. */
  setValue: (update: (v: string) => string) => void
  preview: boolean
  onSave: () => void
  onCancel: () => void
}) {
  const { ws, view } = useMap()
  const qc = useQueryClient()
  const area = useRef<HTMLTextAreaElement>(null)
  const [pending, setPending] = useState(0)
  const [errors, setErrors] = useState<string[]>([])

  const attach = (list: FileList) => {
    const files = Array.from(list).map(named)
    if (files.length === 0) return false
    const marks = files.map((f) => ({ file: f, mark: `![Uploading ${f.name.replace(/[[\]\\]/g, '')}…](uploading:${crypto.randomUUID()})` }))
    const at = area.current?.selectionStart ?? value.length
    const end = area.current?.selectionEnd ?? at
    // Each file goes on a line of its own.
    setValue((v) => {
      const before = v.slice(0, at)
      const after = v.slice(end)
      const open = before && !before.endsWith('\n') ? '\n' : ''
      const close = after && !after.startsWith('\n') ? '\n' : ''
      return before + open + marks.map((m) => m.mark).join('\n') + close + after
    })
    setPending((n) => n + files.length)
    setErrors([])
    for (const { file, mark } of marks) {
      api
        .uploadAttachment(ws.slug, view.map.id, stepId, file)
        .then((a) => {
          setValue((v) => v.replace(mark, fileMarkdown(a.name, a.id, imageTypes.has(a.contentType))))
          void qc.invalidateQueries({ queryKey: [...mapKey(ws.slug, view.map.id), 'attachments', stepId] })
        })
        .catch((e: unknown) => {
          setValue((v) => v.replace(mark, ''))
          setErrors((es) => [...es, `${file.name}: ${errorMessage(e)}`])
        })
        .finally(() => setPending((n) => n - 1))
    }
    return true
  }

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
          onPaste={(e) => {
            if (attach(e.clipboardData.files)) e.preventDefault()
          }}
          onDragOver={(e) => {
            if (e.dataTransfer.types.includes('Files')) e.preventDefault()
          }}
          onDrop={(e) => {
            if (e.dataTransfer.files.length === 0) return
            e.preventDefault()
            attach(e.dataTransfer.files)
          }}
        />
      )}
      {errors.map((m) => (
        <p key={m} className="att-error mt-1 text-sm font-medium" role="alert">
          {m}
        </p>
      ))}
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
