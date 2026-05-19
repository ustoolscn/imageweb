<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { ConnectionLineType, ConnectionMode, Handle, MarkerType, Position, VueFlow, useVueFlow, type Connection, type Edge, type EdgeChange, type EdgeMouseEvent, type EdgeUpdateEvent, type Node, type NodeChange, type NodeDragEvent, type ViewportTransform } from '@vue-flow/core'
import { MiniMap } from '@vue-flow/minimap'
import '@vue-flow/minimap/dist/style.css'
import { listTasks, uploadImage } from '../api'
import type { MediaAsset, Task, UploadedImage } from '../types'
import type { CanvasLLMPayload, CanvasRunPayload, ImageForm } from '../uiTypes'
import { normalizeVideoSettings, videoModelCapability, videoRatioOptions, videoResolutionOptions } from '../lib/videoModels'
import { nanoBananaRatios, nanoBananaSizeBaseOptions, nanoBananaSizeValue, parseNanoBananaSize, parseSeedreamSize, ratioOptions, seedreamRatios, seedreamSizeBaseOptions, seedreamSizeValue, sizeBaseOptions, sizeFromRatio } from '../lib/sizes'
import { displayImageURL, isVideoTask } from '../lib/view'
import AppIcon from './AppIcon.vue'
import CanvasVideoPlayer from './CanvasVideoPlayer.vue'
import RatioPicker from './RatioPicker.vue'

type MediaNodeKind = 'image_media' | 'video_media' | 'audio_media'
type GenerateNodeKind = 'llm' | 'image' | 'video' | 'audio'
type NodeKind = MediaNodeKind | 'asset' | 'ai' | 'prompt' | 'merge' | GenerateNodeKind | 'mask'

type CanvasElement = {
  id: string
  kind: NodeKind
  badge?: string
  task_id?: string
  media_type?: 'image' | 'video' | 'audio'
  media_url?: string
  media_thumbnail_url?: string
  media_filename?: string
  text?: string
  task_type?: ImageForm['task_type']
  model?: string
  size?: string
  quality?: string
  output_format?: string
  output_compression?: number
  background?: string
  moderation?: string
  input_fidelity?: string
  video_ratio?: string
  video_resolution?: ImageForm['video_resolution']
  video_duration?: number
  video_clip_start?: number
  video_clip_end?: number
  reasoning_effort?: string
  generate_audio?: boolean
  watermark?: boolean
  mask_data_url?: string
  mask_tool?: 'pan' | 'brush' | 'eraser'
  mask_brush_size?: number
  frozen?: boolean
  zIndex?: number
  image_view_scale?: number
  image_view_x?: number
  image_view_y?: number
  x: number
  y: number
  width: number
  height: number
}

type CanvasConnection = { id: string; from: string; to: string }
type BoardCanvas = { id: string; name: string; elements: CanvasElement[]; connections: CanvasConnection[] }
type CanvasContextMenuItem = { label: string; icon?: string; action: () => void; disabled?: boolean; danger?: boolean }
type NodeValueType = 'text' | 'image' | 'video' | 'audio' | 'merge'
type DragState =
  | { type: 'pan'; startX: number; startY: number; originX: number; originY: number }
  | { type: 'node'; id: string; startX: number; startY: number; originX: number; originY: number }
  | { type: 'resize'; id: string; startX: number; startY: number; originWidth: number; originHeight: number }
  | null

const props = defineProps<{
  apikey: string
  baseurl: string
  tasks: Task[]
  defaultForm: ImageForm
  models: string[]
  submitting: boolean
  runNodeAction?: (payload: CanvasRunPayload, applyTask: (task: Task) => void) => Promise<unknown> | void
  runLlmAction?: (payload: CanvasLLMPayload, applyResult: (text: string) => void) => Promise<unknown> | void
}>()

const emit = defineEmits<{
  selectTask: [task: Task]
  runNode: [payload: CanvasRunPayload, applyTask: (task: Task) => void]
  runLlm: [payload: CanvasLLMPayload, applyResult: (text: string) => void]
  zenModeChange: [enabled: boolean]
}>()

const STORAGE_KEY = 'image_web_canvases'
const MIN_ZOOM = 0.2
const MAX_ZOOM = 3
const canvases = ref<BoardCanvas[]>(loadCanvases())
const activeCanvasID = ref(canvases.value[0]?.id || '')
const canvasHistory = ref<string[]>([serializeCanvases(canvases.value)])
const pan = ref({ x: 420, y: 220 })
const zoom = ref(0.82)
const dragState = ref<DragState>(null)
const showAssets = ref(true)
const assetsVisibleBeforeZen = ref(true)
const assetsClosingForZen = ref(false)
const zenMode = ref(false)
const uploadingMediaID = ref('')
const runningWorkflow = ref(false)
const runningNodeID = ref('')
const cameraMoving = ref(false)
const runningLineIDs = ref<Set<string>>(new Set())
const nodeRunState = ref<Record<string, { status: 'running' | 'succeeded' | 'failed'; startedAt: number; endedAt?: number; message?: string }>>({})
const runtimeNow = ref(Date.now())
const assetSearch = ref('')
const assetTasks = ref<Task[]>([])
const assetLoaded = ref(false)
const assetLoading = ref(false)
const assetHasMore = ref(false)
const assetTotal = ref(0)
const assetNextBeforeCreatedAt = ref('')
const assetNextBeforeID = ref('')
const assetError = ref('')
const canvasContextMenu = ref<{ x: number; y: number; items: CanvasContextMenuItem[] } | null>(null)
const canvasNotice = ref('')
const pendingFlowConnection = ref<{ nodeId: string; handleType: 'source' | 'target' } | null>(null)
const suppressFlowConnectEnd = ref(false)
const mediaUrlEditor = ref<{ elementID: string; value: string } | null>(null)
const mentionMenu = ref<{ elementID: string; query: string; activeIndex: number } | null>(null)
const maskPaintState = ref<{ elementID: string; point: { x: number; y: number } } | null>(null)
const activeMaskPointer = ref<{ elementID: string; pointerId: number; canvas: HTMLCanvasElement; element: CanvasElement } | null>(null)
const spacePanning = ref(false)
const selectedNodeIDs = ref<Set<string>>(new Set())
const showMiniMap = ref(true)
const miniMapVisibleBeforeZen = ref(true)
const imageViewByNodeID = ref<Record<string, { scale: number; x: number; y: number }>>({})
const loadedCanvasImages = ref<Record<string, 'loaded' | 'error'>>({})
const imagePanState = ref<{ elementID: string; startX: number; startY: number; originX: number; originY: number } | null>(null)
const activeMaskElementID = ref('')
const hoveredMaskElementID = ref('')
const maskCursor = ref<{ elementID: string; x: number; y: number; size: number; visible: boolean }>({ elementID: '', x: 0, y: 0, size: 0, visible: false })
const maskResizeObservers = new WeakMap<HTMLElement, ResizeObserver>()
const activeMaskResizeObservers = new Set<ResizeObserver>()
const gptImageSizeBaseOptions = computed(() => sizeBaseOptions.filter((option) => option.value !== '512'))
let assetRefreshTimer = 0
let runtimeTimer = 0
let historyTimer = 0
let restoringHistory = false
const handledCtrlWheelEvents = new WeakSet<WheelEvent>()
const camera = { x: pan.value.x, y: pan.value.y, zoom: zoom.value }
const flow = useVueFlow('canvas-flow')

const activeCanvas = computed(() => canvases.value.find((canvas) => canvas.id === activeCanvasID.value) || canvases.value[0])
const usableTasks = computed(() => props.tasks.filter(hasMediaAsset))
const visibleAssetTasks = computed(() => assetLoaded.value ? assetTasks.value : usableTasks.value)
const zoomLabel = computed(() => `${Math.round(zoom.value * 100)}%`)
const canUndo = computed(() => canvasHistory.value.length > 1 || serializeCanvasStructure(canvases.value) !== latestHistoryStructure())
const canvasStyle = computed(() => ({
  '--canvas-x': `${pan.value.x}px`,
  '--canvas-y': `${pan.value.y}px`,
  '--canvas-zoom': String(zoom.value),
}))
const flowNodes = computed<Node[]>(() => {
  const elementNodes = (activeCanvas.value?.elements || []).map((element) => {
    const size = renderedNodeSize(element)
    const position = { x: element.x, y: element.y }
    const zIndex = effectiveElementZIndex(element)
    const node = {
      id: element.id,
      type: 'canvas',
      position,
      data: { element },
      draggable: true,
      dragHandle: '.canvas-node-drag',
      connectable: true,
      selectable: true,
      zIndex,
      style: { width: `${size.width}px`, height: `${size.height}px` },
    } as Node
    return node
  })
  return elementNodes
})
const flowEdges = computed<Edge[]>(() => (activeCanvas.value?.connections || []).map((connection) => ({
  id: connection.id,
  source: connection.from,
  target: connection.to,
  sourceHandle: 'output',
  targetHandle: 'input',
  type: 'default',
  animated: isConnectionRunning(connection),
  class: {
    'canvas-flow-edge': true,
    'is-running': isConnectionRunning(connection),
    'is-muted': isConnectionMuted(connection),
  },
  markerEnd: { type: MarkerType.ArrowClosed, color: edgeColor(connection) },
  style: { stroke: edgeColor(connection) },
  zIndex: connectionZIndex(connection),
})))
const flowDefaultEdgeOptions = {
  type: 'default',
  markerEnd: { type: MarkerType.ArrowClosed, color: 'rgba(190, 190, 190, .64)' },
  style: { stroke: 'rgba(190, 190, 190, .58)', strokeWidth: 2.5 },
}
const flowConnectionLineOptions = {
  type: ConnectionLineType.Bezier,
  style: { stroke: 'rgba(230, 230, 230, .78)', strokeWidth: 2.5 },
}
watch(canvases, () => {
  saveCanvases()
  if (!restoringHistory) queueCanvasHistorySnapshot()
}, { deep: true })
watch([assetSearch, () => props.apikey, () => props.baseurl], () => queueAssetRefresh(), { immediate: true })
watch(usableTasks, (tasks) => syncUsableTasksToAssets(tasks), { deep: true })
watch(showAssets, (visible) => {
  if (visible && !assetTasks.value.length) queueAssetRefresh()
})

onMounted(() => {
  runtimeTimer = window.setInterval(() => {
    runtimeNow.value = Date.now()
  }, 1000)
  window.addEventListener('keydown', onCanvasKeyDown)
  window.addEventListener('keyup', onCanvasKeyUp)
  window.addEventListener('wheel', preventBrowserZoomWheel, { capture: true, passive: false })
})

onUnmounted(() => {
  if (runtimeTimer) window.clearInterval(runtimeTimer)
  if (historyTimer) window.clearTimeout(historyTimer)
  if (zenMode.value) emit('zenModeChange', false)
  activeMaskResizeObservers.forEach((observer) => observer.disconnect())
  activeMaskResizeObservers.clear()
  window.removeEventListener('keydown', onCanvasKeyDown)
  window.removeEventListener('keyup', onCanvasKeyUp)
  window.removeEventListener('wheel', preventBrowserZoomWheel, { capture: true })
})

function isTextInputTarget(target: EventTarget | null) {
  return target instanceof HTMLElement && Boolean(target.closest('input, textarea, select, [contenteditable="true"]'))
}

function isMaskSizeInputTarget(target: EventTarget | null) {
  return target instanceof HTMLElement && Boolean(target.closest('.canvas-mask-size input'))
}

function onCanvasKeyDown(event: KeyboardEvent) {
  if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'v' && !isTextInputTarget(event.target)) {
    handleClipboardImagePaste(event)
    return
  }
  if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'z' && !event.shiftKey && !isTextInputTarget(event.target)) {
    event.preventDefault()
    event.stopPropagation()
    undoCanvasChange()
    return
  }
  if (!isTextInputTarget(event.target) || isMaskSizeInputTarget(event.target)) {
    const key = event.key.toLowerCase()
    if (key === 'q' || key === 'w' || key === 'e') {
      const tool = key === 'q' ? 'pan' : key === 'w' ? 'brush' : 'eraser'
      if (setActiveMaskTool(tool)) {
        event.preventDefault()
        event.stopPropagation()
        return
      }
    }
  }
  if (event.key === 'Delete' && !isTextInputTarget(event.target)) {
    const selectedIDs = Array.from(selectedNodeIDs.value).filter((id) => Boolean(elementByID(id)))
    if (selectedIDs.length) {
      event.preventDefault()
      event.stopPropagation()
      canvasContextMenu.value = null
      removeElements(selectedIDs)
    }
    return
  }
  if (event.key === 'Escape' && zenMode.value && !isTextInputTarget(event.target)) {
    event.preventDefault()
    setZenMode(false)
    return
  }
  if (event.code !== 'Space' || isTextInputTarget(event.target)) return
  event.preventDefault()
  spacePanning.value = true
}

async function handleClipboardImagePaste(event: KeyboardEvent) {
  if (!navigator.clipboard?.read || uploadingMediaID.value) return
  try {
    const items = await navigator.clipboard.read()
    const files: File[] = []
    for (const item of items) {
      const imageType = item.types.find((type) => type.startsWith('image/'))
      if (!imageType) continue
      const blob = await item.getType(imageType)
      files.push(new File([blob], clipboardImageFilename(imageType, files.length), { type: imageType }))
    }
    if (!files.length) return
    event.preventDefault()
    event.stopPropagation()
    const center = screenToWorld(window.innerWidth / 2, window.innerHeight / 2)
    for (const [index, file] of files.entries()) {
      const point = {
        x: center.x + index * 34,
        y: center.y + index * 34,
      }
      const element = addLocalClipboardImage(file, point)
      uploadClipboardImage(file, element).catch((error) => {
        showCanvasNotice(error instanceof Error ? `剪贴板图片上传失败：${error.message}` : '剪贴板图片上传失败')
      })
    }
  } catch (error) {
    showCanvasNotice(error instanceof Error ? `读取剪贴板图片失败：${error.message}` : '读取剪贴板图片失败')
  }
}

function addLocalClipboardImage(file: File, point: { x: number; y: number }) {
  const previewURL = URL.createObjectURL(file)
  const uploaded: UploadedImage = {
    url: previewURL,
    thumbnail_url: previewURL,
    filename: file.name,
  }
  return addUploadedMedia(uploaded, 'image', file.name, point)
}

async function uploadClipboardImage(file: File, element: CanvasElement) {
  uploadingMediaID.value = element.id
  try {
    const uploaded = await uploadImage(file)
    if (!elementByID(element.id)) return
    const oldURL = element.media_url || ''
    element.media_url = uploaded.url
    element.media_thumbnail_url = uploaded.thumbnail_url || uploaded.url
    element.media_filename = uploaded.filename || file.name
    if (oldURL.startsWith('blob:')) URL.revokeObjectURL(oldURL)
  } finally {
    if (uploadingMediaID.value === element.id) uploadingMediaID.value = ''
  }
}

function clipboardImageFilename(type: string, index: number) {
  const ext = type.includes('jpeg') ? 'jpg' : type.includes('webp') ? 'webp' : type.includes('gif') ? 'gif' : 'png'
  return `clipboard-${Date.now()}-${index + 1}.${ext}`
}

function onCanvasKeyUp(event: KeyboardEvent) {
  if (event.code !== 'Space') return
  spacePanning.value = false
}

function setZenMode(enabled: boolean) {
  if (enabled && !zenMode.value) {
    assetsVisibleBeforeZen.value = showAssets.value
    miniMapVisibleBeforeZen.value = showMiniMap.value
    showMiniMap.value = false
    if (showAssets.value) {
      assetsClosingForZen.value = true
      showAssets.value = false
      window.setTimeout(() => {
        assetsClosingForZen.value = false
      }, 280)
    }
  }
  zenMode.value = enabled
  if (enabled) {
    canvasContextMenu.value = null
    mentionMenu.value = null
  } else if (assetsVisibleBeforeZen.value && !showAssets.value) {
    showAssets.value = true
    if (miniMapVisibleBeforeZen.value && !showMiniMap.value) showMiniMap.value = true
  }
  emit('zenModeChange', enabled)
}

function blurControl(event: Event) {
  if (event.currentTarget instanceof HTMLElement) event.currentTarget.blur()
}

function loadCanvases(): BoardCanvas[] {
  try {
    const parsed = JSON.parse(localStorage.getItem(STORAGE_KEY) || '')
    if (Array.isArray(parsed) && parsed.length) {
      return parsed.map((canvas, index) => ({
        id: canvas.id || createID(),
        name: canvas.name || `画布 ${index + 1}`,
        elements: ensureElementBadges(Array.isArray(canvas.elements) ? canvas.elements.map((element: Partial<CanvasElement>, elementIndex: number) => normalizeElement(element, elementIndex)) : []),
        connections: Array.isArray(canvas.connections) ? canvas.connections : [],
      }))
    }
  } catch {
    // Use the default below.
  }
  return [{ id: createID(), name: '画布 1', elements: [], connections: [] }]
}

function normalizeCanvases(raw: unknown): BoardCanvas[] {
  if (!Array.isArray(raw) || !raw.length) return [{ id: createID(), name: '画布 1', elements: [], connections: [] }]
  return raw.map((canvas: Partial<BoardCanvas>, index) => ({
    id: canvas.id || createID(),
    name: canvas.name || `画布 ${index + 1}`,
    elements: ensureElementBadges(Array.isArray(canvas.elements) ? canvas.elements.map((element: Partial<CanvasElement>, elementIndex: number) => normalizeElement(element, elementIndex)) : []),
    connections: Array.isArray(canvas.connections) ? canvas.connections : [],
  }))
}


function normalizeElement(raw: Partial<CanvasElement>, index = 0): CanvasElement {
  const rawKind = raw.kind || 'media'
  const kind = normalizeNodeKind(rawKind, raw.media_type, raw.task_type)
  const minSize = minNodeSize(kind)
  const fallbackWidth = isProcessKind(kind) ? minSize.width : 280
  const fallbackHeight = isProcessKind(kind) ? Math.max(minSize.height, 300) : 220
  return {
    id: raw.id || createID(),
    kind,
    badge: typeof raw.badge === 'string' ? raw.badge : '',
    task_id: raw.task_id,
    media_type: raw.media_type,
    media_url: raw.media_url || '',
    media_thumbnail_url: raw.media_thumbnail_url || '',
    media_filename: raw.media_filename || '',
    text: raw.text || '',
    task_type: raw.task_type || (kind === 'video' ? 'video_generation' : 'image_generation'),
    model: raw.model || '',
    size: raw.size || '',
    quality: raw.quality || '',
    output_format: raw.output_format || '',
    output_compression: raw.output_compression,
    background: raw.background || '',
    moderation: raw.moderation || '',
    input_fidelity: raw.input_fidelity || '',
    video_ratio: raw.video_ratio || '',
    video_resolution: raw.video_resolution,
    video_duration: raw.video_duration,
    video_clip_start: Number(raw.video_clip_start) || 0,
    video_clip_end: Number(raw.video_clip_end) || 0,
    reasoning_effort: raw.reasoning_effort || 'low',
    generate_audio: raw.generate_audio,
    watermark: raw.watermark,
    mask_data_url: raw.mask_data_url || '',
    mask_tool: raw.mask_tool || 'brush',
    mask_brush_size: raw.mask_brush_size || 32,
    frozen: Boolean(raw.frozen),
    zIndex: Number.isFinite(raw.zIndex) ? Number(raw.zIndex) : index,
    image_view_scale: Number.isFinite(raw.image_view_scale) ? Number(raw.image_view_scale) : 1,
    image_view_x: Number.isFinite(raw.image_view_x) ? Number(raw.image_view_x) : 0,
    image_view_y: Number.isFinite(raw.image_view_y) ? Number(raw.image_view_y) : 0,
    x: Number(raw.x) || 0,
    y: Number(raw.y) || 0,
    width: Math.max(minSize.width, Number(raw.width) || fallbackWidth),
    height: Math.max(minSize.height, Number(raw.height) || fallbackHeight),
  }
}

