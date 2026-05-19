import type { MediaAsset, UploadedImage } from './types'

export type ViewMode = 'tasks' | 'canvas' | 'plaza'
export type ThemeMode = 'system' | 'dark' | 'light'
export type AppliedThemeMode = 'dark' | 'light'
export type PlazaSort = 'time' | 'likes'
export type PreviewSource = 'reused' | 'new'

export interface ImageForm {
  task_type: 'image_generation' | 'video_generation'
  prompt: string
  model: string
  size: string
  quality: string
  output_format: string
  output_compression: number
  background: string
  moderation: string
  input_fidelity: string
  n: number
  video_ratio: string
  video_resolution: '480p' | '720p' | '1080p'
  video_width: number
  video_height: number
  video_duration: number
  generate_audio: boolean
  watermark: boolean
  reference_video_urls: string
  reference_audio_urls: string
}

export type CanvasRunPayload = {
  node_kind: 'image' | 'video'
  prompt: string
  task_type: ImageForm['task_type']
  model: string
  size: string
  quality: string
  output_format: string
  output_compression: number
  background: string
  moderation: string
  input_fidelity: string
  reference_images: UploadedImage[]
  reference_videos: MediaAsset[]
  reference_audios: MediaAsset[]
  video_ratio: string
  video_resolution: ImageForm['video_resolution']
  video_duration: number
  generate_audio: boolean
  watermark: boolean
}

export type CanvasLLMPayload = {
  prompt: string
  model: string
  reasoning_effort: string
  reference_images: UploadedImage[]
  reference_videos: MediaAsset[]
  reference_audios: MediaAsset[]
}

export type PendingReferenceImage = UploadedImage & {
  preview_url: string
  uploading?: boolean
  upload_error?: string
}

export type PendingReferenceVideo = MediaAsset & {
  cover_url?: string
  loading?: boolean
  error?: string
}

export type PendingReferenceAudio = MediaAsset & {
  loading?: boolean
  error?: string
}

export type PreviewImage = {
  url: string
  label: string
  maskUrl?: string
  editable?: boolean
  source?: PreviewSource
  index?: number
}

export interface SettingsPayload {
  baseurl: string
  apikey: string
}
