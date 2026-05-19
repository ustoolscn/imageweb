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
export const nanoBananaRatios = ['auto', '1:1', '1:4', '1:8', '2:3', '3:2', '3:4', '4:1', '4:3', '4:5', '5:4', '8:1', '9:16', '16:9', '21:9'] as const
export const nanoBananaImageSizes = ['512', '1K', '2K', '4K'] as const
export const seedreamSizePresets = {
  '2K': { '1:1': '2048x2048', '4:3': '2304x1728', '3:4': '1728x2304', '16:9': '2848x1600', '9:16': '1600x2848', '3:2': '2496x1664', '2:3': '1664x2496', '21:9': '3136x1344' },
  '3K': { '1:1': '3072x3072', '4:3': '3456x2592', '3:4': '2592x3456', '16:9': '4096x2304', '9:16': '2304x4096', '2:3': '2496x3744', '3:2': '3744x2496', '21:9': '4704x2016' },
  '4K': { '1:1': '4096x4096', '3:4': '3520x4704', '4:3': '4704x3520', '16:9': '5504x3040', '9:16': '3040x5504', '2:3': '3328x4992', '3:2': '4992x3328', '21:9': '6240x2656' },
} as const
export const seedreamSizeBases = Object.keys(seedreamSizePresets) as Array<keyof typeof seedreamSizePresets>
export const seedreamRatios = ['auto', '1:1', '4:3', '3:4', '16:9', '9:16', '3:2', '2:3', '21:9'] as const

export type RatioOption = keyof typeof ratioSizePresets
export type SizeBase = 'auto' | '512' | '1K' | '2K' | '3K' | '4K'

export const ratioOptions = Object.keys(ratioSizePresets) as RatioOption[]
export const sizeBaseOptions: Array<{ value: SizeBase; label: string; description: string }> = [
  { value: 'auto', label: '自动', description: '由模型自动选择合适尺寸' },
  { value: '1K', label: '1K', description: '标准分辨率' },
  { value: '2K', label: '2K', description: '更高清分辨率' },
  { value: '4K', label: '4K', description: '超高清分辨率' },
]
export const nanoBananaSizeBaseOptions: Array<{ value: SizeBase; label: string; description: string }> = [
  { value: '512', label: '512', description: '512 分辨率' },
  { value: '1K', label: '1K', description: '1K 分辨率' },
  { value: '2K', label: '2K', description: '2K 分辨率' },
  { value: '4K', label: '4K', description: '4K 分辨率' },
]
export const seedreamSizeBaseOptions: Array<{ value: SizeBase; label: string; description: string }> = [
  { value: '2K', label: '2K', description: '2K 分辨率' },
  { value: '3K', label: '3K', description: '3K 分辨率' },
  { value: '4K', label: '4K', description: '4K 分辨率' },
]

export function sizeFromRatio(base: string, ratio: string) {
  if (base === 'auto') return 'auto'
  return ratioSizePresets[ratio as RatioOption]?.[base as keyof typeof ratioSizePresets[RatioOption]] || '1024x1024'
}

export function nanoBananaSizeValue(base: string, ratio: string) {
  const imageSize = nanoBananaImageSizes.includes(base as typeof nanoBananaImageSizes[number]) ? base : '1K'
  const aspectRatio = nanoBananaRatios.includes(ratio as typeof nanoBananaRatios[number]) ? ratio : '1:1'
  return `${imageSize} ${aspectRatio}`
}

export function parseNanoBananaSize(value: string) {
  const [imageSize, aspectRatio] = value.trim().split(/\s+/)
  return {
    imageSize: nanoBananaImageSizes.includes(imageSize as typeof nanoBananaImageSizes[number]) ? imageSize : '1K',
    aspectRatio: nanoBananaRatios.includes(aspectRatio as typeof nanoBananaRatios[number]) ? aspectRatio : '1:1',
  }
}

export function seedreamSizeValue(base: string, ratio: string) {
  const imageSize = seedreamSizeBases.includes(base as keyof typeof seedreamSizePresets) ? base as keyof typeof seedreamSizePresets : '2K'
  if (ratio === 'auto') return imageSize
  const aspectRatio = Object.keys(seedreamSizePresets[imageSize]).includes(ratio) ? ratio as keyof typeof seedreamSizePresets[typeof imageSize] : '16:9'
  return seedreamSizePresets[imageSize][aspectRatio]
}

export function parseSeedreamSize(value: string) {
  if (seedreamSizeBases.includes(value as keyof typeof seedreamSizePresets)) return { imageSize: value as keyof typeof seedreamSizePresets, aspectRatio: 'auto' }
  for (const base of seedreamSizeBases) {
    for (const ratio of seedreamRatios) {
      if (ratio === 'auto') continue
      if (seedreamSizePresets[base][ratio] === value) return { imageSize: base, aspectRatio: ratio }
    }
  }
  return { imageSize: '2K', aspectRatio: '16:9' }
}

export function ratioPreviewStyle(ratio: string) {
  const [a, b] = ratio.split(':').map(Number)
  const scale = 24 / Math.max(a, b)
  return {
    width: `${Math.max(6, Math.round(a * scale))}px`,
    height: `${Math.max(6, Math.round(b * scale))}px`,
  }
}
