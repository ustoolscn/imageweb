import type { ImageForm } from '../uiTypes'

export type VideoResolution = ImageForm['video_resolution']

export type VideoModelCapability = {
  ratios: string[]
  resolutions: VideoResolution[]
  duration: {
    min: number
    max: number
    default: number
  }
  defaultRatio: string
  defaultResolution: VideoResolution
}

const DOUBAO_SEEDANCE_2: VideoModelCapability = {
  ratios: ['21:9', '16:9', '4:3', '1:1', '3:4', '9:16', 'adaptive'],
  resolutions: ['480p', '720p', '1080p'],
  duration: { min: 4, max: 15, default: 5 },
  defaultRatio: '16:9',
  defaultResolution: '720p',
}

const DEFAULT_VIDEO_CAPABILITY: VideoModelCapability = {
  ratios: ['16:9', '9:16', '1:1', '4:3', '3:4'],
  resolutions: ['480p', '720p', '1080p'],
  duration: { min: 1, max: 30, default: 5 },
  defaultRatio: '16:9',
  defaultResolution: '720p',
}

export function videoModelCapability(model?: string): VideoModelCapability {
  const normalized = normalizeModelName(model)
  if (normalized === 'doubao-seedance-2.0' || normalized === 'doubao-seedance-2-0') return DOUBAO_SEEDANCE_2
  return DEFAULT_VIDEO_CAPABILITY
}

export function videoRatioOptions(model?: string) {
  return videoModelCapability(model).ratios
}

export function videoResolutionOptions(model?: string) {
  return videoModelCapability(model).resolutions
}

export function normalizeVideoSettings(input: {
  model?: string
  ratio?: string
  resolution?: string
  duration?: number
}) {
  const capability = videoModelCapability(input.model)
  const ratio = capability.ratios.includes(input.ratio || '') ? input.ratio || capability.defaultRatio : capability.defaultRatio
  const resolution = capability.resolutions.includes(input.resolution as VideoResolution)
    ? input.resolution as VideoResolution
    : capability.defaultResolution
  const rawDuration = Number(input.duration)
  const duration = Math.min(capability.duration.max, Math.max(capability.duration.min, Number.isFinite(rawDuration) ? rawDuration : capability.duration.default))
  const size = videoSizeFor(ratio, resolution)
  return { ratio, resolution, duration, width: size.width, height: size.height }
}

export function videoSizeFor(ratio: string, resolution: VideoResolution) {
  const sizes = {
    '480p': {
      '21:9': { width: 1120, height: 480 },
      '16:9': { width: 854, height: 480 },
      '9:16': { width: 480, height: 854 },
      '1:1': { width: 480, height: 480 },
      '4:3': { width: 640, height: 480 },
      '3:4': { width: 480, height: 640 },
      adaptive: { width: 854, height: 480 },
    },
    '720p': {
      '21:9': { width: 1680, height: 720 },
      '16:9': { width: 1280, height: 720 },
      '9:16': { width: 720, height: 1280 },
      '1:1': { width: 720, height: 720 },
      '4:3': { width: 960, height: 720 },
      '3:4': { width: 720, height: 960 },
      adaptive: { width: 1280, height: 720 },
    },
    '1080p': {
      '21:9': { width: 2520, height: 1080 },
      '16:9': { width: 1920, height: 1080 },
      '9:16': { width: 1080, height: 1920 },
      '1:1': { width: 1080, height: 1080 },
      '4:3': { width: 1440, height: 1080 },
      '3:4': { width: 1080, height: 1440 },
      adaptive: { width: 1920, height: 1080 },
    },
  } as const
  const byResolution = sizes[resolution] || sizes['720p']
  if (ratio in byResolution) return byResolution[ratio as keyof typeof byResolution]
  return byResolution['16:9']
}

export function videoResolutionFromSize(width: number, height: number): VideoResolution {
  const shortSide = Math.min(width || 0, height || 0)
  if (shortSide >= 1000) return '1080p'
  if (shortSide >= 700) return '720p'
  return '480p'
}

function normalizeModelName(model?: string) {
  return (model || '').trim().toLowerCase()
}
