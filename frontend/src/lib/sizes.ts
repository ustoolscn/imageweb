export const ratioSizePresets = {
  '1:1': { '1K': '1024x1024' },
  '3:2': { '1K': '1536x1024' },
  '2:3': { '1K': '1024x1536' },
  '16:9': { '1K': '1824x1024' },
  '9:16': { '1K': '1024x1824' },
  '4:3': { '1K': '1360x1024' },
  '3:4': { '1K': '1024x1360' },
  '21:9': { '1K': '2384x1024' },
} as const

export type RatioOption = keyof typeof ratioSizePresets
export type SizeBase = '1K'

export const ratioOptions = Object.keys(ratioSizePresets) as RatioOption[]

export function sizeFromRatio(base: string, ratio: string) {
  return ratioSizePresets[ratio as RatioOption]?.[base as SizeBase] || '1024x1024'
}

export function ratioPreviewStyle(ratio: string) {
  const [a, b] = ratio.split(':').map(Number)
  const scale = 24 / Math.max(a, b)
  return {
    width: `${Math.max(6, Math.round(a * scale))}px`,
    height: `${Math.max(6, Math.round(b * scale))}px`,
  }
}
