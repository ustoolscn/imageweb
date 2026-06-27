<script setup lang="ts">
import { computed, nextTick, onActivated, onMounted, onUnmounted, ref, watch } from 'vue'
import { ConnectionLineType, ConnectionMode, Handle, Position, VueFlow, useVueFlow, type Connection, type Edge, type EdgeChange, type EdgeMouseEvent, type EdgeUpdateEvent, type Node, type NodeChange, type NodeDragEvent, type ViewportTransform } from '@vue-flow/core'
import { MiniMap } from '@vue-flow/minimap'
import '@vue-flow/minimap/dist/style.css'
import { fetchCanvases, fetchVideoFrames, listTasks, patchCanvasesCloud, saveCanvasesCloud, uploadImage } from '../api'
import type { CanvasPatchPayload } from '../api'
import type { MediaAsset, Task, UploadedImage } from '../types'
import type { CanvasLLMPayload, CanvasRunPayload, ImageForm } from '../uiTypes'
import { downloadFile } from '../lib/download'
import { normalizeVideoSettings, supportsVideoDraft, videoModelCapability, videoRatioLabel, videoRatioOptions, videoResolutionOptions } from '../lib/videoModels'
import { nanoBananaRatios, nanoBananaSizeBaseOptions, nanoBananaSizeValue, parseNanoBananaSize, parseSeedreamSize, ratioOptions, seedreamRatios, seedreamSizeBaseOptions, seedreamSizeValue, sizeBaseOptions, sizeFromRatio } from '../lib/sizes'
import { displayImageURL, isVideoTask } from '../lib/view'
import AppIcon from './AppIcon.vue'
import CanvasVideoPlayer from './CanvasVideoPlayer.vue'
import InlineSelect from './InlineSelect.vue'
import ViewControl3D from './ViewControl3D.vue'

type MediaNodeKind = 'image_media' | 'video_media' | 'audio_media'
type GenerateNodeKind = 'llm' | 'image' | 'video' | 'audio'
type NodeKind = MediaNodeKind | 'asset' | 'ai' | 'prompt' | 'merge' | 'view_control' | GenerateNodeKind | 'mask' | 'tail_frame'

type CanvasElement = {
  id: string
  kind: NodeKind
  badge?: string
  task_id?: string
  task_snapshot?: Task
  generated_params?: string[]
  media_type?: 'image' | 'video' | 'audio'
  media_url?: string
  media_thumbnail_url?: string
  media_first_frame_url?: string
  media_last_frame_url?: string
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
  video_draft?: boolean
  video_frame_role?: UploadedImage['video_frame_role']
  video_first_frame_source_id?: string
  video_last_frame_source_id?: string
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
  view_azimuth?: number
  view_elevation?: number
  view_roll?: number
  view_distance?: number
  view_scene_yaw?: number
  view_scene_pitch?: number
  view_scene_zoom?: number
  x: number
  y: number
  width: number
  height: number
}

type CanvasConnection = { id: string; from: string; to: string }
type BoardCanvas = { id: string; name: string; elements: CanvasElement[]; connections: CanvasConnection[] }
type CanvasLocalMeta = { updatedAt?: string; cloudUpdatedAt?: string; activeCanvasID?: string; workspaceKey?: string }
type CanvasStorageKeys = { canvas: string; meta: string; workspaceKey: string }
type CanvasSaveState = 'loading' | 'local' | 'pending' | 'saving' | 'saved' | 'error'
type CanvasDrawer = '' | 'board' | 'nodes' | 'view' | 'save'
type CanvasCloudDelta = { full: boolean; canvases: BoardCanvas[]; patches: CanvasPatchPayload[]; deleted: string[] }
type CanvasContextMenuItem = { label: string; icon?: string; action: () => void; disabled?: boolean; danger?: boolean }
type CanvasContextMenuState = { x: number; y: number; items: CanvasContextMenuItem[] }
type NodeValueType = 'text' | 'image' | 'video' | 'audio' | 'merge'
type DragState =
  | { type: 'pan'; startX: number; startY: number; originX: number; originY: number }
  | { type: 'node'; id: string; startX: number; startY: number; originX: number; originY: number }
  | { type: 'resize'; id: string; startX: number; startY: number; originWidth: number; originHeight: number }
  | null
type ViewControlDragState =
  | { type: 'scene'; elementID: string; startX: number; startY: number; originYaw: number; originPitch: number }
  | { type: 'camera'; elementID: string; target: HTMLElement }
  | null

const props = defineProps<{
  apikey: string
  baseurl: string
  tasks: Task[]
  defaultForm: ImageForm
  models: string[]
  submitting: boolean
  sharedCanvasIds?: string[]
  canvasTaskSnapshots?: Record<string, Task>
  canvasImport?: { token: number; canvas: unknown } | null
  readOnly?: boolean
  externalCanvasState?: { token: number; canvases: unknown; updated_at: string } | null
  runNodeAction?: (payload: CanvasRunPayload, applyTask: (task: Task) => void) => Promise<unknown> | void
  runLlmAction?: (payload: CanvasLLMPayload, applyResult: (text: string) => void) => Promise<unknown> | void
}>()

const emit = defineEmits<{
  selectTask: [task: Task]
  runNode: [payload: CanvasRunPayload, applyTask: (task: Task) => void]
  runLlm: [payload: CanvasLLMPayload, applyResult: (text: string) => void]
  shareCanvas: [canvas: BoardCanvas]
  unshareCanvas: [canvasID: string]
  taskRefsChange: [tasks: Task[], ids: string[]]
  zenModeChange: [enabled: boolean]
  closeContextMenu: []
  ready: []
}>()

const STORAGE_KEY = 'image_web_canvases'
const STORAGE_META_KEY = 'image_web_canvases_meta'
const STORAGE_SCOPED_PREFIX = `${STORAGE_KEY}:`
const STORAGE_META_SCOPED_PREFIX = `${STORAGE_META_KEY}:`
const IMAGE_MODEL_OPTIONS = ['gpt-image-2', 'nano-banana-2', 'doubao-seedream-5.0-lite']
const VIDEO_MODEL_OPTIONS = ['doubao-seedance-2.0', 'doubao-seedance-1.5-pro']
const ASSET_PAGE_SIZE = 30
const MIN_ZOOM = 0.2
const MAX_ZOOM = 3
const CLOUD_SAVE_DEBOUNCE_MS = 5000
const CANVAS_ROW_PATCH_MAX_BYTES = 256 * 1024
const initialCanvasState = readLocalCanvasState()
const canvases = ref<BoardCanvas[]>(initialCanvasState.canvases)
const activeCanvasID = ref(validActiveCanvasID(initialCanvasState.canvases, initialCanvasState.meta.activeCanvasID))
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
const assetQuery = ref('')
const assetTasks = ref<Task[]>([])
const assetPages = ref<Task[][]>([])
const assetPageIndex = ref(0)
const assetLoaded = ref(false)
const assetLoading = ref(false)
const assetHasMore = ref(false)
const assetTotal = ref(0)
const assetNextBeforeCreatedAt = ref('')
const assetNextBeforeID = ref('')
const assetError = ref('')
const canvasContextMenu = ref<CanvasContextMenuState | null>(null)
const canvasNotice = ref('')
const canvasSaveState = ref<CanvasSaveState>(initialCanvasState.meta.updatedAt ? 'local' : 'saved')
const canvasSaveDetail = ref(initialCanvasState.meta.updatedAt ? `本地 ${formatSaveTime(initialCanvasState.meta.updatedAt)}` : '本地空画布')
const canvasLastSavedAt = ref(initialCanvasState.meta.updatedAt || '')
const canvasLastCloudSavedAt = ref(initialCanvasState.meta.cloudUpdatedAt || '')
const activeCanvasDrawer = ref<CanvasDrawer>('')
const pendingFlowConnection = ref<{ nodeId: string; handleType: 'source' | 'target' } | null>(null)
const suppressFlowConnectEnd = ref(false)
const renameDialog = ref<{ canvasID: string; value: string } | null>(null)
const renameInput = ref<HTMLInputElement | null>(null)
const deleteDialog = ref<{ canvasID: string; name: string } | null>(null)
const mediaUrlEditor = ref<{ elementID: string; value: string } | null>(null)
const mentionMenu = ref<{ elementID: string; query: string; start: number; end: number; activeIndex: number } | null>(null)
const suppressCanvasMentionAfterDelete = ref(false)
const composingEditorIDs = new Set<string>()
const maskPaintState = ref<{ elementID: string; point: { x: number; y: number } } | null>(null)
const activeMaskPointer = ref<{ elementID: string; pointerId: number; canvas: HTMLCanvasElement; element: CanvasElement } | null>(null)
const viewControlDrag = ref<ViewControlDragState>(null)
const spacePanning = ref(false)
const selectedNodeIDs = ref<Set<string>>(new Set())
const hideInspectorDuringDrag = ref(false)
const suppressHandleSelectionID = ref('')
const modelMenuElementID = ref('')
const showMiniMap = ref(true)
const miniMapVisibleBeforeZen = ref(true)
const imageViewByNodeID = ref<Record<string, { scale: number; x: number; y: number }>>({})
const loadedCanvasImages = ref<Record<string, 'loaded' | 'error'>>({})
const imagePanState = ref<{ elementID: string; startX: number; startY: number; originX: number; originY: number } | null>(null)
const videoFrameTimeByNodeID = ref<Record<string, number>>({})
const videoDurationByNodeID = ref<Record<string, number>>({})
const activeMaskElementID = ref('')
const hoveredMaskElementID = ref('')
const maskCursor = ref<{ elementID: string; x: number; y: number; size: number; visible: boolean }>({ elementID: '', x: 0, y: 0, size: 0, visible: false })
const maskResizeObservers = new WeakMap<HTMLElement, ResizeObserver>()
const activeMaskResizeObservers = new Set<ResizeObserver>()
const gptImageSizeBaseOptions = computed(() => sizeBaseOptions.filter((option) => option.value !== '512'))
const inspectedElement = computed(() => {
  const ids = Array.from(selectedNodeIDs.value).filter((id) => Boolean(elementByID(id)))
  const id = ids[ids.length - 1]
  return id ? elementByID(id) : null
})
const modelMenuOpen = computed(() => Boolean(inspectedElement.value && modelMenuElementID.value === inspectedElement.value.id))
const inspectorStyle = computed(() => {
  const element = inspectedElement.value
  if (!element || typeof window === 'undefined') return {}
  const size = renderedNodeSize(element)
  const panelScale = zoom.value
  const gap = 12
  const nodeLeft = element.x * zoom.value + pan.value.x
  const nodeTop = element.y * zoom.value + pan.value.y
  const nodeRight = nodeLeft + size.width * zoom.value
  return {
    left: `${nodeRight + gap}px`,
    top: `${nodeTop}px`,
    transform: `scale(${panelScale})`,
    transformOrigin: 'top left',
  }
})
let assetRefreshTimer = 0
let runtimeTimer = 0
let historyTimer = 0
let cloudSaveTimer = 0
let restoringHistory = false
let loadingCloudCanvases = false
let applyingStoredCanvases = false
let applyingCloudCanvases = false
let pendingCloudUpdatedAt = ''
let activeCanvasStorageKey = currentCanvasStorageKeys().canvas
let cloudSaveInFlight = false
let cloudSaveQueuedDuringInFlight = false
let lastCloudSavePayload = ''
let lastCanvasImportToken = 0
let readySequence = 0
let dragFrame = 0
let pendingDragPoint: { clientX: number; clientY: number } | null = null
const handledCtrlWheelEvents = new WeakSet<WheelEvent>()
const camera = { x: pan.value.x, y: pan.value.y, zoom: zoom.value }
const flow = useVueFlow('canvas-flow')

const activeCanvas = computed(() => canvases.value.find((canvas) => canvas.id === activeCanvasID.value) || canvases.value[0])
const activeCanvasShared = computed(() => Boolean(activeCanvas.value && (props.sharedCanvasIds || []).includes(activeCanvas.value.id)))
const canvasTaskRefs = computed(() => {
  const byID = new Map<string, Task>()
  for (const canvas of canvases.value) {
    for (const element of canvas.elements) {
      if (!element.task_id) continue
      const task = taskForElement(element)
      if (task?.id) byID.set(task.id, task)
    }
  }
  return Array.from(byID.values())
})
const canvasTaskRefIDs = computed(() => {
  const ids = new Set<string>()
  for (const canvas of canvases.value) {
    for (const element of canvas.elements) {
      if (element.task_id) ids.add(element.task_id)
    }
  }
  return Array.from(ids)
})
const canvasTaskRefsSignature = computed(() => JSON.stringify({
  ids: canvasTaskRefIDs.value,
  tasks: canvasTaskRefs.value.map(canvasTaskRefSignatureItem),
}))
const usableTasks = computed(() => props.tasks.filter(hasMediaAsset))
const usableTaskAssetSignature = computed(() => usableTasks.value.map(assetTaskSignature).join('|'))
const visibleAssetTasks = computed(() => {
  if (assetLoaded.value) return assetPages.value[assetPageIndex.value] || assetTasks.value
  const query = assetQuery.value.trim().toLowerCase()
  const items = (query ? usableTasks.value.filter(assetSearchMatches) : usableTasks.value).slice().sort(compareTasksNewestFirst)
  return items.slice(assetPageIndex.value * ASSET_PAGE_SIZE, (assetPageIndex.value + 1) * ASSET_PAGE_SIZE)
})
const assetPageCount = computed(() => Math.max(1, Math.ceil((assetTotal.value || 0) / ASSET_PAGE_SIZE)))
const canPrevAssetPage = computed(() => assetPageIndex.value > 0)
const canNextAssetPage = computed(() => assetLoaded.value ? assetHasMore.value || assetPageIndex.value + 1 < assetPages.value.length : assetPageIndex.value + 1 < assetPageCount.value)
const zoomLabel = computed(() => `${Math.round(zoom.value * 100)}%`)
const canvasOptions = computed(() => canvases.value.map((canvas) => ({ value: canvas.id, label: canvas.name })))
const canUndo = computed(() => canvasHistory.value.length > 1 || serializeCanvasStructure(canvases.value) !== latestHistoryStructure())
const canvasStyle = computed(() => ({
  '--canvas-x': `${pan.value.x}px`,
  '--canvas-y': `${pan.value.y}px`,
  '--canvas-zoom': String(zoom.value),
}))
const canvasSaveLabel = computed(() => {
  if (canvasSaveState.value === 'loading') return '读取中'
  if (canvasSaveState.value === 'pending') return '待同步'
  if (canvasSaveState.value === 'saving') return '保存中'
  if (canvasSaveState.value === 'saved') return '已保存'
  if (canvasSaveState.value === 'error') return '保存失败'
  return '本地保存'
})
const canvasSaveTitle = computed(() => {
  const local = canvasLastSavedAt.value ? `本地：${formatSaveTime(canvasLastSavedAt.value)}` : '本地：尚未保存'
  const cloud = canvasLastCloudSavedAt.value ? `云端：${formatSaveTime(canvasLastCloudSavedAt.value)}` : '云端：尚未同步'
  return `${canvasSaveLabel.value}。${canvasSaveDetail.value || ''} ${local}；${cloud}`
})
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
    'is-connected-to-selection': isConnectionConnectedToSelection(connection),
  },
  style: { stroke: edgeColor(connection) },
  zIndex: connectionZIndex(connection),
})))
const flowDefaultEdgeOptions = {
  type: 'default',
  style: { stroke: 'rgba(190, 190, 190, .58)', strokeWidth: 2.5 },
}
const flowConnectionLineOptions = {
  type: ConnectionLineType.Bezier,
  style: { stroke: 'rgba(230, 230, 230, .78)', strokeWidth: 2.5 },
}
watch(canvases, () => {
  if (props.readOnly) return
  if (applyingStoredCanvases) return
  if (isTransientCanvasMutation()) return
  const saved = applyingCloudCanvases
    ? saveCanvases({ updatedAt: pendingCloudUpdatedAt || nowISO(), cloudUpdatedAt: pendingCloudUpdatedAt, state: 'saved', detail: '已载入云端保存' })
    : saveCanvases()
  if (!applyingCloudCanvases && (saved || (props.apikey && props.baseurl))) queueCloudCanvasSave()
  if (!restoringHistory && !applyingCloudCanvases) queueCanvasHistorySnapshot()
}, { deep: true })
watch(activeCanvasID, () => {
  if (!props.readOnly) saveCanvasMeta()
  activeCanvasDrawer.value = ''
  nextTick(() => focusActiveCanvasElements(180))
})
watch([() => props.apikey, () => props.baseurl], () => {
  if (!props.readOnly) syncWorkspaceCanvases()
}, { immediate: true })
watch([() => props.apikey, () => props.baseurl], () => queueAssetRefresh(), { immediate: true })
watch(() => props.externalCanvasState?.token, () => applyExternalCanvasState(), { immediate: true })
watch(() => props.canvasImport?.token, (token) => {
  if (props.readOnly || !token || token === lastCanvasImportToken || !props.canvasImport?.canvas) return
  lastCanvasImportToken = token
  importCanvasTemplate(props.canvasImport.canvas)
}, { immediate: true })
watch(canvasTaskRefsSignature, () => {
  syncElementTaskSnapshots()
  emit('taskRefsChange', canvasTaskRefs.value.map(compactTaskSnapshot), canvasTaskRefIDs.value)
}, { immediate: true })
watch(usableTaskAssetSignature, () => syncUsableTasksToAssets(usableTasks.value))
watch(showAssets, (visible) => {
  if (visible && !assetTasks.value.length) queueAssetRefresh()
})

onMounted(() => {
  runtimeTimer = window.setInterval(() => {
    runtimeNow.value = Date.now()
  }, 1000)
  window.addEventListener('keydown', onCanvasKeyDown)
  window.addEventListener('keyup', onCanvasKeyUp)
  window.addEventListener('pointerup', stopDrag)
  window.addEventListener('pointercancel', stopDrag)
  window.addEventListener('blur', stopDrag)
  window.addEventListener('wheel', preventBrowserZoomWheel, { capture: true, passive: false })
  window.addEventListener('app-context-menu-opened', closeCanvasContextMenu)
  queueCanvasReady()
})

onActivated(() => {
  queueCanvasReady()
})

onUnmounted(() => {
  readySequence += 1
  if (runtimeTimer) window.clearInterval(runtimeTimer)
  if (historyTimer) window.clearTimeout(historyTimer)
  if (cloudSaveTimer) window.clearTimeout(cloudSaveTimer)
  if (zenMode.value) emit('zenModeChange', false)
  activeMaskResizeObservers.forEach((observer) => observer.disconnect())
  activeMaskResizeObservers.clear()
  window.removeEventListener('keydown', onCanvasKeyDown)
  window.removeEventListener('keyup', onCanvasKeyUp)
  window.removeEventListener('pointerup', stopDrag)
  window.removeEventListener('pointercancel', stopDrag)
  window.removeEventListener('blur', stopDrag)
  window.removeEventListener('wheel', preventBrowserZoomWheel, { capture: true })
  window.removeEventListener('app-context-menu-opened', closeCanvasContextMenu)
})

async function queueCanvasReady() {
  const sequence = ++readySequence
  await nextTick()
  await waitForAnimationFrame()
  await waitForAnimationFrame()
  await waitForCanvasFrame(sequence)
  focusActiveCanvasElements(0)
  if (sequence === readySequence) emit('ready')
}

function waitForAnimationFrame() {
  return new Promise<void>((resolve) => window.requestAnimationFrame(() => resolve()))
}

async function waitForCanvasFrame(sequence: number) {
  for (let attempt = 0; attempt < 24; attempt += 1) {
    if (sequence !== readySequence) return
    const frame = document.querySelector<HTMLElement>('.canvas-flow .vue-flow__pane')
    const rect = frame?.getBoundingClientRect()
    if (rect && rect.width > 0 && rect.height > 0) return
    await waitForAnimationFrame()
  }
}

function closeCanvasContextMenu() {
  canvasContextMenu.value = null
}

function isTextInputTarget(target: EventTarget | null) {
  return target instanceof HTMLElement && Boolean(target.closest('input, textarea, select, [contenteditable="true"]'))
}

function isMaskSizeInputTarget(target: EventTarget | null) {
  return target instanceof HTMLElement && Boolean(target.closest('.canvas-mask-size input'))
}