function normalizeNodeKind(rawKind: string, mediaType?: CanvasElement['media_type'], taskType?: ImageForm['task_type']): NodeKind {
  if (rawKind === 'media') return mediaKindFromType(mediaType || 'image')
  if (rawKind === 'image_media' || rawKind === 'video_media' || rawKind === 'audio_media' || rawKind === 'asset' || rawKind === 'ai' || rawKind === 'image' || rawKind === 'video' || rawKind === 'audio' || rawKind === 'llm' || rawKind === 'mask' || rawKind === 'prompt' || rawKind === 'merge') return rawKind
  return taskType === 'video_generation' ? 'video' : 'image'
}

function mediaKindFromType(type: CanvasElement['media_type'] = 'image'): MediaNodeKind {
  if (type === 'video') return 'video_media'
  if (type === 'audio') return 'audio_media'
  return 'image_media'
}

function mediaTypeFromKind(kind: NodeKind): CanvasElement['media_type'] | undefined {
  if (kind === 'video_media') return 'video'
  if (kind === 'audio_media') return 'audio'
  if (kind === 'image_media') return 'image'
  return undefined
}

function isMediaKind(kind: NodeKind) {
  return kind === 'image_media' || kind === 'video_media' || kind === 'audio_media'
}

function saveCanvases() {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(canvases.value))
}

function serializeCanvases(value: BoardCanvas[]) {
  return JSON.stringify(value)
}

function structuralElement(element: CanvasElement) {
  return {
    id: element.id,
    kind: element.kind,
    badge: element.badge || '',
    x: element.x,
    y: element.y,
    width: element.width,
    height: element.height,
    zIndex: element.zIndex || 0,
    frozen: Boolean(element.frozen),
  }
}

function structuralConnection(connection: CanvasConnection) {
  return {
    id: connection.id,
    from: connection.from,
    to: connection.to,
  }
}

function serializeCanvasStructure(value: BoardCanvas[]) {
  return JSON.stringify(value.map((canvas) => ({
    id: canvas.id,
    name: canvas.name,
    elements: canvas.elements.map(structuralElement),
    connections: canvas.connections.map(structuralConnection),
  })))
}

function latestHistoryStructure() {
  const latest = canvasHistory.value[canvasHistory.value.length - 1]
  if (!latest) return ''
  try {
    return serializeCanvasStructure(normalizeCanvases(JSON.parse(latest)))
  } catch {
    return ''
  }
}

function queueCanvasHistorySnapshot() {
  window.clearTimeout(historyTimer)
  historyTimer = window.setTimeout(() => pushCanvasHistorySnapshot(), 240)
}

function pushCanvasHistorySnapshot() {
  const snapshot = serializeCanvases(canvases.value)
  const structure = serializeCanvasStructure(canvases.value)
  const history = canvasHistory.value
  if (structure === latestHistoryStructure()) return
  canvasHistory.value = [...history.slice(-79), snapshot]
}

function mergeElementStructure(target: CanvasElement, current?: CanvasElement): CanvasElement {
  if (!current) return target
  return {
    ...target,
    badge: current.badge,
    task_id: current.task_id,
    media_type: current.media_type,
    media_url: current.media_url,
    media_thumbnail_url: current.media_thumbnail_url,
    media_filename: current.media_filename,
    text: current.text,
    task_type: current.task_type,
    model: current.model,
    size: current.size,
    quality: current.quality,
    output_format: current.output_format,
    output_compression: current.output_compression,
    background: current.background,
    moderation: current.moderation,
    input_fidelity: current.input_fidelity,
    video_ratio: current.video_ratio,
    video_resolution: current.video_resolution,
    video_duration: current.video_duration,
    video_clip_start: current.video_clip_start,
    video_clip_end: current.video_clip_end,
    reasoning_effort: current.reasoning_effort,
    generate_audio: current.generate_audio,
    watermark: current.watermark,
    mask_data_url: current.mask_data_url,
    mask_tool: current.mask_tool,
    mask_brush_size: current.mask_brush_size,
    image_view_scale: current.image_view_scale,
    image_view_x: current.image_view_x,
    image_view_y: current.image_view_y,
  }
}

function mergeCanvasStructure(target: BoardCanvas[], current: BoardCanvas[]) {
  const currentElements = new Map(current.flatMap((canvas) => canvas.elements.map((element) => [element.id, element] as const)))
  return target.map((canvas) => ({
    ...canvas,
    elements: canvas.elements.map((element) => mergeElementStructure(element, currentElements.get(element.id))),
  }))
}

function restoreCanvasSnapshot(snapshot: string) {
  try {
    restoringHistory = true
    const next = mergeCanvasStructure(normalizeCanvases(JSON.parse(snapshot)), canvases.value)
    canvases.value = next
    if (!next.some((canvas) => canvas.id === activeCanvasID.value)) activeCanvasID.value = next[0]?.id || ''
    selectedNodeIDs.value = new Set()
    canvasContextMenu.value = null
    mentionMenu.value = null
    nextTick(() => {
      restoringHistory = false
      saveCanvases()
    })
  } catch {
    restoringHistory = false
  }
}

function undoCanvasChange() {
  window.clearTimeout(historyTimer)
  const current = serializeCanvasStructure(canvases.value)
  const history = canvasHistory.value
  const latest = latestHistoryStructure()
  if (current !== latest && history[history.length - 1]) {
    restoreCanvasSnapshot(history[history.length - 1])
    return
  }
  if (history.length <= 1) return
  const nextHistory = history.slice(0, -1)
  canvasHistory.value = nextHistory
  restoreCanvasSnapshot(nextHistory[nextHistory.length - 1])
}

function createID() {
  return crypto?.randomUUID?.() || `${Date.now()}-${Math.random().toString(16).slice(2)}`
}

function createCanvas() {
  const next = { id: createID(), name: `画布 ${canvases.value.length + 1}`, elements: [], connections: [] }
  canvases.value.push(next)
  activeCanvasID.value = next.id
  resetView()
}

function renameCanvas() {
  if (!activeCanvas.value) return
  const name = window.prompt('画布名称', activeCanvas.value.name)
  if (name?.trim()) activeCanvas.value.name = name.trim()
}

function deleteCanvas() {
  if (!activeCanvas.value || canvases.value.length <= 1) return
  if (!window.confirm('确定删除当前画布？')) return
  const index = canvases.value.findIndex((canvas) => canvas.id === activeCanvas.value?.id)
  canvases.value.splice(index, 1)
  activeCanvasID.value = canvases.value[Math.max(0, index - 1)]?.id || ''
}

function queueAssetRefresh() {
  window.clearTimeout(assetRefreshTimer)
  assetRefreshTimer = window.setTimeout(() => {
    refreshAssets().catch(() => undefined)
  }, 220)
}

async function refreshAssets() {
  if (!props.apikey || !props.baseurl) {
    assetTasks.value = usableTasks.value
    assetLoaded.value = false
    assetHasMore.value = false
    assetTotal.value = usableTasks.value.length
    return
  }
  assetLoading.value = true
  assetError.value = ''
  try {
    const result = await listTasks(props.apikey, props.baseurl, 'succeeded', assetSearch.value.trim(), false, '', '', 30)
    assetTasks.value = result.data.filter(hasMediaAsset)
    assetLoaded.value = true
    assetHasMore.value = result.has_more
    assetTotal.value = result.total
    assetNextBeforeCreatedAt.value = result.next_before_created_at
    assetNextBeforeID.value = result.next_before_id
  } catch (error) {
    assetError.value = error instanceof Error ? error.message : '素材加载失败'
    assetTasks.value = usableTasks.value
    assetLoaded.value = false
    assetHasMore.value = false
    assetTotal.value = usableTasks.value.length
  } finally {
    assetLoading.value = false
  }
}

async function loadMoreAssets() {
  if (!props.apikey || !props.baseurl || assetLoading.value || !assetHasMore.value) return
  assetLoading.value = true
  assetError.value = ''
  try {
    const result = await listTasks(props.apikey, props.baseurl, 'succeeded', assetSearch.value.trim(), false, assetNextBeforeCreatedAt.value, assetNextBeforeID.value, 30)
    const existing = new Set(assetTasks.value.map((task) => task.id))
    assetTasks.value.push(...result.data.filter((task) => hasMediaAsset(task) && !existing.has(task.id)))
    assetHasMore.value = result.has_more
    assetTotal.value = result.total
    assetNextBeforeCreatedAt.value = result.next_before_created_at
    assetNextBeforeID.value = result.next_before_id
  } catch (error) {
    assetError.value = error instanceof Error ? error.message : '媒体加载失败'
  } finally {
    assetLoading.value = false
  }
}

function assetSearchMatches(task: Task) {
  const query = assetSearch.value.trim().toLowerCase()
  if (!query) return true
  return [
    task.id,
    task.prompt,
    task.final_prompt,
    task.model,
    task.task_type,
  ].filter(Boolean).some((value) => String(value).toLowerCase().includes(query))
}

function syncUsableTasksToAssets(tasks: Task[]) {
  if (!assetLoaded.value) {
    assetTotal.value = tasks.length
    return
  }
  const incoming = tasks.filter((task) => hasMediaAsset(task) && assetSearchMatches(task))
  if (!incoming.length) return
  const incomingByID = new Map(incoming.map((task) => [task.id, task]))
  const existingIDs = new Set(assetTasks.value.map((task) => task.id))
  const added = incoming.filter((task) => !existingIDs.has(task.id))
  assetTasks.value = [
    ...added,
    ...assetTasks.value.map((task) => incomingByID.get(task.id) || task),
  ]
  assetTotal.value = Math.max(assetTotal.value, assetTasks.value.length)
}

function addTask(task: Task) {
  if (!activeCanvas.value) return
  const center = screenToWorld(window.innerWidth / 2, window.innerHeight / 2)
  const offset = activeCanvas.value.elements.length * 36
  const audio = firstAudioAsset(task)
  const video = task.result_videos?.[0]
  const image = task.result_images?.[0]
  const mediaType = video?.url ? 'video' : audio?.url ? 'audio' : 'image'
  const mediaURL = video?.url || audio?.url || image?.url || ''
  const thumbnailURL = video?.thumbnail_url || audio?.thumbnail_url || image?.thumbnail_url || ''
  const filename = video?.filename || audio?.filename || image?.filename || assetLabel(task)
  const width = mediaType === 'video' ? 360 : mediaType === 'audio' ? 280 : 260
  const element: CanvasElement = {
    id: createID(),
    kind: mediaKindFromType(mediaType),
    task_id: task.id,
    media_type: mediaType,
    media_url: mediaURL,
    media_thumbnail_url: thumbnailURL,
    media_filename: filename,
    video_clip_start: 0,
    video_clip_end: video?.duration || audio?.duration || 0,
    text: '',
    x: center.x - width / 2 + (offset % 360),
    y: center.y - 130 + (offset % 260),
    width,
    height: mediaType === 'video' ? 230 : mediaType === 'audio' ? 170 : 286,
    zIndex: maxCanvasZIndex() + 1,
  }
  pushCanvasElement(element)
}

async function uploadMediaFiles(event: Event) {
  const input = event.target as HTMLInputElement
  const files = Array.from(input.files || [])
  if (!files.length || !activeCanvas.value) return
  uploadingMediaID.value = '__sidebar'
  try {
    const center = screenToWorld(window.innerWidth / 2, window.innerHeight / 2)
    for (const [index, file] of files.entries()) {
      const uploaded = await uploadImage(file)
      const type = mediaTypeFromFile(file)
      addUploadedMedia(uploaded, type, file.name, {
        x: center.x + index * 34,
        y: center.y + index * 34,
      })
    }
  } finally {
    uploadingMediaID.value = ''
    input.value = ''
  }
}

function addMediaNode(kind: MediaNodeKind = 'image_media', position?: { x: number; y: number }) {
  if (!activeCanvas.value) return
  const center = position || screenToWorld(window.innerWidth / 2, window.innerHeight / 2)
  const type = mediaTypeFromKind(kind) || 'image'
  const width = type === 'video' ? 360 : type === 'audio' ? 280 : 280
  const height = type === 'video' ? 230 : type === 'audio' ? 170 : 220
  pushCanvasElement({
    id: createID(),
    kind,
    media_type: type,
    media_url: '',
    media_thumbnail_url: '',
    media_filename: '',
    video_clip_start: 0,
    video_clip_end: 0,
    text: '',
    x: center.x - width / 2,
    y: center.y - height / 2,
    width,
    height,
    zIndex: maxCanvasZIndex() + 1,
  }, center)
}

function addAssetNode(position?: { x: number; y: number }) {
  const center = position || screenToWorld(window.innerWidth / 2, window.innerHeight / 2)
  const minSize = minNodeSize('asset')
  pushCanvasElement({
    id: createID(),
    kind: 'asset',
    text: '',
    x: center.x - minSize.width / 2,
    y: center.y - minSize.height / 2,
    width: minSize.width,
    height: minSize.height,
    zIndex: maxCanvasZIndex() + 1,
  }, center)
}

function chooseAssetNodeKind(element: CanvasElement, kind: MediaNodeKind) {
  const center = { x: element.x + renderedNodeSize(element).width / 2, y: element.y + renderedNodeSize(element).height / 2 }
  const type = mediaTypeFromKind(kind) || 'image'
  const minSize = minNodeSize(kind)
  Object.assign(element, {
    kind,
    badge: '',
    media_type: type,
    media_url: '',
    media_thumbnail_url: '',
    media_filename: '',
    video_clip_start: 0,
    video_clip_end: 0,
    width: minSize.width,
    height: minSize.height,
    x: center.x - minSize.width / 2,
    y: center.y - minSize.height / 2,
  })
  assignElementBadge(element)
}

async function uploadMediaIntoNode(event: Event, element: CanvasElement) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  uploadingMediaID.value = element.id
  try {
    const uploaded = await uploadImage(file)
    const type = mediaTypeFromFile(file)
    element.kind = mediaKindFromType(type)
    element.media_type = type
    element.media_url = uploaded.url
    element.media_thumbnail_url = uploaded.thumbnail_url || ''
    element.media_filename = uploaded.filename || file.name
    element.video_clip_start = 0
    element.video_clip_end = 0
    element.width = type === 'video' ? Math.max(element.width, 360) : type === 'audio' ? Math.max(element.width, 280) : element.width
    element.height = type === 'video' ? Math.max(element.height, 230) : type === 'audio' ? Math.max(element.height, 170) : element.height
  } finally {
    uploadingMediaID.value = ''
    input.value = ''
  }
}

function importMediaURLIntoNode(element: CanvasElement) {
  mediaUrlEditor.value = { elementID: element.id, value: element.media_url || '' }
  nextTick(() => {
    const input = document.querySelector<HTMLInputElement>(`[data-media-url-input="${element.id}"]`)
    input?.focus()
    input?.select()
  })
}

function cancelMediaURLInput() {
  mediaUrlEditor.value = null
}

function updateMediaURLInput(value: string) {
  if (!mediaUrlEditor.value) return
  mediaUrlEditor.value = { ...mediaUrlEditor.value, value }
}

function commitMediaURLInput(element: CanvasElement) {
  if (mediaUrlEditor.value?.elementID !== element.id) return
  const trimmed = mediaUrlEditor.value.value.trim()
  if (!trimmed) {
    cancelMediaURLInput()
    return
  }
  if (!/^https?:\/\//i.test(trimmed)) {
    showCanvasNotice('请输入 http 或 https 开头的媒体 URL')
    return
  }
  const type = mediaTypeFromURL(trimmed)
  element.kind = mediaKindFromType(type)
  element.media_type = type
  element.media_url = trimmed
  element.media_thumbnail_url = ''
  element.media_filename = filenameFromURL(trimmed) || (type === 'video' ? '视频 URL' : type === 'audio' ? '音频 URL' : '图片 URL')
  element.video_clip_start = 0
  element.video_clip_end = 0
  element.width = type === 'video' ? Math.max(element.width, 360) : type === 'audio' ? Math.max(element.width, 280) : element.width
  element.height = type === 'video' ? Math.max(element.height, 230) : type === 'audio' ? Math.max(element.height, 170) : element.height
  mediaUrlEditor.value = null
}

function addUploadedMedia(uploaded: UploadedImage, type: 'image' | 'video' | 'audio', filename: string, point: { x: number; y: number }) {
  const width = type === 'video' ? 360 : type === 'audio' ? 280 : 260
  const element: CanvasElement = {
    id: createID(),
    kind: mediaKindFromType(type),
    media_type: type,
    media_url: uploaded.url,
    media_thumbnail_url: uploaded.thumbnail_url,
    media_filename: uploaded.filename || filename,
    video_clip_start: 0,
    video_clip_end: 0,
    text: '',
    x: point.x - width / 2,
    y: point.y - 120,
    width,
    height: type === 'video' ? 230 : type === 'audio' ? 170 : 286,
    zIndex: maxCanvasZIndex() + 1,
  }
  pushCanvasElement(element, point)
  return element
}

function mediaTypeFromFile(file: File): 'image' | 'video' | 'audio' {
  if (file.type.startsWith('video/')) return 'video'
  if (file.type.startsWith('audio/')) return 'audio'
  return 'image'
}

function mediaTypeFromURL(url: string): 'image' | 'video' | 'audio' {
  const path = new URL(url).pathname.toLowerCase()
  if (/\.(mp4|webm|mov|m4v|avi|mkv)$/.test(path)) return 'video'
  if (/\.(mp3|wav|m4a|aac|ogg|flac)$/.test(path)) return 'audio'
  return 'image'
}

function mediaAcceptForKind(kind: NodeKind) {
  if (kind === 'video_media') return 'video/*'
  if (kind === 'audio_media') return 'audio/*'
  return 'image/*'
}

function mediaEmptyLabel(element: CanvasElement) {
  if (element.kind === 'video_media') return '视频媒体'
  if (element.kind === 'audio_media') return '音频媒体'
  return '图片媒体'
}

function filenameFromURL(url: string) {
  try {
    const name = new URL(url).pathname.split('/').filter(Boolean).pop() || ''
    return decodeURIComponent(name)
  } catch {
    return ''
  }
}

function addPromptNode(position?: { x: number; y: number }) {
  const center = position || screenToWorld(window.innerWidth / 2, window.innerHeight / 2)
  pushCanvasElement({ id: createID(), kind: 'prompt', text: '', x: center.x, y: center.y - 85, width: 320, height: 170, zIndex: maxCanvasZIndex() + 1 }, center)
}

function addMergeNode(position?: { x: number; y: number }) {
  const center = position || screenToWorld(window.innerWidth / 2, window.innerHeight / 2)
  const minSize = minNodeSize('merge')
  pushCanvasElement({ id: createID(), kind: 'merge', text: '', x: center.x, y: center.y - minSize.height / 2, width: minSize.width, height: minSize.height, zIndex: maxCanvasZIndex() + 1 }, center)
}

function addAiNode(position?: { x: number; y: number }) {
  const center = position || screenToWorld(window.innerWidth / 2, window.innerHeight / 2)
  const minSize = minNodeSize('ai')
  pushCanvasElement({
    id: createID(),
    kind: 'ai',
    text: '',
    x: center.x - minSize.width / 2,
    y: center.y - minSize.height / 2,
    width: minSize.width,
    height: minSize.height,
    zIndex: maxCanvasZIndex() + 1,
  }, center)
}

function chooseAiNodeKind(element: CanvasElement, kind: GenerateNodeKind) {
  const center = { x: element.x + renderedNodeSize(element).width / 2, y: element.y + renderedNodeSize(element).height / 2 }
  const next = createProcessElement(kind, { x: center.x, y: center.y })
  Object.assign(element, {
    ...next,
    id: element.id,
    badge: '',
    x: center.x - next.width / 2,
    zIndex: element.zIndex,
  })
  assignElementBadge(element)
}

