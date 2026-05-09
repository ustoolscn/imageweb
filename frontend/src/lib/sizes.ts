export const ratioSizePresets = {
  '1:1': { '1K': '1024x1024', '2K': '2048x2048', '4K': '2880x2880' },
  '3:2': { '1K': '1536x1024', '2K': '2048x1360', '4K': '3520x2352' },
  '2:3': { '1K': '1024x1536', '2K': '1360x2048', '4K': '2352x3520' },
  '16:9': { '1K': '1824x1024', '2K': '2048x1152', '4K': '3840x2160' },
  '9:16': { '1K': '1024x1824', '2K': '1152x2048', '4K': '2160x3840' },
  '4:3': { '1K': '1360x1024', '2K': '2048x1536', '4K': '3312x2480' },
  '3:4': { '1K': '1024x1360', '2K': '1536x2048', '4K': '2480x3312' },
  '21:9': { '1K': '2384x1024', '2K': '2048x880', '4K': '3840x1648' },
} as const

export type RatioOption = keyof typeof ratioSizePresets
export type SizeBase = 'auto' | '1K' | '2K' | '4K'

export const ratioOptions = Object.keys(ratioSizePresets) as RatioOption[]
export const sizeBaseOptions: Array<{ value: SizeBase; label: string; description: string }> = [
  { value: 'auto', label: '自动', description: '由模型自动选择合适尺寸' },
  { value: '1K', label: '1K', description: '标准分辨率' },
  { value: '2K', label: '2K', description: '更高清分辨率' },
  { value: '4K', label: '4K', description: '超高清分辨率' },
]

export function sizeFromRatio(base: string, ratio: string) {
  if (base === 'auto') return 'auto'
  return ratioSizePresets[ratio as RatioOption]?.[base as Exclude<SizeBase, 'auto'>] || '1024x1024'
}

export function ratioPreviewStyle(ratio: string) {
  const [a, b] = ratio.split(':').map(Number)
  const scale = 24 / Math.max(a, b)
  return {
    width: `${Math.max(6, Math.round(a * scale))}px`,
    height: `${Math.max(6, Math.round(b * scale))}px`,
  }
}
