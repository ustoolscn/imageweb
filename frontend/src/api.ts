import type { CreateTaskPayload, MediaAsset, PlazaItem, Task, UploadedImage } from './types'

export class APIError extends Error {
  code?: string
  adminContactImage?: string

  constructor(message: string, code?: string, adminContactImage?: string) {
    super(message)
    this.code = code
    this.adminContactImage = adminContactImage
  }
}

async function request<T>(url: string, options?: RequestInit): Promise<T> {
  const response = await fetch(url, options)
  const text = await response.text()
  const data = parseResponseBody(text)
  if (!response.ok) {
    throw new APIError(data?.error || `请求失败：${response.status}`, data?.code, data?.admin_contact_image)
  }
  return data as T
}

function parseResponseBody(text: string): any {
  const trimmed = text.trim()
  if (!trimmed) return null
  try {
    return JSON.parse(trimmed)
  } catch {
    if (trimmed.startsWith('data:')) return { text: parseSSEText(trimmed) }
    return { text: trimmed }
  }
}

function parseSSEText(text: string) {
  const parts: string[] = []
  for (const line of text.split(/\r?\n/)) {
    const trimmed = line.trim()
    if (!trimmed.startsWith('data:')) continue
    const payload = trimmed.slice(5).trim()
    if (!payload || payload === '[DONE]') continue
    try {
      const event = JSON.parse(payload) as {
        choices?: Array<{ delta?: { content?: unknown }; message?: { content?: unknown } }>
        output_text?: string
        text?: string
      }
      if (event.output_text) parts.push(event.output_text)
      if (event.text) parts.push(event.text)
      for (const choice of event.choices || []) {
        appendContentPart(parts, choice.delta?.content)
        appendContentPart(parts, choice.message?.content)
      }
    } catch {
      parts.push(payload)
    }
  }
  return parts.join('').trim()
}

function appendContentPart(parts: string[], content: unknown) {
  if (typeof content === 'string') {
    parts.push(content)
    return
  }
  if (Array.isArray(content)) {
    for (const item of content) {
      if (item && typeof item === 'object' && 'text' in item && typeof item.text === 'string') parts.push(item.text)
    }
  }
}

export type SiteBrand = {
  title: string
  icon: string
  allow_2k?: boolean
  allow_4k?: boolean
}

export async function fetchSiteBrand(baseurl: string) {
  const params = new URLSearchParams()
  if (baseurl) params.set('baseurl', baseurl)
  return request<SiteBrand>(`/api/site-brand?${params}`)
}

function toUploadedImage(data: unknown): UploadedImage {
  const response = data as {
    success?: boolean
    url?: string
    data?: UploadedImage
  }
  if (!response.success) {
    throw new Error('图片上传失败')
  }
  const url = response.data?.url || response.url
  if (!url) {
    throw new Error('图片上传失败：图床未返回链接')
  }
  return {
    url,
    thumbnail_url: response.data?.thumbnail_url,
    filename: response.data?.filename,
    original_size: response.data?.original_size,
    compressed_size: response.data?.compressed_size,
    compression_ratio: response.data?.compression_ratio,
  }
}

export async function fetchModels(baseurl: string, apikey: string) {
  return request<{ data?: Array<{ id: string }>; object?: string }>('/api/models', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ baseurl, apikey }),
  })
}

export async function runLLM(payload: {
  apikey: string
  baseurl: string
  model: string
  reasoning_effort: string
  prompt: string
  reference_images: UploadedImage[]
  reference_videos: MediaAsset[]
  reference_audios: MediaAsset[]
}) {
  return request<{ text: string }>('/api/llm', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  })
}

export async function uploadImage(file: File): Promise<UploadedImage> {
  const form = new FormData()
  form.append('file', file)
  const response = await fetch('/api/upload', {
    method: 'POST',
    body: form,
  })
  const text = await response.text()
  const data = text ? JSON.parse(text) : null
  if (!response.ok) {
    throw new Error(data?.error || `图片上传失败：${response.status}`)
  }
  return toUploadedImage(data)
}

