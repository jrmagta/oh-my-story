import type { CSSProperties } from 'react'

/** Swatches offered for backbone cards; any other #RRGGBB can be picked too. */
export const palette: { name: string; hex: string }[] = [
  { name: 'Red', hex: '#BA3636' },
  { name: 'Amber', hex: '#FCB131' },
  { name: 'Green', hex: '#549334' },
  { name: 'Teal', hex: '#2A9D8F' },
  { name: 'Blue', hex: '#3B82C4' },
  { name: 'Purple', hex: '#7E57C2' },
  { name: 'Pink', hex: '#D9608F' },
  { name: 'Grey', hex: '#8A8A93' },
]

/**
 * Black text on light colours, white on the rest. The cut-off sits above the
 * WCAG midpoint (0.179) so mid-tones such as the brand green and blue get
 * white text, as release bands and status rings do; only light colours like
 * amber get black.
 */
export function textOn(hex: string): '#000' | '#fff' {
  const channel = (i: number) => {
    const c = parseInt(hex.slice(i, i + 2), 16) / 255
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
  }
  const l = 0.2126 * channel(1) + 0.7152 * channel(3) + 0.0722 * channel(5)
  return l > 0.4 ? '#000' : '#fff'
}

/** CSS variables for a coloured backbone card; none for the level's default look. */
export function nodeStyle(colour: string | null): CSSProperties {
  return colour ? ({ '--node-bg': colour, '--node-fg': textOn(colour) } as CSSProperties) : {}
}