function createProcessElement(kind: GenerateNodeKind | 'mask', point: { x: number; y: number }): CanvasElement {
  const isVideo = kind === 'video'
  const isAudio = kind === 'audio'
  const minSize = minNodeSize(kind)
  return {
    id: createID(),
    kind,
    text: '',
    task_type: isVideo ? 'video_generation' : 'image_generation',
    model: isVideo && !props.defaultForm.model.includes('video') && !props.defaultForm.model.includes('seedance') ? 'doubao-seedance-2.0' : props.defaultForm.model,
    size: props.defaultForm.size,
    quality: props.defaultForm.quality,
    output_format: props.defaultForm.output_format,
    output_compression: props.defaultForm.output_compression,
    background: props.defaultForm.background,
    moderation: props.defaultForm.moderation,
    input_fidelity: props.defaultForm.input_fidelity,
    video_ratio: props.defaultForm.video_ratio,
    video_resolution: props.defaultForm.video_resolution,
    video_duration: props.defaultForm.video_duration,
    video_clip_start: 0,
    video_clip_end: 0,
    reasoning_effort: 'low',
    generate_audio: props.defaultForm.generate_audio,
    watermark: props.defaultForm.watermark,
    mask_data_url: '',
    mask_tool: 'brush',
    mask_brush_size: 32,
    frozen: false,
    x: point.x,
    y: point.y - minSize.height / 2,
    width: minSize.width,
    height: isAudio ? minSize.height : Math.max(minSize.height, 300),
  }
}

function canvasNodeModel(element: CanvasElement) {
  if (element.kind !== 'video') return element.model || props.defaultForm.model
  const model = (element.model || '').trim()
  return model && (model.toLowerCase().includes('video') || model.toLowerCase().includes('seedance')) ? model : 'doubao-seedance-2.0'
}

function isNanoBananaElement(element: CanvasElement) {
  return element.kind === 'image' && element.model === 'nano-banana-2'
}

function isSeedreamElement(element: CanvasElement) {
  return element.kind === 'image' && element.model === 'doubao-seedream-5.0-lite'
}

function isNanoBananaSize(size: string) {
  return /^(512|1K|2K|4K) \d+:\d+$/.test(size)
}

function updateCanvasImageModel(element: CanvasElement) {
  if (isNanoBananaElement(element)) {
    const parsed = parseNanoBananaSize(element.size || '')
    element.size = nanoBananaSizeValue(parsed.imageSize, parsed.aspectRatio)
    return
  }
  if (isSeedreamElement(element)) {
    const parsed = parseSeedreamSize(element.size || '')
    element.size = seedreamSizeValue(parsed.imageSize, parsed.aspectRatio)
    if (element.output_format !== 'png') element.output_format = 'jpeg'
    return
  }
  if (isNanoBananaSize(element.size || '') || isSeedreamSize(element.size || '')) element.size = '1024x1024'
}

function isSeedreamSize(size: string) {
  const parsed = parseSeedreamSize(size)
  return seedreamSizeValue(parsed.imageSize, parsed.aspectRatio) === size
}

function nanoImageSize(element: CanvasElement) {
  return parseNanoBananaSize(element.size || '').imageSize
}

function nanoAspectRatio(element: CanvasElement) {
  return parseNanoBananaSize(element.size || '').aspectRatio
}

function updateNanoImageSize(element: CanvasElement, event: Event) {
  element.size = nanoBananaSizeValue((event.target as HTMLSelectElement).value, nanoAspectRatio(element))
}

function updateNanoAspectRatio(element: CanvasElement, event: Event) {
  element.size = nanoBananaSizeValue(nanoImageSize(element), (event.target as HTMLSelectElement).value)
}

function seedreamImageSize(element: CanvasElement) {
  return parseSeedreamSize(element.size || '').imageSize
}

function seedreamAspectRatio(element: CanvasElement) {
  return parseSeedreamSize(element.size || '').aspectRatio
}

function updateSeedreamImageSize(element: CanvasElement, event: Event) {
  element.size = seedreamSizeValue((event.target as HTMLSelectElement).value, seedreamAspectRatio(element))
}

function updateSeedreamAspectRatio(element: CanvasElement, event: Event) {
  element.size = seedreamSizeValue(seedreamImageSize(element), (event.target as HTMLSelectElement).value)
}

function gptImageSizeParts(element: CanvasElement) {
  const size = element.size || props.defaultForm.size || '1024x1024'
  if (size === 'auto') return { base: 'auto', ratio: '1:1' }
  for (const base of gptImageSizeBaseOptions.value) {
    if (base.value === 'auto') continue
    for (const ratio of ratioOptions) {
      if (sizeFromRatio(base.value, ratio) === size) return { base: base.value, ratio }
    }
  }
  return { base: '1K', ratio: '1:1' }
}

function gptImageSizeBase(element: CanvasElement) {
  return gptImageSizeParts(element).base
}

function gptImageRatio(element: CanvasElement) {
  return gptImageSizeParts(element).ratio
}

function updateGptImageSizeBase(element: CanvasElement, event: Event) {
  const base = (event.target as HTMLSelectElement).value
  element.size = base === 'auto' ? 'auto' : sizeFromRatio(base, gptImageRatio(element))
}

function updateGptImageRatio(element: CanvasElement, event: Event) {
  const base = gptImageSizeBase(element)
  if (base === 'auto') return
  element.size = sizeFromRatio(base, (event.target as HTMLSelectElement).value)
}

function supportsTransparentBackground(element: CanvasElement) {
  return (element.output_format || props.defaultForm.output_format) === 'png'
}

function supportsOutputCompression(element: CanvasElement) {
  const format = element.output_format || props.defaultForm.output_format
  return format === 'jpeg' || format === 'webp'
}

function updateCanvasOutputFormat(element: CanvasElement, event: Event) {
  element.output_format = (event.target as HTMLSelectElement).value
  if (element.output_format !== 'png' && element.background === 'transparent') element.background = 'auto'
}

function canvasVideoRatios(element: CanvasElement) {
  return videoRatioOptions(canvasNodeModel(element))
}

function canvasVideoResolutions(element: CanvasElement) {
  return videoResolutionOptions(canvasNodeModel(element))
}

function canvasVideoCapability(element: CanvasElement) {
  return videoModelCapability(canvasNodeModel(element))
}

function normalizeCanvasVideoSettings(element: CanvasElement) {
  const normalized = normalizeVideoSettings({
    model: canvasNodeModel(element),
    ratio: element.video_ratio || props.defaultForm.video_ratio,
    resolution: element.video_resolution || props.defaultForm.video_resolution,
    duration: element.video_duration ?? props.defaultForm.video_duration,
  })
  element.video_ratio = normalized.ratio
  element.video_resolution = normalized.resolution
  element.video_duration = normalized.duration
}

function updateCanvasVideoModel(element: CanvasElement) {
  normalizeCanvasVideoSettings(element)
}

function addGenerateNode(kind: GenerateNodeKind | 'mask' = props.defaultForm.task_type === 'video_generation' ? 'video' : 'image', position?: { x: number; y: number }) {
  if (!activeCanvas.value) return
  const center = position || screenToWorld(window.innerWidth / 2, window.innerHeight / 2)
  const element = createProcessElement(kind, center)
  element.zIndex = maxCanvasZIndex() + 1
  pushCanvasElement(element, center)
}

function removeElement(id: string) {
  if (!activeCanvas.value) return
  activeCanvas.value.elements = activeCanvas.value.elements.filter((element) => element.id !== id)
  activeCanvas.value.connections = activeCanvas.value.connections.filter((connection) => connection.from !== id && connection.to !== id)
}

function openCanvasContextMenu(event: MouseEvent) {
  event.preventDefault()
  const point = screenToWorld(event.clientX, event.clientY)
  canvasContextMenu.value = {
    x: event.clientX,
    y: event.clientY,
    items: [
      { label: '文字提示词', icon: 'text', action: () => addPromptNode(point) },
      { label: '媒体节点', icon: 'gallery', action: () => addAssetNode(point) },
      { label: '汇合节点', icon: 'merge', action: () => addMergeNode(point) },
      { label: '蒙版节点', icon: 'brush', action: () => addGenerateNode('mask', point) },
      { label: 'AI 生成', icon: 'sparkles', action: () => addAiNode(point) },
      { label: '自动整理', icon: 'grid', action: () => autoArrangeCanvas() },
      { label: '复位视图', icon: 'resetView', action: resetView },
    ],
  }
}

function openNodeContextMenu(element: CanvasElement, event: MouseEvent) {
  event.preventDefault()
  const selectedIDs = Array.from(selectedNodeIDs.value).filter((id) => Boolean(elementByID(id)))
  if (selectedIDs.length > 1 && selectedIDs.includes(element.id)) {
    openSelectionContextMenu({ event, nodes: selectedIDs.map((id) => ({ id })) })
    return
  }
  canvasContextMenu.value = {
    x: event.clientX,
    y: event.clientY,
    items: [
      { label: '运行到此节点', icon: 'play', action: () => runToNode(element), disabled: !isRunnableKind(element.kind) || element.frozen || isNodeBusy(element) || isLineBusy(element) },
      { label: element.frozen ? '取消固化' : '固化节点', icon: 'archive', action: () => toggleFrozen(element), disabled: !isRunnableKind(element.kind) || isNodeBusy(element) || isLineBusy(element) },
      { label: '复制节点', icon: 'copy', action: () => duplicateElement(element) },
      { label: '查看任务', icon: 'eye', action: () => taskForElement(element) && emit('selectTask', taskForElement(element)!) , disabled: !taskForElement(element) },
      { label: '删除节点', icon: 'trash', action: () => removeElement(element.id), danger: true },
    ],
  }
}

function openSelectionContextMenu(event: { event: MouseEvent; nodes: Array<{ id: string }> }) {
  const eventIDs = event.nodes.map((node) => node.id).filter((id) => Boolean(elementByID(id)))
  const cachedIDs = Array.from(selectedNodeIDs.value).filter((id) => Boolean(elementByID(id)))
  const selectedIDs = eventIDs.length > 1 ? eventIDs : cachedIDs
  if (selectedIDs.length < 2) return
  event.event.preventDefault()
  event.event.stopPropagation()
  canvasContextMenu.value = {
    x: event.event.clientX,
    y: event.event.clientY,
    items: [
      { label: '删除节点', icon: 'trash', action: () => removeElements(selectedIDs), danger: true },
    ],
  }
}

function pushCanvasElement(element: CanvasElement, _point?: { x: number; y: number }, _source?: CanvasElement) {
  if (!activeCanvas.value) return
  assignElementBadge(element)
  activeCanvas.value.elements.push(element)
}

function effectiveElementZIndex(element: CanvasElement) {
  return Number(element.zIndex) || 0
}

function removeElements(ids: string[]) {
  if (!activeCanvas.value) return
  const selected = new Set(ids)
  activeCanvas.value.elements = activeCanvas.value.elements.filter((element) => !selected.has(element.id))
  activeCanvas.value.connections = activeCanvas.value.connections.filter((connection) => !selected.has(connection.from) && !selected.has(connection.to))
  selectedNodeIDs.value = new Set(Array.from(selectedNodeIDs.value).filter((id) => !selected.has(id)))
}

function sameStringSet(a: string[], b: string[]) {
  if (a.length !== b.length) return false
  const set = new Set(a)
  return b.every((item) => set.has(item))
}

function connectionZIndex(connection: CanvasConnection) {
  const from = elementByID(connection.from)
  const to = elementByID(connection.to)
  const endpointZIndexes = [from, to].filter(Boolean).map((element) => effectiveElementZIndex(element!))
  if (!endpointZIndexes.length) return 0
  return Math.max(...endpointZIndexes) - 1
}

function isConnectionRunning(connection: CanvasConnection) {
  return runningLineIDs.value.has(connection.from) || runningLineIDs.value.has(connection.to)
}

function isConnectionMuted(connection: CanvasConnection) {
  const from = elementByID(connection.from)
  const to = elementByID(connection.to)
  return Boolean(from?.frozen || to?.frozen)
}

function edgeColor(connection: CanvasConnection) {
  if (isConnectionRunning(connection)) return 'rgba(96, 165, 250, .92)'
  if (isConnectionMuted(connection)) return 'rgba(148, 163, 184, .36)'
  const from = elementByID(connection.from)
  const types = from ? outputTypes(from) : []
  if (types.includes('image')) return 'rgba(167, 243, 208, .68)'
  if (types.includes('video')) return 'rgba(125, 211, 252, .72)'
  if (types.includes('audio')) return 'rgba(216, 180, 254, .72)'
  if (types.includes('text')) return 'rgba(253, 230, 138, .72)'
  return 'rgba(190, 190, 190, .58)'
}

function autoArrangeCanvas() {
  if (!activeCanvas.value) return
  autoArrangeElements(activeCanvas.value.elements.map((element) => element.id))
}

function autoArrangeElements(ids: string[]) {
  const elements = ids.map((id) => elementByID(id)).filter(Boolean) as CanvasElement[]
  if (!elements.length) return
  const levels = displayWorkflowLevels()
  const minX = Math.min(...elements.map((element) => element.x))
  const minY = Math.min(...elements.map((element) => element.y))
  const levelValues = Array.from(new Set(elements.map((element) => levels.get(element.id) || 0))).sort((a, b) => a - b)
  let x = minX
  for (const level of levelValues) {
    const items = elements
      .filter((element) => (levels.get(element.id) || 0) === level)
      .sort((a, b) => a.y === b.y ? a.x - b.x : a.y - b.y)
    const columnWidth = Math.max(...items.map((element) => renderedNodeSize(element).width))
    let y = minY
    for (const element of items) {
      element.x = x
      element.y = y
      y += renderedNodeSize(element).height + 64
    }
    x += columnWidth + 120
  }
}

function openEdgeContextMenu(event: EdgeMouseEvent) {
  event.event.preventDefault()
  event.event.stopPropagation()
  const sourceEvent = event.event
  const point = 'touches' in sourceEvent ? sourceEvent.touches[0] || sourceEvent.changedTouches[0] : sourceEvent
  if (!point) return
  canvasContextMenu.value = {
    x: point.clientX,
    y: point.clientY,
    items: [
      { label: '删除连线', icon: 'trash', action: () => removeConnection(event.edge.id), danger: true },
    ],
  }
}

function runCanvasContextAction(item: CanvasContextMenuItem) {
  if (item.disabled) return
  canvasContextMenu.value = null
  item.action()
}

function removeConnection(id: string) {
  if (!activeCanvas.value) return
  activeCanvas.value.connections = activeCanvas.value.connections.filter((connection) => connection.id !== id)
}

function taskForElement(element: CanvasElement) {
  return element.task_id ? props.tasks.find((task) => task.id === element.task_id) || assetTasks.value.find((task) => task.id === element.task_id) : undefined
}

function generatedTask(element: CanvasElement) {
  return element.kind === 'image' || element.kind === 'video' ? taskForElement(element) : undefined
}

function nodeRuntime(element: CanvasElement) {
  return nodeRunState.value[element.id]
}

function isNodeBusy(element: CanvasElement) {
  return runningLineIDs.value.has(element.id) || nodeRuntime(element)?.status === 'running' || generatedTask(element)?.status === 'pending' || generatedTask(element)?.status === 'running'
}

function isLineBusy(element: CanvasElement) {
  if (!isRunnableKind(element.kind)) return false
  for (const id of collectDependencyIDs(element)) {
    if (runningLineIDs.value.has(id)) return true
  }
  return false
}

function addRunningLine(ids: Set<string>) {
  runningLineIDs.value = new Set([...runningLineIDs.value, ...ids])
}

function removeRunningLine(ids: Set<string>) {
  const next = new Set(runningLineIDs.value)
  ids.forEach((id) => next.delete(id))
  runningLineIDs.value = next
}

function isNodeRunning(element: CanvasElement) {
  const task = generatedTask(element)
  return nodeRuntime(element)?.status === 'running' || task?.status === 'pending' || task?.status === 'running'
}

function nodeHeaderRunLabel(element: CanvasElement) {
  const runtime = nodeRuntime(element)
  const task = generatedTask(element)
  if (runtime?.status === 'running') return `运行中 ${formatDuration(runtimeNow.value - runtime.startedAt)}`
  if (task?.status === 'pending') return task.queue_position > 0 ? `排队中 #${task.queue_position}` : '排队中'
  if (task?.status === 'running') return task.upstream_progress > 0 ? `生成中 ${task.upstream_progress}%` : '生成中'
  return '运行'
}

function hasFrozenResult(element: CanvasElement) {
  if (element.kind === 'llm') return Boolean((element.text || '').trim())
  if (element.kind === 'image') return Boolean(taskResultImage(element)?.url)
  if (element.kind === 'video') return Boolean(taskResultVideo(element)?.url)
  return false
}

function nodeProgressLabel(element: CanvasElement) {
  const runtime = nodeRuntime(element)
  const task = generatedTask(element)
  if (runtime?.status === 'running') return `运行中 ${formatDuration(runtimeNow.value - runtime.startedAt)}`
  if (element.frozen) return hasFrozenResult(element) ? '已固化' : '已固化但无结果'
  if (task?.status === 'pending') return task.queue_position > 0 ? `排队中 #${task.queue_position}` : '排队中'
  if (task?.status === 'running') return task.upstream_progress > 0 ? `生成中 ${task.upstream_progress}%` : '生成中'
  if (task?.status === 'succeeded') return `完成 ${formatTaskElapsed(task)}`
  if (task?.status === 'failed') return task.error_message || '失败'
  if (runtime?.status === 'succeeded') return `完成 ${formatDuration((runtime.endedAt || runtimeNow.value) - runtime.startedAt)}`
  if (runtime?.status === 'failed') return runtime.message || '失败'
  return ''
}

function formatTaskElapsed(task: Task) {
  if (task.elapsed_ms > 0) return formatDuration(task.elapsed_ms)
  if (task.started_at && task.completed_at) return formatDuration(new Date(task.completed_at).getTime() - new Date(task.started_at).getTime())
  return ''
}

function formatDuration(ms: number) {
  const seconds = Math.max(0, Math.floor(ms / 1000))
  const minutes = Math.floor(seconds / 60)
  const rest = seconds % 60
  return minutes ? `${minutes}:${String(rest).padStart(2, '0')}` : `${rest}s`
}

function firstAudioAsset(task?: Task) {
  return task?.reference_audios?.find((audio) => audio.url)
}

function localMediaAsset(element: CanvasElement): MediaAsset | undefined {
  const mediaType = element.media_type || mediaTypeFromKind(element.kind)
  if (!element.media_url || mediaType === 'image') return undefined
  return {
    type: mediaType || 'video',
    url: element.media_url,
    thumbnail_url: element.media_thumbnail_url,
    filename: element.media_filename,
    clip_start: cleanClipValue(element.video_clip_start),
    clip_end: cleanClipValue(element.video_clip_end),
  }
}

function localUploadedImage(element: CanvasElement): UploadedImage | undefined {
  const mediaType = element.media_type || mediaTypeFromKind(element.kind)
  if (!element.media_url || mediaType !== 'image') return undefined
  return {
    url: element.media_url,
    filename: element.media_filename,
  }
}

function taskResultImage(element: CanvasElement): UploadedImage | undefined {
  const task = taskForElement(element)
  return task?.result_images?.[0]?.url ? { ...task.result_images[0] } : undefined
}

function taskResultVideo(element: CanvasElement): MediaAsset | undefined {
  const task = taskForElement(element)
  const video = task?.result_videos?.[0]
  if (!video?.url) return undefined
  return withClip({ ...video, type: video.type || 'video' }, element)
}

function originalImageURL(image?: UploadedImage) {
  return image?.url || ''
}

