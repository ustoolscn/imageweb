import type { CreateTaskPayload, Task, UploadedImage } from './types'

async function request<T>(url: string, options?: RequestInit): Promise<T> {
  const response = await fetch(url, options)
  const text = await response.text()
  const data = text ? JSON.parse(text) : null
  if (!response.ok) {
    throw new Error(data?.error || `请求失败：${response.status}`)
  }
  return data as T
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

export async function uploadImage(file: File): Promise<UploadedImage> {
  const form = new FormData()
  form.append('image', file)
  const response = await fetch('https://img.scdn.io/api/v1.php', {
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

export async function listTasks(apikey: string, status: string, q: string, favoriteOnly: boolean): Promise<Task[]> {
  const params = new URLSearchParams({ apikey })
  if (status && status !== 'all') params.set('status', status)
  if (q) params.set('q', q)
  if (favoriteOnly) params.set('favorite', '1')
  const result = await request<{ data: Task[] }>(`/api/tasks?${params}`)
  return result.data
}

export async function getTask(id: string, apikey: string): Promise<Task> {
  return request<Task>(`/api/tasks/${id}?${new URLSearchParams({ apikey })}`)
}

export async function deleteTask(id: string, apikey: string) {
  return request<{ ok: boolean }>(`/api/tasks/${id}?${new URLSearchParams({ apikey })}`, {
    method: 'DELETE',
  })
}

export async function retryTask(id: string, apikey: string): Promise<Task> {
  return request<Task>(`/api/tasks/${id}/retry?${new URLSearchParams({ apikey })}`, {
    method: 'POST',
  })
}

export async function setTaskFavorite(id: string, apikey: string, favorite: boolean): Promise<Task> {
  return request<Task>(`/api/tasks/${id}/favorite?${new URLSearchParams({ apikey })}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ favorite }),
  })
}
