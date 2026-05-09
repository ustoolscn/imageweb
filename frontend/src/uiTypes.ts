import type { UploadedImage } from './types'

export type ViewMode = 'tasks' | 'plaza'
export type ThemeMode = 'dark' | 'light'
export type PlazaSort = 'time' | 'likes'
export type PreviewSource = 'reused' | 'new'

export interface ImageForm {
  prompt: string
  model: string
  size: string
  quality: string
  output_format: string
  output_compression: number
  background: string
  moderation: string
  n: number
}

export type PendingReferenceImage = UploadedImage & {
  preview_url: string
  uploading?: boolean
  upload_error?: string
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
