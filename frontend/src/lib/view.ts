import type { PlazaItem, Task, UploadedImage } from '../types'

export function isFavorite(task: Pick<Task, 'favorite'>) {
  return Boolean(task.favorite)
}

export function canShareTask(task: Pick<Task, 'status' | 'result_images' | 'result_videos'>) {
  return task.status === 'succeeded' && Boolean(task.result_images?.[0]?.url || task.result_videos?.[0]?.url)
}

export function canOpenSource(task: Pick<Task, 'status'>) {
  return task.status === 'succeeded' || task.status === 'failed'
}

export function taskReferenceImages(task: Pick<Task | PlazaItem, 'reference_images'>) {
  return [...(task.reference_images || [])]
}

export function displayImageURL(image?: UploadedImage) {
  return image?.thumbnail_url || image?.url || ''
}

export function taskResultURL(task: Pick<Task, 'result_images' | 'result_videos'>) {
  return task.result_videos?.[0]?.url || task.result_images?.[0]?.url || ''
}

export function isVideoTask(task: Pick<Task | PlazaItem, 'task_type'>) {
  return task.task_type === 'video_generation'
}

export function prettySource(value: string) {
  if (!value) return '暂无数据'
  try {
    return JSON.stringify(JSON.parse(value), null, 2)
  } catch {
    return value
  }
}

export function statusText(value: string) {
  return ({ pending: '排队中', running: '生成中', succeeded: '成功', failed: '失败' } as Record<string, string>)[value] || value
}

export function queueText(task: Pick<Task, 'status' | 'queue_position'>) {
  if (task.status !== 'pending') return ''
  return task.queue_position > 0 ? `前面还有 ${task.queue_position} 个` : '即将开始'
}

export function statusClass(value: string) {
  return `status-${value}`
}

export function formatMs(ms: number) {
  const total = Math.max(0, Math.round(ms / 1000))
  const minutes = Math.floor(total / 60)
  const seconds = total % 60
  return `${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')}`
}

export function elapsed(task: Pick<Task, 'elapsed_ms' | 'started_at' | 'created_at'>, clock: number) {
  if (task.elapsed_ms) return formatMs(task.elapsed_ms)
  const start = new Date(task.started_at || task.created_at).getTime()
  return formatMs(clock - start)
}

export function timeText(task: Pick<Task, 'status' | 'elapsed_ms' | 'started_at' | 'created_at'>, clock: number) {
  if (task.status === 'pending') return `等待 ${formatMs(clock - new Date(task.created_at).getTime())}`
  if (task.status === 'running') return `生成 ${elapsed(task, clock)}`
  return `耗时 ${elapsed(task, clock)}`
}

export function formatTime(value: string) {
  return new Date(value).toLocaleString('zh-CN', { hour12: false })
}

export function maskBaseURL(value: string) {
  try {
    return new URL(value).host || value
  } catch {
    return value
  }
}