export async function createTask(payload: CreateTaskPayload): Promise<Task> {
  return request<Task>('/api/tasks', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  })
}

export async function listTasks(apikey: string, baseurl: string, status: string, q: string, favoriteOnly: boolean, beforeCreatedAt = '', beforeID = '', limit = 30) {
  const params = new URLSearchParams({ apikey, baseurl, limit: String(limit) })
  if (status && status !== 'all') params.set('status', status)
  if (q) params.set('q', q)
  if (favoriteOnly) params.set('favorite', '1')
  if (beforeCreatedAt && beforeID) {
    params.set('before_created_at', beforeCreatedAt)
    params.set('before_id', beforeID)
  }
  return request<{ data: Task[]; has_more: boolean; next_before_created_at: string; next_before_id: string; total: number }>(`/api/tasks?${params}`)
}

export type TaskUpdate = Pick<Task, 'id' | 'status' | 'result_images' | 'result_videos' | 'error_message' | 'elapsed_ms' | 'updated_at' | 'started_at' | 'completed_at' | 'queue_position' | 'upstream_status' | 'upstream_progress'>

export async function fetchTaskUpdates(apikey: string, baseurl: string, ids: string[]): Promise<TaskUpdate[]> {
  if (!ids.length) return []
  return request<{ data: TaskUpdate[] }>(`/api/tasks/updates?${new URLSearchParams({ apikey, baseurl, ids: ids.join(',') })}`).then((result) => result.data)
}

export async function getTask(id: string, apikey: string, baseurl: string): Promise<Task> {
  return request<Task>(`/api/tasks/${id}?${new URLSearchParams({ apikey, baseurl })}`)
}

export async function deleteTask(id: string, apikey: string, baseurl: string) {
  return request<{ ok: boolean }>(`/api/tasks/${id}?${new URLSearchParams({ apikey, baseurl })}`, {
    method: 'DELETE',
  })
}

export async function retryTask(id: string, apikey: string, baseurl: string): Promise<Task> {
  return request<Task>(`/api/tasks/${id}/retry?${new URLSearchParams({ apikey, baseurl })}`, {
    method: 'POST',
  })
}

export async function setTaskFavorite(id: string, apikey: string, baseurl: string, favorite: boolean): Promise<Task> {
  return request<Task>(`/api/tasks/${id}/favorite?${new URLSearchParams({ apikey, baseurl })}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ favorite }),
  })
}

export async function shareTask(id: string, apikey: string, baseurl: string): Promise<PlazaItem> {
  return request<PlazaItem>(`/api/tasks/${id}/share`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ apikey, baseurl }),
  })
}

export async function unshareTask(id: string, apikey: string, baseurl: string) {
  return request<{ ok: boolean }>(`/api/tasks/${id}/share`, {
    method: 'DELETE',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ apikey, baseurl }),
  })
}

export async function listPlazaItems(sort: string, clientID: string, q = '', beforeCreatedAt = '', beforeID = '', beforeLikeCount = 0, limit = 30) {
  const params = new URLSearchParams({ sort, client_id: clientID, limit: String(limit) })
  if (q) params.set('q', q)
  if (beforeCreatedAt && beforeID) {
    params.set('before_created_at', beforeCreatedAt)
    params.set('before_id', beforeID)
    params.set('before_like_count', String(beforeLikeCount))
  }
  return request<{ data: PlazaItem[]; has_more: boolean; next_before_created_at: string; next_before_id: string; next_before_like_count: number; total: number }>(`/api/plaza?${params}`)
}

export async function setPlazaLike(id: string, clientID: string, liked: boolean): Promise<PlazaItem> {
  return request<PlazaItem>(`/api/plaza/${id}/like`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ client_id: clientID, liked }),
  })
}
