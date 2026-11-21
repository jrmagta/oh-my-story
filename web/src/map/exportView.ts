/**
 * Export what the map shows (the map grid or the roadmap, with its filter,
 * card size, collapsed rows and focus) as a PNG or a one-page PDF, in the
 * browser. html-to-image draws a copy of the page into an SVG image, which our
 * CSP allows as a data: image; jsPDF wraps that picture in a PDF. Both are
 * loaded on first use so the map page does not carry them.
 */

/** Controls and editing affordances that make no sense on paper. */
const leaveOut = '.card-menu, .side-add, .add-step, .add-slot, .band-toggle, [data-export="skip"]'

export type ExportKind = 'png' | 'pdf'

/** "Onboarding plan" → "onboarding-plan". */
function fileStem(name: string): string {
  return (
    name
      .toLowerCase()
      .normalize('NFKD')
      .replace(/[^a-z0-9]+/g, '-')
      .replace(/^-+|-+$/g, '')
      .slice(0, 60) || 'map'
  )
}

function today(): string {
  const d = new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

/** The file name for an export: map-name-roadmap-2026-11-21.pdf. */
export function exportName(mapName: string, view: 'map' | 'roadmap', kind: ExportKind): string {
  return `${fileStem(mapName)}${view === 'roadmap' ? '-roadmap' : ''}-${today()}.${kind}`
}

function download(url: string, name: string) {
  const a = document.createElement('a')
  a.href = url
  a.download = name
  document.body.append(a)
  a.click()
  a.remove()
}

/**
 * Draws el (with the export caption showing) and saves it. The page's own
 * background colour is used, so a dark-theme export stays dark.
 */
export async function exportView(el: HTMLElement, name: string, kind: ExportKind): Promise<void> {
  const { toPng } = await import('html-to-image')
  el.classList.add('exporting')
  try {
    const width = el.scrollWidth
    const height = el.scrollHeight
    const png = await toPng(el, {
      pixelRatio: 2,
      width,
      height,
      backgroundColor: getComputedStyle(document.body).backgroundColor,
      filter: (node) => !(node instanceof Element && node.matches(leaveOut)),
    })
    if (kind === 'png') {
      download(png, name)
      return
    }
    const { jsPDF } = await import('jspdf')
    const pdf = new jsPDF({ orientation: width >= height ? 'landscape' : 'portrait', unit: 'px', format: [width, height], hotfixes: ['px_scaling'] })
    // FAST compresses the picture; without it a small map's PDF ran to megabytes.
    pdf.addImage(png, 'PNG', 0, 0, width, height, undefined, 'FAST')
    pdf.save(name)
  } finally {
    el.classList.remove('exporting')
  }
}