function onCanvasKeyDown(event: KeyboardEvent) {
  if (props.readOnly && (event.key === 'Delete' || event.key === 'Backspace' || (event.ctrlKey && event.key.toLowerCase() === 'z'))) return
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

function toggleCanvasDrawer(drawer: CanvasDrawer, event?: Event) {
  event?.stopPropagation()
  activeCanvasDrawer.value = activeCanvasDrawer.value === drawer ? '' : drawer
  if (event) blurControl(event)
}

function drawerGroupClass(drawer: Exclude<CanvasDrawer, ''>) {
  return { 'drawer-open': activeCanvasDrawer.value === drawer }
}

function readLocalCanvasState(): { canvases: BoardCanvas[]; meta: CanvasLocalMeta } {
  const keys = currentCanvasStorageKeys()
  const meta = readCanvasMeta(keys.meta)
  if (keys.workspaceKey && meta.workspaceKey !== keys.workspaceKey) {
    discardUntrustedScopedCanvasState(keys)
    return { canvases: [createBlankCanvas()], meta: {} }
  }
  const payload = localStorage.getItem(keys.canvas) || ''
  try {
    const parsed = JSON.parse(payload || '')
    if (Array.isArray(parsed) && parsed.length) {
      const canvases = normalizeCanvases(parsed)
      const nextMeta = { ...meta }
      if (hasMeaningfulCanvases(canvases) && !nextMeta.updatedAt) nextMeta.updatedAt = nowISO()
      return { canvases, meta: nextMeta }
    }
  } catch {
    // Use the default below.
  }
  return { canvases: [createBlankCanvas()], meta }
}

function createBlankCanvas(): BoardCanvas {
  return { id: createID(), name: '画布 1', elements: [], connections: [] }
}

function discardUntrustedScopedCanvasState(keys: CanvasStorageKeys) {
  try {
    localStorage.removeItem(keys.canvas)
    localStorage.removeItem(keys.meta)
  } catch {
    // Keep rendering with an empty canvas even if localStorage cleanup fails.
  }
}

function normalizeCanvases(raw: unknown): BoardCanvas[] {
  if (!Array.isArray(raw) || !raw.length) return [createBlankCanvas()]
  return raw.map((canvas: Partial<BoardCanvas>, index) => ({
    id: canvas.id || createID(),
    name: canvas.name || `画布 ${index + 1}`,
    elements: ensureElementBadges(Array.isArray(canvas.elements) ? canvas.elements.map((element: Partial<CanvasElement>, elementIndex: number) => normalizeElement(element, elementIndex)) : []),
    connections: Array.isArray(canvas.connections) ? canvas.connections : [],
  }))
}


function normalizeElement(raw: Partial<CanvasElement>, index = 0): CanvasElement {
  const rawWithLegacy = raw as Partial<CanvasElement> & { draft?: boolean }
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
    task_snapshot: raw.task_snapshot && typeof raw.task_snapshot === 'object' ? compactTaskSnapshot(raw.task_snapshot as Task) : undefined,
    generated_params: Array.isArray(raw.generated_params) ? raw.generated_params.filter((item) => typeof item === 'string') : undefined,
    media_type: raw.media_type,
    media_url: raw.media_url || '',
    media_thumbnail_url: raw.media_thumbnail_url || '',
    media_first_frame_url: raw.media_first_frame_url || '',
    media_last_frame_url: raw.media_last_frame_url || '',
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
    video_draft: supportsVideoDraft(raw.model) ? Boolean(raw.video_draft ?? rawWithLegacy.draft) : false,
    video_frame_role: normalizeVideoFrameRole(raw.video_frame_role),
    video_first_frame_source_id: raw.video_first_frame_source_id || '',
    video_last_frame_source_id: raw.video_last_frame_source_id || '',
    video_clip_start: Number(raw.video_clip_start) || 0,
    video_clip_end: Number(raw.video_clip_end) || 0,
    reasoning_effort: raw.reasoning_effort || 'low',
    generate_audio: raw.generate_audio,
    watermark: false,
    mask_data_url: raw.mask_data_url || '',
    mask_tool: raw.mask_tool || 'brush',
    mask_brush_size: raw.mask_brush_size || 32,
    frozen: Boolean(raw.frozen),
    zIndex: Number.isFinite(raw.zIndex) ? Number(raw.zIndex) : index,
    image_view_scale: Number.isFinite(raw.image_view_scale) ? Number(raw.image_view_scale) : 1,
    image_view_x: Number.isFinite(raw.image_view_x) ? Number(raw.image_view_x) : 0,
    image_view_y: Number.isFinite(raw.image_view_y) ? Number(raw.image_view_y) : 0,
    view_azimuth: Number.isFinite(raw.view_azimuth) ? normalizeAzimuth(Number(raw.view_azimuth)) : 30,
    view_elevation: Number.isFinite(raw.view_elevation) ? clamp(Number(raw.view_elevation), -60, 60) : 0,
    view_roll: Number.isFinite(raw.view_roll) ? clamp(Number(raw.view_roll), -45, 45) : 0,
    view_distance: Number.isFinite(raw.view_distance) ? clamp(Number(raw.view_distance), 0, 100) : 50,
    view_scene_yaw: Number.isFinite(raw.view_scene_yaw) ? normalizeAzimuth(Number(raw.view_scene_yaw)) : -28,
    view_scene_pitch: Number.isFinite(raw.view_scene_pitch) ? clamp(Number(raw.view_scene_pitch), -65, 65) : 18,
    view_scene_zoom: Number.isFinite(raw.view_scene_zoom) ? clamp(Number(raw.view_scene_zoom), 0.72, 1.7) : 1,
    x: Number(raw.x) || 0,
    y: Number(raw.y) || 0,
    width: Math.max(minSize.width, Number(raw.width) || fallbackWidth),
    height: Math.max(minSize.height, Number(raw.height) || fallbackHeight),
  }
}