function canvasImageKey(element: CanvasElement, url?: string) {
  return `${element.id}|${url || ''}`
}

function canvasImageStatus(element: CanvasElement, url?: string) {
  return loadedCanvasImages.value[canvasImageKey(element, url)]
}

function markCanvasImageLoaded(element: CanvasElement, url?: string) {
  loadedCanvasImages.value = { ...loadedCanvasImages.value, [canvasImageKey(element, url)]: 'loaded' }
}

function markCanvasImageError(element: CanvasElement, url?: string) {
  loadedCanvasImages.value = { ...loadedCanvasImages.value, [canvasImageKey(element, url)]: 'error' }
}

function withClip(video: MediaAsset, element: CanvasElement): MediaAsset {
  const clipStart = cleanClipValue(element.video_clip_start)
  const clipEnd = cleanClipValue(element.video_clip_end)
  return {
    ...video,
    clip_start: clipStart,
    clip_end: clipEnd && clipEnd > (clipStart || 0) ? clipEnd : undefined,
  }
}

function cleanClipValue(value?: number) {
  const numberValue = Number(value)
  return Number.isFinite(numberValue) && numberValue > 0 ? Math.round(numberValue * 100) / 100 : undefined
}

function imageView(element: CanvasElement) {
  return imageViewByNodeID.value[element.id] || {
    scale: Number(element.image_view_scale) || 1,
    x: Number(element.image_view_x) || 0,
    y: Number(element.image_view_y) || 0,
  }
}

function setImageView(element: CanvasElement, view: { scale: number; x: number; y: number }) {
  const next = {
    scale: Math.round(view.scale * 1000) / 1000,
    x: Math.round(view.x),
    y: Math.round(view.y),
  }
  element.image_view_scale = next.scale
  element.image_view_x = next.x
  element.image_view_y = next.y
  imageViewByNodeID.value = {
    ...imageViewByNodeID.value,
    [element.id]: next,
  }
}

function imageZoomStyle(element: CanvasElement) {
  const view = imageView(element)
  return { transform: `translate(${view.x}px, ${view.y}px) scale(${view.scale})` }
}

function zoomNodeImage(event: WheelEvent, element: CanvasElement) {
  event.preventDefault()
  event.stopPropagation()
  const target = event.currentTarget
  if (!(target instanceof HTMLElement)) return
  const rect = target.getBoundingClientRect()
  const current = imageView(element)
  const nextScale = clamp(current.scale * Math.exp(-event.deltaY * 0.0018), 1, 5)
  const ratio = nextScale / current.scale
  const pointerX = event.clientX - rect.left - rect.width / 2
  const pointerY = event.clientY - rect.top - rect.height / 2
  const nextX = nextScale <= 1 ? 0 : pointerX - (pointerX - current.x) * ratio
  const nextY = nextScale <= 1 ? 0 : pointerY - (pointerY - current.y) * ratio
  setImageView(element, { scale: nextScale, x: nextX, y: nextY })
}

function startNodeImagePan(event: PointerEvent, element: CanvasElement) {
  if (event.button !== 0) return
  event.preventDefault()
  event.stopPropagation()
  const view = imageView(element)
  imagePanState.value = {
    elementID: element.id,
    startX: event.clientX,
    startY: event.clientY,
    originX: view.x,
    originY: view.y,
  }
  ;(event.currentTarget as HTMLElement).setPointerCapture(event.pointerId)
}

function moveNodeImagePan(event: PointerEvent, element: CanvasElement) {
  if (imagePanState.value?.elementID !== element.id) return
  event.preventDefault()
  event.stopPropagation()
  const view = imageView(element)
  if (view.scale <= 1) return
  setImageView(element, {
    scale: view.scale,
    x: imagePanState.value.originX + event.clientX - imagePanState.value.startX,
    y: imagePanState.value.originY + event.clientY - imagePanState.value.startY,
  })
}

function stopNodeImagePan(event?: PointerEvent) {
  event?.stopPropagation()
  imagePanState.value = null
}

function videoClipMax(element: CanvasElement) {
  const taskDuration = taskForElement(element)?.result_videos?.[0]?.duration
  return Math.max(1, Number(element.video_duration || taskDuration || localMediaAsset(element)?.duration || 30))
}

function normalizeVideoClip(element: CanvasElement) {
  const max = videoClipMax(element)
  element.video_clip_start = clamp(Number(element.video_clip_start) || 0, 0, max)
  element.video_clip_end = clamp(Number(element.video_clip_end) || 0, 0, max)
  if (element.video_clip_end && element.video_clip_end <= element.video_clip_start) {
    element.video_clip_end = Math.min(max, element.video_clip_start + 1)
  }
}

function updateVideoClip(element: CanvasElement, field: 'video_clip_start' | 'video_clip_end', event: Event) {
  element[field] = Number((event.target as HTMLInputElement).value) || 0
  normalizeVideoClip(element)
  seekNodeVideo(element, event, Number(element[field]) || 0)
}

function seekNodeVideo(element: CanvasElement, event: Event, seconds: number) {
  const node = (event.currentTarget as HTMLElement | null)?.closest('.canvas-node')
  const video = node?.querySelector('video')
  if (!(video instanceof HTMLVideoElement)) return
  const seek = () => {
    const duration = Number.isFinite(video.duration) && video.duration > 0 ? video.duration : videoClipMax(element)
    video.currentTime = clamp(seconds, 0, duration)
  }
  if (video.readyState > 0) seek()
  else video.addEventListener('loadedmetadata', seek, { once: true })
}

