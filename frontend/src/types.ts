export type TaskStatus = 'pending' | 'running' | 'succeeded' | 'failed'

export interface UploadedImage {
  url: string
  thumbnail_url?: string
  filename?: string
  node_id?: string
  reference_label?: string
  mask_reference_label?: string
  original_size?: number
  compressed_size?: number
  compression_ratio?: number
  mask_url?: string
}

export interface MediaAsset {
  type?: 'video' | 'audio' | string
  url: string
  thumbnail_url?: string
  filename?: string
  node_id?: string
  reference_label?: string
  duration?: number
  clip_start?: number
  clip_end?: number
  width?: number
  height?: number
}

export interface Task {
  id: string
  baseurl: string
  task_type: 'image_generation' | 'video_generation'
  status: TaskStatus
  prompt: string
  final_prompt: string
  model: string
  size: string
  quality: string
  output_format: string
  output_compression: number
  background: string
  moderation: string
  input_fidelity: string
  n: number
  stream: boolean
  style?: string
  response_format?: string
  reference_images: UploadedImage[]
  reference_videos: MediaAsset[]
  reference_audios: MediaAsset[]
  favorite: boolean
  request_headers: string
  request_json: string
  response_headers: string
  response_json: string
  result_images: UploadedImage[]
  result_videos: MediaAsset[]
  upstream_task_id?: string
  upstream_status?: string
  upstream_progress: number
  video_ratio?: string
  video_width?: number
  video_height?: number
  video_duration?: number
  generate_audio?: boolean
  watermark?: boolean
  error_message: string
  elapsed_ms: number
  created_at: string
  updated_at: string
  started_at?: string
  completed_at?: string
  queue_position: number
  shared_to_plaza: boolean
}

export interface CreateTaskPayload {
  apikey: string
  baseurl: string
  node_kind?: 'image' | 'video'
  task_type?: 'image_generation' | 'video_generation'
  prompt: string
  model: string
  size?: string
  quality?: string
  output_format?: string
  output_compression?: number
  background?: string
  moderation?: string
  input_fidelity?: string
  n?: number
  style?: string
  response_format?: string
  reference_images: UploadedImage[]
  reference_videos?: MediaAsset[]
  reference_audios?: MediaAsset[]
  video_ratio?: string
  video_width?: number
  video_height?: number
  video_duration?: number
  generate_audio?: boolean
  watermark?: boolean
}

export interface PlazaItem {
  id: string
  task_id: string
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
  stream: boolean
  style?: string
  response_format?: string
  reference_images: UploadedImage[]
  reference_videos: MediaAsset[]
  reference_audios: MediaAsset[]
  result_images: UploadedImage[]
  result_videos: MediaAsset[]
  video_ratio?: string
  video_width?: number
  video_height?: number
  video_duration?: number
  generate_audio?: boolean
  watermark?: boolean
  like_count: number
  liked: boolean
  created_at: string
}