function normalizeNodeKind(rawKind: string, mediaType?: CanvasElement['media_type'], taskType?: ImageForm['task_type']): NodeKind {
  if (rawKind === 'media') return mediaKindFromType(mediaType || 'image')
  if (rawKind === 'image_media' || rawKind === 'video_media' || rawKind === 'audio_media' || rawKind === 'asset' || rawKind === 'ai' || rawKind === 'image' || rawKind === 'video' || rawKind === 'audio' || rawKind === 'llm' || rawKind === 'mask' || rawKind === 'tail_frame' || rawKind === 'prompt' || rawKind === 'merge' || rawKind === 'view_control') return rawKind
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

function syncWorkspaceCanvases() {
  const keys = currentCanvasStorageKeys()
  if (keys.canvas !== activeCanvasStorageKey) {
    activeCanvasStorageKey = keys.canvas
    const state = readLocalCanvasState()
    applyingStoredCanvases = true
    canvases.value = state.canvases
    activeCanvasID.value = validActiveCanvasID(state.canvases, state.meta.activeCanvasID)
    canvasLastSavedAt.value = state.meta.updatedAt || ''
    canvasLastCloudSavedAt.value = state.meta.cloudUpdatedAt || ''
    lastCloudSavePayload = ''
    canvasSaveState.value = state.meta.updatedAt ? 'local' : 'saved'
    canvasSaveDetail.value = state.meta.updatedAt ? `本地 ${formatSaveTime(state.meta.updatedAt)}` : '本地空画布'
    nextTick(() => {
      applyingStoredCanvases = false
    })
  }
  loadCloudCanvases()
}

function applyExternalCanvasState() {
  if (!props.readOnly || !props.externalCanvasState) return
  const cloudCanvases = normalizeCanvases(props.externalCanvasState.canvases)
  const cloudUpdatedAt = normalizeTimestamp(props.externalCanvasState.updated_at)
  applyingCloudCanvases = true
  canvases.value = cloudCanvases
  activeCanvasID.value = validActiveCanvasID(cloudCanvases, activeCanvasID.value)
  canvasLastSavedAt.value = cloudUpdatedAt
  canvasLastCloudSavedAt.value = cloudUpdatedAt
  lastCloudSavePayload = serializeCanvases(cloudCanvases)
  canvasSaveState.value = 'saved'
  canvasSaveDetail.value = cloudCanvases.length ? `只读 · 云端 ${formatSaveTime(cloudUpdatedAt)}` : '只读 · 暂无云端画布'
  nextTick(() => {
    applyingCloudCanvases = false
    focusActiveCanvasElements(180)
    queueCanvasReady()
  })
}

function saveCanvases(options: { updatedAt?: string; cloudUpdatedAt?: string; state?: CanvasSaveState; detail?: string } = {}) {
  if (props.readOnly) return false
  const updatedAt = options.updatedAt || nowISO()
  try {
    localStorage.setItem(currentCanvasStorageKeys().canvas, JSON.stringify(canvases.value))
    saveCanvasMeta({ updatedAt, cloudUpdatedAt: options.cloudUpdatedAt ?? canvasLastCloudSavedAt.value })
    canvasLastSavedAt.value = updatedAt
    if (options.cloudUpdatedAt) canvasLastCloudSavedAt.value = options.cloudUpdatedAt
    if (options.state) {
      canvasSaveState.value = options.state
      canvasSaveDetail.value = options.detail || ''
    } else if (props.apikey && props.baseurl) {
      canvasSaveState.value = 'pending'
      canvasSaveDetail.value = '本地已保存，等待云端同步'
    } else {
      canvasSaveState.value = 'local'
      canvasSaveDetail.value = `本地 ${formatSaveTime(updatedAt)}`
    }
    return true
  } catch (error) {
    canvasSaveState.value = 'error'
    canvasSaveDetail.value = error instanceof Error ? `本地保存失败：${error.message}` : '本地保存失败'
    return false
  }
}

function saveCanvasMeta(overrides: Partial<CanvasLocalMeta> = {}) {
  const keys = currentCanvasStorageKeys()
  const meta: CanvasLocalMeta = {
    updatedAt: overrides.updatedAt ?? canvasLastSavedAt.value,
    cloudUpdatedAt: overrides.cloudUpdatedAt ?? canvasLastCloudSavedAt.value,
    activeCanvasID: activeCanvasID.value,
  }
  if (keys.workspaceKey) meta.workspaceKey = keys.workspaceKey
  try {
    localStorage.setItem(keys.meta, JSON.stringify(meta))
  } catch (error) {
    canvasSaveState.value = 'error'
    canvasSaveDetail.value = error instanceof Error ? `保存状态记录失败：${error.message}` : '保存状态记录失败'
  }
}

async function loadCloudCanvases() {
  if (props.readOnly) return
  if (!props.apikey || !props.baseurl) return
  const previousState = canvasSaveState.value
  const previousDetail = canvasSaveDetail.value
  try {
    loadingCloudCanvases = true
    canvasSaveState.value = 'loading'
    canvasSaveDetail.value = '正在读取云端画布'
    const result = await fetchCanvases(props.apikey, props.baseurl)
    const cloudCanvases = normalizeCanvases(result.canvases)
    const cloudUpdatedAt = normalizeTimestamp(result.updated_at)
    const cloudPayload = serializeCanvases(cloudCanvases)
    const localPayload = serializeCanvases(canvases.value)
    const cloudHasContent = hasMeaningfulCanvases(cloudCanvases)
    const localHasContent = hasMeaningfulCanvases(canvases.value)
    let localUpdatedAt = canvasLastSavedAt.value
    if (localHasContent && !localUpdatedAt) {
      localUpdatedAt = nowISO()
      saveCanvases({ updatedAt: localUpdatedAt, state: 'pending', detail: '本地已保存，等待云端同步' })
    }
    lastCloudSavePayload = cloudPayload
    canvasLastCloudSavedAt.value = cloudUpdatedAt
    saveCanvasMeta({ cloudUpdatedAt })
    if (cloudHasContent && (!localHasContent || timestampMS(cloudUpdatedAt) > timestampMS(localUpdatedAt))) {
      applyingCloudCanvases = true
      pendingCloudUpdatedAt = cloudUpdatedAt || nowISO()
      canvases.value = cloudCanvases
      if (!cloudCanvases.some((canvas) => canvas.id === activeCanvasID.value)) activeCanvasID.value = cloudCanvases[0]?.id || ''
      canvasSaveState.value = 'saved'
      canvasSaveDetail.value = `云端 ${formatSaveTime(pendingCloudUpdatedAt)}`
      nextTick(() => {
        applyingCloudCanvases = false
        pendingCloudUpdatedAt = ''
        focusActiveCanvasElements(180)
      })
    } else if (localHasContent && localPayload !== cloudPayload) {
      canvasSaveState.value = 'pending'
      canvasSaveDetail.value = '本地版本较新，等待云端同步'
      queueCloudCanvasSave()
    } else {
      canvasSaveState.value = localHasContent ? 'saved' : 'local'
      canvasSaveDetail.value = localHasContent ? `云端 ${formatSaveTime(cloudUpdatedAt || localUpdatedAt)}` : '本地空画布'
    }
  } catch (error) {
    console.warn('[canvas-cloud] load failed', error)
    canvasSaveState.value = hasMeaningfulCanvases(canvases.value) ? 'pending' : previousState
    canvasSaveDetail.value = hasMeaningfulCanvases(canvases.value) ? '云端读取失败，本地内容已保留' : previousDetail
  } finally {
    loadingCloudCanvases = false
  }
}

function queueCloudCanvasSave() {
  if (props.readOnly) return
  if (loadingCloudCanvases || !props.apikey || !props.baseurl) return
  window.clearTimeout(cloudSaveTimer)
  canvasSaveState.value = 'pending'
  canvasSaveDetail.value = '本地已保存，等待云端同步'
  cloudSaveTimer = window.setTimeout(() => saveCanvasesToCloud(), CLOUD_SAVE_DEBOUNCE_MS)
}

function persistCanvasNow() {
  if (props.readOnly) return
  const saved = saveCanvases()
  if (saved || (props.apikey && props.baseurl)) queueCloudCanvasSave()
}

async function saveCanvasesToCloud() {
  if (props.readOnly) return
  if (!props.apikey || !props.baseurl) return
  if (cloudSaveInFlight) {
    cloudSaveQueuedDuringInFlight = true
    return
  }
  const payload = serializeCanvases(canvases.value)
  const delta = canvasCloudDelta()
  const changeCount = canvasDeltaChangeCount(delta)
  if (!changeCount) {
    canvasSaveState.value = 'saved'
    canvasSaveDetail.value = canvasLastCloudSavedAt.value ? `云端 ${formatSaveTime(canvasLastCloudSavedAt.value)}` : '云端已同步'
    return
  }
  try {
    cloudSaveInFlight = true
    canvasSaveState.value = 'saving'
    canvasSaveDetail.value = delta.full ? '首次同步全部画布' : `正在同步 ${changeCount} 项变更`
    const result = delta.full
      ? await saveCanvasesCloud(props.apikey, props.baseurl, canvases.value)
      : await patchCanvasesCloud(props.apikey, props.baseurl, delta.canvases, delta.deleted, delta.patches)
    const savedAt = normalizeTimestamp(result.updated_at) || nowISO()
    lastCloudSavePayload = payload
    canvasLastCloudSavedAt.value = savedAt
    saveCanvasMeta({ cloudUpdatedAt: savedAt })
    if (serializeCanvases(canvases.value) === payload) {
      canvasSaveState.value = 'saved'
      canvasSaveDetail.value = `云端 ${formatSaveTime(savedAt)}`
    } else {
      cloudSaveQueuedDuringInFlight = true
    }
  } catch (error) {
    console.warn('[canvas-cloud] save failed', error)
    canvasSaveState.value = 'error'
    canvasSaveDetail.value = error instanceof Error ? `云端保存失败：${error.message}` : '云端保存失败，本地内容已保留'
  } finally {
    cloudSaveInFlight = false
    if (cloudSaveQueuedDuringInFlight || serializeCanvases(canvases.value) !== lastCloudSavePayload) {
      cloudSaveQueuedDuringInFlight = false
      queueCloudCanvasSave()
    }
  }
}

function serializeCanvases(value: BoardCanvas[]) {
  return JSON.stringify(value)
}

function canvasCloudDelta(): CanvasCloudDelta {
  if (!lastCloudSavePayload) {
    return { full: true, canvases: [...canvases.value], patches: [], deleted: [] }
  }
  const previous = canvasSnapshotMap(lastCloudSavePayload)
  const current = new Map(canvases.value.map((canvas) => [canvas.id, canvas]))
  const changedCanvases: BoardCanvas[] = []
  const patches: CanvasPatchPayload[] = []
  for (const canvas of canvases.value) {
    const previousCanvas = previous.get(canvas.id)
    if (!previousCanvas) {
      changedCanvases.push(canvas)
      continue
    }
    const patch = canvasItemDelta(previousCanvas, canvas)
    if (hasCanvasPatchChanges(patch)) {
      const canvasSnapshot = serializeCanvasItem(canvas)
      if (canvasSnapshot.length <= CANVAS_ROW_PATCH_MAX_BYTES) {
        changedCanvases.push(canvas)
      } else {
        patches.push(patch)
      }
    }
  }
  const deleted = Array.from(previous.keys()).filter((id) => !current.has(id))
  return { full: false, canvases: changedCanvases, patches, deleted }
}

function canvasSnapshotMap(payload: string) {
  const snapshots = new Map<string, BoardCanvas>()
  try {
    const parsed = JSON.parse(payload)
    if (!Array.isArray(parsed)) return snapshots
    for (const canvas of parsed) {
      if (!canvas || typeof canvas !== 'object' || typeof canvas.id !== 'string') continue
      snapshots.set(canvas.id, normalizeCanvases([canvas])[0])
    }
  } catch {
    // Treat unknown baseline as empty so the next sync sends the current canvases.
  }
  return snapshots
}

function canvasItemDelta(previous: BoardCanvas, current: BoardCanvas): CanvasPatchPayload {
  const patch: CanvasPatchPayload = { id: current.id }
  if (previous.name !== current.name) patch.name = current.name
  const previousElements = itemSnapshotMap(previous.elements)
  const currentElementIDs = new Set(current.elements.map((element) => element.id))
  const elements = current.elements.filter((element) => previousElements.get(element.id) !== serializeCanvasItem(element))
  const deletedElementIDs = Array.from(previousElements.keys()).filter((id) => !currentElementIDs.has(id))
  if (elements.length) patch.elements = elements
  if (deletedElementIDs.length) patch.deleted_element_ids = deletedElementIDs
  const previousConnections = itemSnapshotMap(previous.connections)
  const currentConnectionIDs = new Set(current.connections.map((connection) => connection.id))
  const connections = current.connections.filter((connection) => previousConnections.get(connection.id) !== serializeCanvasItem(connection))
  const deletedConnectionIDs = Array.from(previousConnections.keys()).filter((id) => !currentConnectionIDs.has(id))
  if (connections.length) patch.connections = connections
  if (deletedConnectionIDs.length) patch.deleted_connection_ids = deletedConnectionIDs
  return patch
}

function itemSnapshotMap<T extends { id: string }>(items: T[]) {
  const snapshots = new Map<string, string>()
  for (const item of items) snapshots.set(item.id, serializeCanvasItem(item))
  return snapshots
}

function serializeCanvasItem(item: unknown) {
  return JSON.stringify(item)
}

function hasCanvasPatchChanges(patch: CanvasPatchPayload) {
  return Object.prototype.hasOwnProperty.call(patch, 'name')
    || Boolean(patch.elements?.length)
    || Boolean(patch.deleted_element_ids?.length)
    || Boolean(patch.connections?.length)
    || Boolean(patch.deleted_connection_ids?.length)
}

function canvasDeltaChangeCount(delta: CanvasCloudDelta) {
  if (delta.full) return delta.canvases.length
  return delta.canvases.length
    + delta.deleted.length
    + delta.patches.reduce((total, patch) => total
      + (Object.prototype.hasOwnProperty.call(patch, 'name') ? 1 : 0)
      + (patch.elements?.length || 0)
      + (patch.deleted_element_ids?.length || 0)
      + (patch.connections?.length || 0)
      + (patch.deleted_connection_ids?.length || 0), 0)
}

function currentCanvasStorageKeys(): CanvasStorageKeys {
  const suffix = workspaceStorageSuffix()
  return suffix
    ? { canvas: `${STORAGE_SCOPED_PREFIX}${suffix}`, meta: `${STORAGE_META_SCOPED_PREFIX}${suffix}`, workspaceKey: suffix }
    : { canvas: STORAGE_KEY, meta: STORAGE_META_KEY, workspaceKey: '' }
}

function workspaceStorageSuffix() {
  const apiKey = props.apikey.trim()
  const baseURL = normalizeStorageBaseURL(props.baseurl)
  if (!apiKey || !baseURL) return ''
  return hashStorageKey(`${apiKey}|${baseURL}`)
}

function normalizeStorageBaseURL(value: string) {
  return value.trim().replace(/\/+$/, '')
}

function hashStorageKey(value: string) {
  let hash = 2166136261
  for (let index = 0; index < value.length; index += 1) {
    hash ^= value.charCodeAt(index)
    hash = Math.imul(hash, 16777619)
  }
  return (hash >>> 0).toString(36)
}

function readCanvasMeta(key: string): CanvasLocalMeta {
  try {
    const parsed = JSON.parse(localStorage.getItem(key) || '{}')
    if (!parsed || typeof parsed !== 'object') return {}
    const value = parsed as CanvasLocalMeta
    return {
      updatedAt: typeof value.updatedAt === 'string' ? value.updatedAt : '',
      cloudUpdatedAt: typeof value.cloudUpdatedAt === 'string' ? value.cloudUpdatedAt : '',
      activeCanvasID: typeof value.activeCanvasID === 'string' ? value.activeCanvasID : '',
      workspaceKey: typeof value.workspaceKey === 'string' ? value.workspaceKey : '',
    }
  } catch {
    return {}
  }
}

function validActiveCanvasID(items: BoardCanvas[], preferred = '') {
  if (preferred && items.some((canvas) => canvas.id === preferred)) return preferred
  return items[0]?.id || ''
}

function hasMeaningfulCanvases(items: BoardCanvas[]) {
  return items.some((canvas) => canvas.elements.length || canvas.connections.length)
}

function nowISO() {
  return new Date().toISOString()
}

function normalizeTimestamp(value: string) {
  if (!value || value.startsWith('0001-01-01')) return ''
  return value
}

function timestampMS(value = '') {
  const time = Date.parse(value)
  return Number.isFinite(time) ? time : 0
}

function formatSaveTime(value = '') {
  const time = timestampMS(value)
  if (!time) return '尚未同步'
  return new Date(time).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit' })
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
    task_snapshot: current.task_snapshot,
    generated_params: current.generated_params,
    media_type: current.media_type,
    media_url: current.media_url,
    media_thumbnail_url: current.media_thumbnail_url,
    media_first_frame_url: current.media_first_frame_url,
    media_last_frame_url: current.media_last_frame_url,
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
    video_draft: current.video_draft,
    video_frame_role: current.video_frame_role,
    video_first_frame_source_id: current.video_first_frame_source_id,
    video_last_frame_source_id: current.video_last_frame_source_id,
    video_clip_start: current.video_clip_start,
    video_clip_end: current.video_clip_end,
    reasoning_effort: current.reasoning_effort,
    generate_audio: current.generate_audio,
    watermark: false,
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

function nextCanvasName() {
  const names = new Set(canvases.value.map((canvas) => canvas.name.trim()))
  let index = canvases.value.length + 1
  while (names.has(`画布 ${index}`)) index += 1
  return `画布 ${index}`
}

function createCanvas() {
  if (props.readOnly) return
  const next = { id: createID(), name: nextCanvasName(), elements: [], connections: [] }
  canvases.value.push(next)
  activeCanvasID.value = next.id
  resetView()
}

function shareActiveCanvas() {
  if (!activeCanvas.value) return
  const canvas = JSON.parse(JSON.stringify(activeCanvas.value)) as BoardCanvas
  canvas.elements = canvas.elements.map((element) => {
    const task = taskForElement(element)
    return task ? { ...element, task_snapshot: compactTaskSnapshot(task) } : element
  })
  emit('shareCanvas', canvas)
}

function toggleActiveCanvasShare() {
  if (!activeCanvas.value) return
  if (activeCanvasShared.value) emit('unshareCanvas', activeCanvas.value.id)
  else shareActiveCanvas()
}

function importCanvasTemplate(raw: unknown) {
  const source = normalizeCanvases([raw])[0]
  if (!source) {
    showCanvasNotice('画布模板无效')
    return
  }
  const idMap = new Map<string, string>()
  const elements = source.elements.map((element) => {
    const id = createID()
    idMap.set(element.id, id)
    const next = { ...element, id, task_id: undefined, zIndex: (Number(element.zIndex) || 0) + maxCanvasZIndex() + 1 }
    return next
  })
  const connections = source.connections
    .map((connection) => ({ id: createID(), from: idMap.get(connection.from) || '', to: idMap.get(connection.to) || '' }))
    .filter((connection) => connection.from && connection.to)
  const imported: BoardCanvas = {
    id: createID(),
    name: nextImportedCanvasName(source.name),
    elements,
    connections,
  }
  canvases.value.push(imported)
  activeCanvasID.value = imported.id
  showCanvasNotice('已导入广场画布，可直接修改或运行')
  nextTick(() => resetView())
}

function nextImportedCanvasName(name: string) {
  const base = `${(name || '广场画布').trim()} 副本`
  const names = new Set(canvases.value.map((canvas) => canvas.name.trim()))
  if (!names.has(base)) return base
  let index = 2
  while (names.has(`${base} ${index}`)) index += 1
  return `${base} ${index}`
}

function openRenameCanvas() {
  if (!activeCanvas.value) return
  renameDialog.value = { canvasID: activeCanvas.value.id, value: activeCanvas.value.name }
  nextTick(() => {
    renameInput.value?.focus()
    renameInput.value?.select()
  })
}

function closeRenameCanvas() {
  renameDialog.value = null
}

function confirmRenameCanvas() {
  const dialog = renameDialog.value
  if (!dialog) return
  const canvas = canvases.value.find((item) => item.id === dialog.canvasID)
  const name = dialog.value.trim()
  if (canvas && name) canvas.name = name
  closeRenameCanvas()
}

function openDeleteCanvas() {
  if (!activeCanvas.value || canvases.value.length <= 1) return
  deleteDialog.value = { canvasID: activeCanvas.value.id, name: activeCanvas.value.name }
}

function closeDeleteCanvas() {
  deleteDialog.value = null
}

function confirmDeleteCanvas() {
  const dialog = deleteDialog.value
  if (!dialog || canvases.value.length <= 1) return
  const index = canvases.value.findIndex((canvas) => canvas.id === dialog.canvasID)
  if (index < 0) {
    closeDeleteCanvas()
    return
  }
  canvases.value.splice(index, 1)
  activeCanvasID.value = canvases.value[Math.max(0, index - 1)]?.id || ''
  closeDeleteCanvas()
}

function queueAssetRefresh() {
  window.clearTimeout(assetRefreshTimer)
  assetRefreshTimer = window.setTimeout(() => {
    refreshAssets().catch(() => undefined)
  }, 220)
}

async function refreshAssets() {
  assetQuery.value = assetSearch.value.trim()
  assetPageIndex.value = 0
  assetPages.value = []
  if (!props.apikey || !props.baseurl) {
    assetTasks.value = usableTasks.value.filter(assetSearchMatches).slice().sort(compareTasksNewestFirst)
    assetLoaded.value = false
    assetHasMore.value = assetTasks.value.length > ASSET_PAGE_SIZE
    assetTotal.value = assetTasks.value.length
    return
  }
  assetLoading.value = true
  assetError.value = ''
  try {
    const result = await listTasks(props.apikey, props.baseurl, 'succeeded', assetQuery.value, false, '', '', ASSET_PAGE_SIZE)
    const page = result.data.filter(hasMediaAsset)
    assetTasks.value = page
    assetPages.value = [page]
    assetLoaded.value = true
    assetHasMore.value = result.has_more
    assetTotal.value = result.total
    assetNextBeforeCreatedAt.value = result.next_before_created_at
    assetNextBeforeID.value = result.next_before_id
  } catch (error) {
    assetError.value = error instanceof Error ? error.message : '素材加载失败'
    assetTasks.value = usableTasks.value.filter(assetSearchMatches).slice().sort(compareTasksNewestFirst)
    assetLoaded.value = false
    assetHasMore.value = assetTasks.value.length > ASSET_PAGE_SIZE
    assetTotal.value = assetTasks.value.length
  } finally {
    assetLoading.value = false
  }
}

function submitAssetSearch() {
  refreshAssets().catch(() => undefined)
}

function refreshAssetPage() {
  refreshAssets().catch(() => undefined)
}

function previousAssetPage() {
  if (!canPrevAssetPage.value || assetLoading.value) return
  assetPageIndex.value -= 1
  assetTasks.value = assetPages.value[assetPageIndex.value] || assetTasks.value
}

async function nextAssetPage() {
  if (assetLoading.value || !canNextAssetPage.value) return
  if (assetPageIndex.value + 1 < assetPages.value.length) {
    assetPageIndex.value += 1
    assetTasks.value = assetPages.value[assetPageIndex.value] || assetTasks.value
    return
  }
  if (!props.apikey || !props.baseurl) {
    if (assetPageIndex.value + 1 < assetPageCount.value) assetPageIndex.value += 1
    return
  }
  if (!assetHasMore.value) return
  assetLoading.value = true
  assetError.value = ''
  try {
    const result = await listTasks(props.apikey, props.baseurl, 'succeeded', assetQuery.value, false, assetNextBeforeCreatedAt.value, assetNextBeforeID.value, ASSET_PAGE_SIZE)
    const page = result.data.filter(hasMediaAsset)
    assetPageIndex.value += 1
    assetPages.value = [...assetPages.value, page]
    assetTasks.value = page
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
  const query = assetQuery.value.trim().toLowerCase()
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
  const knownIDs = new Set(assetPages.value.flat().map((task) => task.id))
  assetTasks.value.forEach((task) => knownIDs.add(task.id))
  const firstPage = assetPages.value[0] || []
  const oldestFirstPageTime = firstPage.reduce((oldest, task) => {
    const time = timestampMS(task.created_at)
    return oldest ? Math.min(oldest, time) : time
  }, 0)
  const shouldAddToFirstPage = (task: Task) => {
    if (knownIDs.has(task.id)) return false
    if (!firstPage.length || firstPage.length < ASSET_PAGE_SIZE) return true
    const time = timestampMS(task.created_at)
    return Boolean(time && oldestFirstPageTime && time >= oldestFirstPageTime)
  }
  const newFirstPageTasks = incoming.filter(shouldAddToFirstPage)
  let pages = assetPages.value.map((page) => page.map((task) => incomingByID.get(task.id) || task))
  if (newFirstPageTasks.length) {
    pages = pages.length ? pages : [[]]
    pages[0] = mergeAssetTasks(pages[0], newFirstPageTasks)
    assetTotal.value = Math.max(assetTotal.value + newFirstPageTasks.length, pages.flat().length)
  }
  assetPages.value = pages
  assetTasks.value = pages[assetPageIndex.value] || assetTasks.value.map((task) => incomingByID.get(task.id) || task)
}

function mergeAssetTasks(existing: Task[], incoming: Task[]) {
  const byID = new Map<string, Task>()
  for (const task of existing) byID.set(task.id, task)
  for (const task of incoming) byID.set(task.id, task)
  return Array.from(byID.values()).sort(compareTasksNewestFirst)
}

function compareTasksNewestFirst(left: Task, right: Task) {
  const timeDelta = timestampMS(right.created_at) - timestampMS(left.created_at)
  return timeDelta || right.id.localeCompare(left.id)
}

function assetTaskSignature(task: Task) {
  const image = task.result_images?.[0]
  const video = task.result_videos?.[0]
  const audio = firstAudioAsset(task)
  return [
    task.id,
    task.task_type,
    task.prompt,
    task.final_prompt,
    task.model,
    image?.url || '',
    image?.thumbnail_url || '',
    video?.url || '',
    video?.thumbnail_url || video?.first_frame_url || '',
    video?.first_frame_url || '',
    video?.last_frame_url || '',
    audio?.url || '',
    audio?.thumbnail_url || '',
  ].join('\u001f')
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
  const thumbnailURL = video?.thumbnail_url || video?.first_frame_url || audio?.thumbnail_url || image?.thumbnail_url || ''
  const filename = video?.filename || audio?.filename || image?.filename || assetLabel(task)
  const width = mediaType === 'video' ? 360 : mediaType === 'audio' ? 280 : 260
  const element: CanvasElement = {
    id: createID(),
    kind: mediaKindFromType(mediaType),
    task_id: task.id,
    media_type: mediaType,
    media_url: mediaURL,
    media_thumbnail_url: thumbnailURL,
    media_first_frame_url: video?.first_frame_url || '',
    media_last_frame_url: video?.last_frame_url || '',
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
  if (props.readOnly) return
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
    media_first_frame_url: '',
    media_last_frame_url: '',
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
  if (props.readOnly) return
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
    media_first_frame_url: '',
    media_last_frame_url: '',
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
    element.media_thumbnail_url = uploaded.thumbnail_url || uploaded.first_frame_url || ''
    element.media_first_frame_url = uploaded.first_frame_url || ''
    element.media_last_frame_url = uploaded.last_frame_url || ''
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
  element.media_first_frame_url = ''
  element.media_last_frame_url = ''
  element.media_filename = filenameFromURL(trimmed) || (type === 'video' ? '视频 URL' : type === 'audio' ? '音频 URL' : '图片 URL')
  element.video_clip_start = 0
  element.video_clip_end = 0
  element.width = type === 'video' ? Math.max(element.width, 360) : type === 'audio' ? Math.max(element.width, 280) : element.width
  element.height = type === 'video' ? Math.max(element.height, 230) : type === 'audio' ? Math.max(element.height, 170) : element.height
  mediaUrlEditor.value = null
  if (type === 'video') void enrichVideoElementFrames(element)
}

async function enrichVideoElementFrames(element: CanvasElement) {
  if (!element.media_url || (element.media_first_frame_url && element.media_last_frame_url)) return
  const url = element.media_url
  try {
    const frames = await fetchVideoFrames(url, element.media_filename || filenameFromURL(url) || 'video.mp4')
    if (element.media_url !== url) return
    element.media_thumbnail_url = element.media_thumbnail_url || frames.thumbnail_url || frames.first_frame_url || ''
    element.media_first_frame_url = element.media_first_frame_url || frames.first_frame_url || ''
    element.media_last_frame_url = element.media_last_frame_url || frames.last_frame_url || frames.first_frame_url || ''
    persistCanvasNow()
  } catch {
    // URL 视频取帧失败时保留原始 URL，尾帧节点仍可在用户环境允许 CORS 时兜底抽帧。
  }
}

function addUploadedMedia(uploaded: UploadedImage, type: 'image' | 'video' | 'audio', filename: string, point: { x: number; y: number }) {
  const width = type === 'video' ? 360 : type === 'audio' ? 280 : 260
  const element: CanvasElement = {
    id: createID(),
    kind: mediaKindFromType(type),
    media_type: type,
    media_url: uploaded.url,
    media_thumbnail_url: uploaded.thumbnail_url || uploaded.first_frame_url,
    media_first_frame_url: uploaded.first_frame_url,
    media_last_frame_url: uploaded.last_frame_url,
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
  if (props.readOnly) return
  const center = position || screenToWorld(window.innerWidth / 2, window.innerHeight / 2)
  pushCanvasElement({ id: createID(), kind: 'prompt', text: '', x: center.x, y: center.y - 85, width: 320, height: 170, zIndex: maxCanvasZIndex() + 1 }, center)
}

function addMergeNode(position?: { x: number; y: number }) {
  if (props.readOnly) return
  const center = position || screenToWorld(window.innerWidth / 2, window.innerHeight / 2)
  const minSize = minNodeSize('merge')
  pushCanvasElement({ id: createID(), kind: 'merge', text: '', x: center.x, y: center.y - minSize.height / 2, width: minSize.width, height: minSize.height, zIndex: maxCanvasZIndex() + 1 }, center)
}

function addViewControlNode(position?: { x: number; y: number }) {
  if (props.readOnly) return
  const center = position || screenToWorld(window.innerWidth / 2, window.innerHeight / 2)
  const minSize = minNodeSize('view_control')
  pushCanvasElement({
    id: createID(),
    kind: 'view_control',
    text: '',
    view_azimuth: 30,
    view_elevation: 0,
    view_roll: 0,
    view_distance: 50,
    view_scene_yaw: -28,
    view_scene_pitch: 18,
    view_scene_zoom: 1,
    x: center.x - minSize.width / 2,
    y: center.y - minSize.height / 2,
    width: minSize.width,
    height: minSize.height,
    zIndex: maxCanvasZIndex() + 1,
  }, center)
}

function addAiNode(position?: { x: number; y: number }) {
  if (props.readOnly) return
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
    generated_params: [],
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
    video_draft: supportsVideoDraft(props.defaultForm.model) ? props.defaultForm.video_draft : false,
    video_frame_role: '',
    video_first_frame_source_id: '',
    video_last_frame_source_id: '',
    video_clip_start: 0,
    video_clip_end: 0,
    reasoning_effort: 'low',
    generate_audio: props.defaultForm.generate_audio,
    watermark: false,
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

function canvasModelOptions(element: CanvasElement) {
  if (element.kind === 'video') return VIDEO_MODEL_OPTIONS
  if (element.kind === 'image') return IMAGE_MODEL_OPTIONS
  return props.models
}

function modelOptionLabel(model: string) {
  return optionLabel(model)
}

function selectCanvasModel(element: CanvasElement, model: string) {
  element.model = model
  if (element.kind === 'video') updateCanvasVideoModel(element)
  else updateCanvasImageModel(element)
  modelMenuElementID.value = ''
}

function toggleModelMenu(element: CanvasElement) {
  modelMenuElementID.value = modelMenuElementID.value === element.id ? '' : element.id
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
    if (element.output_format !== 'png' && element.background === 'transparent') element.background = 'auto'
    return
  }
  if (isNanoBananaSize(element.size || '') || isSeedreamSize(element.size || '')) element.size = '1024x1024'
  if (element.output_format !== 'png' && element.background === 'transparent') element.background = 'auto'
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

function updateNanoImageSize(element: CanvasElement, value: string) {
  element.size = nanoBananaSizeValue(value, nanoAspectRatio(element))
}

function updateNanoAspectRatio(element: CanvasElement, value: string) {
  element.size = nanoBananaSizeValue(nanoImageSize(element), value)
}

function seedreamImageSize(element: CanvasElement) {
  return parseSeedreamSize(element.size || '').imageSize
}

function seedreamAspectRatio(element: CanvasElement) {
  return parseSeedreamSize(element.size || '').aspectRatio
}

function updateSeedreamImageSize(element: CanvasElement, value: string) {
  element.size = seedreamSizeValue(value, seedreamAspectRatio(element))
}

function updateSeedreamAspectRatio(element: CanvasElement, value: string) {
  element.size = seedreamSizeValue(seedreamImageSize(element), value)
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

function updateGptImageSizeBase(element: CanvasElement, base: string) {
  element.size = base === 'auto' ? 'auto' : sizeFromRatio(base, gptImageRatio(element))
}

function updateGptImageRatio(element: CanvasElement, ratio: string) {
  const base = gptImageSizeBase(element)
  if (base === 'auto') return
  element.size = sizeFromRatio(base, ratio)
}

function supportsOutputCompression(element: CanvasElement) {
  const format = element.output_format || props.defaultForm.output_format
  return format === 'jpeg' || format === 'webp'
}

function updateCanvasOutputFormat(element: CanvasElement, value: string) {
  element.output_format = value
  if (element.output_format !== 'png' && element.background === 'transparent') element.background = 'auto'
}

function canvasOutputFormatOptions(element: CanvasElement) {
  return isSeedreamElement(element) ? ['png', 'jpeg'] : ['png', 'jpeg', 'webp']
}

function canvasVideoResolutionOptions(element: CanvasElement) {
  return canvasVideoResolutions(element).map((resolution) => ({ value: resolution, label: resolution.toUpperCase() }))
}

function optionLabel(value: string) {
  const labels: Record<string, string> = {
    auto: '自动',
    high: '高',
    medium: '中',
    low: '低',
    png: 'PNG',
    jpeg: 'JPEG',
    webp: 'WebP',
    transparent: '透明',
    opaque: '不透明',
    none: '不检查',
    'gpt-image-2': 'GPT Image 2',
    'nano-banana-2': 'Nano Banana 2',
    'doubao-seedream-5.0-lite': '豆包 Seedream 5 Lite',
    'doubao-seedance-2.0': '豆包 Seedance 2',
    'doubao-seedance-1.5-pro': '豆包 Seedance 1.5 Pro',
  }
  return labels[value] || value
}

function optionHint(field: string, value: string) {
  const hints: Record<string, Record<string, string>> = {
    model: {
      'gpt-image-2': '通用生成与编辑',
      'nano-banana-2': 'Gemini 图片生成',
      'doubao-seedream-5.0-lite': '轻量图像生成',
      'doubao-seedance-2.0': '视频生成模型',
      'doubao-seedance-1.5-pro': '视频生成模型',
    },
    size: {
      auto: '由模型决定',
      '512': '低成本预览',
      '1K': '日常清晰度',
      '2K': '更高细节',
      '3K': '大图输出',
      '4K': '最高规格',
    },
    quality: {
      auto: '自动权衡',
      high: '细节优先',
      medium: '均衡输出',
      low: '速度优先',
    },
    format: {
      png: '无损，支持透明',
      jpeg: '体积小，适合照片',
      webp: '压缩效率高',
    },
    background: {
      auto: '模型决定',
      transparent: '透明背景',
      opaque: '强制不透明',
    },
    moderation: {
      low: '低强度审核',
      auto: '自动审核',
    },
    fidelity: {
      high: '更贴近输入图',
      low: '更自由改写',
    },
    reasoning: {
      none: '直接输出',
      low: '轻量思考',
      medium: '均衡推理',
      high: '更深推理',
    },
    resolution: {
      '720p': '快速预览',
      '1080p': '高清输出',
      '2k': '更高细节',
      '4k': '最高规格',
    },
  }
  return hints[field]?.[value] || ''
}

function ratioHint(ratio: string) {
  if (ratio === 'auto' || ratio === 'adaptive') return '自动'
  const [width, height] = ratio.split(':').map((part) => Number(part))
  if (!width || !height) return '比例'
  if (width === height) return '方图'
  return width > height ? '横图' : '竖图'
}

function ratioPreviewStyle(ratio: string) {
  if (ratio === 'auto' || ratio === 'adaptive') return {}
  const [width, height] = ratio.split(':').map((part) => Number(part))
  if (!width || !height) return {}
  const maxWidth = 34
  const maxHeight = 24
  const scale = Math.min(maxWidth / width, maxHeight / height)
  return {
    width: `${Math.max(8, width * scale)}px`,
    height: `${Math.max(8, height * scale)}px`,
  }
}

function canvasVideoRatios(element: CanvasElement) {
  return videoRatioOptions(canvasNodeModel(element))
}

function canvasVideoResolutions(element: CanvasElement) {
  return videoResolutionOptions(canvasNodeModel(element), canvasVideoDraft(element))
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
    draft: canvasVideoDraft(element),
  })
  element.video_ratio = normalized.ratio
  element.video_resolution = normalized.resolution
  element.video_duration = normalized.duration
}

function updateCanvasVideoModel(element: CanvasElement) {
  normalizeCanvasVideoSettings(element)
}

function canvasVideoSizeLabel(element: CanvasElement) {
  const normalized = normalizeVideoSettings({
    model: canvasNodeModel(element),
    ratio: element.video_ratio || props.defaultForm.video_ratio,
    resolution: element.video_resolution || props.defaultForm.video_resolution,
    duration: element.video_duration ?? props.defaultForm.video_duration,
    draft: canvasVideoDraft(element),
  })
  if (normalized.ratio === 'adaptive') return 'auto'
  return `${normalized.width}x${normalized.height}`
}

function canvasGenerateAudio(element: CanvasElement) {
  return element.generate_audio ?? props.defaultForm.generate_audio
}

function canvasSupportsDraft(element: CanvasElement) {
  return supportsVideoDraft(canvasNodeModel(element))
}

function canvasVideoDraft(element: CanvasElement) {
  return canvasSupportsDraft(element) ? Boolean(element.video_draft) : false
}

function toggleCanvasVideoDraft(element: CanvasElement) {
  element.video_draft = !canvasVideoDraft(element)
  normalizeCanvasVideoSettings(element)
}

function addGenerateNode(kind: GenerateNodeKind | 'mask' = props.defaultForm.task_type === 'video_generation' ? 'video' : 'image', position?: { x: number; y: number }) {
  if (props.readOnly) return
  if (!activeCanvas.value) return
  const center = position || screenToWorld(window.innerWidth / 2, window.innerHeight / 2)
  const element = createProcessElement(kind, center)
  element.zIndex = maxCanvasZIndex() + 1
  pushCanvasElement(element, center)
}

function estimateContextMenuWidth(items: CanvasContextMenuItem[]) {
  const labelWidth = Math.max(0, ...items.map((item) => Array.from(item.label).reduce((width, char) => width + (char.charCodeAt(0) <= 0x7f ? 7 : 13), 0)))
  return Math.min(260, Math.max(168, labelWidth + 18 + 9 + 20 + 14))
}

function placeContextMenu(clientX: number, clientY: number, items: CanvasContextMenuItem[]): CanvasContextMenuState {
  emit('closeContextMenu')
  const margin = 8
  const width = Math.min(estimateContextMenuWidth(items), window.innerWidth - margin * 2)
  const height = Math.min(items.length * 34 + 12, window.innerHeight - margin * 2)
  const x = clientX + width + margin > window.innerWidth ? clientX - width : clientX
  const y = clientY + height + margin > window.innerHeight ? clientY - height : clientY
  return {
    x: clamp(x, margin, window.innerWidth - width - margin),
    y: clamp(y, margin, window.innerHeight - height - margin),
    items,
  }
}

function removeElement(id: string) {
  if (props.readOnly) return
  if (!activeCanvas.value) return
  activeCanvas.value.elements = activeCanvas.value.elements.filter((element) => element.id !== id)
  activeCanvas.value.connections = activeCanvas.value.connections.filter((connection) => connection.from !== id && connection.to !== id)
}

function openCanvasContextMenu(event: MouseEvent) {
  if (props.readOnly) return
  event.preventDefault()
  const point = screenToWorld(event.clientX, event.clientY)
  canvasContextMenu.value = placeContextMenu(event.clientX, event.clientY, [
    { label: '文字提示词', icon: 'text', action: () => addPromptNode(point) },
    { label: '媒体节点', icon: 'gallery', action: () => addAssetNode(point) },
    { label: '汇合节点', icon: 'merge', action: () => addMergeNode(point) },
    { label: 'AI 生成', icon: 'sparkles', action: () => addAiNode(point) },
    { label: '自动整理', icon: 'grid', action: () => autoArrangeCanvas() },
    { label: '复位视图', icon: 'resetView', action: resetView },
  ])
}

function openNodeContextMenu(element: CanvasElement, event: MouseEvent) {
  if (props.readOnly) return
  event.preventDefault()
  const selectedIDs = Array.from(selectedNodeIDs.value).filter((id) => Boolean(elementByID(id)))
  if (selectedIDs.length > 1 && selectedIDs.includes(element.id)) {
    openSelectionContextMenu({ event, nodes: selectedIDs.map((id) => ({ id })) })
    return
  }
  const downloadable = downloadableMediaForElement(element)
  canvasContextMenu.value = placeContextMenu(event.clientX, event.clientY, [
    { label: '运行到此节点', icon: 'play', action: () => runToNode(element), disabled: !isRunnableKind(element.kind) || element.frozen || isNodeBusy(element) || isLineBusy(element) },
    { label: element.frozen ? '取消固化' : '固化节点', icon: 'archive', action: () => toggleFrozen(element), disabled: !isRunnableKind(element.kind) || isNodeBusy(element) || isLineBusy(element) },
    ...(shouldShowDownloadAction(element) ? [{ label: '下载素材', icon: 'download', action: () => downloadNodeMedia(element), disabled: !downloadable }] : []),
    { label: '复制节点', icon: 'copy', action: () => duplicateElement(element) },
    { label: '查看任务', icon: 'eye', action: () => taskForElement(element) && emit('selectTask', taskForElement(element)!) , disabled: !taskForElement(element) },
    { label: '删除节点', icon: 'trash', action: () => removeElement(element.id), danger: true },
  ])
}

function shouldShowDownloadAction(element: CanvasElement) {
  return isMediaKind(element.kind) || element.kind === 'image' || element.kind === 'video' || element.kind === 'audio' || element.kind === 'tail_frame'
}

function downloadableMediaForElement(element: CanvasElement): { url: string; filename?: string } | undefined {
  const localImage = localUploadedImage(element)
  if (localImage?.url) return { url: localImage.url, filename: localImage.filename }
  const localAsset = localMediaAsset(element)
  if (localAsset?.url) return { url: localAsset.url, filename: localAsset.filename }
  const resultImage = taskResultImage(element)
  if (resultImage?.url) return { url: resultImage.url, filename: resultImage.filename }
  const resultVideo = taskResultVideo(element)
  if (resultVideo?.url) return { url: resultVideo.url, filename: resultVideo.filename }
  const audio = firstAudioAsset(taskForElement(element))
  if (audio?.url) return { url: audio.url, filename: audio.filename }
  return undefined
}

function downloadNodeMedia(element: CanvasElement) {
  const media = downloadableMediaForElement(element)
  if (!media?.url) {
    showCanvasNotice('当前素材没有可下载地址')
    return
  }
  downloadFile(media.url, media.filename || filenameFromURL(media.url) || `${nodeBadge(element) || 'media'}`)
}

function openSelectionContextMenu(event: { event: MouseEvent; nodes: Array<{ id: string }> }) {
  if (props.readOnly) return
  const eventIDs = event.nodes.map((node) => node.id).filter((id) => Boolean(elementByID(id)))
  const cachedIDs = Array.from(selectedNodeIDs.value).filter((id) => Boolean(elementByID(id)))
  const selectedIDs = eventIDs.length > 1 ? eventIDs : cachedIDs
  if (selectedIDs.length < 2) return
  event.event.preventDefault()
  event.event.stopPropagation()
  canvasContextMenu.value = placeContextMenu(event.event.clientX, event.event.clientY, [
    { label: '删除节点', icon: 'trash', action: () => removeElements(selectedIDs), danger: true },
  ])
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

function isConnectionConnectedToSelection(connection: CanvasConnection) {
  const selected = selectedNodeIDs.value
  return selected.has(connection.from) || selected.has(connection.to)
}

function edgeColor(connection: CanvasConnection) {
  if (isConnectionConnectedToSelection(connection)) return 'rgba(147, 197, 253, .96)'
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
  const flows = workflowArrangeFlows(elements)
  const minX = Math.min(...elements.map((element) => element.x))
  const minY = Math.min(...elements.map((element) => element.y))
  const columnGap = 140
  const rowGap = 64
  const flowGap = 140
  let flowTop = minY
  for (const flow of flows) {
    const levels = workflowLevelsForElements(flow)
    const levelValues = Array.from(new Set(flow.map((element) => levels.get(element.id) || 0))).sort((a, b) => a - b)
    let x = minX
    let flowHeight = 0
    for (const level of levelValues) {
      const items = flow
        .filter((element) => (levels.get(element.id) || 0) === level)
        .sort((a, b) => a.y === b.y ? a.x - b.x : a.y - b.y)
      const columnWidth = Math.max(...items.map((element) => renderedNodeSize(element).width))
      let y = flowTop
      for (const element of items) {
        element.x = x
        element.y = y
        const size = renderedNodeSize(element)
        y += size.height + rowGap
      }
      flowHeight = Math.max(flowHeight, y - flowTop - rowGap)
      x += columnWidth + columnGap
    }
    flowTop += flowHeight + flowGap
  }
}

function workflowArrangeFlows(elements: CanvasElement[]) {
  const canvas = activeCanvas.value
  if (!canvas) return [elements]
  const selectedIDs = new Set(elements.map((element) => element.id))
  const byID = new Map(elements.map((element) => [element.id, element]))
  const seen = new Set<string>()
  const flows: CanvasElement[][] = []
  for (const element of elements.sort((a, b) => a.y === b.y ? a.x - b.x : a.y - b.y)) {
    if (seen.has(element.id)) continue
    const queue = [element.id]
    const flowIDs = new Set<string>()
    while (queue.length) {
      const id = queue.shift()!
      if (seen.has(id) || !selectedIDs.has(id)) continue
      seen.add(id)
      flowIDs.add(id)
      for (const connection of canvas.connections) {
        if (connection.from === id && selectedIDs.has(connection.to)) queue.push(connection.to)
        if (connection.to === id && selectedIDs.has(connection.from)) queue.push(connection.from)
      }
    }
    flows.push(Array.from(flowIDs).map((id) => byID.get(id)).filter(Boolean) as CanvasElement[])
  }
  return flows.sort((a, b) => {
    const topA = Math.min(...a.map((element) => element.y))
    const topB = Math.min(...b.map((element) => element.y))
    if (topA !== topB) return topA - topB
    return Math.min(...a.map((element) => element.x)) - Math.min(...b.map((element) => element.x))
  })
}

function workflowLevelsForElements(elements: CanvasElement[]) {
  const ids = new Set(elements.map((element) => element.id))
  const incomingCount = new Map<string, number>(elements.map((element) => [element.id, 0]))
  const outgoing = new Map<string, string[]>(elements.map((element) => [element.id, []]))
  const connections = (activeCanvas.value?.connections || []).filter((connection) => ids.has(connection.from) && ids.has(connection.to))
  for (const connection of connections) {
    if (!ids.has(connection.from) || !ids.has(connection.to)) continue
    incomingCount.set(connection.to, (incomingCount.get(connection.to) || 0) + 1)
    outgoing.get(connection.from)?.push(connection.to)
  }
  const levels = new Map<string, number>(elements.map((element) => [element.id, 0]))
  const queue = elements
    .filter((element) => (incomingCount.get(element.id) || 0) === 0)
    .sort((a, b) => a.y === b.y ? a.x - b.x : a.y - b.y)
    .map((element) => element.id)
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
  for (let pass = 0; pass < elements.length; pass += 1) {
    let changed = false
    for (const connection of connections) {
      const targetLevel = levels.get(connection.to) || 0
      const sourceLevel = levels.get(connection.from) || 0
      const alignedSourceLevel = Math.max(0, targetLevel - 1)
      if (sourceLevel < alignedSourceLevel) {
        levels.set(connection.from, alignedSourceLevel)
        changed = true
      }
    }
    if (!changed) break
  }
  return levels
}

function openEdgeContextMenu(event: EdgeMouseEvent) {
  if (props.readOnly) return
  event.event.preventDefault()
  event.event.stopPropagation()
  const sourceEvent = event.event
  const point = 'touches' in sourceEvent ? sourceEvent.touches[0] || sourceEvent.changedTouches[0] : sourceEvent
  if (!point) return
  canvasContextMenu.value = placeContextMenu(point.clientX, point.clientY, [
    { label: '删除连线', icon: 'trash', action: () => removeConnection(event.edge.id), danger: true },
  ])
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
  const candidates: Task[] = []
  const pushTask = (task?: Task) => {
    if (!task?.id) return
    if (element.task_id && task.id !== element.task_id) return
    if (!candidates.some((item) => item === task)) candidates.push(task)
  }
  if (element.task_id) {
    pushTask(element.task_snapshot)
    pushTask(props.canvasTaskSnapshots?.[element.task_id])
    pushTask(props.tasks.find((item) => item.id === element.task_id))
    pushTask(assetTasks.value.find((item) => item.id === element.task_id))
  } else {
    pushTask(element.task_snapshot)
  }
  return bestTaskCandidate(candidates)
}

function bestTaskCandidate(candidates: Task[]) {
  return candidates.reduce<Task | undefined>((best, task) => {
    if (!best) return task
    return compareTaskCompleteness(task, best) > 0 ? task : best
  }, undefined)
}

function compareTaskCompleteness(left: Task, right: Task) {
  const mediaDelta = taskMediaScore(left) - taskMediaScore(right)
  if (mediaDelta) return mediaDelta
  const statusDelta = taskStatusScore(left.status) - taskStatusScore(right.status)
  if (statusDelta) return statusDelta
  const timeDelta = timestampMS(left.updated_at) - timestampMS(right.updated_at)
  if (timeDelta) return timeDelta
  return left.id.localeCompare(right.id)
}

function taskMediaScore(task: Task) {
  return snapshotImages(task.result_images).filter((image) => image?.url).length
    + snapshotMedia(task.result_videos).filter((video) => video?.url).length
    + snapshotMedia(task.reference_audios).filter((audio) => audio?.url).length
}

function taskStatusScore(status: Task['status']) {
  if (status === 'succeeded') return 4
  if (status === 'running') return 3
  if (status === 'pending') return 2
  if (status === 'failed') return 1
  return 0
}

function syncElementTaskSnapshots() {
  const byID = new Map<string, Task>()
  for (const task of props.tasks) byID.set(task.id, task)
  for (const [id, task] of Object.entries(props.canvasTaskSnapshots || {})) byID.set(id, task)
  let changed = false
  for (const canvas of canvases.value) {
    for (const element of canvas.elements) {
      if (!element.task_id) continue
      const task = byID.get(element.task_id)
      if (!task) continue
      const previousTask = element.task_snapshot
      if (JSON.stringify(canvasTaskRefSignatureItem(previousTask)) === JSON.stringify(canvasTaskRefSignatureItem(task))) continue
      const snapshot = compactTaskSnapshot(task)
      element.task_snapshot = snapshot
      if (snapshot.status === 'succeeded') element.generated_params = taskParamChips(snapshot)
      if (shouldAutoFreezeFromTaskUpdate(element, snapshot, previousTask)) element.frozen = true
      changed = true
    }
  }
  if (changed) persistCanvasNow()
}

function compactTaskSnapshot(task: Task): Task {
  return {
    ...task,
    request_headers: '',
    request_json: '',
    response_headers: '',
    response_json: '',
    reference_images: compactSnapshotImages(task.reference_images),
    reference_videos: compactSnapshotMedia(task.reference_videos),
    reference_audios: compactSnapshotMedia(task.reference_audios),
    result_images: compactSnapshotImages(task.result_images),
    result_videos: compactSnapshotMedia(task.result_videos),
  }
}

function snapshotImages(images?: UploadedImage[] | null): UploadedImage[] {
  return Array.isArray(images) ? images : []
}

function snapshotMedia(items?: MediaAsset[] | null): MediaAsset[] {
  return Array.isArray(items) ? items : []
}

function compactSnapshotImages(images?: UploadedImage[] | null): UploadedImage[] {
  return snapshotImages(images).filter((image) => image?.url).map((image) => ({
    url: image.url,
    thumbnail_url: image.thumbnail_url || '',
    filename: image.filename || '',
    node_id: image.node_id || '',
    reference_label: image.reference_label || '',
    video_frame_role: image.video_frame_role || '',
    mask_reference_label: image.mask_reference_label || '',
    mask_url: image.mask_url && !image.mask_url.startsWith('data:') ? image.mask_url : '',
    original_size: image.original_size,
    compressed_size: image.compressed_size,
    compression_ratio: image.compression_ratio,
  }))
}

function compactSnapshotMedia(items?: MediaAsset[] | null): MediaAsset[] {
  return snapshotMedia(items).filter((item) => item?.url).map((item) => ({
    type: item.type || '',
    url: item.url,
    thumbnail_url: item.thumbnail_url || '',
    first_frame_url: item.first_frame_url || '',
    last_frame_url: item.last_frame_url || '',
    filename: item.filename || '',
    node_id: item.node_id || '',
    reference_label: item.reference_label || '',
    duration: item.duration,
    clip_start: item.clip_start,
    clip_end: item.clip_end,
    width: item.width,
    height: item.height,
  }))
}

function shouldAutoFreezeFromTaskUpdate(element: CanvasElement, task: Task, previousTask?: Task) {
  if (element.frozen || task.status !== 'succeeded' || !hasFrozenResult(element, task)) return false
  if (!previousTask || previousTask.id !== task.id) return true
  return previousTask.status !== 'succeeded' || !hasFrozenResult(element, previousTask)
}

function canvasTaskRefSignatureItem(task?: Task) {
  return task ? {
    id: task.id,
    status: task.status,
    created_at: task.created_at,
    updated_at: task.updated_at,
    started_at: task.started_at,
    completed_at: task.completed_at,
    elapsed_ms: task.elapsed_ms,
    upstream_progress: task.upstream_progress,
    queue_position: task.queue_position,
    error_message: task.error_message,
    result_images: snapshotImages(task.result_images).filter((image) => image?.url).map((image) => image.url).join('|'),
    result_videos: snapshotMedia(task.result_videos).filter((video) => video?.url).map((video) => video.url).join('|'),
  } : null
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
  if (task?.status === 'pending') return task.queue_position > 0 ? `排队中 #${task.queue_position}` : '排队中'
  if (task?.status === 'running') return taskRunningLabel(task)
  if (runtime?.status === 'running') return `运行中 ${formatDuration(runtimeNow.value - runtime.startedAt)}`
  return '运行'
}

function hasFrozenResult(element: CanvasElement, task = taskForElement(element)) {
  if (element.kind === 'llm') return Boolean((element.text || '').trim())
  if (element.kind === 'image') return Boolean(task?.result_images?.[0]?.url || taskResultImage(element)?.url)
  if (element.kind === 'video') return Boolean(task?.result_videos?.[0]?.url || taskResultVideo(element)?.url)
  if (element.kind === 'tail_frame') return Boolean(localUploadedImage(element)?.url)
  return false
}

function nodeProgressLabel(element: CanvasElement) {
  const runtime = nodeRuntime(element)
  const task = generatedTask(element)
  if (element.frozen) return hasFrozenResult(element) ? '已固化' : '已固化但无结果'
  if (task?.status === 'pending') return task.queue_position > 0 ? `排队中 #${task.queue_position}` : '排队中'
  if (task?.status === 'running') return taskRunningLabel(task)
  if (task?.status === 'succeeded') return `完成 ${formatTaskElapsed(task)}`
  if (task?.status === 'failed') return task.error_message || '失败'
  if (runtime?.status === 'running') return `运行中 ${formatDuration(runtimeNow.value - runtime.startedAt)}`
  if (runtime?.status === 'succeeded') return `完成 ${formatDuration((runtime.endedAt || runtimeNow.value) - runtime.startedAt)}`
  if (runtime?.status === 'failed') return runtime.message || '失败'
  return ''
}

function taskRunningLabel(task: Task) {
  const pieces = ['生成中']
  if (task.upstream_progress > 0) pieces.push(`${task.upstream_progress}%`)
  const elapsed = formatTaskRunningElapsed(task)
  if (elapsed) pieces.push(elapsed)
  return pieces.join(' ')
}

function generatedParamChips(element: CanvasElement) {
  if (isNodeRunning(element)) return []
  const task = generatedTask(element)
  const storedParams = Array.isArray(element.generated_params) ? element.generated_params.filter(Boolean) : []
  if (storedParams.length) return storedParams
  return task?.status === 'succeeded' ? taskParamChips(task) : []
}

function taskParamChips(task: Task) {
  const chips = [`模型 ${modelOptionLabel(task.model)}`]
  if (task.task_type === 'video_generation') {
    if (task.video_ratio) chips.push(`比例 ${videoRatioLabel(task.video_ratio)}`)
    const resolution = task.video_height ? `${task.video_height}P` : ''
    if (resolution) chips.push(`分辨率 ${resolution}`)
    if (task.video_duration) chips.push(`时长 ${task.video_duration}s`)
    chips.push(`音频 ${task.generate_audio ? '开' : '关'}`)
    if (task.draft !== undefined) chips.push(`样片 ${task.draft ? '开' : '关'}`)
    return chips
  }
  if (task.size) chips.push(`尺寸 ${task.size}`)
  if (task.quality) chips.push(`质量 ${optionLabel(task.quality)}`)
  if (task.output_format) chips.push(`格式 ${optionLabel(task.output_format)}`)
  if (task.output_format === 'jpeg' || task.output_format === 'webp') chips.push(`压缩 ${task.output_compression}`)
  if (task.moderation) chips.push(`审核 ${optionLabel(task.moderation)}`)
  if (task.input_fidelity) chips.push(`保真 ${optionLabel(task.input_fidelity)}`)
  if (task.n > 1) chips.push(`数量 ${task.n}`)
  if (task.reference_images?.length) chips.push(`参考图 ${task.reference_images.length}`)
  if (task.reference_videos?.length) chips.push(`参考视频 ${task.reference_videos.length}`)
  if (task.reference_audios?.length) chips.push(`参考音频 ${task.reference_audios.length}`)
  return chips
}

function llmPayloadParamChips(payload: CanvasLLMPayload) {
  return [
    `模型 ${payload.model}`,
    `推理 ${optionLabel(payload.reasoning_effort)}`,
    `图片 ${payload.reference_images.length}`,
    `视频 ${payload.reference_videos.length}`,
    `音频 ${payload.reference_audios.length}`,
  ]
}

function formatTaskElapsed(task: Task) {
  if (task.elapsed_ms > 0) return formatDuration(task.elapsed_ms)
  if (task.started_at && task.completed_at) return formatDuration(new Date(task.completed_at).getTime() - new Date(task.started_at).getTime())
  return ''
}

function formatTaskRunningElapsed(task: Task) {
  const startTime = timestampMS(task.started_at || '') || timestampMS(task.created_at || '')
  if (!startTime) return ''
  return formatDuration(runtimeNow.value - startTime)
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
    thumbnail_url: element.media_thumbnail_url || element.media_first_frame_url,
    first_frame_url: element.media_first_frame_url,
    last_frame_url: element.media_last_frame_url,
    filename: element.media_filename,
  }
}

function localUploadedImage(element: CanvasElement): UploadedImage | undefined {
  if (element.kind === 'view_control') return viewControlSourceImage(element)
  const mediaType = element.media_type || mediaTypeFromKind(element.kind)
  if (!element.media_url || mediaType !== 'image') return undefined
  return {
    url: element.media_url,
    thumbnail_url: element.media_thumbnail_url,
    filename: element.media_filename,
  }
}

function viewControlSourceImage(element: CanvasElement): UploadedImage | undefined {
  const source = connectedInputs(element).find((item) => item.kind !== 'view_control' && outputTypes(item).includes('image'))
  return source ? localUploadedImage(source) || taskResultImage(source) : undefined
}

function viewControlSourceElement(element: CanvasElement): CanvasElement | undefined {
  return connectedInputs(element).find((item) => item.kind !== 'view_control' && outputTypes(item).includes('image'))
}

function referenceIdentitySource(element: CanvasElement): CanvasElement {
  if (element.kind !== 'view_control') return element
  return viewControlSourceElement(element) || element
}

function taskResultImage(element: CanvasElement): UploadedImage | undefined {
  const task = taskForElement(element)
  return task?.result_images?.[0]?.url ? { ...task.result_images[0] } : undefined
}

function taskResultVideo(element: CanvasElement): MediaAsset | undefined {
  const task = taskForElement(element)
  const video = task?.result_videos?.[0]
  if (!video?.url) return undefined
  return { ...video, type: video.type || 'video' }
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
  selectCanvasElement(element, event)
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

function taskVideoCover(task: Task) {
  const video = task.result_videos?.[0]
  return video?.thumbnail_url || video?.first_frame_url || ''
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
  return isMediaKind(element.kind) || element.kind === 'image' || element.kind === 'video' || element.kind === 'mask' || element.kind === 'tail_frame'
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

function isEditorComposing(event: Event, element: CanvasElement) {
  return composingEditorIDs.has(element.id) || ('isComposing' in event && Boolean(event.isComposing))
}

function onEditorCompositionStart(_event: CompositionEvent, element: CanvasElement) {
  composingEditorIDs.add(element.id)
  mentionMenu.value = null
}

function onEditorCompositionEnd(event: CompositionEvent, element: CanvasElement) {
  composingEditorIDs.delete(element.id)
  onPromptTextInput(event, element, true)
}

function onPromptTextInput(event: Event, element: CanvasElement, force = false) {
  if (!force && isEditorComposing(event, element)) return
  if (!force && event instanceof KeyboardEvent && event.key === 'Enter') return
  const target = event.currentTarget as HTMLElement
  const selection = editorSelectionOffsets(target)
  if (!force && event instanceof KeyboardEvent && selection.start !== selection.end) {
    mentionMenu.value = null
    return
  }
  const cursor = selection.end
  const text = editorPlainText(target)
  element.text = text
  if (suppressCanvasMentionAfterDelete.value) {
    mentionMenu.value = null
    suppressCanvasMentionAfterDelete.value = false
    nextTick(() => setEditorCaret(target, cursor))
    return
  }
  if (event instanceof KeyboardEvent && (event.key === 'Backspace' || event.key === 'Delete')) {
    mentionMenu.value = null
    nextTick(() => setEditorCaret(target, cursor))
    return
  }
  const before = text.slice(0, cursor)
  const match = before.match(/(^|\s)@([^\s@]*)$/)
  if (!match) {
    mentionMenu.value = null
    nextTick(() => setEditorCaret(target, cursor))
    return
  }
  const query = match[2] || ''
  const start = cursor - query.length - 1
  const previous = mentionMenu.value?.elementID === element.id && mentionMenu.value.query === query ? mentionMenu.value.activeIndex : 0
  mentionMenu.value = { elementID: element.id, query, start, end: cursor, activeIndex: previous }
  nextTick(() => setEditorCaret(target, cursor))
}

function onRichEditorKeydown(event: KeyboardEvent, element: CanvasElement) {
  if (isEditorComposing(event, element)) return
  if (event.key === 'Backspace' || event.key === 'Delete') {
    handleRichEditorDelete(event, element)
    return
  }
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
    return
  }
  if (event.key === 'Enter') {
    event.preventDefault()
    insertEditableTextAtSelection(element, '\n', event.currentTarget as HTMLElement)
  }
}

function handleRichEditorDelete(event: KeyboardEvent, element: CanvasElement) {
  const target = event.currentTarget as HTMLElement
  const selection = editorSelectionOffsets(target)
  let start = selection.start
  let end = selection.end
  if (start === end) {
    const mentionRange = editableMentionRangeAt(element.text || editorPlainText(target), event.key === 'Backspace' ? start - 1 : start)
    if (mentionRange) {
      start = mentionRange.start
      end = mentionRange.end
    } else if (event.key === 'Backspace') {
      if (start <= 0) {
        mentionMenu.value = null
        return
      }
      start -= 1
    } else {
      const text = element.text || editorPlainText(target)
      if (end >= text.length) {
        mentionMenu.value = null
        return
      }
      end += 1
    }
  }
  if (start === end) {
    mentionMenu.value = null
    return
  }
  event.preventDefault()
  suppressCanvasMentionAfterDelete.value = true
  mentionMenu.value = null
  const text = element.text || editorPlainText(target)
  element.text = `${text.slice(0, start)}${text.slice(end)}`
  nextTick(() => setEditorCaret(target, start))
}

function editableMentionRangeAt(text: string, index: number) {
  if (index < 0) return null
  const pattern = /@([A-Z]+\d{2})\b/g
  for (const match of text.matchAll(pattern)) {
    const start = match.index ?? 0
    const end = start + match[0].length
    if (index >= start && index < end) return { start, end }
  }
  return null
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
  const menu = mentionMenu.value?.elementID === element.id ? mentionMenu.value : null
  const target = document.activeElement instanceof HTMLElement && document.activeElement.isContentEditable ? document.activeElement : null
  if (!menu || !target) {
    element.text = `${element.text || ''}${token}`
    mentionMenu.value = null
    return
  }
  const value = element.text || editorPlainText(target)
  const nextValue = `${value.slice(0, menu.start)}${token}${value.slice(menu.end)}`
  element.text = nextValue
  mentionMenu.value = null
  nextTick(() => {
    target.focus({ preventScroll: true })
    setEditorCaret(target, menu.start + token.length)
  })
}

function insertEditableTextAtSelection(element: CanvasElement, text: string, target: HTMLElement) {
  const selection = editorSelectionOffsets(target)
  const value = editorPlainText(target)
  element.text = `${value.slice(0, selection.start)}${text}${value.slice(selection.end)}`
  mentionMenu.value = null
  nextTick(() => {
    target.focus({ preventScroll: true })
    setEditorCaret(target, selection.start + text.length)
  })
}

function editableText(target: HTMLElement) {
  if (target instanceof HTMLTextAreaElement) return target.value
  return editorPlainText(target)
}

function editorPlainText(target: HTMLElement) {
  return editorNodeText(target)
}

function editorCaretOffset(target: HTMLElement) {
  const selection = window.getSelection()
  if (!selection?.rangeCount) return editorPlainText(target).length
  const range = selection.getRangeAt(0)
  if (!target.contains(range.endContainer)) return editorPlainText(target).length
  return editorOffsetForBoundary(target, range.endContainer, range.endOffset)
}

function editorSelectionOffsets(target: HTMLElement) {
  const selection = window.getSelection()
  if (!selection?.rangeCount) {
    const end = editorPlainText(target).length
    return { start: end, end }
  }
  const range = selection.getRangeAt(0)
  if (!target.contains(range.startContainer) || !target.contains(range.endContainer)) {
    const end = editorPlainText(target).length
    return { start: end, end }
  }
  const start = editorOffsetForBoundary(target, range.startContainer, range.startOffset)
  const end = editorOffsetForBoundary(target, range.endContainer, range.endOffset)
  return { start: Math.min(start, end), end: Math.max(start, end) }
}

function editorNodeText(node: globalThis.Node): string {
  if (node.nodeType === globalThis.Node.TEXT_NODE) return normalizeEditorText(node.textContent || '')
  if (node instanceof HTMLBRElement) return '\n'
  return Array.from(node.childNodes).map(editorNodeText).join('')
}

function editorNodeTextLength(node: globalThis.Node): number {
  return editorNodeText(node).length
}

function editorOffsetForBoundary(root: HTMLElement, boundaryNode: globalThis.Node, boundaryOffset: number) {
  let offset = 0
  let found = false
  const walk = (node: globalThis.Node) => {
    if (found) return
    if (node === boundaryNode) {
      if (node.nodeType === globalThis.Node.TEXT_NODE) {
        offset += editorTextPrefixLength(node.textContent || '', boundaryOffset)
      } else {
        const children = Array.from(node.childNodes)
        for (let index = 0; index < Math.min(boundaryOffset, children.length); index += 1) {
          offset += editorNodeTextLength(children[index])
        }
      }
      found = true
      return
    }
    if (node.nodeType === globalThis.Node.TEXT_NODE || node instanceof HTMLBRElement) {
      offset += editorNodeTextLength(node)
      return
    }
    for (const child of Array.from(node.childNodes)) walk(child)
  }
  walk(root)
  return found ? offset : editorPlainText(root).length
}

function setEditorCaret(target: HTMLElement, offset: number) {
  target.focus({ preventScroll: true })
  let remaining = Math.max(0, offset)
  const placeCaret = (node: globalThis.Node, nodeOffset: number) => {
    const range = document.createRange()
    range.setStart(node, nodeOffset)
    range.collapse(true)
    const selection = window.getSelection()
    selection?.removeAllRanges()
    selection?.addRange(range)
  }
  const walk = (node: globalThis.Node): boolean => {
    for (const child of Array.from(node.childNodes)) {
      if (child.nodeType === globalThis.Node.TEXT_NODE) {
        const length = editorNodeTextLength(child)
        if (remaining <= length) {
          placeCaret(child, rawOffsetForEditorText(child.textContent || '', remaining))
          return true
        }
        remaining -= length
        continue
      }
      if (child instanceof HTMLBRElement) {
        const index = Array.prototype.indexOf.call(node.childNodes, child)
        if (remaining <= 0) {
          placeCaret(node, index)
          return true
        }
        if (remaining <= 1) {
          const next = child.nextSibling
          if (next?.nodeType === globalThis.Node.TEXT_NODE && (next.textContent || '').startsWith('\u200b')) {
            placeCaret(next, 1)
            return true
          }
          placeCaret(node, index + 1)
          return true
        }
        remaining -= 1
        continue
      }
      if (child instanceof HTMLElement && child.classList.contains('canvas-mention-token')) {
        const tokenLength = child.textContent?.length || 0
        if (remaining <= 0) {
          placeCaret(node, Array.prototype.indexOf.call(node.childNodes, child))
          return true
        }
        if (remaining <= tokenLength) {
          placeCaret(node, Array.prototype.indexOf.call(node.childNodes, child) + 1)
          return true
        }
        remaining -= tokenLength
        continue
      }
      if (walk(child)) return true
    }
    return false
  }
  if (walk(target)) return
  const range = document.createRange()
  range.selectNodeContents(target)
  range.collapse(false)
  const selection = window.getSelection()
  selection?.removeAllRanges()
  selection?.addRange(range)
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
  const source = text || ''
  let html = ''
  let lastIndex = 0
  const pattern = /(^|\s)@([A-Z]+\d{2})\b/g
  for (const match of source.matchAll(pattern)) {
    const index = match.index ?? 0
    const prefix = match[1] || ''
    const token = match[2] || ''
    const mentionStart = index + prefix.length
    html += escapeEditorText(source.slice(lastIndex, mentionStart))
    html += `<span class="canvas-mention-token" contenteditable="false">@${escapeHTML(token)}</span>`
    lastIndex = mentionStart + token.length + 1
  }
  html += escapeEditorText(source.slice(lastIndex))
  return html
}

function escapeEditorText(text: string) {
  return escapeHTML(text).replace(/\n/g, '<br data-editor-newline="true">&#8203;')
}

function normalizeEditorText(text: string) {
  return text.replace(/\u00a0/g, ' ').replace(/\u200b/g, '')
}

function editorTextPrefixLength(text: string, offset: number) {
  return normalizeEditorText(text.slice(0, Math.max(0, offset))).length
}

function rawOffsetForEditorText(text: string, normalizedOffset: number) {
  if (normalizedOffset <= 0) return text.startsWith('\u200b') ? 1 : 0
  let visible = 0
  for (let index = 0; index < text.length; index += 1) {
    if (text[index] !== '\u200b') visible += 1
    if (visible >= normalizedOffset) return index + 1
  }
  return text.length
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
  return kind === 'llm' || kind === 'image' || kind === 'video' || kind === 'audio' || kind === 'tail_frame'
}

function hasInspectorPanel(kind: NodeKind) {
  return kind === 'llm' || kind === 'image' || kind === 'video' || kind === 'audio'
}

function isRunnableKind(kind: NodeKind) {
  return kind === 'llm' || kind === 'image' || kind === 'video' || kind === 'tail_frame'
}

function acceptsInput(kind: NodeKind) {
  return !isMediaKind(kind) && kind !== 'asset' && kind !== 'ai'
}

function hasOutput(kind: NodeKind) {
  return kind !== 'asset' && kind !== 'ai'
}

function isPromptLike(kind: NodeKind) {
  return kind === 'prompt' || kind === 'llm' || kind === 'merge' || kind === 'view_control'
}

function isTextKind(kind: NodeKind) {
  return kind === 'prompt' || kind === 'llm'
}

function canConnect(from: CanvasElement, to: CanvasElement) {
  if (from.id === to.id || !hasOutput(from.kind) || !acceptsInput(to.kind)) return false
  if (from.kind === 'view_control') return to.kind === 'image'
  if (to.kind === 'merge') return true
  if (to.kind === 'view_control') return outputTypes(from).includes('image')
  if (from.kind === 'image_media') return to.kind === 'image' || to.kind === 'video' || to.kind === 'mask'
  if (to.kind === 'tail_frame') return outputTypes(from).includes('video')
  return outputTypes(from).some((type) => acceptedInputTypes(to).includes(type))
}

function outputTypes(element: CanvasElement): NodeValueType[] {
  if (element.kind === 'prompt' || element.kind === 'llm') return ['text']
  if (element.kind === 'view_control') return ['text', 'image']
  if (element.kind === 'image_media' || element.kind === 'image' || element.kind === 'mask' || element.kind === 'tail_frame') return ['image']
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
  if (element.kind === 'tail_frame') return ['video']
  if (element.kind === 'view_control') return ['image']
  if (element.kind === 'merge') return ['text', 'image', 'video', 'audio', 'merge']
  return []
}

function connectableTargetKinds(from: CanvasElement): NodeKind[] {
  const candidates: NodeKind[] = ['prompt', 'llm', 'image', 'video', 'audio', 'merge', 'view_control', 'mask', 'tail_frame']
  return candidates.filter((kind) => canConnect(from, { ...from, id: '__target__', kind } as CanvasElement))
}

function connectableSourceKinds(to: CanvasElement): NodeKind[] {
  const candidates: NodeKind[] = ['prompt', 'llm', 'image', 'video', 'audio', 'merge', 'view_control', 'mask', 'image_media', 'video_media', 'audio_media']
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
  if (element.kind === 'view_control') return '3D 视角控制'
  if (element.kind === 'llm') return '生文字节点'
  if (element.kind === 'image') return '生图节点'
  if (element.kind === 'mask') return '蒙版节点'
  if (element.kind === 'tail_frame') return '尾帧节点'
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
  if (kind === 'view_control') return 'VIEW'
  if (kind === 'mask') return 'MASK'
  if (kind === 'tail_frame') return 'IMAGE'
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
  if (element.kind === 'view_control') return viewControlPrompt(element)
  const upstreamText = connectedInputs(element)
    .filter((item) => isPromptLike(item.kind))
    .map((item) => promptTextFor(item, visited))
  if (element.kind === 'prompt') upstreamText.push(element.text || '')
  return upstreamText.map((text) => text.trim()).filter(Boolean).join('\n')
}

function viewControlPrompt(element: CanvasElement) {
  const azimuth = Math.round(element.view_azimuth ?? 0)
  const elevation = Math.round(element.view_elevation ?? 0)
  const roll = Math.round(element.view_roll ?? 0)
  const distance = Math.round(element.view_distance ?? 50)
  const direction = viewControlDirectionLabel(element)
  return [
    `基于参考图生成同一主体的新视角：${direction}。`,
    `相机方位角 ${azimuth} 度，俯仰角 ${elevation} 度，画面滚转 ${roll} 度，镜头距离 ${distance}%。`,
    '保持主体身份、材质、颜色和关键细节一致，只改变观察视角；补全新视角中合理可见的结构。',
    element.text?.trim(),
  ].filter(Boolean).join('\n')
}

function viewControlDirectionLabel(element: CanvasElement) {
  const azimuth = normalizeAzimuth(element.view_azimuth ?? 0)
  const elevation = element.view_elevation ?? 0
  const absAzimuth = Math.abs(azimuth)
  const horizontal = absAzimuth < 8 ? '正面' : absAzimuth > 172 ? '背面' : azimuth > 0 ? `向右旋转 ${Math.round(absAzimuth)} 度` : `向左旋转 ${Math.round(absAzimuth)} 度`
  const vertical = Math.abs(elevation) < 8 ? '水平视角' : elevation > 0 ? `俯视 ${Math.round(Math.abs(elevation))} 度` : `仰视 ${Math.round(Math.abs(elevation))} 度`
  return `${horizontal}，${vertical}`
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
  const selectedFrameSourceIDs = selectedVideoFrameSourceIDs(element)
  const allowNonImageReferences = !(element.kind === 'video' && canvasSupportsDraft(element))
  for (const source of sources) {
    const referenceSource = referenceIdentitySource(source)
    const label = nodeBadge(referenceSource)
    const frameRole = videoFrameRoleForSource(element, referenceSource)
    const useImageSource = !selectedFrameSourceIDs.size || selectedFrameSourceIDs.has(source.id)
    if (source.kind === 'mask') continue
    const localImage = useImageSource ? withReferenceMeta(localUploadedImage(source), referenceSource, label, frameRole) : undefined
    const localAsset = allowNonImageReferences ? withMediaReferenceMeta(localMediaAsset(source), source, label) : undefined
    if (localImage?.url) reference_images.push(localImage)
    if (localAsset?.url && localAsset.type === 'video') reference_videos.push(localAsset)
    if (localAsset?.url && localAsset.type === 'audio') reference_audios.push(localAsset)
    const image = localImage?.url || !useImageSource ? undefined : withReferenceMeta(taskResultImage(source), referenceSource, label, frameRole)
    const video = localAsset?.url || !allowNonImageReferences ? undefined : withMediaReferenceMeta(taskResultVideo(source), source, label)
    if (image?.url) reference_images.push(image)
    if (video?.url) reference_videos.push(video)
    for (const audio of taskForElement(source)?.reference_audios || []) {
      const audioAsset = allowNonImageReferences ? withMediaReferenceMeta({ ...audio, type: audio.type || 'audio' }, source, label) : undefined
      if (audioAsset?.url) reference_audios.push(audioAsset)
    }
  }
  for (const source of sources.filter((item) => item.kind === 'mask')) {
    const sourceElement = maskSourceElement(source)
    if (selectedFrameSourceIDs.size && (!sourceElement || !selectedFrameSourceIDs.has(sourceElement.id))) continue
    const image = sourceElement ? localUploadedImage(sourceElement) || taskResultImage(sourceElement) : undefined
    const frameRole = videoFrameRoleForSource(element, sourceElement || source)
    const maskedImage = image?.url ? withReferenceMeta({
      ...image,
      mask_url: source.mask_data_url || image.mask_url,
      mask_reference_label: nodeBadge(source),
    }, sourceElement || source, sourceElement ? nodeBadge(sourceElement) : nodeBadge(source), frameRole) : undefined
    if (maskedImage?.url) reference_images.push(maskedImage)
  }
  return {
    reference_images: uniqueReferenceImages(reference_images),
    reference_videos: uniqueMediaAssets(reference_videos),
    reference_audios: uniqueMediaAssets(reference_audios),
  }
}

function inputSummaryCounts(element: CanvasElement) {
  const refs = mediaReferences(element)
  return {
    prompts: referencePromptElements(element).length,
    images: refs.reference_images.length,
    videos: refs.reference_videos.length,
    audios: refs.reference_audios.length,
  }
}

function referencePromptElements(element: CanvasElement) {
  const prompts = new Map<string, CanvasElement>()
  for (const source of connectedInputs(element)) {
    for (const item of expandedPromptSources(source)) {
      if ((item.kind === 'prompt' || item.kind === 'llm') && promptTextFor(item).trim()) prompts.set(item.id, item)
    }
  }
  return Array.from(prompts.values())
}

function expandedPromptSources(element: CanvasElement, visited = new Set<string>()): CanvasElement[] {
  if (visited.has(element.id)) return []
  visited.add(element.id)
  if (element.kind === 'merge') return connectedInputs(element).flatMap((source) => expandedPromptSources(source, visited))
  return element.kind === 'prompt' || element.kind === 'llm' || element.kind === 'view_control' ? [element] : []
}

function referenceSourceElements(element: CanvasElement) {
  const sources = new Map<string, CanvasElement>()
  const chainElements = connectedComponentElements(element)
  for (const source of connectedInputs(element)) {
    const directSources = expandedReferenceSources(source)
    for (const item of directSources) {
      sources.set(item.id, item)
      for (const mentioned of mentionedElements(referenceMentionText(item), chainElements)) {
        sources.set(mentioned.id, mentioned)
      }
    }
  }
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

function referenceMentionText(element: CanvasElement) {
  if (element.kind === 'prompt' || element.kind === 'llm' || element.kind === 'view_control') return promptTextFor(element)
  return ''
}

function mentionedElements(text: string, scopeElements = activeCanvas.value?.elements || []) {
  const badges = new Set(Array.from(text.matchAll(/@([A-Z]+\d{2})/g)).map((match) => match[1]))
  if (!badges.size) return []
  return scopeElements.filter((item) => badges.has(nodeBadge(item)) && isMentionableElement(item))
}

function withReferenceMeta(image: UploadedImage | undefined, element: CanvasElement, label: string, videoFrameRole?: UploadedImage['video_frame_role']) {
  return image?.url ? { ...image, node_id: element.id, reference_label: label, video_frame_role: normalizeVideoFrameRole(videoFrameRole) } : undefined
}

function normalizeVideoFrameRole(role?: string): UploadedImage['video_frame_role'] {
  return role === 'first_frame' || role === 'last_frame' ? role : ''
}

function videoFrameRoleForSource(element: CanvasElement, source: CanvasElement): UploadedImage['video_frame_role'] {
  if (element.kind !== 'video' || !canvasSupportsDraft(element)) return ''
  if (source.id && source.id === element.video_first_frame_source_id) return 'first_frame'
  if (source.id && source.id === element.video_last_frame_source_id) return 'last_frame'
  return ''
}

function selectedVideoFrameSourceIDs(element: CanvasElement) {
  const ids = new Set<string>()
  if (element.kind !== 'video' || !canvasSupportsDraft(element)) return ids
  if (element.video_first_frame_source_id) ids.add(element.video_first_frame_source_id)
  if (element.video_last_frame_source_id) ids.add(element.video_last_frame_source_id)
  return ids
}

function videoFrameCandidates(element: CanvasElement) {
  if (element.kind !== 'video' || !canvasSupportsDraft(element)) return []
  return referenceSourceElements(element)
    .filter((source) => source.kind !== 'mask')
    .map((source) => {
      const image = localUploadedImage(source) || taskResultImage(source)
      if (!image?.url) return undefined
      return { id: source.id, label: nodeBadge(source) || elementTitle(source), image }
    })
    .filter(Boolean) as { id: string; label: string; image: UploadedImage }[]
}

function setVideoNodeFrameSource(element: CanvasElement, role: UploadedImage['video_frame_role'], sourceID: string) {
  const normalizedRole = normalizeVideoFrameRole(role)
  if (element.kind !== 'video' || !normalizedRole || !sourceID) return
  if (normalizedRole === 'first_frame') {
    element.video_first_frame_source_id = element.video_first_frame_source_id === sourceID ? '' : sourceID
    if (element.video_last_frame_source_id === element.video_first_frame_source_id) element.video_last_frame_source_id = ''
    return
  }
  element.video_last_frame_source_id = element.video_last_frame_source_id === sourceID ? '' : sourceID
  if (element.video_first_frame_source_id === element.video_last_frame_source_id) element.video_first_frame_source_id = ''
}

function videoNodeFrameRoleForCandidate(element: CanvasElement, sourceID: string) {
  if (sourceID === element.video_first_frame_source_id) return 'first_frame'
  if (sourceID === element.video_last_frame_source_id) return 'last_frame'
  return ''
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
  return connectedInputs(element).find((item) => outputTypes(item).includes('image'))
}

function updateVideoFrameTime(element: CanvasElement, seconds: number) {
  videoFrameTimeByNodeID.value = { ...videoFrameTimeByNodeID.value, [element.id]: seconds }
}

function updateVideoDuration(element: CanvasElement, seconds: number) {
  videoDurationByNodeID.value = { ...videoDurationByNodeID.value, [element.id]: seconds }
}

function videoFrameFilename(element: CanvasElement, seconds: number) {
  const time = String(Math.max(0, seconds)).replace(/\./g, '-')
  return `${nodeBadge(element) || 'VIDEO'}-tail-${time}s.png`
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
    mask.crossOrigin = 'anonymous'
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

function freezeGeneratedElement(element: CanvasElement, task?: Task) {
  if (!isRunnableKind(element.kind) || !hasFrozenResult(element, task)) return
  element.frozen = true
  persistCanvasNow()
}

async function runGenerateNode(element: CanvasElement) {
  syncActiveEditable()
  if (element.kind === 'mask' || element.kind === 'audio' || element.frozen) return
  runningNodeID.value = element.id
  element.generated_params = []
  nodeRunState.value = { ...nodeRunState.value, [element.id]: { status: 'running', startedAt: Date.now() } }
  try {
    if (element.kind === 'tail_frame') {
      await runTailFrameNode(element)
      freezeGeneratedElement(element)
      nodeRunState.value = { ...nodeRunState.value, [element.id]: { ...nodeRunState.value[element.id], status: 'succeeded', endedAt: Date.now() } }
      return
    }
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
        element.generated_params = llmPayloadParamChips(payload)
      }
      if (props.runLlmAction) await props.runLlmAction(payload, applyResult)
      else emit('runLlm', payload, applyResult)
      freezeGeneratedElement(element)
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
      video_draft: canvasVideoDraft(element),
      watermark: false,
    }
    let latestTask: Task | undefined
    const applyTask = (task: Task) => {
      latestTask = task
      element.task_id = task.id
      element.task_snapshot = compactTaskSnapshot(task)
      element.generated_params = task.status === 'succeeded' ? taskParamChips(task) : []
      if (task.status === 'succeeded') freezeGeneratedElement(element, task)
      persistCanvasNow()
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
  if (props.readOnly) return
  if (runningWorkflow.value) return
  runningWorkflow.value = true
  const blockedIDs = frozenUpstreamIDs()
  const workflowIDs = new Set((activeCanvas.value?.elements || []).filter((element) => isRunnableKind(element.kind) && !element.frozen && !blockedIDs.has(element.id) && !isLineBusy(element)).map((element) => element.id))
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
  if (element.frozen) {
    visited.add(element.id)
    return
  }
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
  if (element.frozen) return seen
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

function frozenUpstreamIDs() {
  const blocked = new Set<string>()
  for (const element of activeCanvas.value?.elements || []) {
    if (element.frozen) collectUpstreamIDs(element, blocked)
  }
  return blocked
}

function collectUpstreamIDs(element: CanvasElement, blocked: Set<string>, seen = new Set<string>()) {
  if (seen.has(element.id)) return
  seen.add(element.id)
  for (const source of connectedInputs(element)) {
    if (isRunnableKind(source.kind)) blocked.add(source.id)
    collectUpstreamIDs(source, blocked, seen)
  }
}

async function runTailFrameNode(element: CanvasElement) {
  const sourceElement = connectedInputs(element).find((item) => outputTypes(item).includes('video'))
  let source = sourceElement ? localMediaAsset(sourceElement) || taskResultVideo(sourceElement) : undefined
  if (!source?.url) throw new Error('尾帧节点没有可用的上游视频')
  if (!source.last_frame_url) {
    try {
      const frames = await fetchVideoFrames(source.url, source.filename || videoFrameFilename(sourceElement || element, 0))
      source = { ...source, thumbnail_url: source.thumbnail_url || frames.thumbnail_url || frames.first_frame_url, first_frame_url: frames.first_frame_url, last_frame_url: frames.last_frame_url || frames.first_frame_url }
      if (sourceElement && sourceElement.media_url === source.url) {
        sourceElement.media_thumbnail_url = sourceElement.media_thumbnail_url || source.thumbnail_url || ''
        sourceElement.media_first_frame_url = source.first_frame_url || ''
        sourceElement.media_last_frame_url = source.last_frame_url || ''
      }
    } catch {
      throw new Error('该视频没有可用尾帧，请使用已上传到图库的视频素材')
    }
  }
  if (source.last_frame_url) {
    const filename = videoFrameFilename(sourceElement || element, source.duration || sourceElement?.video_duration || 0)
    element.media_type = 'image'
    element.media_url = source.last_frame_url
    element.media_thumbnail_url = source.last_frame_url
    element.media_filename = filename
    return
  }
  throw new Error('该视频没有可用的尾帧图片')
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
  if (kind === 'tail_frame') return { width: 360, height: 260 }
  if (kind === 'image_media') return { width: 260, height: 220 }
  if (kind === 'video_media') return { width: 360, height: 230 }
  if (kind === 'audio_media') return { width: 280, height: 170 }
  if (kind === 'llm') return { width: 380, height: 240 }
  if (kind === 'mask') return { width: 380, height: 260 }
  if (kind === 'prompt') return { width: 260, height: 140 }
  if (kind === 'merge') return { width: 260, height: 190 }
  if (kind === 'view_control') return { width: 420, height: 470 }
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
  if (kind === 'video' || kind === 'video_media') return 'rgba(2, 132, 199, .92)'
  if (kind === 'image' || kind === 'image_media' || kind === 'mask' || kind === 'tail_frame') return 'rgba(5, 150, 105, .9)'
  if (kind === 'audio' || kind === 'audio_media') return 'rgba(124, 58, 237, .9)'
  if (kind === 'prompt' || kind === 'llm') return 'rgba(180, 83, 9, .9)'
  if (kind === 'view_control') return 'rgba(109, 40, 217, .9)'
  if (kind === 'merge') return 'rgba(71, 85, 105, .88)'
  return 'rgba(51, 65, 85, .88)'
}

function onFlowConnect(connection: Connection) {
  if (props.readOnly) return
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
  if (props.readOnly) return
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
  if (props.readOnly) return
  suppressFlowConnectEnd.value = false
  pendingFlowConnection.value = params.nodeId && (params.handleType === 'source' || params.handleType === 'target')
    ? { nodeId: params.nodeId, handleType: params.handleType }
    : null
}

function onFlowConnectEnd(event?: MouseEvent | TouchEvent) {
  if (props.readOnly) return
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
  if (props.readOnly) return
  let nextSelected = selectedNodeIDs.value
  const selectedNow: string[] = []
  for (const change of changes) {
    if (change.type !== 'select') continue
    if (nextSelected === selectedNodeIDs.value) nextSelected = new Set(selectedNodeIDs.value)
    if (suppressHandleSelectionID.value && change.id === suppressHandleSelectionID.value) {
      nextSelected.delete(change.id)
      continue
    }
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
  modelMenuElementID.value = ''
}

function onFlowNodeDragStart(_event: NodeDragEvent) {
  hideInspectorDuringDrag.value = true
  modelMenuElementID.value = ''
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
  await nextTick()
  hideInspectorDuringDrag.value = false
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
  selectCanvasElement(target)
  hideInspectorDuringDrag.value = true
  dragState.value = { type: 'node', id: target.id, startX: event.clientX, startY: event.clientY, originX: target.x, originY: target.y }
}

function onCanvasNodePointerDown(event: PointerEvent, element: CanvasElement) {
  closeMentionMenu()
  if (spacePanning.value) return
  if (isCanvasHandleTarget(event.target)) {
    suppressHandleSelection(element)
    return
  }
  event.stopPropagation()
  if (event.button === 0) selectCanvasElement(element, event)
  if (event.button !== 0 || !event.altKey) return
  if (!(event.target instanceof HTMLElement) || !event.target.closest('.canvas-node-drag')) return
  event.stopImmediatePropagation()
  startNodeDrag(event, element)
}

function isCanvasHandleTarget(target: EventTarget | null) {
  return target instanceof HTMLElement && Boolean(target.closest('.canvas-flow-handle'))
}

function suppressHandleSelection(element: CanvasElement) {
  suppressHandleSelectionID.value = element.id
  selectedNodeIDs.value = new Set(Array.from(selectedNodeIDs.value).filter((id) => id !== element.id))
  modelMenuElementID.value = ''
  window.setTimeout(() => {
    if (suppressHandleSelectionID.value === element.id) suppressHandleSelectionID.value = ''
  }, 0)
}

function selectCanvasElement(element: CanvasElement, event?: PointerEvent) {
  const additive = Boolean(event?.shiftKey || event?.ctrlKey || event?.metaKey)
  const next = additive ? new Set(selectedNodeIDs.value) : new Set<string>()
  if (additive && next.has(element.id)) next.delete(element.id)
  else next.add(element.id)
  selectedNodeIDs.value = next
  bringElementToFront(element)
  hideInspectorDuringDrag.value = false
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
    video_first_frame_source_id: '',
    video_last_frame_source_id: '',
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
  canvasContextMenu.value = placeContextMenu(event.clientX, event.clientY, targetKinds.map((kind) => ({
      label: elementTitle({ id: '__label__', kind, x: 0, y: 0, width: 0, height: 0 } as CanvasElement),
      icon: contextIconForKind(kind),
      action: () => createConnectedTarget(from, kind, point),
    })))
}

function openConnectionSourceMenu(to: CanvasElement, event: { clientX: number; clientY: number }) {
  const point = screenToWorld(event.clientX, event.clientY)
  const sourceKinds = connectableSourceKinds(to)
  if (!sourceKinds.length) {
    showInvalidSourceNotice(to)
    return
  }
  canvasContextMenu.value = placeContextMenu(event.clientX, event.clientY, sourceKinds.map((kind) => ({
      label: elementTitle({ id: '__label__', kind, x: 0, y: 0, width: 0, height: 0 } as CanvasElement),
      icon: contextIconForKind(kind),
      action: () => createConnectedSource(to, kind, point),
    })))
}

function contextIconForKind(kind: NodeKind) {
  if (kind === 'prompt' || kind === 'llm') return 'text'
  if (kind === 'image' || kind === 'image_media' || kind === 'tail_frame') return 'image'
  if (kind === 'video' || kind === 'video_media') return 'video'
  if (kind === 'audio' || kind === 'audio_media') return 'audio'
  if (kind === 'merge') return 'merge'
  if (kind === 'view_control') return 'compass'
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
  if (kind === 'tail_frame' && (localMediaAsset(from)?.url || taskResultVideo(from)?.url)) {
    void runGenerateNode(target).catch((error) => showCanvasNotice(error instanceof Error ? error.message : '尾帧提取失败'))
  }
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
  if (kind === 'view_control') {
    const minSize = minNodeSize('view_control')
    return {
      id: createID(),
      kind: 'view_control',
      text: '',
      view_azimuth: 30,
      view_elevation: 0,
      view_roll: 0,
      view_distance: 50,
      view_scene_yaw: -28,
      view_scene_pitch: 18,
      view_scene_zoom: 1,
      x: point.x - minSize.width / 2,
      y: point.y - minSize.height / 2,
      width: minSize.width,
      height: minSize.height,
      zIndex: maxCanvasZIndex() + 1,
    }
  }
  if (kind === 'tail_frame') {
    const minSize = minNodeSize('tail_frame')
    return {
      id: createID(),
      kind: 'tail_frame',
      media_type: 'image',
      media_url: '',
      media_thumbnail_url: '',
      media_first_frame_url: '',
      media_last_frame_url: '',
      media_filename: '',
      text: '尾帧',
      x: point.x - minSize.width / 2,
      y: point.y - minSize.height / 2,
      width: minSize.width,
      height: minSize.height,
      zIndex: maxCanvasZIndex() + 1,
    }
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
      media_first_frame_url: '',
      media_last_frame_url: '',
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
  queueDragPoint(event)
}

function queueDragPoint(event: PointerEvent) {
  pendingDragPoint = { clientX: event.clientX, clientY: event.clientY }
  if (dragFrame) return
  dragFrame = window.requestAnimationFrame(() => {
    dragFrame = 0
    flushDragPoint()
  })
}

function flushDragPoint(point = pendingDragPoint) {
  const state = dragState.value
  if (!point || !state || state.type === 'pan' || !activeCanvas.value) return
  pendingDragPoint = null
  const element = elementByID(state.id)
  if (!element) return
  if (state.type === 'node') {
    element.x = state.originX + (point.clientX - state.startX) / camera.zoom
    element.y = state.originY + (point.clientY - state.startY) / camera.zoom
  } else {
    const min = minNodeSize(element.kind)
    element.width = Math.max(min.width, state.originWidth + (point.clientX - state.startX) / camera.zoom)
    element.height = Math.max(min.height, state.originHeight + (point.clientY - state.startY) / camera.zoom)
  }
}

function stopDrag(event?: PointerEvent | Event) {
  if (event instanceof PointerEvent) flushDragPoint({ clientX: event.clientX, clientY: event.clientY })
  else flushDragPoint()
  if (dragFrame) {
    window.cancelAnimationFrame(dragFrame)
    dragFrame = 0
  }
  pendingDragPoint = null
  const state = dragState.value
  dragState.value = null
  hideInspectorDuringDrag.value = false
  if (state?.type === 'node' || state?.type === 'resize') {
    persistCanvasNow()
    queueCanvasHistorySnapshot()
  }
}

function isTransientCanvasMutation() {
  const type = dragState.value?.type
  return type === 'node' || type === 'resize'
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
  focusActiveCanvasElements(160)
}

function focusActiveCanvasElements(duration = 160) {
  const elements = activeCanvas.value?.elements || []
  if (!elements.length) {
    setCanvasViewport({ x: 420, y: 220, zoom: 0.82 }, duration)
    return
  }
  const bounds = elements.reduce((box, element) => {
    const size = renderedNodeSize(element)
    const left = element.x
    const top = element.y
    const right = element.x + size.width
    const bottom = element.y + size.height
    return {
      left: Math.min(box.left, left),
      top: Math.min(box.top, top),
      right: Math.max(box.right, right),
      bottom: Math.max(box.bottom, bottom),
    }
  }, { left: Infinity, top: Infinity, right: -Infinity, bottom: -Infinity })
  const frame = document.querySelector<HTMLElement>('.canvas-flow')?.getBoundingClientRect()
  const assetInset = showAssets.value ? 280 : 0
  const width = Math.max(320, (frame?.width || window.innerWidth) - assetInset)
  const height = Math.max(260, frame?.height || window.innerHeight)
  const padding = width < 720 ? 56 : 104
  const boundsWidth = Math.max(1, bounds.right - bounds.left)
  const boundsHeight = Math.max(1, bounds.bottom - bounds.top)
  const nextZoom = clamp(Math.min((width - padding * 2) / boundsWidth, (height - padding * 2) / boundsHeight), MIN_ZOOM, 1.08)
  const centerX = bounds.left + boundsWidth / 2
  const centerY = bounds.top + boundsHeight / 2
  setCanvasViewport({
    x: width / 2 - centerX * nextZoom,
    y: height / 2 - centerY * nextZoom,
    zoom: nextZoom,
  }, duration)
}

function setCanvasViewport(viewport: ViewportTransform, duration = 0) {
  pan.value = { x: viewport.x, y: viewport.y }
  zoom.value = viewport.zoom
  camera.x = viewport.x
  camera.y = viewport.y
  camera.zoom = viewport.zoom
  flow.setViewport(viewport, { duration })
}

function toggleMiniMap(event?: Event) {
  event?.preventDefault()
  event?.stopPropagation()
  showMiniMap.value = !showMiniMap.value
  if (event) blurControl(event)
}

function viewControlKnobStyle(element: CanvasElement) {
  const point = viewControlCameraPoint(element)
  return {
    opacity: String(point.depth < -0.16 ? 0.58 : 1),
    transform: `translate(calc(-50% + ${point.x}px), calc(-50% + ${point.y}px)) scale(${point.scale}) rotate(${point.azimuth}deg)`,
    zIndex: String(point.depth < 0 ? 4 : 8),
  }
}

function viewControlPreviewStyle(element: CanvasElement) {
  const distance = clamp(element.view_distance ?? 50, 0, 100)
  const scale = 1.1 - distance / 500
  return {
    transform: `translate(-50%, -50%) scale(${scale})`,
  }
}

function viewControlRigStyle(element: CanvasElement) {
  const roll = clamp(element.view_roll ?? 0, -45, 45)
  const yaw = normalizeAzimuth(element.view_scene_yaw ?? -28)
  const pitch = clamp(element.view_scene_pitch ?? 18, -65, 65)
  const sceneZoom = clamp(element.view_scene_zoom ?? 1, 0.72, 1.7)
  return {
    transform: `scale(${sceneZoom}) rotateX(${pitch}deg) rotateY(${yaw}deg) rotateZ(${roll}deg)`,
  }
}

function viewControlSightlineStyle(element: CanvasElement) {
  const point = viewControlCameraPoint(element)
  const length = Math.max(34, Math.hypot(point.x, point.y) - 24)
  const angle = Math.atan2(point.y, point.x) * 180 / Math.PI
  return {
    opacity: String(point.depth < -0.16 ? 0.38 : 0.82),
    width: `${length}px`,
    transform: `rotate(${angle}deg)`,
    zIndex: String(point.depth < 0 ? 3 : 7),
  }
}

function viewControlCameraPoint(element: CanvasElement) {
  const azimuth = normalizeAzimuth(element.view_azimuth ?? 0)
  const elevation = clamp(element.view_elevation ?? 0, -60, 60)
  const azimuthRad = azimuth * Math.PI / 180
  const elevationRad = elevation * Math.PI / 180
  const cosElevation = Math.cos(elevationRad)
  const depth = Math.cos(azimuthRad) * cosElevation
  const x = Math.sin(azimuthRad) * cosElevation * 104
  const y = -Math.sin(elevationRad) * 72 + depth * 14
  return {
    azimuth,
    depth,
    x,
    y,
    scale: 0.76 + (depth + 1) * 0.16,
  }
}

function startViewControlSceneDrag(event: PointerEvent, element: CanvasElement) {
  if (event.button !== 0) return
  if (event.target instanceof HTMLElement && event.target.closest('button, input, .view-control-knob')) return
  event.preventDefault()
  event.stopPropagation()
  ;(event.currentTarget as HTMLElement).setPointerCapture(event.pointerId)
  viewControlDrag.value = {
    type: 'scene',
    elementID: element.id,
    startX: event.clientX,
    startY: event.clientY,
    originYaw: element.view_scene_yaw ?? -28,
    originPitch: element.view_scene_pitch ?? 18,
  }
}

function startViewControlCameraDrag(event: PointerEvent, element: CanvasElement) {
  if (event.button !== 0) return
  event.preventDefault()
  event.stopPropagation()
  const stage = (event.currentTarget as HTMLElement).closest<HTMLElement>('.canvas-view-control-stage')
  if (!stage) return
  stage.setPointerCapture(event.pointerId)
  viewControlDrag.value = { type: 'camera', elementID: element.id, target: stage }
  updateViewControlFromPointer(event, element, stage)
}

function dragViewControl(event: PointerEvent, element: CanvasElement) {
  const state = viewControlDrag.value
  if (!state || state.elementID !== element.id) return
  event.preventDefault()
  event.stopPropagation()
  if (state.type === 'scene') {
    element.view_scene_yaw = normalizeAzimuth(state.originYaw + (event.clientX - state.startX) * 0.7)
    element.view_scene_pitch = clamp(state.originPitch - (event.clientY - state.startY) * 0.5, -65, 65)
    return
  }
  updateViewControlFromPointer(event, element, state.target)
}

function stopViewControlDrag(event?: PointerEvent) {
  if (event?.currentTarget instanceof HTMLElement && event.currentTarget.hasPointerCapture(event.pointerId)) {
    event.currentTarget.releasePointerCapture(event.pointerId)
  }
  viewControlDrag.value = null
}

function updateViewControlFromPointer(event: PointerEvent, element: CanvasElement, target: HTMLElement) {
  const rect = target.getBoundingClientRect()
  const centerX = rect.left + rect.width / 2
  const centerY = rect.top + rect.height / 2
  const dx = event.clientX - centerX
  const dy = event.clientY - centerY
  element.view_azimuth = normalizeAzimuth(Math.round(clamp(dx / Math.max(1, rect.width * 0.42), -1, 1) * 180))
  element.view_elevation = clamp(Math.round(clamp(-dy / Math.max(1, rect.height * 0.36), -1, 1) * 60), -60, 60)
}

function nudgeViewControl(element: CanvasElement, axis: 'azimuth' | 'elevation' | 'roll', delta: number) {
  if (axis === 'azimuth') element.view_azimuth = normalizeAzimuth(Math.round((element.view_azimuth ?? 0) + delta))
  if (axis === 'elevation') element.view_elevation = clamp(Math.round((element.view_elevation ?? 0) + delta), -60, 60)
  if (axis === 'roll') element.view_roll = clamp(Math.round((element.view_roll ?? 0) + delta), -45, 45)
}

function setViewControlAzimuth(element: CanvasElement, value: number) {
  element.view_azimuth = normalizeAzimuth(value)
}

function updateViewControlPose(element: CanvasElement, value: { azimuth: number; elevation: number; distance: number }) {
  element.view_azimuth = normalizeAzimuth(value.azimuth)
  element.view_elevation = clamp(Math.round(value.elevation), -60, 60)
  element.view_distance = clamp(Math.round(value.distance), 0, 100)
}

function resetViewControl(element: CanvasElement) {
  element.view_azimuth = 30
  element.view_elevation = 0
  element.view_roll = 0
  element.view_distance = 50
  element.view_scene_yaw = -28
  element.view_scene_pitch = 18
  element.view_scene_zoom = 1
}

function zoomViewControlScene(event: WheelEvent, element: CanvasElement) {
  event.preventDefault()
  event.stopPropagation()
  const current = element.view_scene_zoom ?? 1
  const next = current * Math.exp(-event.deltaY * 0.0018)
  element.view_scene_zoom = clamp(Number(next.toFixed(3)), 0.72, 1.7)
}

function normalizeAzimuth(value: number) {
  const normalized = ((value + 180) % 360 + 360) % 360 - 180
  return Object.is(normalized, -0) ? 0 : normalized
}

function onMiniMapClick(event: { position: { x: number; y: number } }) {
  flow.setCenter(event.position.x, event.position.y, { zoom: camera.zoom, duration: 160 })
}

function onCanvasPointerDownCapture(event: PointerEvent) {
  closeMentionMenuFromPointer(event)
  if (event.button !== 0 || spacePanning.value) return
  if (event.target instanceof HTMLElement && event.target.closest('.canvas-topbar, .canvas-assets-fab, .canvas-minimap-toggle, .canvas-vueflow-minimap, .asset-sidebar, .canvas-inspector, .context-menu')) return
}

function clamp(value: number, min: number, max: number) {
  return Math.min(max, Math.max(min, value))
}
</script>

<template>
  <section class="canvas-workspace" :class="{ 'space-panning': spacePanning, 'zen-mode': zenMode, 'assets-open': showAssets, 'read-only': readOnly }" @click="canvasContextMenu = null; activeCanvasDrawer = ''" @contextmenu.prevent.stop="openCanvasContextMenu" @wheel.capture="onCanvasWheel" @pointerdown.capture="onCanvasPointerDownCapture" @pointermove="onPointerMove" @pointerup="stopDrag" @pointercancel="stopDrag">
    <div class="canvas-topbar glass-panel" :class="{ 'zen-collapsed': zenMode }" @pointerdown.stop>
      <div class="canvas-console-track">
        <div class="canvas-console-content" :aria-hidden="zenMode">
          <div class="canvas-drawer-tabs">
            <button type="button" :class="{ active: activeCanvasDrawer === 'board' }" title="画布管理" @click="toggleCanvasDrawer('board', $event)"><AppIcon name="gallery" /><span>画布</span></button>
            <button type="button" :class="{ active: activeCanvasDrawer === 'nodes' }" title="节点工具" @click="toggleCanvasDrawer('nodes', $event)"><AppIcon name="sparkles" /><span>节点</span></button>
            <button type="button" :class="{ active: activeCanvasDrawer === 'view' }" title="视图工具" @click="toggleCanvasDrawer('view', $event)"><AppIcon name="resetView" /><span>视图</span></button>
            <button type="button" class="canvas-save-tab" :class="[`is-${canvasSaveState}`, { active: activeCanvasDrawer === 'save' }]" :title="canvasSaveTitle" @click="toggleCanvasDrawer('save', $event)"><span aria-hidden="true"></span><strong>{{ canvasSaveLabel }}</strong></button>
          </div>
          <div class="canvas-switcher canvas-console-group canvas-group-board" :class="drawerGroupClass('board')">
            <InlineSelect class="toolbar-status-select canvas-board-select" label="画布" :model-value="activeCanvasID" :options="canvasOptions" @update:model-value="activeCanvasID = $event" />
            <template v-if="!readOnly">
              <button type="button" title="新建画布" @click="createCanvas"><AppIcon name="add" /></button>
              <button type="button" title="重命名画布" @click="openRenameCanvas"><AppIcon name="pencil" /></button>
              <button type="button" :title="activeCanvasShared ? '取消广场分享' : '分享到广场'" :disabled="!activeCanvas" @click="toggleActiveCanvasShare"><AppIcon :name="activeCanvasShared ? 'eyeOff' : 'share'" /></button>
              <button type="button" title="更新广场分享" :disabled="!activeCanvas || !activeCanvasShared" @click="shareActiveCanvas"><AppIcon name="upload" /></button>
              <button type="button" title="删除画布" :disabled="canvases.length <= 1" @click="openDeleteCanvas"><AppIcon name="close" /></button>
            </template>
          </div>
          <div class="canvas-save-status canvas-console-group canvas-group-save" :class="[`is-${canvasSaveState}`, drawerGroupClass('save')]" :title="canvasSaveTitle">
            <span aria-hidden="true"></span>
            <strong>{{ canvasSaveLabel }}</strong>
            <small>{{ canvasSaveDetail }}</small>
          </div>
          <span class="canvas-tool-divider" aria-hidden="true"></span>
          <div v-if="!readOnly" class="canvas-switcher canvas-node-tools canvas-console-group canvas-group-nodes" :class="drawerGroupClass('nodes')">
            <button type="button" title="添加文字提示词" @click="addPromptNode()"><AppIcon name="text" /></button>
            <button type="button" title="添加媒体节点" @click="addAssetNode()"><AppIcon name="gallery" /></button>
            <button type="button" title="添加汇合节点" @click="addMergeNode()"><AppIcon name="merge" /></button>
            <button type="button" title="添加 AI 生成节点" @click="addAiNode()"><AppIcon name="sparkles" /></button>
            <button type="button" class="canvas-run-button" title="按连接顺序运行整张画布" :disabled="runningWorkflow" @click="runCanvasWorkflow"><AppIcon :name="runningWorkflow ? 'stop' : 'play'" /></button>
          </div>
          <span class="canvas-tool-divider" aria-hidden="true"></span>
          <div class="canvas-zoom-controls canvas-console-group canvas-group-view" :class="drawerGroupClass('view')">
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

    <VueFlow id="canvas-flow" class="canvas-flow" :nodes="flowNodes" :edges="flowEdges" :min-zoom="MIN_ZOOM" :max-zoom="MAX_ZOOM" :default-viewport="{ x: pan.x, y: pan.y, zoom }" :nodes-draggable="!spacePanning && !readOnly" :pan-on-drag="false" :elevate-nodes-on-select="false" :elevate-edges-on-select="true" :default-edge-options="flowDefaultEdgeOptions" :connection-line-options="flowConnectionLineOptions" :connection-mode="ConnectionMode.Strict" :is-valid-connection="isValidFlowConnection" :connect-on-click="!readOnly" :edges-updatable="!readOnly" :edges-focusable="!readOnly" :nodes-focusable="true" pan-activation-key-code="Space" :selection-key-code="true" :select-nodes-on-drag="!readOnly" :zoom-on-scroll="false" :delete-key-code="readOnly ? null : 'Delete'" @connect="onFlowConnect" @connect-start="onFlowConnectStart" @connect-end="onFlowConnectEnd" @connectStart="onFlowConnectStart" @connectEnd="onFlowConnectEnd" @edge-update="onFlowEdgeUpdate" @edgeUpdate="onFlowEdgeUpdate" @nodes-change="onFlowNodesChange" @edges-change="onFlowEdgesChange" @pane-click="onFlowPaneClick" @node-drag-start="onFlowNodeDragStart" @node-drag-stop="onFlowNodeDragStop" @edge-context-menu="openEdgeContextMenu" @selection-context-menu="openSelectionContextMenu" @viewport-change="onFlowViewportChange">
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
            data-placeholder="输入这里要使用的文字提示词，可输入 @ 引用连接节点素材..." @pointerdown.stop="selectCanvasElement(element, $event)"
            @mousedown.stop @click.stop @wheel.stop @input="onPromptTextInput($event, element)"
            @keyup="onPromptTextInput($event, element)" @keydown="onRichEditorKeydown($event, element)"
            @compositionstart="onEditorCompositionStart($event, element)"
            @compositionend="onEditorCompositionEnd($event, element)"
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
            <div class="canvas-node-summary compact">
              <span>提示词 {{ inputSummaryCounts(element).prompts }}</span>
              <span>图片 {{ inputSummaryCounts(element).images }}</span>
              <span>视频 {{ inputSummaryCounts(element).videos }}</span>
              <span>音频 {{ inputSummaryCounts(element).audios }}</span>
            </div>
          </div>
        </div>

        <div v-else-if="element.kind === 'view_control'" class="canvas-view-control-node" @pointerdown.stop>
          <ViewControl3D
            :image-url="displayImageURL(viewControlSourceImage(element)) || originalImageURL(viewControlSourceImage(element))"
            :azimuth="element.view_azimuth ?? 0"
            :elevation="element.view_elevation ?? 0"
            :distance="element.view_distance ?? 50"
            :roll="element.view_roll ?? 0"
            @change="updateViewControlPose(element, $event)"
          />
          <div class="canvas-view-control-fields">
            <label>
              <span>方位 {{ Math.round(element.view_azimuth ?? 0) }}°</span>
              <input v-model.number="element.view_azimuth" type="range" min="-180" max="180" />
            </label>
            <label>
              <span>俯仰 {{ Math.round(element.view_elevation ?? 0) }}°</span>
              <input v-model.number="element.view_elevation" type="range" min="-60" max="60" />
            </label>
            <label>
              <span>滚转 {{ Math.round(element.view_roll ?? 0) }}°</span>
              <input v-model.number="element.view_roll" type="range" min="-45" max="45" />
            </label>
            <label>
              <span>距离 {{ Math.round(element.view_distance ?? 50) }}%</span>
              <input v-model.number="element.view_distance" type="range" min="0" max="100" />
            </label>
          </div>
          <div class="canvas-view-presets">
            <button type="button" :class="{ active: Math.abs(normalizeAzimuth(element.view_azimuth ?? 0)) < 8 }" @click="setViewControlAzimuth(element, 0)">正面</button>
            <button type="button" :class="{ active: Math.abs(normalizeAzimuth(element.view_azimuth ?? 0) + 90) < 8 }" @click="setViewControlAzimuth(element, -90)">左侧</button>
            <button type="button" :class="{ active: Math.abs(Math.abs(normalizeAzimuth(element.view_azimuth ?? 0)) - 180) < 8 }" @click="setViewControlAzimuth(element, 180)">背面</button>
            <button type="button" :class="{ active: Math.abs(normalizeAzimuth(element.view_azimuth ?? 0) - 90) < 8 }" @click="setViewControlAzimuth(element, 90)">右侧</button>
          </div>
          <div class="canvas-view-control-footer">
            <span>{{ viewControlDirectionLabel(element) }}</span>
            <button type="button" title="重置视角" @click="resetViewControl(element)"><AppIcon name="resetView" :size="13" /></button>
          </div>
        </div>

        <div v-else-if="element.kind === 'tail_frame'" class="canvas-tail-frame-node">
          <div v-if="localUploadedImage(element)?.url" class="canvas-zoomable-image" @wheel="zoomNodeImage($event, element)"
            @pointerdown="startNodeImagePan($event, element)" @pointermove="moveNodeImagePan($event, element)"
            @pointerup="stopNodeImagePan" @pointercancel="stopNodeImagePan">
            <div v-if="canvasImageStatus(element, localUploadedImage(element)?.url) !== 'loaded'" class="canvas-image-placeholder"
              :class="{ error: canvasImageStatus(element, localUploadedImage(element)?.url) === 'error' }">
              {{ canvasImageStatus(element, localUploadedImage(element)?.url) === 'error' ? '图片加载失败' : '图片加载中' }}
            </div>
            <img :src="localUploadedImage(element)?.url" alt="尾帧图片" :style="imageZoomStyle(element)" draggable="false" crossorigin="anonymous"
              @load="markCanvasImageLoaded(element, localUploadedImage(element)?.url)"
              @error="markCanvasImageError(element, localUploadedImage(element)?.url)" />
          </div>
          <div v-else class="canvas-tail-frame-empty">
            <span>{{ nodeRuntime(element)?.status === 'running' ? '正在提取尾帧...' : '连接视频后运行，自动输出尾帧图片' }}</span>
          </div>
        </div>

        <div v-else-if="element.kind === 'mask'" class="canvas-generate-node canvas-mask-node"
          @pointerenter="enterMaskNode(element)" @pointerleave="leaveMaskNode(element)">
          <div v-if="maskSourceImage(element)" class="canvas-mask-editor"
            :class="[`tool-${element.mask_tool || 'brush'}`, { panning: imagePanState?.elementID === element.id }]"
            @pointerdown.stop @wheel="zoomNodeImage($event, element)" @pointerenter="activeMaskElementID = element.id"
            @pointerleave="hideMaskCursor(element)">
            <img :src="originalImageURL(maskSourceImage(element))" alt="蒙版上游图片" crossorigin="anonymous"
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
            :data-node-id="element.id" data-placeholder="运行后展示、编辑 LLM 输出，可输入 @ 引用连接节点素材..." @pointerdown.stop="selectCanvasElement(element, $event)"
            @wheel.stop @input="onPromptTextInput($event, element)" @keyup="onPromptTextInput($event, element)"
            @compositionstart="onEditorCompositionStart($event, element)"
            @compositionend="onEditorCompositionEnd($event, element)"
            @blur="syncEditableText($event, element)" @keydown="onRichEditorKeydown($event, element)"
            v-html="renderEditableText(element.text)"></div>
          <div v-else class="canvas-result-preview">
            <template v-if="generatedTask(element)?.status === 'succeeded'">
              <CanvasVideoPlayer v-if="element.kind === 'video'"
                :src="generatedTask(element)?.result_videos?.[0]?.url"
                @time="updateVideoFrameTime(element, $event)"
                @duration="updateVideoDuration(element, $event)" />
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
                <img :src="originalImageURL(generatedTask(element)?.result_images?.[0])" alt="生成结果" crossorigin="anonymous"
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
          <div v-if="generatedParamChips(element).length" class="canvas-generated-params">
            <span v-for="param in generatedParamChips(element)" :key="param">{{ param }}</span>
          </div>
          <div class="canvas-node-summary">
            <span>提示词 {{ inputSummaryCounts(element).prompts }}</span>
            <span>图片 {{ inputSummaryCounts(element).images }}</span>
            <span>视频 {{ inputSummaryCounts(element).videos }}</span>
            <span>音频 {{ inputSummaryCounts(element).audios }}</span>
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
              <img :src="element.media_url" alt="画布素材" decoding="async" :style="imageZoomStyle(element)" draggable="false" crossorigin="anonymous"
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
            <img :src="originalImageURL(taskForElement(element)!.result_images?.[0])" alt="生成素材" crossorigin="anonymous"
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

    <div v-if="renameDialog" class="modal-backdrop canvas-modal-backdrop" @click.self="closeRenameCanvas" @wheel.self.prevent.stop>
      <section class="canvas-rename-modal light-modal" @keydown.esc.prevent="closeRenameCanvas">
        <button class="modal-close" @click="closeRenameCanvas"><AppIcon name="close" /></button>
        <h2>重命名画布</h2>
        <p class="settings-hint">给当前画布换一个更容易识别的名字。</p>
        <label>画布名称<input ref="renameInput" v-model="renameDialog.value" type="text" maxlength="40" placeholder="画布名称" @keydown.enter.prevent="confirmRenameCanvas" /></label>
        <div class="modal-actions-row">
          <button class="cancel" @click="closeRenameCanvas"><AppIcon name="close" />取消</button>
          <button class="confirm" :disabled="!renameDialog.value.trim()" @click="confirmRenameCanvas"><AppIcon name="check" />保存</button>
        </div>
      </section>
    </div>

    <div v-if="deleteDialog" class="modal-backdrop canvas-modal-backdrop" @click.self="closeDeleteCanvas" @wheel.self.prevent.stop>
      <section class="canvas-delete-modal light-modal" @keydown.esc.prevent="closeDeleteCanvas">
        <button class="modal-close" @click="closeDeleteCanvas"><AppIcon name="close" /></button>
        <h2>删除画布</h2>
        <p class="settings-hint">确定删除“{{ deleteDialog.name }}”？这个画布里的节点和连线会一起删除。</p>
        <div class="modal-actions-row">
          <button class="cancel" @click="closeDeleteCanvas"><AppIcon name="close" />取消</button>
          <button class="confirm danger" @click="confirmDeleteCanvas"><AppIcon name="trash" />删除</button>
        </div>
      </section>
    </div>

    <aside v-if="inspectedElement && hasInspectorPanel(inspectedElement.kind) && !hideInspectorDuringDrag" class="canvas-inspector glass-panel" :style="inspectorStyle" @pointerdown.stop @click.stop @contextmenu.prevent.stop>
      <template v-for="element in [inspectedElement]" :key="element.id">
        <header class="canvas-inspector-head compact">
          <span>{{ nodeBadge(element) || element.kind.toUpperCase() }}</span>
          <strong>{{ elementTitle(element) }}</strong>
        </header>

        <section v-if="element.kind === 'image' || element.kind === 'video'" class="canvas-setting-section primary">
          <div class="canvas-model-picker">
            <button type="button" class="canvas-model-current" :class="{ open: modelMenuOpen }" @click="toggleModelMenu(element)">
              <span>模型</span>
              <strong>{{ modelOptionLabel(canvasNodeModel(element)) }}</strong>
            </button>
            <div v-if="modelMenuOpen" class="canvas-model-menu">
              <button
                v-for="model in canvasModelOptions(element)"
                :key="model"
                type="button"
                class="canvas-model-option"
                :class="{ active: (element.model || canvasNodeModel(element)) === model }"
                @click="selectCanvasModel(element, model)"
              >
                <strong>{{ modelOptionLabel(model) }}</strong>
                <small>{{ model }}</small>
              </button>
            </div>
          </div>
        </section>

        <section v-if="element.kind === 'image'" class="canvas-setting-section">
          <div class="canvas-inline-title">
            <strong>分辨率</strong>
          </div>
          <div v-if="!isNanoBananaElement(element) && !isSeedreamElement(element)" class="canvas-pill-grid">
            <button
              v-for="option in gptImageSizeBaseOptions"
              :key="option.value"
              type="button"
              class="canvas-pill-choice"
              :class="{ active: gptImageSizeBase(element) === option.value }"
              :title="optionHint('size', option.value)"
              @click="updateGptImageSizeBase(element, option.value)"
            >
              {{ option.label }}
            </button>
          </div>
          <div v-else-if="isNanoBananaElement(element)" class="canvas-pill-grid">
            <button
              v-for="option in nanoBananaSizeBaseOptions"
              :key="option.value"
              type="button"
              class="canvas-pill-choice"
              :class="{ active: nanoImageSize(element) === option.value }"
              :title="optionHint('size', option.value)"
              @click="updateNanoImageSize(element, option.value)"
            >
              {{ option.label }}
            </button>
          </div>
          <div v-else class="canvas-pill-grid">
            <button
              v-for="option in seedreamSizeBaseOptions"
              :key="option.value"
              type="button"
              class="canvas-pill-choice"
              :class="{ active: seedreamImageSize(element) === option.value }"
              :title="optionHint('size', option.value)"
              @click="updateSeedreamImageSize(element, option.value)"
            >
              {{ option.label }}
            </button>
          </div>
        </section>

        <section v-if="element.kind === 'image'" class="canvas-setting-section">
          <div class="canvas-inline-title">
            <strong>比例</strong>
          </div>
          <div v-if="!isNanoBananaElement(element) && !isSeedreamElement(element)" class="canvas-ratio-grid" :class="{ disabled: gptImageSizeBase(element) === 'auto' }">
            <button
              v-for="ratio in ratioOptions"
              :key="ratio"
              type="button"
              class="canvas-ratio-card"
              :class="{ active: gptImageRatio(element) === ratio }"
              :disabled="gptImageSizeBase(element) === 'auto'"
              @click="updateGptImageRatio(element, ratio)"
            >
              <span class="canvas-ratio-preview"><i :style="ratioPreviewStyle(ratio)"></i></span>
              <strong>{{ ratio }}</strong>
            </button>
          </div>
          <div v-else-if="isNanoBananaElement(element)" class="canvas-ratio-grid">
            <button
              v-for="ratio in nanoBananaRatios"
              :key="ratio"
              type="button"
              class="canvas-ratio-card"
              :class="{ active: nanoAspectRatio(element) === ratio }"
              @click="updateNanoAspectRatio(element, ratio)"
            >
              <span class="canvas-ratio-preview" :class="{ auto: ratio === 'auto' }"><i :style="ratioPreviewStyle(ratio)"></i></span>
              <strong>{{ ratio }}</strong>
            </button>
          </div>
          <div v-else class="canvas-ratio-grid">
            <button
              v-for="ratio in seedreamRatios"
              :key="ratio"
              type="button"
              class="canvas-ratio-card"
              :class="{ active: seedreamAspectRatio(element) === ratio }"
              @click="updateSeedreamAspectRatio(element, ratio)"
            >
              <span class="canvas-ratio-preview" :class="{ auto: ratio === 'auto' }"><i :style="ratioPreviewStyle(ratio)"></i></span>
              <strong>{{ ratio }}</strong>
            </button>
          </div>
        </section>

        <section v-if="element.kind === 'image' && !isNanoBananaElement(element) && !isSeedreamElement(element)" class="canvas-setting-section">
          <div class="canvas-inline-title">
            <strong>质量</strong>
          </div>
          <div class="canvas-pill-grid">
            <button v-for="quality in ['auto', 'high', 'medium', 'low']" :key="quality" type="button" class="canvas-pill-choice" :class="{ active: (element.quality || props.defaultForm.quality) === quality }" :title="optionHint('quality', quality)" @click="element.quality = quality">
              {{ optionLabel(quality) }}
            </button>
          </div>
        </section>

        <section v-if="element.kind === 'image' && !isNanoBananaElement(element)" class="canvas-setting-section">
          <div class="canvas-inline-title">
            <strong>输出</strong>
          </div>
          <div class="canvas-pill-grid">
            <button v-for="format in canvasOutputFormatOptions(element)" :key="format" type="button" class="canvas-pill-choice" :class="{ active: (element.output_format || props.defaultForm.output_format) === format }" :title="optionHint('format', format)" @click="updateCanvasOutputFormat(element, format)">
              {{ optionLabel(format) }}
            </button>
          </div>
        </section>

        <section v-if="element.kind === 'image' && !isNanoBananaElement(element) && !isSeedreamElement(element)" class="canvas-setting-section advanced">
          <div class="canvas-inline-title">
            <strong>高级</strong>
          </div>
          <label v-if="supportsOutputCompression(element)" class="canvas-slider-field">
            <span><strong>压缩</strong><em>{{ element.output_compression ?? props.defaultForm.output_compression ?? 80 }}</em></span>
            <input v-model.number="element.output_compression" type="range" min="0" max="100" />
          </label>
          <div class="canvas-mini-row">
            <span>审核</span>
            <div class="canvas-pill-grid two">
              <button v-for="moderation in ['low', 'auto']" :key="moderation" type="button" class="canvas-pill-choice" :class="{ active: (element.moderation || props.defaultForm.moderation) === moderation }" :title="optionHint('moderation', moderation)" @click="element.moderation = moderation">
                {{ optionLabel(moderation) }}
              </button>
            </div>
          </div>
          <div class="canvas-mini-row">
            <span>保真</span>
            <div class="canvas-pill-grid two">
              <button v-for="fidelity in ['high', 'low']" :key="fidelity" type="button" class="canvas-pill-choice" :class="{ active: (element.input_fidelity || props.defaultForm.input_fidelity) === fidelity }" :title="optionHint('fidelity', fidelity)" @click="element.input_fidelity = fidelity">
                {{ optionLabel(fidelity) }}
              </button>
            </div>
          </div>
        </section>

        <section v-if="element.kind === 'video'" class="canvas-setting-section">
          <div class="canvas-inline-title">
            <strong>视频规格</strong>
          </div>
          <div class="canvas-ratio-grid">
            <button v-for="ratio in canvasVideoRatios(element)" :key="ratio" type="button" class="canvas-ratio-card" :class="{ active: (element.video_ratio || canvasVideoCapability(element).defaultRatio) === ratio }" @click="element.video_ratio = ratio">
              <span class="canvas-ratio-preview" :class="{ auto: ratio === 'adaptive' }"><i :style="ratioPreviewStyle(ratio)"></i></span>
              <strong>{{ videoRatioLabel(ratio) }}</strong>
            </button>
          </div>
          <div class="canvas-pill-grid">
            <button v-for="resolution in canvasVideoResolutions(element)" :key="resolution" type="button" class="canvas-pill-choice" :class="{ active: (element.video_resolution || props.defaultForm.video_resolution) === resolution }" :title="optionHint('resolution', resolution)" @click="element.video_resolution = resolution">
              {{ resolution.toUpperCase() }}
            </button>
          </div>
          <div class="canvas-readonly-field">
            <span>尺寸</span>
            <strong>{{ canvasVideoSizeLabel(element) }}</strong>
          </div>
          <label class="canvas-number-field">
            <span>时长</span>
            <input v-model.number="element.video_duration" type="number" :min="canvasVideoCapability(element).duration.min" :max="canvasVideoCapability(element).duration.max" />
          </label>
          <button type="button" class="canvas-toggle-choice" :class="{ active: canvasGenerateAudio(element) }" @click="element.generate_audio = !canvasGenerateAudio(element)">
            <span>
              <strong>生成音频</strong>
              <small>{{ canvasGenerateAudio(element) ? '随视频生成声音' : '仅生成画面' }}</small>
            </span>
            <i class="canvas-switch-indicator" aria-hidden="true"></i>
          </button>
          <button v-if="canvasSupportsDraft(element)" type="button" class="canvas-toggle-choice" :class="{ active: canvasVideoDraft(element) }" @click="toggleCanvasVideoDraft(element)">
            <span>
              <strong>样片模式</strong>
              <small>draft</small>
            </span>
            <i class="canvas-switch-indicator" aria-hidden="true"></i>
          </button>
        </section>

        <section v-if="element.kind === 'video' && canvasSupportsDraft(element)" class="canvas-setting-section">
          <div class="canvas-inline-title">
            <strong>首尾帧</strong>
            <small>{{ videoFrameCandidates(element).length ? '从上游图片选择' : '连接图片节点后选择' }}</small>
          </div>
          <div v-if="videoFrameCandidates(element).length" class="canvas-frame-source-list">
            <div v-for="candidate in videoFrameCandidates(element)" :key="candidate.id" class="canvas-frame-source-row">
              <span>{{ candidate.label }}</span>
              <div class="canvas-frame-source-actions">
                <button type="button" class="canvas-pill-choice" :class="{ active: videoNodeFrameRoleForCandidate(element, candidate.id) === 'first_frame' }" @click="setVideoNodeFrameSource(element, 'first_frame', candidate.id)">首帧</button>
                <button type="button" class="canvas-pill-choice" :class="{ active: videoNodeFrameRoleForCandidate(element, candidate.id) === 'last_frame' }" @click="setVideoNodeFrameSource(element, 'last_frame', candidate.id)">尾帧</button>
              </div>
            </div>
          </div>
          <p v-else class="canvas-empty-hint">连接图片节点后选择首尾帧</p>
        </section>

        <section v-if="element.kind === 'llm'" class="canvas-setting-section">
          <div class="canvas-inline-title">
            <strong>文字模型</strong>
          </div>
          <div class="canvas-model-picker">
            <button type="button" class="canvas-model-current" :class="{ open: modelMenuOpen }" @click="toggleModelMenu(element)">
              <span>模型</span>
              <strong>{{ element.model || '请选择模型' }}</strong>
            </button>
            <div v-if="modelMenuOpen" class="canvas-model-menu">
              <button v-for="model in props.models" :key="model" type="button" class="canvas-model-option" :class="{ active: element.model === model }" @click="element.model = model; modelMenuElementID = ''">
                <strong>{{ model }}</strong>
                <small>语言模型</small>
              </button>
            </div>
          </div>
          <div class="canvas-pill-grid canvas-reasoning-grid">
            <button
              v-for="reasoning in [
                { value: 'none', label: '关闭', hint: '直接输出' },
                { value: 'low', label: '低', hint: '轻量思考' },
                { value: 'medium', label: '中', hint: '均衡推理' },
                { value: 'high', label: '高', hint: '更深推理' }
              ]"
              :key="reasoning.value"
              type="button"
              class="canvas-pill-choice canvas-reasoning-choice"
              :class="{ active: (element.reasoning_effort || 'none') === reasoning.value }"
              :title="reasoning.hint"
              @click="element.reasoning_effort = reasoning.value"
            >
              <strong>{{ reasoning.label }}</strong>
              <small>{{ reasoning.hint }}</small>
            </button>
          </div>
        </section>

        <section v-if="element.kind === 'audio'" class="canvas-setting-section">
          <div class="canvas-inline-title">
            <strong>音频模型</strong>
          </div>
          <div class="canvas-readonly-field">
            <span>模型</span>
            <strong>敬请期待</strong>
          </div>
        </section>
      </template>
    </aside>

    <Transition :name="assetsClosingForZen ? 'canvas-assets-zen-close' : 'canvas-assets-slide'">
      <aside v-if="showAssets" class="asset-sidebar glass-panel" :class="{ 'closing-for-zen': assetsClosingForZen }" @pointerdown.stop>
        <div class="asset-sidebar-fixed">
          <div class="canvas-sidebar-head">
            <strong>素材</strong>
            <small>{{ assetTotal || usableTasks.length }} 条</small>
            <button type="button" title="刷新素材" :disabled="assetLoading" @click="refreshAssetPage"><AppIcon name="refresh" :size="14" /></button>
          </div>
          <form class="asset-search" @submit.prevent="submitAssetSearch">
            <input v-model="assetSearch" type="search" placeholder="提示词、模型、图片、视频/音频" />
            <button type="submit" :disabled="assetLoading"><AppIcon name="search" :size="14" />搜索</button>
          </form>
        </div>
        <div class="asset-list-scroll">
          <button v-for="task in visibleAssetTasks" :key="task.id" type="button" class="asset-row"
            :title="assetPromptTitle(task)" @click="addTask(task)">
            <span v-if="isVideoTask(task)" class="asset-video-thumb">
              <img v-if="taskVideoCover(task)" :src="taskVideoCover(task)" alt="视频封面" crossorigin="anonymous" />
              <span v-else class="asset-prompt-icon">视频</span>
              <span class="asset-video-play" aria-hidden="true"><AppIcon name="play" :size="14" /></span>
            </span>
            <span v-else-if="firstAudioAsset(task)" class="asset-prompt-icon">音频</span>
            <img v-else :src="originalImageURL(task.result_images?.[0])" alt="素材" crossorigin="anonymous" />
            <span>
              <strong>{{ assetLabel(task) }}</strong>
              <small>{{ task.prompt || task.model }}</small>
            </span>
          </button>
          <div v-if="assetError" class="asset-error">{{ assetError }}</div>
          <div v-if="assetLoading && !visibleAssetTasks.length" class="asset-empty">加载中...</div>
          <div v-else-if="!visibleAssetTasks.length" class="asset-empty">没有匹配素材</div>
        </div>
        <div class="asset-pagination">
          <button type="button" :disabled="!canPrevAssetPage || assetLoading" @click="previousAssetPage">上一页</button>
          <span>{{ assetPageIndex + 1 }} / {{ assetPageCount }}</span>
          <button type="button" :disabled="!canNextAssetPage || assetLoading" @click="nextAssetPage">{{ assetLoading ? '加载中' : '下一页' }}</button>
        </div>
      </aside>
    </Transition>
  </section>
</template>


