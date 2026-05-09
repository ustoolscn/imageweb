export type TaskStatus = 'pending' | 'running' | 'succeeded' | 'failed'

export interface UploadedImage {
  url: string
  thumbnail_url?: string
  filename?: string
  original_size?: number
  compressed_size?: number
  compression_ratio?: number
  mask_url?: string
}

export interface Task {
  id: string
  baseurl: string
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
  n: number
  stream: boolean
  style?: string
  response_format?: string
  reference_images: UploadedImage[]
  favorite: boolean
  request_headers: string
  request_json: string
  response_headers: string
  response_json: string
  result_images: UploadedImage[]
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
  prompt: string
  model: string
  size: string
  quality: string
  output_format: string
  output_compression: number
  background: string
  moderation: string
  n: number
  style?: string
  response_format?: string
  reference_images: UploadedImage[]
}

export interface PlazaItem {
  id: string
  task_id: string
  prompt: string
  model: string
  size: string
  quality: string
  output_format: string
  output_compression: number
  background: string
  moderation: string
  n: number
  stream: boolean
  style?: string
  response_format?: string
  reference_images: UploadedImage[]
  result_images: UploadedImage[]
  like_count: number
  liked: boolean
  created_at: string
}
