import ReactMarkdown, { defaultUrlTransform } from 'react-markdown'
import { useMap } from '../pages/map'

const scheme = 'attachment:'

/**
 * Markdown with links to the map's files: `![name](attachment:<id>)` shows the
 * image and `[name](attachment:<id>)` links to the file, through the
 * workspace in the app and through the link on a share page.
 */
export function Markdown({ children }: { children: string }) {
  const { files } = useMap()
  return (
    <ReactMarkdown urlTransform={(url) => (url.startsWith(scheme) ? files.url(url.slice(scheme.length)) : defaultUrlTransform(url))}>
      {children}
    </ReactMarkdown>
  )
}

/** The Markdown that shows a file uploaded from an editor: an image inline, anything else as a link. */
export function fileMarkdown(name: string, id: string, image: boolean): string {
  const text = name.replace(/[[\]\\]/g, '')
  return `${image ? '!' : ''}[${text}](${scheme}${id})`
}