function renderMarkdown(text?: string) {
  const source = text?.trim()
  if (!source) return '<p class="canvas-markdown-placeholder">运行后展示 LLM 输出，可作为后续提示词继续使用</p>'
  const blocks: string[] = []
  let inCode = false
  let codeLines: string[] = []
  let listItems: string[] = []
  const flushList = () => {
    if (!listItems.length) return
    blocks.push(`<ul>${listItems.map((item) => `<li>${inlineMarkdown(item)}</li>`).join('')}</ul>`)
    listItems = []
  }
  const flushCode = () => {
    if (!codeLines.length) return
    blocks.push(`<pre><code>${escapeHTML(codeLines.join('\n'))}</code></pre>`)
    codeLines = []
  }
  for (const line of source.split(/\r?\n/)) {
    if (line.trim().startsWith('```')) {
      if (inCode) {
        flushCode()
        inCode = false
      } else {
        flushList()
        inCode = true
      }
      continue
    }
    if (inCode) {
      codeLines.push(line)
      continue
    }
    const trimmed = line.trim()
    if (!trimmed) {
      flushList()
      continue
    }
    const heading = trimmed.match(/^(#{1,3})\s+(.+)$/)
    if (heading) {
      flushList()
      const level = heading[1].length + 2
      blocks.push(`<h${level}>${inlineMarkdown(heading[2])}</h${level}>`)
      continue
    }
    const list = trimmed.match(/^[-*]\s+(.+)$/)
    if (list) {
      listItems.push(list[1])
      continue
    }
    flushList()
    blocks.push(`<p>${inlineMarkdown(trimmed)}</p>`)
  }
  flushList()
  if (inCode) flushCode()
  return blocks.join('')
}

function inlineMarkdown(text: string) {
  return escapeHTML(text)
    .replace(/`([^`]+)`/g, '<code>$1</code>')
    .replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
    .replace(/(^|\s)@([A-Z]\d{2})\b/g, '$1<span class="canvas-mention-token">@$2</span>')
}

function escapeHTML(text: string) {
  return text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}

function hasMediaAsset(task: Task) {
  return Boolean(task.result_images?.[0]?.url || task.result_videos?.[0]?.url || firstAudioAsset(task)?.url)
}

function assetLabel(task: Task) {
  if (isVideoTask(task)) return '视频素材'
  if (firstAudioAsset(task)) return '音频素材'
  return '图片素材'
}

function assetPromptTitle(task: Task) {
  return task.prompt || task.final_prompt || task.model || '无提示词'
}

function mentionCandidates(element: CanvasElement) {
  const query = mentionMenu.value?.elementID === element.id ? mentionMenu.value.query.toLowerCase() : ''
  const downstream = downstreamElementIDs(element.id)
  const levels = workflowLevels()
  const referenceLevel = mentionReferenceLevel(element, levels)
  return connectedComponentElements(element)
    .filter((item) => item.id !== element.id && isMentionableElement(item) && !downstream.has(item.id) && (levels.get(item.id) || 0) <= referenceLevel)
    .map((item) => ({ element: item, label: mentionLabel(item), detail: mentionDetail(item) }))
    .filter((item) => !query || `${item.label} ${item.detail}`.toLowerCase().includes(query))
}

function mentionReferenceLevel(element: CanvasElement, levels: Map<string, number>) {
  const outgoingTargets = (activeCanvas.value?.connections || [])
    .filter((connection) => connection.from === element.id)
    .map((connection) => connection.to)
    .filter((id) => elementByID(id))
  if (!outgoingTargets.length) return levels.get(element.id) || 0
  return Math.max(...outgoingTargets.map((id) => Math.max(0, (levels.get(id) || 0) - 1)))
}

function workflowLevels() {
  const elements = activeCanvas.value?.elements || []
  const elementIDs = new Set(elements.map((element) => element.id))
  const incomingCount = new Map<string, number>(elements.map((element) => [element.id, 0]))
  const outgoing = new Map<string, string[]>(elements.map((element) => [element.id, []]))
  for (const connection of activeCanvas.value?.connections || []) {
    if (!elementIDs.has(connection.from) || !elementIDs.has(connection.to)) continue
    incomingCount.set(connection.to, (incomingCount.get(connection.to) || 0) + 1)
    outgoing.get(connection.from)?.push(connection.to)
  }
  const levels = new Map<string, number>(elements.map((element) => [element.id, 0]))
  const queue = elements.filter((element) => (incomingCount.get(element.id) || 0) === 0).map((element) => element.id)
  const visited = new Set<string>()
  while (queue.length) {
    const id = queue.shift()!
    visited.add(id)
    const nextLevel = (levels.get(id) || 0) + 1
    for (const targetID of outgoing.get(id) || []) {
      levels.set(targetID, Math.max(levels.get(targetID) || 0, nextLevel))
      incomingCount.set(targetID, Math.max(0, (incomingCount.get(targetID) || 0) - 1))
      if ((incomingCount.get(targetID) || 0) === 0) queue.push(targetID)
    }
  }
  for (const element of elements) {
    if (!visited.has(element.id)) levels.set(element.id, levels.get(element.id) || 0)
  }
  return levels
}

function displayWorkflowLevels() {
  const levels = workflowLevels()
  for (const connection of activeCanvas.value?.connections || []) {
    const source = elementByID(connection.from)
    const target = elementByID(connection.to)
    if (!source || !target) continue
    const targetInputLevel = Math.max(0, (levels.get(target.id) || 0) - 1)
    levels.set(source.id, Math.max(levels.get(source.id) || 0, targetInputLevel))
  }
  return levels
}

function downstreamElementIDs(id: string) {
  const canvas = activeCanvas.value
  const seen = new Set<string>()
  const queue = [id]
  while (queue.length) {
    const current = queue.shift()!
    for (const connection of canvas?.connections || []) {
      if (connection.from !== current || seen.has(connection.to)) continue
      seen.add(connection.to)
      queue.push(connection.to)
    }
  }
  return seen
}

function connectedComponentElements(element: CanvasElement) {
  const canvas = activeCanvas.value
  if (!canvas) return []
  const seen = new Set<string>()
  const queue = [element.id]
  while (queue.length) {
    const id = queue.shift()!
    if (seen.has(id)) continue
    seen.add(id)
    for (const connection of canvas.connections) {
      if (connection.from === id && !seen.has(connection.to)) queue.push(connection.to)
      if (connection.to === id && !seen.has(connection.from)) queue.push(connection.from)
    }
  }
  return canvas.elements.filter((item) => seen.has(item.id))
}

function isMentionableElement(element: CanvasElement) {
  return isMediaKind(element.kind) || element.kind === 'image' || element.kind === 'video' || element.kind === 'mask'
}

function mentionLabel(element: CanvasElement) {
  return nodeBadge(element)
}

function mentionName(element: CanvasElement) {
  return isMediaKind(element.kind) ? element.media_filename || elementTitle(element) : elementTitle(element)
}

function mentionDetail(element: CanvasElement) {
  if (isMediaKind(element.kind)) return element.kind === 'video_media' ? '视频媒体' : element.kind === 'audio_media' ? '音频媒体' : '图片媒体'
  if (element.kind === 'image') return '生图节点'
  if (element.kind === 'video') return '生视频节点'
  if (element.kind === 'mask') return '蒙版'
  return ''
}

function onPromptTextInput(event: Event, element: CanvasElement) {
  const before = textBeforeCaret()
  const match = before.match(/(^|\s)@([^\s@]*)$/)
  if (!match) {
    mentionMenu.value = null
    return
  }
  const query = match[2] || ''
  const previous = mentionMenu.value?.elementID === element.id && mentionMenu.value.query === query ? mentionMenu.value.activeIndex : 0
  mentionMenu.value = { elementID: element.id, query, activeIndex: previous }
}

function onRichEditorKeydown(event: KeyboardEvent, element: CanvasElement) {
  const menu = mentionMenu.value
  if (menu?.elementID === element.id) {
    const candidates = mentionCandidates(element)
    if (event.key === 'ArrowDown' && candidates.length) {
      event.preventDefault()
      menu.activeIndex = (menu.activeIndex + 1) % candidates.length
      scrollActiveMentionIntoView()
      return
    }
    if (event.key === 'ArrowUp' && candidates.length) {
      event.preventDefault()
      menu.activeIndex = (menu.activeIndex - 1 + candidates.length) % candidates.length
      scrollActiveMentionIntoView()
      return
    }
    if (event.key === 'Enter' && candidates.length) {
      event.preventDefault()
      const item = candidates[clamp(menu.activeIndex, 0, candidates.length - 1)]
      insertMention(element, item.label)
      return
    }
    if (event.key === 'Escape') {
      event.preventDefault()
      closeMentionMenu()
      return
    }
  }
  if (event.key === 'Escape' && element.kind === 'llm') {
    event.preventDefault()
    blurEditable(event, element)
  }
}

function scrollActiveMentionIntoView() {
  window.setTimeout(() => {
    document.querySelector('.canvas-mention-menu button.active')?.scrollIntoView({ block: 'nearest' })
  })
}

function hideMentionMenuSoon() {
  window.setTimeout(() => {
    mentionMenu.value = null
  }, 120)
}

function closeMentionMenu() {
  mentionMenu.value = null
}

function closeMentionMenuFromPointer(event: PointerEvent) {
  const target = event.target as HTMLElement | null
  if (canvasContextMenu.value && event.button === 0 && !target?.closest('.canvas-context-menu')) {
    canvasContextMenu.value = null
  }
  if (target?.closest('.canvas-mention-menu')) return
  closeMentionMenu()
  if (spacePanning.value && event.button === 0) {
    event.preventDefault()
    event.stopPropagation()
    event.stopImmediatePropagation()
      ; (event.currentTarget as HTMLElement).setPointerCapture(event.pointerId)
    dragState.value = { type: 'pan', startX: event.clientX, startY: event.clientY, originX: camera.x, originY: camera.y }
  }
}

function setActiveMentionIndex(element: CanvasElement, index: number) {
  if (mentionMenu.value?.elementID !== element.id) return
  mentionMenu.value.activeIndex = index
}

function insertMention(element: CanvasElement, label: string) {
  const token = `@${mentionBadgeFromLabel(label)} `
  if (document.activeElement instanceof HTMLTextAreaElement) {
    replaceCurrentMentionWithToken(label)
    mentionMenu.value = null
    return
  }
  if (!(document.activeElement instanceof HTMLElement) || !document.activeElement.isContentEditable) {
    element.text = `${element.text || ''}${token}`
    mentionMenu.value = null
    return
  }
  replaceCurrentMentionWithToken(label)
  mentionMenu.value = null
}

function editableText(target: HTMLElement) {
  if (target instanceof HTMLTextAreaElement) return target.value.trimEnd()
  return target.innerText.replace(/\u00a0/g, ' ').trimEnd()
}

function textBeforeCaret() {
  const active = document.activeElement
  if (active instanceof HTMLTextAreaElement || active instanceof HTMLInputElement) {
    return active.value.slice(0, active.selectionStart || 0)
  }
  const selection = window.getSelection()
  if (!selection?.rangeCount) return ''
  const range = selection.getRangeAt(0).cloneRange()
  const root = document.activeElement
  if (!(root instanceof HTMLElement)) return ''
  range.selectNodeContents(root)
  range.setEnd(selection.anchorNode || root, selection.anchorOffset)
  return range.toString()
}

function replaceCurrentMentionWithToken(label: string) {
  const active = document.activeElement
  if (active instanceof HTMLTextAreaElement) {
    const caret = active.selectionStart || 0
    const before = active.value.slice(0, caret)
    const after = active.value.slice(active.selectionEnd || caret)
    const match = before.match(/(^|\s)@([^\s@]*)$/)
    const token = `@${mentionBadgeFromLabel(label)} `
    const start = match ? caret - match[0].trimStart().length : caret
    const next = `${active.value.slice(0, start)}${token}${after}`
    active.value = next
    const element = active.dataset.nodeId ? elementByID(active.dataset.nodeId) : undefined
    if (element) element.text = next
    const nextCaret = start + token.length
    active.focus()
    active.setSelectionRange(nextCaret, nextCaret)
    return
  }
  const selection = window.getSelection()
  if (!selection?.rangeCount) return
  const range = selection.getRangeAt(0)
  const before = textBeforeCaret()
  const match = before.match(/(^|\s)@([^\s@]*)$/)
  if (match) {
    range.setStart(range.startContainer, Math.max(0, range.startOffset - match[0].trimStart().length))
  }
  range.deleteContents()
  const token = document.createElement('span')
  token.className = 'canvas-mention-token'
  token.contentEditable = 'false'
  token.textContent = `@${mentionBadgeFromLabel(label)}`
  range.insertNode(document.createTextNode(' '))
  range.insertNode(token)
  range.collapse(false)
  selection.removeAllRanges()
  selection.addRange(range)
}

function syncEditableText(event: Event, element: CanvasElement) {
  element.text = editableText(event.currentTarget as HTMLElement)
}

function blurEditable(event: Event, element: CanvasElement) {
  syncEditableText(event, element)
    ; (event.currentTarget as HTMLElement).blur()
}

function syncActiveEditable() {
  const target = document.activeElement
  if (!(target instanceof HTMLElement) || (!target.isContentEditable && !(target instanceof HTMLTextAreaElement))) return
  const id = target.dataset.nodeId
  const element = id ? elementByID(id) : undefined
  if (element) element.text = editableText(target)
}

function renderEditableText(text?: string) {
  const source = escapeHTML(text || '')
  return source.replace(/(^|\s)@([A-Z]+\d{2})\b/g, '$1<span class="canvas-mention-token" contenteditable="false">@$2</span>')
}

function mentionBadgeFromLabel(label: string) {
  return label.trim().split(/\s+/)[0]
}

function elementByID(id: string) {
  return activeCanvas.value?.elements.find((element) => element.id === id)
}

function connectedInputs(element: CanvasElement) {
  return (activeCanvas.value?.connections || []).filter((connection) => connection.to === element.id).map((connection) => elementByID(connection.from)).filter(Boolean) as CanvasElement[]
}

function upstreamElements(element: CanvasElement, kind?: NodeKind) {
  const items = connectedInputs(element)
  return kind ? items.filter((item) => item.kind === kind) : items
}

function isProcessKind(kind: NodeKind) {
  return kind === 'llm' || kind === 'image' || kind === 'video' || kind === 'audio'
}

function isRunnableKind(kind: NodeKind) {
  return kind === 'llm' || kind === 'image' || kind === 'video'
}

function acceptsInput(kind: NodeKind) {
  return !isMediaKind(kind) && kind !== 'asset' && kind !== 'ai'
}

function hasOutput(kind: NodeKind) {
  return kind !== 'asset' && kind !== 'ai'
}

function isPromptLike(kind: NodeKind) {
  return kind === 'prompt' || kind === 'llm' || kind === 'merge'
}

function isTextKind(kind: NodeKind) {
  return kind === 'prompt' || kind === 'llm'
}

function canConnect(from: CanvasElement, to: CanvasElement) {
  if (from.id === to.id || !hasOutput(from.kind) || !acceptsInput(to.kind)) return false
  if (to.kind === 'merge') return true
  if (from.kind === 'image_media') return to.kind === 'image' || to.kind === 'video' || to.kind === 'mask'
  return outputTypes(from).some((type) => acceptedInputTypes(to).includes(type))
}

function outputTypes(element: CanvasElement): NodeValueType[] {
  if (element.kind === 'prompt' || element.kind === 'llm') return ['text']
  if (element.kind === 'image_media' || element.kind === 'image' || element.kind === 'mask') return ['image']
  if (element.kind === 'video_media' || element.kind === 'video') return ['video']
  if (element.kind === 'audio_media' || element.kind === 'audio') return ['audio']
  if (element.kind === 'merge') return ['merge']
  return []
}

function acceptedInputTypes(element: CanvasElement): NodeValueType[] {
  if (element.kind === 'prompt') return ['text']
  if (element.kind === 'llm') return ['text', 'image', 'video', 'audio', 'merge']
  if (element.kind === 'image') return ['text', 'image', 'merge']
  if (element.kind === 'video') return ['text', 'image', 'video', 'audio', 'merge']
  if (element.kind === 'audio') return ['text', 'audio', 'merge']
  if (element.kind === 'mask') return ['image']
  if (element.kind === 'merge') return ['text', 'image', 'video', 'audio', 'merge']
  return []
}

function connectableTargetKinds(from: CanvasElement): NodeKind[] {
  const candidates: NodeKind[] = ['prompt', 'llm', 'image', 'video', 'audio', 'merge', 'mask']
  return candidates.filter((kind) => canConnect(from, { ...from, id: '__target__', kind } as CanvasElement))
}

function connectableSourceKinds(to: CanvasElement): NodeKind[] {
  const candidates: NodeKind[] = ['prompt', 'llm', 'image', 'video', 'audio', 'merge', 'mask', 'image_media', 'video_media', 'audio_media']
  return candidates.filter((kind) => canConnect({ ...to, id: '__source__', kind } as CanvasElement, to))
}

function connectableTargetLabels(from: CanvasElement) {
  return connectableTargetKinds(from).map((kind) => elementTitle({ id: '__label__', kind, x: 0, y: 0, width: 0, height: 0 } as CanvasElement)).join('、')
}

function connectableSourceLabels(to: CanvasElement) {
  return connectableSourceKinds(to).map((kind) => elementTitle({ id: '__label__', kind, x: 0, y: 0, width: 0, height: 0 } as CanvasElement)).join('、')
}

function showCanvasNotice(message: string) {
  canvasNotice.value = message
  window.clearTimeout((showCanvasNotice as unknown as { timer?: number }).timer)
  ;(showCanvasNotice as unknown as { timer?: number }).timer = window.setTimeout(() => {
    canvasNotice.value = ''
  }, 2800)
}

function showInvalidConnectionNotice(from: CanvasElement, to?: CanvasElement) {
  const targets = connectableTargetLabels(from)
  const targetText = to ? `不能连接到「${elementTitle(to)}」。` : ''
  showCanvasNotice(`${targetText}「${elementTitle(from)}」只能连接到：${targets || '无'}`)
}

function showInvalidSourceNotice(to: CanvasElement, from?: CanvasElement) {
  const sources = connectableSourceLabels(to)
  const sourceText = from ? `「${elementTitle(from)}」不能连接到这里。` : ''
  showCanvasNotice(`${sourceText}「${elementTitle(to)}」可接入：${sources || '无'}`)
}

function elementTitle(element: CanvasElement) {
  if (element.kind === 'asset') return '媒体'
  if (element.kind === 'ai') return 'AI 生成'
  if (element.kind === 'prompt') return '文字提示词'
  if (element.kind === 'merge') return '汇合节点'
  if (element.kind === 'llm') return '生文字节点'
  if (element.kind === 'image') return '生图节点'
  if (element.kind === 'mask') return '蒙版节点'
  if (element.kind === 'video') return '生视频节点'
  if (element.kind === 'audio') return '生音频节点'
  if (element.kind === 'image_media') return '图片媒体'
  if (element.kind === 'video_media') return '视频媒体'
  if (element.kind === 'audio_media') return '音频媒体'
  return '媒体节点'
}

function nodeBadge(element: CanvasElement) {
  const prefix = nodeBadgePrefix(element.kind)
  if (!prefix) return ''
  if (element.badge && element.badge.startsWith(prefix)) return element.badge
  return ''
}

function nodeBadgePrefix(kind: NodeKind) {
  if (kind === 'asset' || kind === 'ai') return ''
  if (kind === 'prompt' || kind === 'llm') return 'TEXT'
  if (kind === 'image_media' || kind === 'image') return 'IMAGE'
  if (kind === 'video_media' || kind === 'video') return 'VIDEO'
  if (kind === 'audio_media' || kind === 'audio') return 'AUDIO'
  if (kind === 'merge') return 'MERGE'
  if (kind === 'mask') return 'MASK'
  return ''
}

function ensureElementBadges(elements: CanvasElement[]) {
  for (const element of elements) assignElementBadge(element, elements)
  return elements
}

function assignElementBadge(element: CanvasElement, elements = activeCanvas.value?.elements || []) {
  const prefix = nodeBadgePrefix(element.kind)
  if (!prefix) {
    element.badge = ''
    return ''
  }
  const existing = String(element.badge || '').trim()
  const used = new Set(elements.filter((item) => item.id !== element.id).map((item) => item.badge).filter(Boolean) as string[])
  if (existing.startsWith(prefix) && !used.has(existing)) return existing
  let index = 1
  let badge = `${prefix}${String(index).padStart(2, '0')}`
  while (used.has(badge)) {
    index += 1
    badge = `${prefix}${String(index).padStart(2, '0')}`
  }
  element.badge = badge
  return badge
}

function promptTextFor(element: CanvasElement, visited = new Set<string>()): string {
  if (visited.has(element.id)) return ''
  visited.add(element.id)
  if (element.kind === 'llm') return (element.text || '').trim()
  const upstreamText = connectedInputs(element)
    .filter((item) => isPromptLike(item.kind))
    .map((item) => promptTextFor(item, visited))
  if (element.kind === 'prompt') upstreamText.push(element.text || '')
  return upstreamText.map((text) => text.trim()).filter(Boolean).join('\n')
}

function llmInputPrompt(element: CanvasElement) {
  return connectedInputs(element).filter((item) => isPromptLike(item.kind)).map((item) => promptTextFor(item)).filter(Boolean).join('\n')
}

function buildNodePrompt(element: CanvasElement) {
  const prompt = promptTextFor(element)
  const upstreamVideos = connectedInputs(element).filter((item) => item.kind === 'video' || item.kind === 'video_media' || taskResultVideo(item)?.url)
  if (element.kind === 'video' && upstreamVideos.length > 1) {
    return [prompt, '按上游视频连接顺序进行视频拼接；如果视频属性设置了截取区间，只使用对应片段。'].filter(Boolean).join('\n')
  }
  return prompt
}

function mediaReferences(element: CanvasElement) {
  const reference_images: UploadedImage[] = []
  const reference_videos: MediaAsset[] = []
  const reference_audios: MediaAsset[] = []
  const sources = referenceSourceElements(element)
  for (const source of sources) {
    const label = nodeBadge(source)
    const localImage = withReferenceMeta(localUploadedImage(source), source, label)
    const localAsset = withMediaReferenceMeta(localMediaAsset(source), source, label)
    if (localImage?.url) reference_images.push(localImage)
    if (localAsset?.url && localAsset.type === 'video') reference_videos.push(localAsset)
    if (localAsset?.url && localAsset.type === 'audio') reference_audios.push(localAsset)
    const image = localImage?.url ? undefined : withReferenceMeta(taskResultImage(source), source, label)
    const video = localAsset?.url ? undefined : withMediaReferenceMeta(taskResultVideo(source), source, label)
    if (image?.url) reference_images.push(image)
    if (video?.url) reference_videos.push(video)
    for (const audio of taskForElement(source)?.reference_audios || []) {
      const audioAsset = withMediaReferenceMeta({ ...audio, type: audio.type || 'audio' }, source, label)
      if (audioAsset?.url) reference_audios.push(audioAsset)
    }
  }
  for (const source of sources.filter((item) => item.kind === 'mask')) {
    const sourceElement = maskSourceElement(source)
    const image = sourceElement ? localUploadedImage(sourceElement) || taskResultImage(sourceElement) : undefined
    const maskedImage = image?.url ? withReferenceMeta({
      ...image,
      mask_url: source.mask_data_url || image.mask_url,
      mask_reference_label: nodeBadge(source),
    }, sourceElement || source, sourceElement ? nodeBadge(sourceElement) : nodeBadge(source)) : undefined
    if (maskedImage?.url) reference_images.push(maskedImage)
  }
  return {
    reference_images: uniqueReferenceImages(reference_images),
    reference_videos: uniqueMediaAssets(reference_videos),
    reference_audios: uniqueMediaAssets(reference_audios),
  }
}

function referenceSourceElements(element: CanvasElement) {
  const prompt = element.kind === 'llm' ? llmInputPrompt(element) : promptTextFor(element)
  const sources = new Map<string, CanvasElement>()
  for (const source of connectedInputs(element)) {
    for (const item of expandedReferenceSources(source)) sources.set(item.id, item)
  }
  for (const source of mentionedElements(prompt)) sources.set(source.id, source)
  return Array.from(sources.values())
}

function expandedReferenceSources(element: CanvasElement, visited = new Set<string>()): CanvasElement[] {
  if (visited.has(element.id)) return []
  visited.add(element.id)
  if (element.kind === 'merge') {
    return connectedInputs(element).flatMap((source) => expandedReferenceSources(source, visited))
  }
  return [element]
}

function mentionedElements(text: string) {
  const badges = new Set(Array.from(text.matchAll(/@([A-Z]+\d{2})/g)).map((match) => match[1]))
  if (!badges.size) return []
  return (activeCanvas.value?.elements || []).filter((item) => badges.has(nodeBadge(item)) && isMentionableElement(item))
}

function withReferenceMeta(image: UploadedImage | undefined, element: CanvasElement, label: string) {
  return image?.url ? { ...image, node_id: element.id, reference_label: label } : undefined
}

function withMediaReferenceMeta(asset: MediaAsset | undefined, element: CanvasElement, label: string) {
  return asset?.url ? { ...asset, node_id: element.id, reference_label: label } : undefined
}

function uniqueReferenceImages(images: UploadedImage[]) {
  const seen = new Set<string>()
  const maskedURLs = new Set(images.filter((image) => image.mask_url && image.url).map((image) => normalizedReferenceURL(image.url)))
  return images.filter((image) => {
    if (!image.mask_url && maskedURLs.has(normalizedReferenceURL(image.url))) return false
    const key = `${image.reference_label || ''}|${image.url}|${image.mask_url || ''}`
    if (seen.has(key)) return false
    seen.add(key)
    return true
  })
}

function normalizedReferenceURL(url: string) {
  return url.trim()
}

function uniqueMediaAssets(items: MediaAsset[]) {
  const seen = new Set<string>()
  return items.filter((item) => {
    const key = `${item.reference_label || ''}|${item.type || ''}|${item.url}|${item.clip_start || ''}|${item.clip_end || ''}`
    if (seen.has(key)) return false
    seen.add(key)
    return true
  })
}

function maskSourceImage(element: CanvasElement) {
  const source = maskSourceElement(element)
  return source ? localUploadedImage(source) || taskResultImage(source) : undefined
}

function maskSourceElement(element: CanvasElement) {
  return connectedInputs(element).find((item) => item.kind === 'image_media' || item.kind === 'image')
}

function prepareMaskCanvas(event: Event, element: CanvasElement) {
  const img = event.target as HTMLImageElement
  const wrap = img.closest('.canvas-mask-editor')
  const canvas = wrap?.querySelector('canvas')
  if (!canvas || !img.naturalWidth || !img.naturalHeight) return
  canvas.width = img.naturalWidth
  canvas.height = img.naturalHeight
  syncMaskCanvasFrame(img, canvas)
  observeMaskCanvasFrame(img, canvas, element)
  if (element.mask_data_url) {
    const ctx = canvas.getContext('2d')
    const mask = new Image()
    mask.onload = () => ctx?.drawImage(mask, 0, 0, canvas.width, canvas.height)
    mask.src = element.mask_data_url
  }
}

function observeMaskCanvasFrame(img: HTMLImageElement, canvas: HTMLCanvasElement, element: CanvasElement) {
  const wrap = img.closest('.canvas-mask-editor')
  if (!(wrap instanceof HTMLElement) || maskResizeObservers.has(wrap)) return
  const observer = new ResizeObserver(() => {
    syncMaskCanvasFrame(img, canvas)
    if (maskCursor.value.elementID === element.id) {
      maskCursor.value = { ...maskCursor.value, visible: false }
    }
  })
  observer.observe(wrap)
  maskResizeObservers.set(wrap, observer)
  activeMaskResizeObservers.add(observer)
}

function syncMaskCanvasFrame(img: HTMLImageElement, canvas: HTMLCanvasElement) {
  const wrap = img.closest('.canvas-mask-editor')
  if (!(wrap instanceof HTMLElement) || !img.naturalWidth || !img.naturalHeight) return
  const width = wrap.clientWidth
  const height = wrap.clientHeight
  const scale = Math.min(width / img.naturalWidth, height / img.naturalHeight)
  const drawnWidth = img.naturalWidth * scale
  const drawnHeight = img.naturalHeight * scale
  const left = (width - drawnWidth) / 2
  const top = (height - drawnHeight) / 2
  for (const item of [img, canvas]) {
    item.style.left = `${left}px`
    item.style.top = `${top}px`
    item.style.right = 'auto'
    item.style.bottom = 'auto'
    item.style.width = `${drawnWidth}px`
    item.style.height = `${drawnHeight}px`
  }
}

function maskPoint(event: PointerEvent, canvas: HTMLCanvasElement, element: CanvasElement) {
  void element
  const img = canvas.parentElement?.querySelector('img')
  if (img instanceof HTMLImageElement) syncMaskCanvasFrame(img, canvas)
  const rect = canvas.getBoundingClientRect()
  return {
    x: clamp(((event.clientX - rect.left) / rect.width) * canvas.width, 0, canvas.width),
    y: clamp(((event.clientY - rect.top) / rect.height) * canvas.height, 0, canvas.height),
  }
}

function stopMaskSizeEvent(event: Event) {
  event.stopPropagation()
}

function maskCursorStyle(element: CanvasElement) {
  if (!maskCursor.value.visible || maskCursor.value.elementID !== element.id || element.mask_tool === 'pan') return { display: 'none' }
  return {
    left: `${maskCursor.value.x}px`,
    top: `${maskCursor.value.y}px`,
    width: `${maskCursor.value.size}px`,
    height: `${maskCursor.value.size}px`,
  }
}

function updateMaskCursor(event: PointerEvent, element: CanvasElement, canvas: HTMLCanvasElement) {
  activeMaskElementID.value = element.id
  if (element.mask_tool === 'pan') {
    maskCursor.value = { ...maskCursor.value, elementID: element.id, visible: false }
    return
  }
  const editor = canvas.closest('.canvas-mask-editor')
  if (!(editor instanceof HTMLElement)) return
  const editorRect = editor.getBoundingClientRect()
  const img = canvas.parentElement?.querySelector('img')
  if (img instanceof HTMLImageElement) syncMaskCanvasFrame(img, canvas)
  const canvasRect = canvas.getBoundingClientRect()
  const editorScaleX = editorRect.width / (editor.clientWidth || editorRect.width || 1)
  const editorScaleY = editorRect.height / (editor.clientHeight || editorRect.height || 1)
  const scale = (canvasRect.width / canvas.width) / editorScaleX
  maskCursor.value = {
    elementID: element.id,
    x: (event.clientX - editorRect.left) / editorScaleX,
    y: (event.clientY - editorRect.top) / editorScaleY,
    size: Math.max(8, (element.mask_brush_size || 32) * scale),
    visible: true,
  }
}

function hideMaskCursor(element: CanvasElement) {
  if (maskCursor.value.elementID === element.id) maskCursor.value = { ...maskCursor.value, visible: false }
}

function enterMaskNode(element: CanvasElement) {
  hoveredMaskElementID.value = element.id
  activeMaskElementID.value = element.id
}

function leaveMaskNode(element: CanvasElement) {
  if (hoveredMaskElementID.value === element.id) hoveredMaskElementID.value = ''
  hideMaskCursor(element)
}

function setMaskTool(element: CanvasElement, tool: NonNullable<CanvasElement['mask_tool']>) {
  activeMaskElementID.value = element.id
  element.mask_tool = tool
  if (tool === 'pan') hideMaskCursor(element)
}

function activeMaskElement() {
  return hoveredMaskElementID.value ? elementByID(hoveredMaskElementID.value) : undefined
}

function setActiveMaskTool(tool: NonNullable<CanvasElement['mask_tool']>) {
  const element = activeMaskElement()
  if (!element || element.kind !== 'mask') return false
  setMaskTool(element, tool)
  return true
}

function drawMask(event: PointerEvent, element: CanvasElement, canvas: HTMLCanvasElement, connectFromLast = true) {
  const ctx = canvas.getContext('2d')
  if (!ctx) return
  const point = maskPoint(event, canvas, element)
  ctx.globalCompositeOperation = element.mask_tool === 'eraser' ? 'destination-out' : 'source-over'
  ctx.strokeStyle = '#fff'
  ctx.fillStyle = '#fff'
  ctx.lineWidth = element.mask_brush_size || 32
  ctx.lineCap = 'round'
  ctx.lineJoin = 'round'
  ctx.beginPath()
  const last = maskPaintState.value?.elementID === element.id ? maskPaintState.value.point : null
  if (connectFromLast && last) {
    ctx.moveTo(last.x, last.y)
    ctx.lineTo(point.x, point.y)
    ctx.stroke()
  } else {
    ctx.arc(point.x, point.y, (element.mask_brush_size || 32) / 2, 0, Math.PI * 2)
    ctx.fill()
  }
  maskPaintState.value = { elementID: element.id, point }
}

function startMaskPointer(event: PointerEvent, element: CanvasElement) {
  event.preventDefault()
  event.stopPropagation()
  const canvas = event.currentTarget as HTMLCanvasElement
  activeMaskElementID.value = element.id
  if (element.mask_tool === 'pan') {
    startNodeImagePan(event, element)
    return
  }
  canvas.setPointerCapture(event.pointerId)
  activeMaskPointer.value = { elementID: element.id, pointerId: event.pointerId, canvas, element }
  maskPaintState.value = null
  window.addEventListener('pointermove', moveMaskPaintFromWindow, { passive: false })
  window.addEventListener('pointerup', stopMaskPaintFromWindow, { passive: false })
  window.addEventListener('pointercancel', stopMaskPaintFromWindow, { passive: false })
  drawMask(event, element, canvas, false)
}

function moveMaskPointer(event: PointerEvent, element: CanvasElement) {
  event.preventDefault()
  event.stopPropagation()
  const canvas = event.currentTarget as HTMLCanvasElement
  activeMaskElementID.value = element.id
  if (element.mask_tool === 'pan') {
    moveNodeImagePan(event, element)
    return
  }
  updateMaskCursor(event, element, canvas)
  if (!isActiveMaskEvent(event, element.id)) return
  const events = event.getCoalescedEvents?.() || [event]
  events.forEach((item) => drawMask(item, element, canvas))
}

function stopMaskPointer(event: PointerEvent, element: CanvasElement) {
  event.preventDefault()
  event.stopPropagation()
  if (element.mask_tool === 'pan') {
    stopNodeImagePan(event)
    return
  }
  const canvas = event.currentTarget as HTMLCanvasElement
  finishMaskPaint(event, element, canvas)
}

function moveMaskPaintFromWindow(event: PointerEvent) {
  const active = activeMaskPointer.value
  if (!active || !isActiveMaskEvent(event, active.elementID)) return
  event.preventDefault()
  const events = event.getCoalescedEvents?.() || [event]
  events.forEach((item) => drawMask(item, active.element, active.canvas))
}

function stopMaskPaintFromWindow(event: PointerEvent) {
  const active = activeMaskPointer.value
  if (!active || !isActiveMaskEvent(event, active.elementID)) return
  event.preventDefault()
  finishMaskPaint(event, active.element, active.canvas)
}

function isActiveMaskEvent(event: PointerEvent, elementID: string) {
  return activeMaskPointer.value?.elementID === elementID && activeMaskPointer.value.pointerId === event.pointerId
}

function finishMaskPaint(event: PointerEvent, element: CanvasElement, canvas: HTMLCanvasElement) {
  if (!isActiveMaskEvent(event, element.id)) return
  if (canvas.hasPointerCapture(event.pointerId)) canvas.releasePointerCapture(event.pointerId)
  window.removeEventListener('pointermove', moveMaskPaintFromWindow)
  window.removeEventListener('pointerup', stopMaskPaintFromWindow)
  window.removeEventListener('pointercancel', stopMaskPaintFromWindow)
  element.mask_data_url = canvas.toDataURL('image/png')
  maskPaintState.value = null
  activeMaskPointer.value = null
}

function clearMask(element: CanvasElement, event?: Event) {
  const canvas = (event?.currentTarget as HTMLElement | undefined)?.closest('.canvas-generate-node')?.querySelector('canvas')
  const ctx = canvas?.getContext('2d')
  if (canvas && ctx) ctx.clearRect(0, 0, canvas.width, canvas.height)
  element.mask_data_url = ''
}

function toggleFrozen(element: CanvasElement) {
  if (!isRunnableKind(element.kind) || isNodeBusy(element)) return
  element.frozen = !element.frozen
}

async function runGenerateNode(element: CanvasElement) {
  syncActiveEditable()
  if (element.kind === 'mask' || element.kind === 'audio' || element.frozen) return
  runningNodeID.value = element.id
  nodeRunState.value = { ...nodeRunState.value, [element.id]: { status: 'running', startedAt: Date.now() } }
  try {
    if (element.kind === 'llm') {
      const refs = mediaReferences(element)
      const payload = {
        prompt: llmInputPrompt(element),
        model: element.model || props.defaultForm.model,
        reasoning_effort: element.reasoning_effort || 'low',
        reference_images: refs.reference_images,
        reference_videos: refs.reference_videos,
        reference_audios: refs.reference_audios,
      }
      const applyResult = (text: string) => {
        element.text = text
      }
      if (props.runLlmAction) await props.runLlmAction(payload, applyResult)
      else emit('runLlm', payload, applyResult)
      nodeRunState.value = { ...nodeRunState.value, [element.id]: { ...nodeRunState.value[element.id], status: 'succeeded', endedAt: Date.now() } }
      return
    }
    const refs = mediaReferences(element)
    if (element.kind === 'video') normalizeCanvasVideoSettings(element)
    const payload: CanvasRunPayload = {
      node_kind: element.kind === 'video' ? 'video' : 'image',
      prompt: buildNodePrompt(element),
      task_type: element.kind === 'video' ? 'video_generation' : 'image_generation',
      model: canvasNodeModel(element),
      size: element.size || props.defaultForm.size,
      quality: element.quality || props.defaultForm.quality,
      output_format: element.output_format || props.defaultForm.output_format,
      output_compression: Number(element.output_compression ?? props.defaultForm.output_compression),
      background: element.background || props.defaultForm.background,
      moderation: element.moderation || props.defaultForm.moderation,
      input_fidelity: element.input_fidelity || props.defaultForm.input_fidelity,
      reference_images: refs.reference_images,
      reference_videos: refs.reference_videos,
      reference_audios: refs.reference_audios,
      video_ratio: element.video_ratio || props.defaultForm.video_ratio,
      video_resolution: element.video_resolution || props.defaultForm.video_resolution,
      video_duration: Number(element.video_duration ?? props.defaultForm.video_duration),
      generate_audio: element.generate_audio ?? props.defaultForm.generate_audio,
      watermark: element.watermark ?? props.defaultForm.watermark,
    }
    let latestTask: Task | undefined
    const applyTask = (task: Task) => {
      latestTask = task
      element.task_id = task.id
    }
    if (props.runNodeAction) await props.runNodeAction(payload, applyTask)
    else emit('runNode', payload, applyTask)
    const task = latestTask || generatedTask(element)
    nodeRunState.value = { ...nodeRunState.value, [element.id]: { ...nodeRunState.value[element.id], status: task?.status === 'failed' ? 'failed' : 'succeeded', endedAt: Date.now(), message: task?.error_message } }
    if (task?.status === 'failed') throw new Error(task.error_message || '任务生成失败')
  } catch (error) {
    nodeRunState.value = { ...nodeRunState.value, [element.id]: { ...nodeRunState.value[element.id], status: 'failed', endedAt: Date.now(), message: error instanceof Error ? error.message : '运行失败' } }
    throw error
  } finally {
    runningNodeID.value = ''
  }
}

async function runToNode(element: CanvasElement) {
  if (!isRunnableKind(element.kind) || isLineBusy(element)) return
  const lineIDs = collectDependencyIDs(element)
  addRunningLine(lineIDs)
  try {
    const visited = new Set<string>()
    const visiting = new Set<string>()
    await runElementWithDependencies(element, visited, visiting)
  } finally {
    runningNodeID.value = ''
    removeRunningLine(lineIDs)
  }
}

async function runCanvasWorkflow() {
  if (runningWorkflow.value) return
  runningWorkflow.value = true
  const workflowIDs = new Set((activeCanvas.value?.elements || []).filter((element) => isRunnableKind(element.kind) && !element.frozen && !isLineBusy(element)).map((element) => element.id))
  addRunningLine(workflowIDs)
  try {
    const visited = new Set<string>()
    const visiting = new Set<string>()
    for (const element of elementsByWorkflowLevel()) {
      if (isRunnableKind(element.kind) && !element.frozen && workflowIDs.has(element.id)) await runElementWithDependencies(element, visited, visiting)
    }
  } finally {
    runningNodeID.value = ''
    removeRunningLine(workflowIDs)
    runningWorkflow.value = false
  }
}

function elementsByWorkflowLevel() {
  const levels = workflowLevels()
  return [...(activeCanvas.value?.elements || [])].sort((a, b) => {
    const levelDelta = (levels.get(a.id) || 0) - (levels.get(b.id) || 0)
    if (levelDelta) return levelDelta
    return a.y === b.y ? a.x - b.x : a.y - b.y
  })
}

async function runElementWithDependencies(element: CanvasElement, visited: Set<string>, visiting: Set<string>) {
  if (visited.has(element.id) || visiting.has(element.id)) return
  visiting.add(element.id)
  for (const source of runnableDependencyInputs(element)) {
    if (isRunnableKind(source.kind)) await runElementWithDependencies(source, visited, visiting)
  }
  visiting.delete(element.id)
  if (isRunnableKind(element.kind) && !element.frozen) await runGenerateNode(element)
  visited.add(element.id)
}

function collectDependencyIDs(element: CanvasElement, seen = new Set<string>()) {
  if (seen.has(element.id)) return seen
  seen.add(element.id)
  for (const source of runnableDependencyInputs(element)) {
    if (isRunnableKind(source.kind)) collectDependencyIDs(source, seen)
  }
  return new Set(Array.from(seen).filter((id) => {
    const element = elementByID(id)
    return Boolean(element && isRunnableKind(element.kind) && !element.frozen)
  }))
}

function runnableDependencyInputs(element: CanvasElement, visited = new Set<string>()): CanvasElement[] {
  if (visited.has(element.id)) return []
  visited.add(element.id)
  return connectedInputs(element).flatMap((source) => source.kind === 'merge' ? runnableDependencyInputs(source, visited) : [source])
}

function screenToWorld(clientX: number, clientY: number) {
  const rect = document.querySelector('.canvas-workspace')?.getBoundingClientRect()
  return { x: (clientX - (rect?.left || 0) - camera.x) / camera.zoom, y: (clientY - (rect?.top || 0) - camera.y) / camera.zoom }
}

function minNodeSize(kind: NodeKind) {
  if (kind === 'asset') return { width: 320, height: 330 }
  if (kind === 'ai') return { width: 360, height: 230 }
  if (kind === 'image') return { width: 560, height: 260 }
  if (kind === 'video') return { width: 640, height: 270 }
  if (kind === 'audio') return { width: 420, height: 220 }
  if (kind === 'image_media') return { width: 260, height: 220 }
  if (kind === 'video_media') return { width: 360, height: 230 }
  if (kind === 'audio_media') return { width: 280, height: 170 }
  if (kind === 'llm') return { width: 380, height: 240 }
  if (kind === 'mask') return { width: 380, height: 260 }
  if (kind === 'prompt') return { width: 260, height: 140 }
  if (kind === 'merge') return { width: 240, height: 140 }
  return { width: 180, height: 140 }
}

function renderedNodeSize(element: CanvasElement) {
  const minSize = minNodeSize(element.kind)
  return {
    width: Math.max(minSize.width, element.width),
    height: Math.max(minSize.height, element.height),
  }
}

function miniMapNodeColor(node: { data?: { element?: CanvasElement } }) {
  const kind = node.data?.element?.kind
  if (kind === 'video' || kind === 'video_media') return 'rgba(125, 211, 252, .86)'
  if (kind === 'image' || kind === 'image_media' || kind === 'mask') return 'rgba(167, 243, 208, .82)'
  if (kind === 'audio' || kind === 'audio_media') return 'rgba(216, 180, 254, .82)'
  if (kind === 'prompt' || kind === 'llm') return 'rgba(253, 230, 138, .82)'
  if (kind === 'merge') return 'rgba(248, 250, 252, .72)'
  return 'rgba(226, 232, 240, .78)'
}

function onFlowConnect(connection: Connection) {
  suppressFlowConnectEnd.value = true
  pendingFlowConnection.value = null
  if (!activeCanvas.value || !connection.source || !connection.target || connection.source === connection.target) return
  const from = elementByID(connection.source)
  const to = elementByID(connection.target)
  if (!from || !to) return
  if (!canConnect(from, to)) {
    showInvalidConnectionNotice(from, to)
    return
  }
  if (!activeCanvas.value.connections.some((item) => item.from === connection.source && item.to === connection.target)) {
    activeCanvas.value.connections.push({ id: createID(), from: connection.source, to: connection.target })
  }
}

function isValidFlowConnection(connection: Connection) {
  if (!connection.source || !connection.target || connection.source === connection.target) return false
  const from = elementByID(connection.source)
  const to = elementByID(connection.target)
  return Boolean(from && to && canConnect(from, to))
}

function onFlowEdgeUpdate(event: EdgeUpdateEvent) {
  if (!activeCanvas.value) return
  const connection = event.connection
  if (!connection.source || !connection.target || connection.source === connection.target) return
  const from = elementByID(connection.source)
  const to = elementByID(connection.target)
  if (!from || !to) return
  if (!canConnect(from, to)) {
    showInvalidConnectionNotice(from, to)
    return
  }
  const existing = activeCanvas.value.connections.find((item) => item.id === event.edge.id)
  if (!existing) return
  const duplicate = activeCanvas.value.connections.some((item) => item.id !== existing.id && item.from === connection.source && item.to === connection.target)
  if (duplicate) {
    removeConnection(existing.id)
    return
  }
  existing.from = connection.source
  existing.to = connection.target
}

function onFlowConnectStart(params: { nodeId?: string | null; handleType?: string | null }) {
  suppressFlowConnectEnd.value = false
  pendingFlowConnection.value = params.nodeId && (params.handleType === 'source' || params.handleType === 'target')
    ? { nodeId: params.nodeId, handleType: params.handleType }
    : null
}

function onFlowConnectEnd(event?: MouseEvent | TouchEvent) {
  const pending = pendingFlowConnection.value
  pendingFlowConnection.value = null
  if (!pending) return
  if (suppressFlowConnectEnd.value) {
    suppressFlowConnectEnd.value = false
    return
  }
  const point = event && 'changedTouches' in event ? event.changedTouches[0] : event
  if (!point) return
  const anchor = elementByID(pending.nodeId)
  if (!anchor) return
  if (pending.handleType === 'source') {
    const target = inputElementAt(point.clientX, point.clientY)
    if (target && target.id !== anchor.id) {
      if (!canConnect(anchor, target)) showInvalidConnectionNotice(anchor, target)
      return
    }
    window.setTimeout(() => openConnectionTargetMenu(anchor, point), 0)
    return
  }
  const source = outputElementAt(point.clientX, point.clientY)
  if (source && source.id !== anchor.id) {
    if (!canConnect(source, anchor)) showInvalidSourceNotice(anchor, source)
    return
  }
  window.setTimeout(() => openConnectionSourceMenu(anchor, point), 0)
}

function onFlowNodesChange(changes: NodeChange[]) {
  let nextSelected = selectedNodeIDs.value
  const selectedNow: string[] = []
  for (const change of changes) {
    if (change.type !== 'select') continue
    if (nextSelected === selectedNodeIDs.value) nextSelected = new Set(selectedNodeIDs.value)
    if (change.selected) {
      nextSelected.add(change.id)
      selectedNow.push(change.id)
    }
    else nextSelected.delete(change.id)
  }
  for (const id of selectedNow) {
    const element = elementByID(id)
    if (element) bringElementToFront(element)
  }
  if (nextSelected !== selectedNodeIDs.value) {
    selectedNodeIDs.value = nextSelected
  }
}

function onFlowEdgesChange(changes: EdgeChange[]) {
  if (!activeCanvas.value) return
  const removedIDs = changes.filter((change) => change.type === 'remove').map((change) => change.id)
  if (!removedIDs.length) return
  const removed = new Set(removedIDs)
  activeCanvas.value.connections = activeCanvas.value.connections.filter((connection) => !removed.has(connection.id))
}

function onFlowPaneClick() {
  selectedNodeIDs.value = new Set()
  canvasContextMenu.value = null
}

function onFlowNodeDragStart(_event: NodeDragEvent) {
  return
}

function syncFlowNodePositions(nodes: Node[]) {
  if (!activeCanvas.value) return
  const elementsByID = new Map(activeCanvas.value.elements.map((element) => [element.id, element]))
  for (const node of nodes) {
    const element = elementsByID.get(node.id)
    if (!element) continue
    element.x = node.position.x
    element.y = node.position.y
  }
}

async function onFlowNodeDragStop(_event: NodeDragEvent) {
  await nextTick()
  syncFlowNodePositions(flow.getNodes.value)
  saveCanvases()
  queueCanvasHistorySnapshot()
}

function onFlowViewportChange(viewport: ViewportTransform) {
  pan.value = { x: viewport.x, y: viewport.y }
  zoom.value = viewport.zoom
  camera.x = viewport.x
  camera.y = viewport.y
  camera.zoom = viewport.zoom
}

function startNodeDrag(event: PointerEvent, element: CanvasElement) {
  if (event.button !== 0) return
  event.preventDefault()
    ; (event.currentTarget as HTMLElement).setPointerCapture(event.pointerId)
  const target = event.altKey ? duplicateElement(element) : element
  dragState.value = { type: 'node', id: target.id, startX: event.clientX, startY: event.clientY, originX: target.x, originY: target.y }
}

function onCanvasNodePointerDown(event: PointerEvent, element: CanvasElement) {
  closeMentionMenu()
  if (spacePanning.value) return
  event.stopPropagation()
  if (event.button !== 0 || !event.altKey) return
  if (!(event.target instanceof HTMLElement) || !event.target.closest('.canvas-node-drag')) return
  event.stopImmediatePropagation()
  startNodeDrag(event, element)
}

function maxCanvasZIndex() {
  return Math.max(0, ...(activeCanvas.value?.elements || []).map((item) => Number(item.zIndex) || 0))
}

function bringElementToFront(element: CanvasElement) {
  const otherMax = Math.max(0, ...(activeCanvas.value?.elements || []).filter((item) => item.id !== element.id).map((item) => Number(item.zIndex) || 0))
  if ((Number(element.zIndex) || 0) <= otherMax) element.zIndex = otherMax + 1
}


function duplicateElement(element: CanvasElement) {
  if (!activeCanvas.value) return element
  const copy: CanvasElement = {
    ...element,
    id: createID(),
    badge: '',
    x: element.x + 28,
    y: element.y + 28,
    zIndex: maxCanvasZIndex() + 2000,
  }
  pushCanvasElement(copy, undefined, element)
  return copy
}

function startResize(event: PointerEvent, element: CanvasElement) {
  if (event.button !== 0) return
  event.preventDefault()
    ; (event.currentTarget as HTMLElement).setPointerCapture(event.pointerId)
  dragState.value = { type: 'resize', id: element.id, startX: event.clientX, startY: event.clientY, originWidth: element.width, originHeight: element.height }
}

function openConnectionTargetMenu(from: CanvasElement, event: { clientX: number; clientY: number }) {
  const point = screenToWorld(event.clientX, event.clientY)
  const targetKinds = connectableTargetKinds(from)
  if (!targetKinds.length) {
    showInvalidConnectionNotice(from)
    return
  }
  canvasContextMenu.value = {
    x: event.clientX,
    y: event.clientY,
    items: targetKinds.map((kind) => ({
      label: elementTitle({ id: '__label__', kind, x: 0, y: 0, width: 0, height: 0 } as CanvasElement),
      icon: contextIconForKind(kind),
      action: () => createConnectedTarget(from, kind, point),
    })),
  }
}

function openConnectionSourceMenu(to: CanvasElement, event: { clientX: number; clientY: number }) {
  const point = screenToWorld(event.clientX, event.clientY)
  const sourceKinds = connectableSourceKinds(to)
  if (!sourceKinds.length) {
    showInvalidSourceNotice(to)
    return
  }
  canvasContextMenu.value = {
    x: event.clientX,
    y: event.clientY,
    items: sourceKinds.map((kind) => ({
      label: elementTitle({ id: '__label__', kind, x: 0, y: 0, width: 0, height: 0 } as CanvasElement),
      icon: contextIconForKind(kind),
      action: () => createConnectedSource(to, kind, point),
    })),
  }
}

function contextIconForKind(kind: NodeKind) {
  if (kind === 'prompt' || kind === 'llm') return 'text'
  if (kind === 'image' || kind === 'image_media') return 'image'
  if (kind === 'video' || kind === 'video_media') return 'video'
  if (kind === 'audio' || kind === 'audio_media') return 'audio'
  if (kind === 'merge') return 'merge'
  if (kind === 'mask') return 'brush'
  return 'sparkles'
}

function createConnectedTarget(from: CanvasElement, kind: NodeKind, point: { x: number; y: number }) {
  if (!activeCanvas.value) return
  const target = createElementForConnectionTarget(kind, point)
  if (!target || !canConnect(from, target)) {
    if (target) showInvalidConnectionNotice(from, target)
    return
  }
  pushCanvasElement(target, point)
  activeCanvas.value.connections.push({ id: createID(), from: from.id, to: target.id })
}

function createConnectedSource(to: CanvasElement, kind: NodeKind, point: { x: number; y: number }) {
  if (!activeCanvas.value) return
  const source = createElementForConnectionTarget(kind, point)
  if (!source || !canConnect(source, to)) {
    if (source) showInvalidSourceNotice(to, source)
    return
  }
  pushCanvasElement(source, point)
  activeCanvas.value.connections.push({ id: createID(), from: source.id, to: to.id })
}

function createElementForConnectionTarget(kind: NodeKind, point: { x: number; y: number }): CanvasElement | undefined {
  if (kind === 'prompt') {
    const minSize = minNodeSize('prompt')
    return { id: createID(), kind: 'prompt', text: '', x: point.x, y: point.y - minSize.height / 2, width: minSize.width, height: minSize.height, zIndex: maxCanvasZIndex() + 1 }
  }
  if (kind === 'merge') {
    const minSize = minNodeSize('merge')
    return { id: createID(), kind: 'merge', text: '', x: point.x, y: point.y - minSize.height / 2, width: minSize.width, height: minSize.height, zIndex: maxCanvasZIndex() + 1 }
  }
  if (kind === 'llm' || kind === 'image' || kind === 'video' || kind === 'audio' || kind === 'mask') {
    const element = createProcessElement(kind, point)
    element.zIndex = maxCanvasZIndex() + 1
    return element
  }
  if (kind === 'image_media' || kind === 'video_media' || kind === 'audio_media') {
    const type = mediaTypeFromKind(kind) || 'image'
    const minSize = minNodeSize(kind)
    return {
      id: createID(),
      kind,
      media_type: type,
      media_url: '',
      media_thumbnail_url: '',
      media_filename: '',
      text: '',
      video_clip_start: 0,
      video_clip_end: 0,
      x: point.x - minSize.width / 2,
      y: point.y - minSize.height / 2,
      width: minSize.width,
      height: minSize.height,
      zIndex: maxCanvasZIndex() + 1,
    }
  }
  return undefined
}

function outputElementAt(clientX: number, clientY: number) {
  const hitRadius = 18
  const outputs = Array.from(document.querySelectorAll<HTMLElement>('.canvas-flow-handle.output'))
  for (const output of outputs) {
    const rect = output.getBoundingClientRect()
    const centerX = rect.left + rect.width / 2
    const centerY = rect.top + rect.height / 2
    const insideBox = clientX >= rect.left - hitRadius && clientX <= rect.right + hitRadius && clientY >= rect.top - hitRadius && clientY <= rect.bottom + hitRadius
    const nearCenter = Math.hypot(clientX - centerX, clientY - centerY) <= Math.max(hitRadius, rect.width)
    if (insideBox || nearCenter) {
      const id = output.dataset.nodeId || output.closest<HTMLElement>('.vue-flow__node')?.dataset.id
      const element = id ? elementByID(id) : undefined
      if (element) return element
    }
  }
  return undefined
}

function inputElementAt(clientX: number, clientY: number) {
  const hitRadius = 18
  const inputs = Array.from(document.querySelectorAll<HTMLElement>('.canvas-flow-handle.input'))
  for (const input of inputs) {
    const rect = input.getBoundingClientRect()
    const centerX = rect.left + rect.width / 2
    const centerY = rect.top + rect.height / 2
    const insideBox = clientX >= rect.left - hitRadius && clientX <= rect.right + hitRadius && clientY >= rect.top - hitRadius && clientY <= rect.bottom + hitRadius
    const nearCenter = Math.hypot(clientX - centerX, clientY - centerY) <= Math.max(hitRadius, rect.width)
    if (insideBox || nearCenter) {
      const id = input.dataset.nodeId || input.closest<HTMLElement>('.vue-flow__node')?.dataset.id
      const element = id ? elementByID(id) : undefined
      if (element) return element
    }
  }
  return undefined
}

function onPointerMove(event: PointerEvent) {
  const state = dragState.value
  if (!state || !activeCanvas.value) return
  if (state.type === 'pan') {
    const viewport = {
      x: state.originX + event.clientX - state.startX,
      y: state.originY + event.clientY - state.startY,
      zoom: camera.zoom,
    }
    pan.value = { x: viewport.x, y: viewport.y }
    camera.x = viewport.x
    camera.y = viewport.y
    flow.setViewport(viewport)
    return
  }
  const element = elementByID(state.id)
  if (!element) return
  if (state.type === 'node') {
    element.x = state.originX + (event.clientX - state.startX) / camera.zoom
    element.y = state.originY + (event.clientY - state.startY) / camera.zoom
  } else {
    const min = minNodeSize(element.kind)
    element.width = Math.max(min.width, state.originWidth + (event.clientX - state.startX) / camera.zoom)
    element.height = Math.max(min.height, state.originHeight + (event.clientY - state.startY) / camera.zoom)
  }
}

function stopDrag(event?: PointerEvent) {
  dragState.value = null
}

function onCanvasWheel(event: WheelEvent) {
  preventBrowserZoomWheel(event)
}

function preventBrowserZoomWheel(event: WheelEvent) {
  if (!event.ctrlKey && !event.metaKey) return
  event.preventDefault()
  if (handledCtrlWheelEvents.has(event)) return
  handledCtrlWheelEvents.add(event)
  zoomCanvasAtPoint(event)
}

function zoomCanvasAtPoint(event: WheelEvent) {
  const target = event.target as HTMLElement | null
  if (!target?.closest('.canvas-workspace')) return
  const factor = Math.exp(-event.deltaY * 0.0024)
  const nextZoom = clamp(zoom.value * factor, MIN_ZOOM, MAX_ZOOM)
  if (Math.abs(nextZoom - zoom.value) < 0.0001) return
  const worldX = (event.clientX - pan.value.x) / zoom.value
  const worldY = (event.clientY - pan.value.y) / zoom.value
  const nextPan = {
    x: event.clientX - worldX * nextZoom,
    y: event.clientY - worldY * nextZoom,
  }
  pan.value = nextPan
  zoom.value = nextZoom
  camera.x = nextPan.x
  camera.y = nextPan.y
  camera.zoom = nextZoom
  flow.setViewport({ x: nextPan.x, y: nextPan.y, zoom: nextZoom }, { duration: 0 })
}

function setZoom(value: number) {
  flow.zoomTo(clamp(value, MIN_ZOOM, MAX_ZOOM), { duration: 120 })
}

function resetView() {
  flow.setViewport({ x: 420, y: 220, zoom: 0.82 }, { duration: 160 })
}

function toggleMiniMap(event?: Event) {
  event?.preventDefault()
  event?.stopPropagation()
  showMiniMap.value = !showMiniMap.value
  if (event) blurControl(event)
}

function onMiniMapClick(event: { position: { x: number; y: number } }) {
  flow.setCenter(event.position.x, event.position.y, { zoom: camera.zoom, duration: 160 })
}

function onCanvasPointerDownCapture(event: PointerEvent) {
  closeMentionMenuFromPointer(event)
  if (event.button !== 0 || spacePanning.value) return
  if (event.target instanceof HTMLElement && event.target.closest('.canvas-topbar, .canvas-assets-fab, .canvas-minimap-toggle, .canvas-vueflow-minimap, .asset-sidebar, .context-menu')) return
}

function clamp(value: number, min: number, max: number) {
  return Math.min(max, Math.max(min, value))
}
</script>

<template>
  <section class="canvas-workspace" :class="{ 'space-panning': spacePanning, 'zen-mode': zenMode }" @click="canvasContextMenu = null" @contextmenu.prevent.stop="openCanvasContextMenu" @wheel.capture="onCanvasWheel" @pointerdown.capture="onCanvasPointerDownCapture" @pointermove="onPointerMove" @pointerup="stopDrag" @pointercancel="stopDrag">
    <div class="canvas-topbar glass-panel" :class="{ 'zen-collapsed': zenMode }" @pointerdown.stop>
      <div class="canvas-console-track">
        <div class="canvas-console-content" :aria-hidden="zenMode">
          <div class="canvas-switcher">
            <select v-model="activeCanvasID" aria-label="选择画布">
              <option v-for="canvas in canvases" :key="canvas.id" :value="canvas.id">{{ canvas.name }}</option>
            </select>
            <button type="button" title="新建画布" @click="createCanvas"><AppIcon name="add" /></button>
            <button type="button" title="重命名画布" @click="renameCanvas"><AppIcon name="pencil" /></button>
            <button type="button" title="删除画布" :disabled="canvases.length <= 1" @click="deleteCanvas"><AppIcon name="close" /></button>
          </div>
          <span class="canvas-tool-divider" aria-hidden="true"></span>
          <div class="canvas-switcher canvas-node-tools">
            <button type="button" title="添加文字提示词" @click="addPromptNode()"><AppIcon name="text" /></button>
            <button type="button" title="添加媒体节点" @click="addAssetNode()"><AppIcon name="gallery" /></button>
            <button type="button" title="添加汇合节点" @click="addMergeNode()"><AppIcon name="merge" /></button>
            <button type="button" title="添加蒙版节点" @click="addGenerateNode('mask')"><AppIcon name="brush" /></button>
            <button type="button" title="添加 AI 生成节点" @click="addAiNode()"><AppIcon name="sparkles" /></button>
            <button type="button" class="canvas-run-button" title="按连接顺序运行整张画布" :disabled="runningWorkflow" @click="runCanvasWorkflow"><AppIcon :name="runningWorkflow ? 'stop' : 'play'" /></button>
          </div>
          <span class="canvas-tool-divider" aria-hidden="true"></span>
          <div class="canvas-zoom-controls">
            <button type="button" title="撤回上一步 Ctrl+Z" :disabled="!canUndo" @click="undoCanvasChange"><AppIcon name="undo" /></button>
            <button type="button" title="全局自动整理" @click="autoArrangeCanvas"><AppIcon name="grid" /></button>
            <button type="button" title="缩小" @click="setZoom(zoom - 0.1)"><AppIcon name="zoomOut" /></button>
            <span class="canvas-zoom-label">{{ zoomLabel }}</span>
            <button type="button" title="放大" @click="setZoom(zoom + 0.1)"><AppIcon name="zoomIn" /></button>
            <button type="button" title="复位视图" @click="resetView"><AppIcon name="resetView" /></button>
          </div>
        </div>
      </div>
      <button type="button" class="canvas-zen-button" :title="zenMode ? '退出禅模式' : '禅模式'" @click="setZenMode(!zenMode); blurControl($event)"><AppIcon name="zen" /></button>
    </div>
    <button type="button" class="canvas-assets-fab" :class="{ active: showAssets }" title="显示/隐藏素材" @pointerdown.stop @click="showAssets = !showAssets; blurControl($event)">
      <AppIcon name="gallery" />
    </button>
    <button type="button" class="canvas-minimap-toggle glass-panel" :class="{ active: showMiniMap }" :title="showMiniMap && !zenMode ? '隐藏小地图' : '显示小地图'" @pointerdown.stop @click.stop="toggleMiniMap($event)"><AppIcon name="map" /></button>

    <VueFlow id="canvas-flow" class="canvas-flow" :nodes="flowNodes" :edges="flowEdges" :min-zoom="MIN_ZOOM" :max-zoom="MAX_ZOOM" :default-viewport="{ x: pan.x, y: pan.y, zoom }" :nodes-draggable="!spacePanning" :pan-on-drag="false" :elevate-nodes-on-select="false" :elevate-edges-on-select="true" :default-edge-options="flowDefaultEdgeOptions" :connection-line-options="flowConnectionLineOptions" :connection-mode="ConnectionMode.Strict" :is-valid-connection="isValidFlowConnection" :connect-on-click="true" :edges-updatable="true" :edges-focusable="true" :nodes-focusable="true" pan-activation-key-code="Space" :selection-key-code="true" :select-nodes-on-drag="true" :zoom-on-scroll="false" delete-key-code="Delete" @connect="onFlowConnect" @connect-start="onFlowConnectStart" @connect-end="onFlowConnectEnd" @connectStart="onFlowConnectStart" @connectEnd="onFlowConnectEnd" @edge-update="onFlowEdgeUpdate" @edgeUpdate="onFlowEdgeUpdate" @nodes-change="onFlowNodesChange" @edges-change="onFlowEdgesChange" @pane-click="onFlowPaneClick" @node-drag-start="onFlowNodeDragStart" @node-drag-stop="onFlowNodeDragStop" @edge-context-menu="openEdgeContextMenu" @selection-context-menu="openSelectionContextMenu" @viewport-change="onFlowViewportChange">
      <MiniMap
        v-if="showMiniMap"
        class="canvas-vueflow-minimap glass-panel"
        position="bottom-left"
        :width="188"
        :height="118"
        :node-color="miniMapNodeColor"
        node-stroke-color="rgba(15, 23, 42, .55)"
        :node-stroke-width="1"
        :node-border-radius="4"
        mask-color="rgba(255, 255, 255, .07)"
        mask-stroke-color="rgba(255, 255, 255, .82)"
        :mask-stroke-width="1"
        :mask-border-radius="6"
        :aria-label="null"
        pannable
        zoomable
      />
      <template #node-canvas="{ data }">
      <article v-for="element in [data.element]" :key="element.id" class="canvas-node"
        :class="[`canvas-node-${element.kind}`, { running: isNodeRunning(element), frozen: element.frozen }]"
        @pointerdown="onCanvasNodePointerDown($event, element)" @contextmenu.prevent.stop="openNodeContextMenu(element, $event)">
        <Handle v-if="acceptsInput(element.kind)" id="input" type="target" :position="Position.Left" class="canvas-flow-handle input" />
        <Handle v-if="hasOutput(element.kind)" id="output" type="source" :position="Position.Right" class="canvas-flow-handle output" />
        <div class="canvas-node-drag" title="拖动节点">
          <span><small v-if="nodeBadge(element)">{{ nodeBadge(element) }}</small>{{ elementTitle(element) }}</span>
          <div class="canvas-node-actions">
            <button v-if="isRunnableKind(element.kind)" type="button" class="canvas-node-freeze"
              :class="{ active: element.frozen, warning: element.frozen && !hasFrozenResult(element) }"
              :title="element.frozen ? '取消固化，后续流程会重新运行该节点' : '固化当前结果，后续流程跳过该节点'"
              :disabled="isNodeBusy(element) || isLineBusy(element)" @pointerdown.stop
              @click.stop="toggleFrozen(element)"><AppIcon name="archive" :size="13" />{{ element.frozen ? '已固化' : '固化' }}</button>
            <button v-if="isRunnableKind(element.kind)" type="button" class="canvas-node-run" title="运行到此节点并停止"
              :disabled="element.frozen || isNodeBusy(element) || isLineBusy(element)" @pointerdown.stop
              @click.stop="runToNode(element)"><AppIcon name="play" :size="13" />{{ nodeHeaderRunLabel(element) }}</button>
            <button type="button" class="canvas-node-remove" title="移除" @pointerdown.stop
              @click.stop="removeElement(element.id)"><AppIcon name="close" :size="13" /></button>
          </div>
        </div>

        <div v-if="element.kind === 'prompt'" class="canvas-prompt-node">
          <div class="canvas-rich-editor" contenteditable="true" :data-node-id="element.id"
            data-placeholder="输入这里要使用的文字提示词，可输入 @ 引用连接节点素材..." @pointerdown.stop
            @mousedown.stop @click.stop @wheel.stop @input="onPromptTextInput($event, element)"
            @keyup="onPromptTextInput($event, element)" @keydown="onRichEditorKeydown($event, element)"
            @blur="syncEditableText($event, element)" v-html="renderEditableText(element.text)"></div>
        </div>

        <div v-else-if="element.kind === 'asset'" class="canvas-picker-node canvas-asset-node" @pointerdown.stop>
          <button type="button" @click="chooseAssetNodeKind(element, 'image_media')"><AppIcon name="image" />图片媒体</button>
          <button type="button" @click="chooseAssetNodeKind(element, 'video_media')"><AppIcon name="video" />视频媒体</button>
          <button type="button" @click="chooseAssetNodeKind(element, 'audio_media')"><AppIcon name="audio" />音频媒体</button>
        </div>

        <div v-else-if="element.kind === 'ai'" class="canvas-picker-node canvas-ai-node" @pointerdown.stop>
          <button type="button" @click="chooseAiNodeKind(element, 'llm')"><AppIcon name="text" />生文字</button>
          <button type="button" @click="chooseAiNodeKind(element, 'video')"><AppIcon name="video" />生视频</button>
          <button type="button" @click="chooseAiNodeKind(element, 'image')"><AppIcon name="image" />生图片</button>
          <button type="button" @click="chooseAiNodeKind(element, 'audio')"><AppIcon name="audio" />生音频</button>
        </div>

        <div v-else-if="element.kind === 'merge'" class="canvas-merge-node">
          <div class="canvas-merge-symbol">
            <span>汇合</span>
            <small>输入 {{ connectedInputs(element).length }}</small>
          </div>
        </div>

        <div v-else-if="element.kind === 'mask'" class="canvas-generate-node canvas-mask-node"
          @pointerenter="enterMaskNode(element)" @pointerleave="leaveMaskNode(element)">
          <div v-if="maskSourceImage(element)" class="canvas-mask-editor"
            :class="[`tool-${element.mask_tool || 'brush'}`, { panning: imagePanState?.elementID === element.id }]"
            @pointerdown.stop @wheel="zoomNodeImage($event, element)" @pointerenter="activeMaskElementID = element.id"
            @pointerleave="hideMaskCursor(element)">
            <img :src="originalImageURL(maskSourceImage(element))" alt="蒙版上游图片"
              :style="imageZoomStyle(element)" draggable="false" @load="prepareMaskCanvas($event, element)" />
            <canvas :style="imageZoomStyle(element)" @pointerdown.stop="startMaskPointer($event, element)"
              @pointermove.stop="moveMaskPointer($event, element)" @pointerup.stop="stopMaskPointer($event, element)"
              @pointercancel.stop="stopMaskPointer($event, element)"></canvas>
            <span class="canvas-mask-cursor" :class="`tool-${element.mask_tool || 'brush'}`"
              :style="maskCursorStyle(element)"></span>
          </div>
          <div v-else class="canvas-mask-empty">连接一个媒体素材到本节点</div>
          <div class="canvas-mask-tools" @pointerdown.stop>
            <div class="canvas-mask-tool-toggle">
              <button type="button" :class="{ active: element.mask_tool === 'pan' }" title="拖动视图 Q"
                @click="setMaskTool(element, 'pan')"><AppIcon name="compass" :size="14" />拖动</button>
              <button type="button" :class="{ active: element.mask_tool !== 'pan' && element.mask_tool !== 'eraser' }" title="涂抹 W"
                @click="setMaskTool(element, 'brush')"><AppIcon name="brush" :size="14" />涂抹</button>
              <button type="button" :class="{ active: element.mask_tool === 'eraser' }" title="擦除 E"
                @click="setMaskTool(element, 'eraser')"><AppIcon name="eraser" :size="14" />擦除</button>
            </div>
            <label class="canvas-mask-size nodrag" @pointerdown.stop @mousedown.stop @click.stop>
              <span>画笔</span>
              <input v-model.number="element.mask_brush_size" class="nodrag" type="range" min="8" max="96"
                @pointerdown="stopMaskSizeEvent" @pointermove="stopMaskSizeEvent" @pointerup="stopMaskSizeEvent"
                @mousedown="stopMaskSizeEvent" @mousemove="stopMaskSizeEvent" @mouseup="stopMaskSizeEvent"
                @click="stopMaskSizeEvent" />
            </label>
            <button type="button" class="canvas-mask-clear" @click="clearMask(element, $event)"><AppIcon name="trash" :size="14" />清空</button>
          </div>
        </div>

        <div v-else-if="isProcessKind(element.kind)" class="canvas-generate-node">
          <div v-if="element.kind === 'llm'" class="canvas-rich-editor canvas-llm-editor" contenteditable="true"
            :data-node-id="element.id" data-placeholder="运行后展示、编辑 LLM 输出，可输入 @ 引用连接节点素材..." @pointerdown.stop
            @wheel.stop @input="onPromptTextInput($event, element)" @keyup="onPromptTextInput($event, element)"
            @blur="syncEditableText($event, element)" @keydown="onRichEditorKeydown($event, element)"
            v-html="renderEditableText(element.text)"></div>
          <div v-else class="canvas-result-preview">
            <template v-if="generatedTask(element)?.status === 'succeeded'">
              <CanvasVideoPlayer v-if="element.kind === 'video'"
                :src="generatedTask(element)?.result_videos?.[0]?.url" />
              <div v-else-if="element.kind === 'audio'" class="canvas-audio-media">
                <span>音频</span>
                <audio :src="firstAudioAsset(generatedTask(element))?.url" controls preload="metadata"></audio>
              </div>
              <div v-else class="canvas-zoomable-image" @wheel="zoomNodeImage($event, element)"
                @pointerdown="startNodeImagePan($event, element)" @pointermove="moveNodeImagePan($event, element)"
                @pointerup="stopNodeImagePan" @pointercancel="stopNodeImagePan">
                <div v-if="canvasImageStatus(element, originalImageURL(generatedTask(element)?.result_images?.[0])) !== 'loaded'"
                  class="canvas-image-placeholder"
                  :class="{ error: canvasImageStatus(element, originalImageURL(generatedTask(element)?.result_images?.[0])) === 'error' }">
                  {{ canvasImageStatus(element, originalImageURL(generatedTask(element)?.result_images?.[0])) === 'error' ? '图片加载失败' : '图片加载中' }}
                </div>
                <img :src="originalImageURL(generatedTask(element)?.result_images?.[0])" alt="生成结果"
                  :style="imageZoomStyle(element)" draggable="false"
                  @load="markCanvasImageLoaded(element, originalImageURL(generatedTask(element)?.result_images?.[0]))"
                  @error="markCanvasImageError(element, originalImageURL(generatedTask(element)?.result_images?.[0]))" />
              </div>
            </template>
            <span v-else-if="generatedTask(element)?.status === 'failed'">{{ generatedTask(element)?.error_message ||
              '生成失败'
            }}</span>
            <span v-else-if="generatedTask(element)">生成中...</span>
            <span v-else>{{ element.kind === 'audio' ? '音频生成结果将在这里显示' : '连接提示词和媒体后运行' }}</span>
          </div>
          <div v-if="element.kind === 'video'" class="canvas-video-trim" @pointerdown.stop>
            <span>截取</span>
            <div class="canvas-trim-range">
              <input :value="element.video_clip_start" class="clip-start" type="range" min="0"
                :max="videoClipMax(element)" step="0.1" @input="updateVideoClip(element, 'video_clip_start', $event)" />
              <input :value="element.video_clip_end" class="clip-end" type="range" min="0" :max="videoClipMax(element)"
                step="0.1" @input="updateVideoClip(element, 'video_clip_end', $event)" />
            </div>
            <label>从<input :value="element.video_clip_start" type="number" min="0" :max="videoClipMax(element)"
                step="0.1" @input="updateVideoClip(element, 'video_clip_start', $event)" /></label>
            <label>到<input :value="element.video_clip_end" type="number" min="0" :max="videoClipMax(element)" step="0.1"
                @input="updateVideoClip(element, 'video_clip_end', $event)" /></label>
          </div>
          <div class="canvas-node-fields"
            :class="{ 'video-fields': element.kind === 'video', 'image-fields': element.kind === 'image', 'llm-fields': element.kind === 'llm' || element.kind === 'audio' }"
            @pointerdown.stop>
            <span class="canvas-node-field-label">{{ element.kind === 'video' ? '视频' : element.kind === 'image' ? '图片' : element.kind === 'audio' ? '音频' :
              '文字'
            }}</span>
            <label v-if="element.kind !== 'llm' && element.kind !== 'audio'" class="canvas-param-field"><span>模型</span><select v-model="element.model"
                @change="element.kind === 'video' ? updateCanvasVideoModel(element) : updateCanvasImageModel(element)">
                <option v-for="model in props.models" :key="model" :value="model">{{ model }}</option>
              </select></label>
            <label v-if="element.kind === 'image' && !isNanoBananaElement(element) && !isSeedreamElement(element)" class="canvas-param-field"><span>分辨率</span><select :value="gptImageSizeBase(element)" @change="updateGptImageSizeBase(element, $event)">
                <option v-for="option in gptImageSizeBaseOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
              </select></label>
            <label v-if="element.kind === 'image' && !isNanoBananaElement(element) && !isSeedreamElement(element)" class="canvas-param-field"><span>比例</span><select :value="gptImageRatio(element)" :disabled="gptImageSizeBase(element) === 'auto'" @change="updateGptImageRatio(element, $event)">
                <option v-for="ratio in ratioOptions" :key="ratio" :value="ratio">{{ ratio }}</option>
              </select></label>
            <label v-if="isNanoBananaElement(element)" class="canvas-param-field"><span>分辨率</span><select :value="nanoImageSize(element)" @change="updateNanoImageSize(element, $event)">
                <option v-for="option in nanoBananaSizeBaseOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
              </select></label>
            <label v-if="isNanoBananaElement(element)" class="canvas-param-field"><span>比例</span><select :value="nanoAspectRatio(element)" @change="updateNanoAspectRatio(element, $event)">
                <option v-for="ratio in nanoBananaRatios" :key="ratio" :value="ratio">{{ ratio }}</option>
              </select></label>
            <label v-if="isSeedreamElement(element)" class="canvas-param-field"><span>分辨率</span><select :value="seedreamImageSize(element)" @change="updateSeedreamImageSize(element, $event)">
                <option v-for="option in seedreamSizeBaseOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
              </select></label>
            <label v-if="isSeedreamElement(element)" class="canvas-param-field"><span>比例</span><select :value="seedreamAspectRatio(element)" @change="updateSeedreamAspectRatio(element, $event)">
                <option v-for="ratio in seedreamRatios" :key="ratio" :value="ratio">{{ ratio }}</option>
              </select></label>
            <label v-if="element.kind === 'image' && !isNanoBananaElement(element) && !isSeedreamElement(element)" class="canvas-param-field"><span>质量</span><select v-model="element.quality">
                <option>auto</option>
                <option>high</option>
                <option>medium</option>
                <option>low</option>
              </select></label>
            <label v-if="element.kind === 'image' && !isNanoBananaElement(element)" class="canvas-param-field"><span>格式</span><select :value="element.output_format || props.defaultForm.output_format" @change="updateCanvasOutputFormat(element, $event)">
                <option>png</option>
                <option>jpeg</option>
                <option v-if="!isSeedreamElement(element)">webp</option>
              </select></label>
            <label v-if="element.kind === 'image' && !isNanoBananaElement(element) && !isSeedreamElement(element) && supportsOutputCompression(element)" class="canvas-param-field"><span>压缩</span><input
                v-model.number="element.output_compression" type="number" min="0" max="100" /></label>
            <label v-if="element.kind === 'image' && !isNanoBananaElement(element) && !isSeedreamElement(element)" class="canvas-param-field"><span>背景</span><select v-model="element.background">
                <option>auto</option>
                <option v-if="supportsTransparentBackground(element)">transparent</option>
                <option>opaque</option>
              </select></label>
            <label v-if="element.kind === 'image' && !isNanoBananaElement(element) && !isSeedreamElement(element)" class="canvas-param-field"><span>审核</span><select v-model="element.moderation">
                <option>low</option>
                <option>auto</option>
              </select></label>
            <label v-if="element.kind === 'image' && !isNanoBananaElement(element) && !isSeedreamElement(element)" class="canvas-param-field"><span>保真</span><select v-model="element.input_fidelity">
                <option>high</option>
                <option>low</option>
              </select></label>
            <label v-if="element.kind === 'video'" class="canvas-param-field canvas-ratio-param"><span>比例</span><RatioPicker
                :model-value="element.video_ratio || canvasVideoCapability(element).defaultRatio"
                :ratios="canvasVideoRatios(element)" compact @update:model-value="element.video_ratio = $event" /></label>
            <label v-if="element.kind === 'video'" class="canvas-param-field"><span>分辨率</span><select v-model="element.video_resolution">
                <option v-for="resolution in canvasVideoResolutions(element)" :key="resolution" :value="resolution">{{
                  resolution.toUpperCase() }}</option>
              </select></label>
            <label v-if="element.kind === 'video'" class="canvas-param-field"><span>时长</span><input
                v-model.number="element.video_duration" type="number" :min="canvasVideoCapability(element).duration.min"
                :max="canvasVideoCapability(element).duration.max" /></label>
            <label v-if="element.kind === 'video'" class="canvas-check-field"><input v-model="element.generate_audio"
                type="checkbox" />音频</label>
            <label v-if="element.kind === 'llm'" class="canvas-param-field"><span>模型</span><select v-model="element.model">
                <option v-for="model in props.models" :key="model" :value="model">{{ model }}</option>
              </select></label>
            <label v-if="element.kind === 'llm'" class="canvas-param-field"><span>推理</span><select v-model="element.reasoning_effort">
                <option value="none">不检查</option>
                <option value="low">低</option>
                <option value="medium">中</option>
                <option value="high">高</option>
              </select></label>
            <label v-if="element.kind === 'audio'" class="canvas-param-field"><span>模型</span><select v-model="element.model">
                <option value="">请选择音频模型</option>
              </select></label>
          </div>
          <div class="canvas-node-summary">
            <span>提示词 {{ connectedInputs(element).filter((item) => isPromptLike(item.kind)).length }}</span>
            <span>媒体 {{ connectedInputs(element).filter((item) => isMediaKind(item.kind)).length }}</span>
            <span v-if="nodeProgressLabel(element)"
              :class="{ failed: nodeRuntime(element)?.status === 'failed' || generatedTask(element)?.status === 'failed' || (element.frozen && !hasFrozenResult(element)) }">{{
                nodeProgressLabel(element) }}</span>
          </div>
        </div>

        <template v-else-if="isMediaKind(element.kind)">
          <div v-if="!element.media_url" class="canvas-media-empty" @pointerdown.stop>
            <span>{{ mediaEmptyLabel(element) }}</span>
            <form v-if="mediaUrlEditor?.elementID === element.id" class="canvas-media-url-form"
              @submit.prevent="commitMediaURLInput(element)">
              <input :data-media-url-input="element.id" :value="mediaUrlEditor?.value || ''" type="url"
                placeholder="粘贴媒体 URL..."
                @input="updateMediaURLInput(($event.target as HTMLInputElement).value)"
                @keydown.esc.prevent="cancelMediaURLInput" />
              <button type="submit" title="确定"><AppIcon name="check" :size="13" /></button>
              <button type="button" title="取消" @click="cancelMediaURLInput"><AppIcon name="close" :size="13" /></button>
            </form>
            <div v-else class="canvas-media-actions">
              <label :class="{ uploading: uploadingMediaID === element.id }">
                {{ uploadingMediaID === element.id ? '上传中...' : '上传' }}
                <input type="file" :accept="mediaAcceptForKind(element.kind)" :disabled="Boolean(uploadingMediaID)"
                  @change="uploadMediaIntoNode($event, element)" />
              </label>
              <button type="button" @click="importMediaURLIntoNode(element)">URL</button>
            </div>
          </div>
          <template v-else>
            <template v-if="element.kind === 'video_media'">
              <CanvasVideoPlayer :src="element.media_url" />
              <div class="canvas-video-trim media-trim" @pointerdown.stop>
                <span>截取</span>
                <div class="canvas-trim-range">
                  <input :value="element.video_clip_start" class="clip-start" type="range" min="0"
                    :max="videoClipMax(element)" step="0.1"
                    @input="updateVideoClip(element, 'video_clip_start', $event)" />
                  <input :value="element.video_clip_end" class="clip-end" type="range" min="0"
                    :max="videoClipMax(element)" step="0.1"
                    @input="updateVideoClip(element, 'video_clip_end', $event)" />
                </div>
                <label>从<input :value="element.video_clip_start" type="number" min="0" :max="videoClipMax(element)"
                    step="0.1" @input="updateVideoClip(element, 'video_clip_start', $event)" /></label>
                <label>到<input :value="element.video_clip_end" type="number" min="0" :max="videoClipMax(element)"
                    step="0.1" @input="updateVideoClip(element, 'video_clip_end', $event)" /></label>
              </div>
            </template>
            <div v-else-if="element.kind === 'audio_media'" class="canvas-audio-media">
              <span>音频</span>
              <audio :src="element.media_url" controls preload="metadata"></audio>
            </div>
            <div v-else class="canvas-zoomable-image" @wheel="zoomNodeImage($event, element)"
              @pointerdown="startNodeImagePan($event, element)" @pointermove="moveNodeImagePan($event, element)"
              @pointerup="stopNodeImagePan" @pointercancel="stopNodeImagePan">
              <div v-if="canvasImageStatus(element, element.media_url) !== 'loaded'" class="canvas-image-placeholder"
                :class="{ error: canvasImageStatus(element, element.media_url) === 'error' }">
                {{ canvasImageStatus(element, element.media_url) === 'error' ? '图片加载失败' : '图片加载中' }}
              </div>
              <img :src="element.media_url" alt="画布素材" decoding="async" :style="imageZoomStyle(element)" draggable="false"
                @load="markCanvasImageLoaded(element, element.media_url)" @error="markCanvasImageError(element, element.media_url)" />
            </div>
            <form v-if="mediaUrlEditor?.elementID === element.id"
              class="canvas-node-detail canvas-media-url-form canvas-media-url-form-floating" @pointerdown.stop
              @submit.prevent="commitMediaURLInput(element)">
              <input :data-media-url-input="element.id" :value="mediaUrlEditor?.value || ''" type="url"
                placeholder="粘贴媒体 URL..."
                @input="updateMediaURLInput(($event.target as HTMLInputElement).value)"
                @keydown.esc.prevent="cancelMediaURLInput" />
              <button type="submit" title="确定"><AppIcon name="check" :size="13" /></button>
              <button type="button" title="取消" @click="cancelMediaURLInput"><AppIcon name="close" :size="13" /></button>
            </form>
            <div v-else class="canvas-node-detail canvas-media-replace-group" @pointerdown.stop>
              <label class="canvas-media-replace">
                上传
                <input type="file" :accept="mediaAcceptForKind(element.kind)" :disabled="Boolean(uploadingMediaID)"
                  @change="uploadMediaIntoNode($event, element)" />
              </label>
              <button type="button" @click="importMediaURLIntoNode(element)">URL</button>
            </div>
          </template>
        </template>

        <template v-else-if="taskForElement(element)">
          <CanvasVideoPlayer v-if="isVideoTask(taskForElement(element)!)"
            :src="taskForElement(element)!.result_videos?.[0]?.url" />
          <div v-else-if="firstAudioAsset(taskForElement(element)!)" class="canvas-audio-media">
            <span>音频</span>
            <audio :src="firstAudioAsset(taskForElement(element)!)?.url" controls preload="metadata"></audio>
          </div>
          <div v-else class="canvas-zoomable-image" @wheel="zoomNodeImage($event, element)"
            @pointerdown="startNodeImagePan($event, element)" @pointermove="moveNodeImagePan($event, element)"
            @pointerup="stopNodeImagePan" @pointercancel="stopNodeImagePan">
            <div v-if="canvasImageStatus(element, originalImageURL(taskForElement(element)!.result_images?.[0])) !== 'loaded'"
              class="canvas-image-placeholder"
              :class="{ error: canvasImageStatus(element, originalImageURL(taskForElement(element)!.result_images?.[0])) === 'error' }">
              {{ canvasImageStatus(element, originalImageURL(taskForElement(element)!.result_images?.[0])) === 'error' ? '图片加载失败' : '图片加载中' }}
            </div>
            <img :src="originalImageURL(taskForElement(element)!.result_images?.[0])" alt="生成素材"
              :style="imageZoomStyle(element)" draggable="false" @dblclick="emit('selectTask', taskForElement(element)!)"
              @load="markCanvasImageLoaded(element, originalImageURL(taskForElement(element)!.result_images?.[0]))"
              @error="markCanvasImageError(element, originalImageURL(taskForElement(element)!.result_images?.[0]))" />
          </div>
          <div v-if="connectedInputs(element).length" class="canvas-node-inputs">
            <span v-for="source in connectedInputs(element)" :key="source.id">{{ elementTitle(source) }}</span>
          </div>
          <button type="button" class="canvas-node-detail"
            @click="emit('selectTask', taskForElement(element)!)">详情</button>
        </template>
        <div v-if="mentionMenu?.elementID === element.id && mentionCandidates(element).length"
          class="canvas-mention-menu" @pointerdown.stop>
          <button v-for="(item, index) in mentionCandidates(element)" :key="item.element.id" type="button"
            :class="{ active: mentionMenu?.activeIndex === index }" @pointerenter="setActiveMentionIndex(element, index)"
            @mousedown.prevent="insertMention(element, item.label)">
            <strong>{{ item.label }} {{ mentionName(item.element) }}</strong>
            <small>{{ item.detail }}</small>
          </button>
        </div>
        <span class="canvas-resize-handle" title="调整大小" @pointerdown.stop="startResize($event, element)"></span>
      </article>
      </template>
    </VueFlow>

    <div v-if="canvasContextMenu" class="context-menu canvas-context-menu" :style="{ left: `${canvasContextMenu.x}px`, top: `${canvasContextMenu.y}px` }" @click.stop @contextmenu.prevent>
      <button v-for="item in canvasContextMenu.items" :key="item.label" type="button" :class="{ danger: item.danger }" :disabled="item.disabled" @click="runCanvasContextAction(item)">
        <AppIcon v-if="item.icon" :name="item.icon" :size="14" />
        <span>{{ item.label }}</span>
      </button>
    </div>

    <div v-if="canvasNotice" class="canvas-notice">{{ canvasNotice }}</div>

    <Transition :name="assetsClosingForZen ? 'canvas-assets-zen-close' : 'canvas-assets-slide'">
      <aside v-if="showAssets" class="asset-sidebar glass-panel" :class="{ 'closing-for-zen': assetsClosingForZen }" @pointerdown.stop>
        <div class="canvas-sidebar-head">
          <strong>素材</strong>
          <small>{{ visibleAssetTasks.length }}/{{ assetTotal || usableTasks.length }}</small>
        </div>
        <label class="asset-search">
          <span>刷新素材</span>
          <input v-model="assetSearch" type="search" placeholder="提示词、模型、图片、视频/音频" />
        </label>
        <button v-for="task in visibleAssetTasks" :key="task.id" type="button" class="asset-row"
          :title="assetPromptTitle(task)" @click="addTask(task)">
          <video v-if="isVideoTask(task)" :src="task.result_videos?.[0]?.url" muted playsinline preload="metadata" />
          <span v-else-if="firstAudioAsset(task)" class="asset-prompt-icon">音频</span>
          <img v-else :src="originalImageURL(task.result_images?.[0])" alt="素材" />
          <span>
            <strong>{{ assetLabel(task) }}</strong>
            <small>{{ task.prompt || task.model }}</small>
          </span>
        </button>
        <button v-if="assetHasMore" type="button" class="asset-load-more" :disabled="assetLoading"
          @click="loadMoreAssets">
          {{ assetLoading ? '加载中...' : '加载更多' }}
        </button>
        <div v-if="assetError" class="asset-error">{{ assetError }}</div>
        <div v-if="assetLoading && !visibleAssetTasks.length" class="asset-empty">加载中...</div>
        <div v-else-if="!visibleAssetTasks.length" class="asset-empty">没有匹配素材</div>
      </aside>
    </Transition>
  </section>
</template>


