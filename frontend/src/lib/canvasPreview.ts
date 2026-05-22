import type { MediaAsset, PlazaItem, Task, UploadedImage } from '../types'

type CanvasPreviewKind = 'image' | 'video'

type CanvasPreviewMedia = {
  type: CanvasPreviewKind
  url: string
  thumbnail_url?: string
  label: string
  width?: number
  height?: number
}

type CanvasElementLike = {
  id?: unknown
  kind?: unknown
  task_snapshot?: Task
  media_type?: unknown
  media_url?: unknown
  media_thumbnail_url?: unknown
  media_filename?: unknown
  width?: unknown
  height?: unknown
  x?: unknown
  y?: unknown
  zIndex?: unknown
}

type CanvasConnectionLike = {
  from?: unknown
  to?: unknown
}

type CanvasLike = {
  elements?: unknown[]
  connections?: unknown[]
}

export function canvasPreviewMedia(item: PlazaItem): CanvasPreviewMedia | undefined {
  if (item.item_type !== 'canvas') return undefined
  const canvas = item.canvas as CanvasLike | undefined
  if (!Array.isArray(canvas?.elements)) return undefined

  const elements = canvas.elements.filter(isCanvasElement)
  if (!elements.length) return undefined

  const connections = Array.isArray(canvas.connections) ? canvas.connections.filter(isCanvasConnection) : []
  const outgoingIDs = new Set(connections.map((connection) => stringValue(connection.from)).filter(Boolean))
  const terminalElements = elements.filter((element) => {
    const id = stringValue(element.id)
    return id && !outgoingIDs.has(id)
  })

  return bestPreviewFromElements(terminalElements, connections) || bestPreviewFromElements(elements, connections)
}

export function canvasPreviewUrl(item: PlazaItem) {
  const media = canvasPreviewMedia(item)
  return media?.thumbnail_url || media?.url || ''
}

export function canvasPreviewAspectRatio(item: PlazaItem) {
  const media = canvasPreviewMedia(item)
  if (media?.width && media.height) return `${media.width} / ${media.height}`
  return '4 / 3'
}

function bestPreviewFromElements(elements: CanvasElementLike[], connections: CanvasConnectionLike[]) {
  const candidates = elements
    .map((element) => ({ element, media: mediaFromElement(element), depth: nodeDepth(element, connections) }))
    .filter((item): item is { element: CanvasElementLike; media: CanvasPreviewMedia; depth: number } => Boolean(item.media))
    .sort((a, b) => (
      b.depth - a.depth ||
      numberValue(b.element.x) - numberValue(a.element.x) ||
      numberValue(b.element.y) - numberValue(a.element.y) ||
      numberValue(b.element.zIndex) - numberValue(a.element.zIndex)
    ))
  return candidates[0]?.media
}

function mediaFromElement(element: CanvasElementLike): CanvasPreviewMedia | undefined {
  const localMedia = localMediaFromElement(element)
  if (localMedia) return localMedia

  const task = element.task_snapshot
  const image = task?.result_images?.find((item) => item.url)
  if (image?.url) return imagePreview(image, element)

  const video = task?.result_videos?.find((item) => item.url)
  if (video?.url) return videoPreview(video, element)

  return undefined
}

function localMediaFromElement(element: CanvasElementLike): CanvasPreviewMedia | undefined {
  const url = stringValue(element.media_url)
  if (!url) return undefined

  const mediaType = stringValue(element.media_type) || mediaTypeFromKind(stringValue(element.kind))
  if (mediaType === 'video') {
    return {
      type: 'video',
      url,
      thumbnail_url: stringValue(element.media_thumbnail_url),
      label: stringValue(element.media_filename) || '画布视频',
    }
  }

  if (mediaType === 'image' || isImageURL(url)) {
    return {
      type: 'image',
      url,
      thumbnail_url: stringValue(element.media_thumbnail_url),
      label: stringValue(element.media_filename) || '画布图片',
    }
  }

  return undefined
}

function imagePreview(image: UploadedImage, element: CanvasElementLike): CanvasPreviewMedia {
  const taskSize = parseSize(element.task_snapshot?.size)
  return {
    type: 'image',
    url: image.url,
    thumbnail_url: image.thumbnail_url,
    label: image.filename || stringValue(element.kind) || '画布图片',
    ...taskSize,
  }
}

function videoPreview(video: MediaAsset, element: CanvasElementLike): CanvasPreviewMedia {
  return {
    type: 'video',
    url: video.url,
    thumbnail_url: video.thumbnail_url,
    label: video.filename || stringValue(element.kind) || '画布视频',
    width: positiveNumber(video.width) || positiveNumber(element.task_snapshot?.video_width),
    height: positiveNumber(video.height) || positiveNumber(element.task_snapshot?.video_height),
  }
}

function nodeDepth(element: CanvasElementLike, connections: CanvasConnectionLike[], seen = new Set<string>()): number {
  const id = stringValue(element.id)
  if (!id || seen.has(id)) return 0
  seen.add(id)
  const upstream = connections.filter((connection) => stringValue(connection.to) === id)
  if (!upstream.length) return 0
  return 1 + Math.max(...upstream.map((connection) => nodeDepth({ id: connection.from }, connections, new Set(seen))))
}

function isCanvasElement(value: unknown): value is CanvasElementLike {
  return Boolean(value && typeof value === 'object')
}

function isCanvasConnection(value: unknown): value is CanvasConnectionLike {
  return Boolean(value && typeof value === 'object')
}

function mediaTypeFromKind(kind: string) {
  if (kind === 'image_media' || kind === 'tail_frame') return 'image'
  if (kind === 'video_media') return 'video'
  return ''
}

function isImageURL(url: string) {
  return /\.(avif|gif|jpe?g|png|webp)(?:[?#].*)?$/i.test(url)
}

function parseSize(value?: string) {
  const match = value?.toLowerCase().match(/^(\d+)x(\d+)$/)
  if (!match) return undefined
  const width = Number(match[1])
  const height = Number(match[2])
  return width > 0 && height > 0 ? { width, height } : undefined
}

function stringValue(value: unknown) {
  return typeof value === 'string' ? value.trim() : ''
}

function numberValue(value: unknown) {
  return typeof value === 'number' && Number.isFinite(value) ? value : 0
}

function positiveNumber(value: unknown) {
  const number = numberValue(value)
  return number > 0 ? number : undefined
}
