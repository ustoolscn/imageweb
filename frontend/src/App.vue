<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { APIError, createTask, deleteTask, fetchModels, fetchSiteBrand, fetchTaskUpdates, getTask, listPlazaItems, listTasks, retryTask, runLLM, setPlazaLike, setTaskFavorite, shareTask, unshareTask, uploadImage } from './api'
import AdminContactModal from './components/AdminContactModal.vue'
import AppIcon from './components/AppIcon.vue'
import AppToolbar from './components/AppToolbar.vue'
import CanvasWorkspace from './components/CanvasWorkspace.vue'
import Composer from './components/Composer.vue'
import ImageViewer from './components/ImageViewer.vue'
import PlazaDetailModal from './components/PlazaDetailModal.vue'
import PlazaGrid from './components/PlazaGrid.vue'
import SettingsModal from './components/SettingsModal.vue'
import SizeModal from './components/SizeModal.vue'
import SourceModal from './components/SourceModal.vue'
import TaskDetailModal from './components/TaskDetailModal.vue'
import TaskGrid from './components/TaskGrid.vue'
import { nanoBananaRatios, nanoBananaSizeBaseOptions, nanoBananaSizeValue, parseNanoBananaSize, parseSeedreamSize, ratioOptions, seedreamRatios, seedreamSizeBaseOptions, seedreamSizeValue, sizeBaseOptions, sizeFromRatio, type SizeBase } from './lib/sizes'
import { normalizeVideoSettings, videoResolutionFromSize, videoSizeFor } from './lib/videoModels'
import { canOpenSource, canShareTask, isFavorite, maskBaseURL } from './lib/view'
import type { CreateTaskPayload, MediaAsset, PlazaItem, Task, UploadedImage } from './types'
import type { CanvasLLMPayload, CanvasRunPayload, ImageForm, PendingReferenceAudio, PendingReferenceImage, PendingReferenceVideo, PreviewImage, SettingsPayload, ThemeMode, ViewMode, AppliedThemeMode } from './uiTypes'

const MASK_EDIT_MAX_SIDE = 1600
const MASK_EDIT_MAX_DPR = 1.5
const imageModels = ['gpt-image-2', 'nano-banana-2', 'doubao-seedream-5.0-lite']
const NANO_BANANA_MODEL = 'nano-banana-2'
const SEEDREAM_MODEL = 'doubao-seedream-5.0-lite'

const savedModel = localStorage.getItem('image_web_model') || 'gpt-image-2'
const savedTheme = parseSavedTheme(localStorage.getItem('image_web_theme'))
const baseurl = ref(localStorage.getItem('image_web_baseurl') || '')
const apikey = ref(localStorage.getItem('image_web_apikey') || '')
const tasks = ref<Task[]>([])
const totalTasks = ref(0)
const plazaItems = ref<PlazaItem[]>([])
const totalPlazaItems = ref(0)
const viewMode = ref<ViewMode>('tasks')
const canvasZenMode = ref(false)
const hideCanvasForCompact = ref(false)
const themeMode = ref<ThemeMode>(savedTheme)
const systemThemeMode = ref<AppliedThemeMode>(getSystemThemeMode())
const plazaSort = ref<'time' | 'likes'>('time')
const plazaClientID = ref(localStorage.getItem('image_web_plaza_client_id') || '')
const models = ref<string[]>([...imageModels])
const siteTitle = ref('图片生成工作台')
const siteIcon = ref('AI')
const sizeAccess = reactive({ allow2K: true, allow4K: true })
const status = ref('all')
const keyword = ref('')
const plazaKeyword = ref('')
const favoriteOnly = ref(false)
const loading = ref(false)
const loadingMore = ref(false)
const hasMoreTasks = ref(false)
const hasMorePlazaItems = ref(false)
const nextTaskBeforeCreatedAt = ref('')
const nextTaskBeforeID = ref('')
const nextPlazaBeforeCreatedAt = ref('')
const nextPlazaBeforeID = ref('')
const nextPlazaBeforeLikeCount = ref(0)
const submitting = ref(false)
const baseURLBlocked = ref(false)
const adminContactImage = ref('')
const showAdminContact = ref(false)
const message = ref('')
const clock = ref(Date.now())
const showMobileComposer = ref(false)
const showSettingsModal = ref(false)
const settingsDraft = reactive({ baseurl: baseurl.value, apikey: apikey.value })
const showSizeModal = ref(false)
const showReferenceVideoModal = ref(false)
const showReferenceAudioModal = ref(false)
const videoURLDraft = ref('')
const audioURLDraft = ref('')
const addingReferenceVideo = ref(false)
const addingReferenceAudio = ref(false)
const selectedTask = ref<Task | null>(null)
const selectedPlazaItem = ref<PlazaItem | null>(null)
const sourceTask = ref<Task | null>(null)
const previewImage = ref<PreviewImage | null>(null)
const contextMenu = ref<{ x: number; y: number; items: Array<{ label: string; action: () => void; disabled?: boolean; danger?: boolean }> } | null>(null)
const maskCanvas = ref<HTMLCanvasElement | null>(null)
const maskBaseImage = ref<HTMLImageElement | null>(null)
const maskDrawing = ref(false)
const maskTool = ref<'brush' | 'eraser'>('brush')
const maskBrushSize = ref(36)
const lastMaskPoint = ref<{ x: number; y: number } | null>(null)
const activeMaskPointer = ref<{ pointerId: number; canvas: HTMLCanvasElement } | null>(null)
const referenceMaskFiles = ref<Array<File | null>>([])
const referenceMaskPreviews = ref<Array<string | null>>([])
let pollTimer: number | undefined
let clockTimer: number | undefined
let systemThemeQuery: MediaQueryList | undefined
let compactCanvasQuery: MediaQueryList | undefined

function parseSavedTheme(value: string | null): ThemeMode {
  return value === 'light' || value === 'dark' || value === 'system' ? value : 'system'
}

function getSystemThemeMode(): AppliedThemeMode {
  return window.matchMedia?.('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

function syncSystemThemeMode(event?: MediaQueryListEvent) {
  systemThemeMode.value = event?.matches ? 'dark' : getSystemThemeMode()
}

function syncCompactCanvasMode(event?: MediaQueryListEvent) {
  hideCanvasForCompact.value = event?.matches ?? Boolean(window.matchMedia?.('(max-width: 820px)').matches)
}

const form = reactive<ImageForm>({
  task_type: 'image_generation',
  prompt: '',
  model: imageModels.includes(savedModel) ? savedModel : 'gpt-image-2',
  size: savedModel === NANO_BANANA_MODEL ? '1K 1:1' : '1024x1024',
  quality: 'auto',
  output_format: 'png',
  output_compression: 100,
  background: 'auto',
  moderation: 'low',
  input_fidelity: 'high',
  n: 1,
  video_ratio: '16:9',
  video_resolution: '720p',
  video_width: 1280,
  video_height: 720,
  video_duration: 5,
  generate_audio: true,
  watermark: false,
  reference_video_urls: '',
  reference_audio_urls: '',
})

const sizeDraft = reactive<{ base: SizeBase; ratio: string }>({
  base: 'auto',
  ratio: '1:1',
})
const pendingSizeSync = ref(form.size)

const referenceImages = ref<PendingReferenceImage[]>([])
const reusedReferenceImages = ref<UploadedImage[]>([])
const referenceVideos = ref<PendingReferenceVideo[]>([])
const referenceAudios = ref<PendingReferenceAudio[]>([])

const hasConfig = computed(() => Boolean(baseurl.value && apikey.value))
const runningCount = computed(() => tasks.value.filter((task) => task.status === 'pending' || task.status === 'running').length)
const visibleSubtitle = computed(() => viewMode.value === 'plaza' ? `公开广场 · 已加载 ${plazaItems.value.length} 条 · 总计 ${totalPlazaItems.value} 条` : (hasConfig.value ? `${maskBaseURL(baseurl.value)} · 已加载 ${tasks.value.length} 条 · 总计 ${totalTasks.value} 条` : '通过 URL 传入 baseurl 和 apikey 后开始使用'))
const draftSize = computed(() => sizeFromRatio(sizeDraft.base, sizeDraft.ratio))
const isNanoBananaForm = computed(() => form.model === NANO_BANANA_MODEL)
const isSeedreamForm = computed(() => form.model === SEEDREAM_MODEL)
const currentDraftSize = computed(() => isNanoBananaForm.value ? nanoBananaSizeValue(sizeDraft.base, sizeDraft.ratio) : isSeedreamForm.value ? seedreamSizeValue(sizeDraft.base, sizeDraft.ratio) : draftSize.value)
const currentRatioOptions = computed(() => isNanoBananaForm.value ? [...nanoBananaRatios] : isSeedreamForm.value ? [...seedreamRatios] : ratioOptions)
const currentSizeBaseOptions = computed(() => isNanoBananaForm.value ? nanoBananaSizeBaseOptions.filter((option) => {
  if (option.value === '2K') return sizeAccess.allow2K
  if (option.value === '4K') return sizeAccess.allow4K
  return true
}) : isSeedreamForm.value ? seedreamSizeBaseOptions.filter((option) => {
  if (option.value === '2K') return sizeAccess.allow2K
  if (option.value === '4K') return sizeAccess.allow4K
  return true
}) : availableSizeBaseOptions.value)
const availableSizeBaseOptions = computed(() => sizeBaseOptions.filter((option) => {
  if (option.value === '2K') return sizeAccess.allow2K
  if (option.value === '4K') return sizeAccess.allow4K
  return true
}))
const appliedThemeMode = computed<AppliedThemeMode>(() => themeMode.value === 'system' ? systemThemeMode.value : themeMode.value)

watch(() => form.model, (model) => {
  localStorage.setItem('image_web_model', model)
})

watch(themeMode, (theme) => {
  localStorage.setItem('image_web_theme', theme)
}, { immediate: true })

watch(viewMode, (mode) => {
  if (mode !== 'canvas') canvasZenMode.value = false
  if (mode === 'canvas' && hideCanvasForCompact.value) viewMode.value = 'tasks'
})

watch(hideCanvasForCompact, (hidden) => {
  if (hidden && viewMode.value === 'canvas') viewMode.value = 'tasks'
})

function openContextMenu(event: MouseEvent, items: Array<{ label: string; action: () => void; disabled?: boolean; danger?: boolean }>) {
  event.preventDefault()
  contextMenu.value = { x: event.clientX, y: event.clientY, items }
}

function closeContextMenu() {
  contextMenu.value = null
}

function runContextAction(item: { action: () => void; disabled?: boolean }) {
  if (item.disabled) return
  contextMenu.value = null
  item.action()
}

function openTaskContextMenu(task: Task, event: MouseEvent) {
  openContextMenu(event, [
    { label: '查看详情', action: () => { selectedTask.value = task } },
    { label: '复用配置', action: () => reuseTask(task) },
    { label: '重新生成', action: () => rerunTask(task) },
    { label: isFavorite(task) ? '取消收藏' : '收藏', action: () => toggleFavorite(task) },
    { label: task.shared_to_plaza ? '取消广场分享' : '分享到广场', action: () => toggleTaskShare(task), disabled: !canShareTask(task) },
    { label: '查看源数据', action: () => openSourceTask(task), disabled: !canOpenSource(task) },
    { label: '删除记录', action: () => removeTask(task), danger: true },
  ])
}

function openSettings() {
  settingsDraft.baseurl = baseurl.value
  settingsDraft.apikey = apikey.value
  showSettingsModal.value = true
}

async function saveSettings(settings?: SettingsPayload) {
  if (settings) {
    settingsDraft.baseurl = settings.baseurl
    settingsDraft.apikey = settings.apikey
  }
  baseurl.value = settingsDraft.baseurl.trim()
  apikey.value = settingsDraft.apikey.trim()
  localStorage.setItem('image_web_baseurl', baseurl.value)
  localStorage.setItem('image_web_apikey', apikey.value)
  baseURLBlocked.value = false
  adminContactImage.value = ''
  showAdminContact.value = false
  showSettingsModal.value = false
  await refreshSiteBrand()
  if (hasConfig.value) {
    await loadModels()
    await resetTasks()
    startPolling()
    startClock()
  }
  showMessage('配置已保存')
}

function toggleTheme() {
  if (themeMode.value === 'system') themeMode.value = 'light'
  else if (themeMode.value === 'light') themeMode.value = 'dark'
  else themeMode.value = 'system'
}

function toggleFavoriteOnly() {
  favoriteOnly.value = !favoriteOnly.value
  resetTasks()
}

function updateFormField(field: keyof ImageForm, value: string | number | boolean) {
  if (field === 'task_type') {
    form.task_type = value === 'video_generation' ? 'video_generation' : 'image_generation'
    form.model = form.task_type === 'video_generation' ? 'doubao-seedance-2.0' : 'gpt-image-2'
    if (form.task_type === 'video_generation') normalizeVideoForm()
    return
  }
  if (field === 'generate_audio' || field === 'watermark') {
    form[field] = Boolean(value)
    return
  }
  if (field === 'output_compression' || field === 'n' || field === 'video_duration') {
    form[field] = Number(value)
    if (field === 'video_duration') normalizeVideoForm()
    return
  }
  const text = String(value)
  if (field === 'video_resolution') {
    form.video_resolution = text === '480p' || text === '1080p' || text === '720p' ? text : '720p'
    normalizeVideoForm()
    return
  }
  if (field === 'video_ratio') {
    form.video_ratio = text
    normalizeVideoForm()
    return
  }
  if (field === 'prompt' || field === 'model' || field === 'size' || field === 'quality' || field === 'output_format' || field === 'background' || field === 'moderation' || field === 'input_fidelity' || field === 'reference_video_urls' || field === 'reference_audio_urls') {
    form[field] = text
    if (field === 'model') syncModelSize(text)
    if (field === 'output_format' && form.model === SEEDREAM_MODEL && text === 'webp') form.output_format = 'jpeg'
    if (field === 'output_format' && text !== 'png' && form.background === 'transparent') form.background = 'auto'
    if (field === 'model' && form.task_type === 'video_generation') normalizeVideoForm()
  }
}

function syncModelSize(model: string) {
  if (model === NANO_BANANA_MODEL) {
    const parsed = parseNanoBananaSize(form.size)
    sizeDraft.base = parsed.imageSize as SizeBase
    sizeDraft.ratio = parsed.aspectRatio
    form.size = nanoBananaSizeValue(sizeDraft.base, sizeDraft.ratio)
    pendingSizeSync.value = form.size
    return
  }
  if (model === SEEDREAM_MODEL) {
    const parsed = parseSeedreamSize(form.size)
    sizeDraft.base = parsed.imageSize as SizeBase
    sizeDraft.ratio = parsed.aspectRatio
    form.size = seedreamSizeValue(sizeDraft.base, sizeDraft.ratio)
    form.output_format = form.output_format === 'png' ? 'png' : 'jpeg'
    pendingSizeSync.value = form.size
    return
  }
  if (isNanoBananaSize(form.size) || isSeedreamSize(form.size)) {
    form.size = '1024x1024'
    syncSizeDraft(form.size)
  }
}

function isNanoBananaSize(size: string) {
  return /^(512|1K|2K|4K) \d+:\d+$/.test(size)
}

function isSeedreamSize(size: string) {
  const parsed = parseSeedreamSize(size)
  return seedreamSizeValue(parsed.imageSize, parsed.aspectRatio) === size
}

function ensurePlazaClientID() {
  if (!plazaClientID.value) {
    plazaClientID.value = createClientID()
    localStorage.setItem('image_web_plaza_client_id', plazaClientID.value)
  }
  return plazaClientID.value
}

function createClientID() {
  if (crypto?.randomUUID) return crypto.randomUUID()
  const bytes = new Uint8Array(16)
  if (crypto?.getRandomValues) crypto.getRandomValues(bytes)
  else for (let index = 0; index < bytes.length; index++) bytes[index] = Math.floor(Math.random() * 256)
  bytes[6] = (bytes[6] & 0x0f) | 0x40
  bytes[8] = (bytes[8] & 0x3f) | 0x80
  const hex = [...bytes].map((byte) => byte.toString(16).padStart(2, '0')).join('')
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
}

async function toggleFavorite(task: Task, event?: Event) {
  event?.stopPropagation()
  try {
    const updated = await setTaskFavorite(task.id, apikey.value, baseurl.value, !task.favorite)
    patchTask(updated)
    if (selectedTask.value?.id === updated.id) selectedTask.value = updated
    showMessage(updated.favorite ? '已收藏' : '已取消收藏')
    if (favoriteOnly.value && !updated.favorite) await refreshTasks()
  } catch (error) {
    showMessage(error instanceof Error ? error.message : '收藏更新失败')
  }
}

function patchTask(updated: Partial<Task> & { id: string }) {
  const index = tasks.value.findIndex((task) => task.id === updated.id)
  const patched = index >= 0 ? { ...tasks.value[index], ...updated } : selectedTask.value?.id === updated.id ? { ...selectedTask.value, ...updated } : null
  if (index >= 0 && patched) tasks.value[index] = patched
  if (selectedTask.value?.id === updated.id && patched) selectedTask.value = patched
}

function openPreviewImage(url: string, label: string, event?: Event, maskUrl = '') {
  event?.stopPropagation()
  previewImage.value = { url, label, maskUrl }
}

function openEditablePreview(source: 'reused' | 'new', index: number, url: string, label: string, event?: Event) {
  event?.stopPropagation()
  const maskUrl = source === 'reused' ? reusedReferenceImages.value[index]?.mask_url || '' : referenceImages.value[index]?.mask_url || referenceMaskPreviews.value[index] || ''
  previewImage.value = { url, label, maskUrl, editable: true, source, index }
  maskTool.value = 'brush'
  nextTick(() => loadMaskCanvas())
}

function closePreviewImage() {
  previewImage.value = null
}

function setMaskBaseImage(element: HTMLImageElement | null) {
  maskBaseImage.value = element
}

function setMaskCanvas(element: HTMLCanvasElement | null) {
  maskCanvas.value = element
}

function maskPreviewURL(maskUrl: string) {
  return `/api/mask-preview?${new URLSearchParams({ url: maskUrl })}`
}

async function openSourceTask(task: Task, event?: Event) {
  event?.stopPropagation()
  if (!canOpenSource(task)) return
  try {
    sourceTask.value = await getTask(task.id, apikey.value, baseurl.value)
  } catch (error) {
    showMessage(error instanceof Error ? error.message : '源数据加载失败')
  }
}

onMounted(() => {
  systemThemeQuery = window.matchMedia?.('(prefers-color-scheme: dark)')
  systemThemeQuery?.addEventListener('change', syncSystemThemeMode)
  compactCanvasQuery = window.matchMedia?.('(max-width: 820px)')
  compactCanvasQuery?.addEventListener('change', syncCompactCanvasMode)
  syncSystemThemeMode()
  syncCompactCanvasMode()
  loadConfigFromURL()
  refreshSiteBrand()
  syncSizeDraft(form.size)
  window.addEventListener('resize', syncMaskCanvasDisplaySize)
  if (hasConfig.value) {
    loadModels()
    refreshTasks()
    startPolling()
    startClock()
  }
  window.addEventListener('scroll', onPageScroll, { passive: true })
})

onUnmounted(() => {
  if (pollTimer) window.clearInterval(pollTimer)
  if (clockTimer) window.clearInterval(clockTimer)
  systemThemeQuery?.removeEventListener('change', syncSystemThemeMode)
  compactCanvasQuery?.removeEventListener('change', syncCompactCanvasMode)
  window.removeEventListener('resize', syncMaskCanvasDisplaySize)
  window.removeEventListener('scroll', onPageScroll)
  revokePreviews()
})

function loadConfigFromURL() {
  const url = new URL(window.location.href)
  const nextBaseURL = url.searchParams.get('baseurl') || ''
  const nextAPIKey = url.searchParams.get('apikey') || ''
  let changed = false
  if (nextBaseURL) {
    baseurl.value = nextBaseURL
    baseURLBlocked.value = false
    adminContactImage.value = ''
    showAdminContact.value = false
    localStorage.setItem('image_web_baseurl', nextBaseURL)
    url.searchParams.delete('baseurl')
    changed = true
  }
  if (nextAPIKey) {
    apikey.value = nextAPIKey
    localStorage.setItem('image_web_apikey', nextAPIKey)
    url.searchParams.delete('apikey')
    changed = true
  }
  if (changed) {
    const clean = url.pathname + (url.searchParams.toString() ? `?${url.searchParams}` : '') + url.hash
    window.history.replaceState({}, '', clean)
  }
}

async function refreshSiteBrand() {
  try {
    const brand = await fetchSiteBrand(baseurl.value)
    siteTitle.value = brand.title || '图片生成工作台'
    siteIcon.value = brand.icon || 'AI'
    sizeAccess.allow2K = brand.allow_2k !== false
    sizeAccess.allow4K = brand.allow_4k !== false
    syncSizeDraft(pendingSizeSync.value)
  } catch {
    siteTitle.value = '图片生成工作台'
    siteIcon.value = 'AI'
    sizeAccess.allow2K = true
    sizeAccess.allow4K = true
  }
  applySiteBrandMeta()
}

function applySiteBrandMeta() {
  document.title = siteTitle.value
  const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]') || document.createElement('link')
  link.rel = 'icon'
  link.href = iconHref(siteIcon.value)
  document.head.appendChild(link)
}

function iconHref(value: string) {
  if (value.startsWith('http://') || value.startsWith('https://')) return value
  const text = (value || 'AI').slice(0, 4)
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect width="64" height="64" rx="16" fill="#5b8cff"/><text x="32" y="39" text-anchor="middle" font-size="22" font-family="Arial, sans-serif" font-weight="700" fill="white">${escapeHTML(text)}</text></svg>`
  return `data:image/svg+xml,${encodeURIComponent(svg)}`
}

function escapeHTML(value: string) {
  return value.replace(/[&<>"]/g, (char) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[char] || char))
}

async function loadModels() {
  if (!hasConfig.value || baseURLBlocked.value) return
  try {
    const result = await fetchModels(baseurl.value, apikey.value)
    const ids = result.data?.map((item) => item.id).filter(Boolean) || []
    const nextModels = [...imageModels, 'doubao-seedance-2.0']
    for (const id of ids) {
      if (id && !nextModels.includes(id)) nextModels.push(id)
    }
    models.value = nextModels
    form.model = form.task_type === 'video_generation' ? 'doubao-seedance-2.0' : (imageModels.includes(form.model) ? form.model : 'gpt-image-2')
    syncModelSize(form.model)
    if (form.task_type === 'video_generation') normalizeVideoForm()
  } catch (error) {
    handleRequestError(error, '模型加载失败')
  }
}

async function refreshTasks(limit = Math.max(30, tasks.value.length)) {
  if (!apikey.value || baseURLBlocked.value) return
  loading.value = true
  try {
    const result = await listTasks(apikey.value, baseurl.value, status.value, keyword.value, favoriteOnly.value, '', '', limit)
    tasks.value = result.data
    totalTasks.value = result.total
    hasMoreTasks.value = result.has_more
    nextTaskBeforeCreatedAt.value = result.next_before_created_at
    nextTaskBeforeID.value = result.next_before_id
  } catch (error) {
    handleRequestError(error, '任务加载失败')
  } finally {
    loading.value = false
  }
}

async function resetTasks() {
  tasks.value = []
  hasMoreTasks.value = false
  nextTaskBeforeCreatedAt.value = ''
  nextTaskBeforeID.value = ''
  await refreshTasks(30)
}

async function refreshPlazaItems(limit = Math.max(30, plazaItems.value.length)) {
  loading.value = true
  try {
    const result = await listPlazaItems(plazaSort.value, ensurePlazaClientID(), plazaKeyword.value, '', '', 0, limit)
    plazaItems.value = result.data
    totalPlazaItems.value = result.total
    hasMorePlazaItems.value = result.has_more
    nextPlazaBeforeCreatedAt.value = result.next_before_created_at
    nextPlazaBeforeID.value = result.next_before_id
    nextPlazaBeforeLikeCount.value = result.next_before_like_count
  } catch (error) {
    showMessage(error instanceof Error ? error.message : '广场加载失败')
  } finally {
    loading.value = false
  }
}

async function resetPlazaItems() {
  plazaItems.value = []
  hasMorePlazaItems.value = false
  nextPlazaBeforeCreatedAt.value = ''
  nextPlazaBeforeID.value = ''
  nextPlazaBeforeLikeCount.value = 0
  await refreshPlazaItems(30)
}

async function loadMoreTasks() {
  if (!apikey.value || baseURLBlocked.value || loading.value || loadingMore.value || !hasMoreTasks.value) return
  loadingMore.value = true
  try {
    const result = await listTasks(apikey.value, baseurl.value, status.value, keyword.value, favoriteOnly.value, nextTaskBeforeCreatedAt.value, nextTaskBeforeID.value)
    const existing = new Set(tasks.value.map((task) => task.id))
    tasks.value.push(...result.data.filter((task) => !existing.has(task.id)))
    totalTasks.value = result.total
    hasMoreTasks.value = result.has_more
    nextTaskBeforeCreatedAt.value = result.next_before_created_at
    nextTaskBeforeID.value = result.next_before_id
  } catch (error) {
    handleRequestError(error, '任务加载失败')
  } finally {
    loadingMore.value = false
  }
}

async function loadMorePlazaItems() {
  if (loading.value || loadingMore.value || !hasMorePlazaItems.value) return
  loadingMore.value = true
  try {
    const result = await listPlazaItems(plazaSort.value, ensurePlazaClientID(), plazaKeyword.value, nextPlazaBeforeCreatedAt.value, nextPlazaBeforeID.value, nextPlazaBeforeLikeCount.value)
    const existing = new Set(plazaItems.value.map((item) => item.id))
    plazaItems.value.push(...result.data.filter((item) => !existing.has(item.id)))
    totalPlazaItems.value = result.total
    hasMorePlazaItems.value = result.has_more
    nextPlazaBeforeCreatedAt.value = result.next_before_created_at
    nextPlazaBeforeID.value = result.next_before_id
    nextPlazaBeforeLikeCount.value = result.next_before_like_count
  } catch (error) {
    showMessage(error instanceof Error ? error.message : '广场加载失败')
  } finally {
    loadingMore.value = false
  }
}

function startPolling() {
  if (pollTimer) window.clearInterval(pollTimer)
  pollTimer = window.setInterval(() => {
    refreshUnfinishedTasks()
  }, runningCount.value > 0 ? 2500 : 8000)
}

async function refreshUnfinishedTasks() {
  if (!apikey.value || baseURLBlocked.value) return
  const ids = tasks.value.filter((task) => task.status === 'pending' || task.status === 'running').map((task) => task.id)
  if (!ids.length) return
  try {
    const updates = await fetchTaskUpdates(apikey.value, baseurl.value, ids)
    for (const update of updates) patchTask(update)
  } catch (error) {
    handleRequestError(error, '任务更新失败')
  }
}

function onPageScroll() {
  const remaining = document.documentElement.scrollHeight - window.scrollY - window.innerHeight
  if (remaining >= 520) return
  if (viewMode.value === 'plaza') loadMorePlazaItems()
  else loadMoreTasks()
}

function stopPolling() {
  if (!pollTimer) return
  window.clearInterval(pollTimer)
  pollTimer = undefined
}

function handleRequestError(error: unknown, fallback: string) {
  const text = error instanceof Error ? error.message : fallback
  if (error instanceof APIError && error.code === 'baseurl_not_authorized') {
    baseURLBlocked.value = true
    adminContactImage.value = error.adminContactImage || ''
    stopPolling()
    return
  }
  showMessage(text)
}

function startClock() {
  clockTimer = window.setInterval(() => {
    clock.value = Date.now()
  }, 1000)
}

async function submitTask(mode: ImageForm['task_type'] = form.task_type) {
  if (!hasConfig.value) {
    showMessage('请通过 URL 传入 baseurl 和 apikey')
    return
  }
  if (!form.prompt.trim()) {
    showMessage('请输入提示词')
    return
  }
  submitting.value = true
  try {
    if (referenceImages.value.some((image) => image.uploading)) {
      showMessage('参考图还在上传，请稍后提交')
      return
    }
    const failedImage = referenceImages.value.find((image) => image.upload_error)
    if (failedImage) {
      showMessage(failedImage.upload_error || '参考图上传失败，请删除后重新上传')
      return
    }
    const reference_images: UploadedImage[] = [
      ...reusedReferenceImages.value.map((image) => ({ ...image })),
      ...referenceImages.value.map(({ preview_url, uploading, upload_error, ...image }) => ({ ...image })),
    ]
    const isVideo = mode === 'video_generation'
    if (isVideo) normalizeVideoForm()
    const taskType = isVideo ? 'video_generation' : 'image_generation'
    const taskCount = 1
    const createdTasks: Task[] = []
    for (let index = 0; index < taskCount; index++) {
      const basePayload: Pick<CreateTaskPayload, 'apikey' | 'baseurl' | 'task_type' | 'prompt' | 'model' | 'reference_images'> = {
        apikey: apikey.value,
        baseurl: baseurl.value,
        task_type: taskType,
        prompt: form.prompt,
        model: form.model,
        reference_images,
      }
      const videoSettings = normalizeVideoSettings({
        model: form.model,
        ratio: form.video_ratio,
        resolution: form.video_resolution,
        duration: form.video_duration,
      })
      const createPayload: CreateTaskPayload = isVideo ? {
        ...basePayload,
        reference_videos: referenceVideos.value.map(toVideoAsset),
        reference_audios: referenceAudios.value.map(toAudioAsset),
        video_ratio: videoSettings.ratio,
        video_width: videoSettings.width,
        video_height: videoSettings.height,
        video_duration: videoSettings.duration,
        generate_audio: form.generate_audio,
        watermark: form.watermark,
      } : {
        ...basePayload,
        size: form.size,
        quality: form.quality,
        output_format: form.output_format,
        output_compression: Number(form.output_compression),
        background: form.background,
        moderation: form.moderation,
        input_fidelity: form.input_fidelity,
        n: 1,
      }
      console.info('[task-submit] createTask payload', createPayload)
      const created = await createTask(createPayload)
      console.info('[task-submit] created task', {
        id: created.id,
        task_type: created.task_type,
        model: created.model,
        video_ratio: created.video_ratio,
        video_width: created.video_width,
        video_height: created.video_height,
        video_duration: created.video_duration,
      })
      createdTasks.push(created)
    }
    form.prompt = ''
    if (!isVideo) {
      referenceImages.value = []
      reusedReferenceImages.value = []
      clearFileInputs()
      revokePreviews()
    }
    const existing = new Set(tasks.value.map((task) => task.id))
    tasks.value.unshift(...createdTasks.filter((task) => !existing.has(task.id)))
    totalTasks.value += createdTasks.length
    await refreshUnfinishedTasks()
    showMessage('任务已提交，生成会在后台继续执行')
  } catch (error) {
    showMessage(error instanceof Error ? error.message : '提交失败')
  } finally {
    submitting.value = false
  }
}

async function runCanvasNode(payload: CanvasRunPayload, applyTask?: (task: Task) => void) {
  console.info('[canvas-run-node] payload', {
    node_kind: payload.node_kind,
    task_type: payload.task_type,
    model: payload.model,
    video_ratio: payload.video_ratio,
    video_resolution: payload.video_resolution,
    video_duration: payload.video_duration,
    reference_images: payload.reference_images.length,
    reference_videos: payload.reference_videos.length,
    reference_audios: payload.reference_audios.length,
  })
  if (!hasConfig.value) {
    showMessage('请先配置 baseurl 和 apikey')
    return
  }
  if (!payload.prompt.trim()) {
    showMessage('生成节点缺少提示词')
    return
  }
  try {
    const isVideo = payload.node_kind === 'video'
    const videoSettings = normalizeVideoSettings({
      model: payload.model,
      ratio: payload.video_ratio,
      resolution: payload.video_resolution,
      duration: payload.video_duration,
    })
    const referenceImages = await prepareCanvasReferenceImages(payload.reference_images)
    const taskType: ImageForm['task_type'] = isVideo ? 'video_generation' : 'image_generation'
    const basePayload: Pick<CreateTaskPayload, 'apikey' | 'baseurl' | 'node_kind' | 'task_type' | 'prompt' | 'model' | 'reference_images'> = {
      apikey: apikey.value,
      baseurl: baseurl.value,
      node_kind: payload.node_kind,
      task_type: taskType,
      prompt: payload.prompt,
      model: payload.model,
      reference_images: referenceImages,
    }
    const createPayload: CreateTaskPayload = isVideo ? {
      ...basePayload,
      reference_videos: payload.reference_videos,
      reference_audios: payload.reference_audios,
      video_ratio: videoSettings.ratio,
      video_width: videoSettings.width,
      video_height: videoSettings.height,
      video_duration: videoSettings.duration,
      generate_audio: payload.generate_audio,
      watermark: payload.watermark,
    } : {
      ...basePayload,
      size: payload.size,
      quality: payload.quality,
      output_format: payload.output_format,
      output_compression: Number(payload.output_compression),
      background: payload.background,
      moderation: payload.moderation,
      input_fidelity: payload.input_fidelity,
      n: 1,
    }
    console.info('[canvas-run-node] createTask payload', createPayload)
    const created = await createTask(createPayload)
    console.info('[canvas-run-node] created task', {
      id: created.id,
      task_type: created.task_type,
      model: created.model,
      status: created.status,
      video_ratio: created.video_ratio,
      video_width: created.video_width,
      video_height: created.video_height,
      video_duration: created.video_duration,
    })
    if (!tasks.value.some((task) => task.id === created.id)) {
      tasks.value.unshift(created)
      totalTasks.value += 1
    }
    applyTask?.(created)
    const completed = await waitForCanvasTask(created.id)
    console.info('[canvas-run-node] completed task', {
      id: completed.id,
      task_type: completed.task_type,
      model: completed.model,
      status: completed.status,
      result_images: completed.result_images?.length || 0,
      result_videos: completed.result_videos?.length || 0,
      first_image: completed.result_images?.[0]?.url || '',
      first_video: completed.result_videos?.[0]?.url || '',
      request_json: completed.request_json,
      response_json: completed.response_json,
    })
    applyTask?.(completed)
    showMessage('画布生成节点已提交')
  } catch (error) {
    showMessage(error instanceof Error ? error.message : '画布节点提交失败')
  }
}

async function waitForCanvasTask(id: string) {
  let latest = tasks.value.find((task) => task.id === id)
  for (;;) {
    if (latest?.status === 'succeeded' || latest?.status === 'failed') return latest
    await delay(2500)
    latest = await getTask(id, apikey.value, baseurl.value)
    patchTask(latest)
  }
}

function delay(ms: number) {
  return new Promise((resolve) => window.setTimeout(resolve, ms))
}

async function runCanvasLLM(payload: CanvasLLMPayload, applyResult?: (text: string) => void) {
  if (!hasConfig.value) {
    showMessage('请先配置 baseurl 和 apikey')
    return ''
  }
  if (!payload.prompt.trim() && !payload.reference_images.length && !payload.reference_videos.length && !payload.reference_audios.length) {
    showMessage('LLM 节点缺少上游参数')
    return ''
  }
  try {
    const result = await runLLM({
      apikey: apikey.value,
      baseurl: baseurl.value,
      model: payload.model || form.model,
      reasoning_effort: payload.reasoning_effort || 'low',
      prompt: payload.prompt,
      reference_images: payload.reference_images,
      reference_videos: payload.reference_videos,
      reference_audios: payload.reference_audios,
    })
    showMessage('LLM 节点已运行')
    applyResult?.(result.text || '')
  } catch (error) {
    showMessage(error instanceof Error ? error.message : 'LLM 节点运行失败')
  }
}

function toVideoAsset(video: PendingReferenceVideo): MediaAsset {
  return {
    type: 'video',
    url: video.url,
    thumbnail_url: video.cover_url || video.thumbnail_url,
    filename: video.filename,
    duration: video.duration,
    width: video.width,
    height: video.height,
  }
}

function toAudioAsset(audio: PendingReferenceAudio): MediaAsset {
  return {
    type: 'audio',
    url: audio.url,
    filename: audio.filename,
    duration: audio.duration,
  }
}

async function removeTask(task: Task) {
  if (!confirm('确定删除这个任务记录吗？')) return
  await deleteTask(task.id, apikey.value, baseurl.value)
  if (selectedTask.value?.id === task.id) selectedTask.value = null
  await refreshTasks()
}

async function rerunTask(task: Task) {
  await retryTask(task.id, apikey.value, baseurl.value)
  await refreshTasks()
  showMessage('已创建重新生成任务')
}

async function toggleTaskShare(task: Task, event?: Event) {
  event?.stopPropagation()
  if (!canShareTask(task)) return
  try {
    if (task.shared_to_plaza) {
      await unshareTask(task.id, apikey.value, baseurl.value)
      patchTask({ id: task.id, shared_to_plaza: false })
      plazaItems.value = plazaItems.value.filter((item) => item.task_id !== task.id)
      totalPlazaItems.value = Math.max(0, totalPlazaItems.value - 1)
      if (selectedTask.value?.id === task.id) selectedTask.value = { ...selectedTask.value, shared_to_plaza: false }
      if (selectedPlazaItem.value?.task_id === task.id) selectedPlazaItem.value = null
      showMessage('已从广场取消分享')
    } else {
      const item = await shareTask(task.id, apikey.value, baseurl.value)
      patchTask({ id: task.id, shared_to_plaza: true })
      if (selectedTask.value?.id === task.id) selectedTask.value = { ...selectedTask.value, shared_to_plaza: true }
      if (viewMode.value === 'plaza') await refreshPlazaItems()
      else if (!plazaItems.value.some((current) => current.id === item.id)) totalPlazaItems.value += 1
      showMessage('已分享到广场')
    }
  } catch (error) {
    showMessage(error instanceof Error ? error.message : (task.shared_to_plaza ? '取消分享失败' : '分享失败'))
  }
}

async function togglePlazaLike(item: PlazaItem, event?: Event) {
  event?.stopPropagation()
  try {
    const updated = await setPlazaLike(item.id, ensurePlazaClientID(), !item.liked)
    const index = plazaItems.value.findIndex((current) => current.id === updated.id)
    if (index >= 0) plazaItems.value[index] = updated
    if (selectedPlazaItem.value?.id === updated.id) selectedPlazaItem.value = updated
    showMessage(updated.liked ? '已点赞' : '已取消点赞')
  } catch (error) {
    showMessage(error instanceof Error ? error.message : '点赞失败')
  }
}

async function prepareCanvasReferenceImages(images: UploadedImage[]) {
  const prepared: UploadedImage[] = []
  for (const image of images) {
    if (!image.mask_url?.startsWith('data:image/')) {
      prepared.push({ ...image })
      continue
    }
    const maskBlob = await visualMaskDataURLToEditableMaskBlob(image.mask_url)
    if (!maskBlob) {
      prepared.push({ ...image, mask_url: '' })
      continue
    }
    const uploadedMask = await uploadImage(new File([maskBlob], `canvas-mask-${prepared.length + 1}.png`, { type: 'image/png' }))
    prepared.push({ ...image, mask_url: uploadedMask.url })
  }
  return prepared
}

async function visualMaskDataURLToEditableMaskBlob(dataURL: string) {
  const maskImage = await loadImageElement(dataURL).catch(() => null)
  if (!maskImage?.naturalWidth || !maskImage.naturalHeight) return null
  const source = document.createElement('canvas')
  source.width = maskImage.naturalWidth
  source.height = maskImage.naturalHeight
  const sourceCtx = source.getContext('2d')
  if (!sourceCtx) return null
  sourceCtx.drawImage(maskImage, 0, 0)
  const pixels = sourceCtx.getImageData(0, 0, source.width, source.height)
  for (let index = 0; index < pixels.data.length; index += 4) {
    const painted = pixels.data[index + 3] > 0
    pixels.data[index] = 255
    pixels.data[index + 1] = 255
    pixels.data[index + 2] = 255
    pixels.data[index + 3] = painted ? 0 : 255
  }
  sourceCtx.putImageData(pixels, 0, 0)
  return new Promise<Blob | null>((resolve) => source.toBlob(resolve, 'image/png'))
}

function loadImageElement(src: string) {
  return new Promise<HTMLImageElement>((resolve, reject) => {
    const image = new Image()
    image.onload = () => resolve(image)
    image.onerror = () => reject(new Error('图片加载失败'))
    image.src = src
  })
}

function reuseTask(task: Task | PlazaItem) {
  if ('task_type' in task) {
    form.task_type = task.task_type || 'image_generation'
  } else {
    form.task_type = 'image_generation'
  }
  form.prompt = task.prompt
  form.model = task.model
  form.size = task.size
  syncModelSize(form.model)
  form.quality = task.quality
  form.output_format = task.output_format
  form.output_compression = task.output_compression
  form.background = task.background
  form.moderation = task.moderation
  form.input_fidelity = task.input_fidelity || 'high'
  form.n = task.n
  if ('video_ratio' in task) {
    form.video_ratio = task.video_ratio || '16:9'
    form.video_resolution = videoResolutionFromSize(task.video_width || 1280, task.video_height || 720)
    syncVideoDimensions()
    form.video_duration = task.video_duration || 5
    form.generate_audio = task.generate_audio !== false
    form.watermark = Boolean(task.watermark)
    form.reference_video_urls = (task.reference_videos || []).map((item) => item.url).join('\n')
    referenceVideos.value = (task.reference_videos || []).map((item, index) => ({
      ...item,
      type: item.type || 'video',
      filename: item.filename || `视频 ${index + 1}`,
      cover_url: item.thumbnail_url,
    }))
    referenceAudios.value = (task.reference_audios || []).map((item, index) => ({
      ...item,
      type: item.type || 'audio',
      filename: item.filename || `音频 ${index + 1}`,
    }))
    form.reference_audio_urls = ''
  } else {
    referenceVideos.value = []
    referenceAudios.value = []
    form.reference_video_urls = ''
    form.reference_audio_urls = ''
  }
  referenceImages.value = []
  reusedReferenceImages.value = [...(task.reference_images || [])]
  clearFileInputs()
  revokePreviews()
  syncSizeDraft(form.size)
  if (viewMode.value === 'plaza') {
    viewMode.value = 'tasks'
    if (hasConfig.value && !tasks.value.length) refreshTasks()
  }
  showMessage('已复用任务配置和参考图')
}

function openResultImage(task: Task | PlazaItem) {
  const url = task.result_videos?.[0]?.url || task.result_images?.[0]?.url
  if (!url) return
  window.open(url, '_blank', 'noopener,noreferrer')
}

function syncVideoDimensions() {
  const size = videoSizeFor(form.video_ratio, form.video_resolution)
  form.video_width = size.width
  form.video_height = size.height
}

function normalizeVideoForm() {
  const normalized = normalizeVideoSettings({
    model: form.model,
    ratio: form.video_ratio,
    resolution: form.video_resolution,
    duration: form.video_duration,
  })
  form.video_ratio = normalized.ratio
  form.video_resolution = normalized.resolution
  form.video_duration = normalized.duration
  form.video_width = normalized.width
  form.video_height = normalized.height
}

function switchView(mode: ViewMode) {
  viewMode.value = mode
  if (mode === 'plaza' && !plazaItems.value.length) resetPlazaItems()
  if (mode === 'canvas' && hasConfig.value && !tasks.value.length) refreshTasks()
}

function switchPlazaSort(sort: 'time' | 'likes') {
  plazaSort.value = sort
  resetPlazaItems()
}

function addResultToReferences(task: Task) {
  const image = task.result_images?.[0]
  if (!image?.url) return
  reusedReferenceImages.value.push({ ...image })
  selectedTask.value = null
  showMessage('已加入参考图')
}

function appendReferenceFiles(files: File[]) {
  if (!files.length) return
  const start = referenceImages.value.length
  referenceImages.value.push(...files.map((file) => ({ url: '', filename: file.name, preview_url: URL.createObjectURL(file), uploading: true })))
  referenceMaskFiles.value.push(...files.map(() => null))
  referenceMaskPreviews.value.push(...files.map(() => null))
  files.forEach((file, offset) => uploadReferenceFile(file, start + offset))
}

async function uploadReferenceFile(file: File, index: number) {
  try {
    const uploaded = await uploadImage(file)
    const current = referenceImages.value[index]
    if (!current) return
    referenceImages.value[index] = { ...uploaded, preview_url: current.preview_url, uploading: false }
  } catch (error) {
    const current = referenceImages.value[index]
    if (!current) return
    referenceImages.value[index] = { ...current, uploading: false, upload_error: error instanceof Error ? error.message : '参考图上传失败' }
  }
}

function onReferenceChange(event: Event) {
  const files = Array.from((event.target as HTMLInputElement).files || [])
  appendReferenceFiles(files)
  ;(event.target as HTMLInputElement).value = ''
}

function onPromptPaste(event: ClipboardEvent) {
  const files = Array.from(event.clipboardData?.items || [])
    .filter((item) => item.kind === 'file' && item.type.startsWith('image/'))
    .map((item, index) => item.getAsFile() || new File([], `剪贴板图片-${index + 1}.png`, { type: item.type }))
    .filter((file) => file.size > 0)
  if (!files.length) return
  event.preventDefault()
  appendReferenceFiles(files)
  showMessage(`已从剪贴板添加 ${files.length} 张参考图，正在上传`)
}

function removeReference(index: number) {
  URL.revokeObjectURL(referenceImages.value[index]?.preview_url)
  if (referenceMaskPreviews.value[index]) URL.revokeObjectURL(referenceMaskPreviews.value[index])
  referenceImages.value.splice(index, 1)
  referenceMaskFiles.value.splice(index, 1)
  referenceMaskPreviews.value.splice(index, 1)
}

function removeReusedReference(index: number) {
  reusedReferenceImages.value.splice(index, 1)
}

function openReferenceVideoModal() {
  videoURLDraft.value = ''
  showReferenceVideoModal.value = true
}

async function addReferenceVideoFromURL() {
  const url = videoURLDraft.value.trim()
  if (!url) {
    showMessage('请输入参考视频 URL')
    return
  }
  if (!/^https?:\/\//i.test(url)) {
    showMessage('参考视频 URL 必须以 http:// 或 https:// 开头')
    return
  }
  if (referenceVideos.value.some((video) => video.url === url)) {
    showMessage('这个参考视频已经添加过了')
    return
  }
  addingReferenceVideo.value = true
  const item: PendingReferenceVideo = {
    type: 'video',
    url,
    filename: `视频 ${referenceVideos.value.length + 1}`,
    loading: true,
  }
  referenceVideos.value.push(item)
  const index = referenceVideos.value.length - 1
  showReferenceVideoModal.value = false
  try {
    const cover = await captureVideoCover(url)
    if (referenceVideos.value[index]) {
      referenceVideos.value[index] = { ...referenceVideos.value[index], cover_url: cover, thumbnail_url: cover, loading: false }
    }
  } catch {
    if (referenceVideos.value[index]) {
      referenceVideos.value[index] = { ...referenceVideos.value[index], loading: false }
    }
  } finally {
    addingReferenceVideo.value = false
  }
}

function removeReferenceVideo(index: number) {
  referenceVideos.value.splice(index, 1)
}

function openReferenceAudioModal() {
  audioURLDraft.value = ''
  showReferenceAudioModal.value = true
}

function addReferenceAudioFromURL() {
  const url = audioURLDraft.value.trim()
  if (!url) {
    showMessage('请输入参考音频 URL')
    return
  }
  if (!/^https?:\/\//i.test(url)) {
    showMessage('参考音频 URL 必须以 http:// 或 https:// 开头')
    return
  }
  if (referenceAudios.value.some((audio) => audio.url === url)) {
    showMessage('这个参考音频已经添加过了')
    return
  }
  addingReferenceAudio.value = true
  referenceAudios.value.push({
    type: 'audio',
    url,
    filename: `音频 ${referenceAudios.value.length + 1}`,
  })
  audioURLDraft.value = ''
  showReferenceAudioModal.value = false
  addingReferenceAudio.value = false
}

function removeReferenceAudio(index: number) {
  referenceAudios.value.splice(index, 1)
}

function captureVideoCover(url: string) {
  return new Promise<string>((resolve, reject) => {
    const video = document.createElement('video')
    const cleanup = () => {
      video.pause()
      video.removeAttribute('src')
      video.load()
    }
    const fail = () => {
      cleanup()
      reject(new Error('无法读取视频封面'))
    }
    video.crossOrigin = 'anonymous'
    video.muted = true
    video.playsInline = true
    video.preload = 'auto'
    video.addEventListener('error', fail, { once: true })
    video.addEventListener('loadeddata', () => {
      try {
        if (video.duration && Number.isFinite(video.duration)) video.currentTime = Math.min(0.1, Math.max(0, video.duration - 0.1))
      } catch {
        // Some streams cannot seek; draw the currently loaded frame instead.
        drawVideoCover(video, cleanup, resolve, reject)
      }
    }, { once: true })
    video.addEventListener('seeked', () => drawVideoCover(video, cleanup, resolve, reject), { once: true })
    window.setTimeout(fail, 12000)
    video.src = url
  })
}

function drawVideoCover(video: HTMLVideoElement, cleanup: () => void, resolve: (value: string) => void, reject: (reason?: unknown) => void) {
  try {
    const sourceWidth = video.videoWidth || 320
    const sourceHeight = video.videoHeight || 180
    const scale = Math.min(1, 480 / Math.max(sourceWidth, sourceHeight))
    const width = Math.max(1, Math.round(sourceWidth * scale))
    const height = Math.max(1, Math.round(sourceHeight * scale))
    const canvas = document.createElement('canvas')
    canvas.width = width
    canvas.height = height
    const ctx = canvas.getContext('2d')
    if (!ctx) throw new Error('无法创建封面')
    ctx.drawImage(video, 0, 0, width, height)
    const cover = canvas.toDataURL('image/jpeg', 0.82)
    cleanup()
    resolve(cover)
  } catch (error) {
    cleanup()
    reject(error)
  }
}

function clearFileInputs() {
  document.querySelectorAll<HTMLInputElement>('input[type="file"]').forEach((input) => (input.value = ''))
}

async function loadMaskCanvas() {
  if (!previewImage.value?.editable || !maskCanvas.value || !maskBaseImage.value) return
  const image = maskBaseImage.value
  if (!image.complete) await new Promise((resolve) => image.addEventListener('load', resolve, { once: true }))
  const canvas = maskCanvas.value
  resizeMaskCanvasForEditing(canvas, image)
  syncMaskCanvasDisplaySize()
  const ctx = canvas.getContext('2d')
  if (!ctx) return
  ctx.clearRect(0, 0, canvas.width, canvas.height)
  const existing = currentMaskURL()
  if (existing) {
    const mask = new Image()
    await new Promise<void>((resolve, reject) => {
      mask.onload = () => resolve()
      mask.onerror = () => reject(new Error('蒙板加载失败'))
      mask.src = existing.startsWith('blob:') ? existing : maskPreviewURL(existing)
    }).catch(() => undefined)
    if (mask.complete && mask.naturalWidth) {
      if (existing.startsWith('blob:')) drawVisibleMask(ctx, mask, canvas.width, canvas.height)
      else ctx.drawImage(mask, 0, 0, canvas.width, canvas.height)
    }
  }
}

function drawVisibleMask(ctx: CanvasRenderingContext2D, mask: HTMLImageElement, width: number, height: number) {
  const source = document.createElement('canvas')
  source.width = width
  source.height = height
  const sourceCtx = source.getContext('2d')
  if (!sourceCtx) return
  sourceCtx.drawImage(mask, 0, 0, width, height)
  const data = sourceCtx.getImageData(0, 0, width, height)
  for (let index = 0; index < data.data.length; index += 4) {
    const editable = data.data[index + 3] < 255
    data.data[index] = 255
    data.data[index + 1] = 255
    data.data[index + 2] = 255
    data.data[index + 3] = editable ? 184 : 0
  }
  ctx.putImageData(data, 0, 0)
}

function currentMaskURL() {
  const image = previewImage.value
  if (!image) return ''
  if (image.maskUrl) return image.maskUrl
  if (!image.editable || image.index === undefined) return ''
  if (image.source === 'reused') return reusedReferenceImages.value[image.index]?.mask_url || ''
  return referenceImages.value[image.index]?.mask_url || referenceMaskPreviews.value[image.index] || ''
}

function syncMaskCanvasDisplaySize() {
  const canvas = maskCanvas.value
  if (!canvas) return
  canvas.style.removeProperty('width')
  canvas.style.removeProperty('height')
}

function canvasPoint(event: PointerEvent, canvas = maskCanvas.value) {
  if (!canvas) return null
  const rect = canvas.getBoundingClientRect()
  const scale = Math.min(rect.width / canvas.width, rect.height / canvas.height)
  const drawnWidth = canvas.width * scale
  const drawnHeight = canvas.height * scale
  const offsetX = (rect.width - drawnWidth) / 2
  const offsetY = (rect.height - drawnHeight) / 2
  return {
    x: clampNumber((event.clientX - rect.left - offsetX) / scale, 0, canvas.width),
    y: clampNumber((event.clientY - rect.top - offsetY) / scale, 0, canvas.height),
  }
}

function resizeMaskCanvasForEditing(canvas: HTMLCanvasElement, image: HTMLImageElement) {
  const rect = image.getBoundingClientRect()
  const displayWidth = rect.width || image.naturalWidth
  const displayHeight = rect.height || image.naturalHeight
  const dpr = Math.min(window.devicePixelRatio || 1, MASK_EDIT_MAX_DPR)
  const sideScale = Math.min(1, MASK_EDIT_MAX_SIDE / Math.max(displayWidth * dpr, displayHeight * dpr))
  canvas.width = Math.max(1, Math.round(displayWidth * dpr * sideScale))
  canvas.height = Math.max(1, Math.round(displayHeight * dpr * sideScale))
  canvas.dataset.sourceWidth = String(image.naturalWidth)
  canvas.dataset.sourceHeight = String(image.naturalHeight)
}

function drawMaskPoint(event: PointerEvent, canvas = maskCanvas.value, connectFromLast = true) {
  const point = canvasPoint(event, canvas)
  const ctx = canvas?.getContext('2d')
  if (!point || !canvas || !ctx) return
  ctx.globalCompositeOperation = maskTool.value === 'eraser' ? 'destination-out' : 'source-over'
  ctx.strokeStyle = '#fff'
  ctx.fillStyle = '#fff'
  ctx.lineWidth = maskBrushSize.value
  ctx.lineCap = 'round'
  ctx.lineJoin = 'round'
  ctx.beginPath()
  if (connectFromLast && lastMaskPoint.value) {
    ctx.moveTo(lastMaskPoint.value.x, lastMaskPoint.value.y)
    ctx.lineTo(point.x, point.y)
    ctx.stroke()
  } else {
    ctx.arc(point.x, point.y, maskBrushSize.value / 2, 0, Math.PI * 2)
    ctx.fill()
  }
  lastMaskPoint.value = point
}

function startMaskDraw(event: PointerEvent) {
  if (!previewImage.value?.editable) return
  event.preventDefault()
  event.stopPropagation()
  const canvas = event.currentTarget as HTMLCanvasElement
  maskDrawing.value = true
  lastMaskPoint.value = null
  canvas.setPointerCapture(event.pointerId)
  activeMaskPointer.value = { pointerId: event.pointerId, canvas }
  window.addEventListener('pointermove', moveMaskDrawFromWindow, { passive: false })
  window.addEventListener('pointerup', stopMaskDrawFromWindow, { passive: false })
  window.addEventListener('pointercancel', stopMaskDrawFromWindow, { passive: false })
  drawMaskPoint(event, canvas, false)
}

function moveMaskDraw(event: PointerEvent) {
  event.preventDefault()
  event.stopPropagation()
  const canvas = event.currentTarget as HTMLCanvasElement
  if (!isActiveMaskDrawEvent(event)) return
  const events = event.getCoalescedEvents?.() || [event]
  events.forEach((item) => drawMaskPoint(item, canvas))
}

function stopMaskDraw(event: PointerEvent) {
  event.preventDefault()
  event.stopPropagation()
  const canvas = event.currentTarget as HTMLCanvasElement
  finishMaskDraw(event, canvas)
}

function moveMaskDrawFromWindow(event: PointerEvent) {
  const active = activeMaskPointer.value
  if (!active || !isActiveMaskDrawEvent(event)) return
  event.preventDefault()
  const events = event.getCoalescedEvents?.() || [event]
  events.forEach((item) => drawMaskPoint(item, active.canvas))
}

function stopMaskDrawFromWindow(event: PointerEvent) {
  const active = activeMaskPointer.value
  if (!active || !isActiveMaskDrawEvent(event)) return
  event.preventDefault()
  finishMaskDraw(event, active.canvas)
}

function isActiveMaskDrawEvent(event: PointerEvent) {
  return maskDrawing.value && activeMaskPointer.value?.pointerId === event.pointerId
}

function finishMaskDraw(event: PointerEvent, canvas: HTMLCanvasElement) {
  if (!isActiveMaskDrawEvent(event)) return
  maskDrawing.value = false
  lastMaskPoint.value = null
  if (canvas.hasPointerCapture(event.pointerId)) canvas.releasePointerCapture(event.pointerId)
  window.removeEventListener('pointermove', moveMaskDrawFromWindow)
  window.removeEventListener('pointerup', stopMaskDrawFromWindow)
  window.removeEventListener('pointercancel', stopMaskDrawFromWindow)
  activeMaskPointer.value = null
}

function clampNumber(value: number, min: number, max: number) {
  return Math.min(max, Math.max(min, value))
}

function clearMaskCanvas() {
  const canvas = maskCanvas.value
  const ctx = canvas?.getContext('2d')
  if (!canvas || !ctx) return
  ctx.clearRect(0, 0, canvas.width, canvas.height)
}

async function exportEditableAreaMask(sourceCanvas: HTMLCanvasElement) {
  const sourceWidth = Number(sourceCanvas.dataset.sourceWidth) || sourceCanvas.width
  const sourceHeight = Number(sourceCanvas.dataset.sourceHeight) || sourceCanvas.height
  const normalized = document.createElement('canvas')
  normalized.width = sourceWidth
  normalized.height = sourceHeight
  const normalizedCtx = normalized.getContext('2d')
  if (!normalizedCtx) return null
  normalizedCtx.imageSmoothingEnabled = true
  normalizedCtx.drawImage(sourceCanvas, 0, 0, sourceWidth, sourceHeight)

  const output = document.createElement('canvas')
  output.width = sourceWidth
  output.height = sourceHeight
  const outputCtx = output.getContext('2d')
  if (!outputCtx) return null
  const source = normalizedCtx.getImageData(0, 0, sourceWidth, sourceHeight)
  const mask = outputCtx.createImageData(sourceWidth, sourceHeight)
  for (let index = 0; index < source.data.length; index += 4) {
    const painted = source.data[index + 3] > 0
    mask.data[index] = 255
    mask.data[index + 1] = 255
    mask.data[index + 2] = 255
    mask.data[index + 3] = painted ? 0 : 255
  }
  outputCtx.putImageData(mask, 0, 0)
  return new Promise<Blob | null>((resolve) => output.toBlob(resolve, 'image/png'))
}

async function saveMaskCanvas() {
  const image = previewImage.value
  const canvas = maskCanvas.value
  if (!image?.editable || image.index === undefined || !canvas) return
  const blob = await exportEditableAreaMask(canvas)
  if (!blob) return
  const file = new File([blob], `mask-${image.index + 1}.png`, { type: 'image/png' })
  if (image.source === 'reused') {
    const uploaded = await uploadImage(file)
    reusedReferenceImages.value[image.index] = { ...reusedReferenceImages.value[image.index], mask_url: uploaded.url }
    previewImage.value = { ...image, maskUrl: uploaded.url }
  } else {
    const previousMaskPreview = referenceMaskPreviews.value[image.index]
    if (previousMaskPreview) URL.revokeObjectURL(previousMaskPreview)
    referenceMaskFiles.value[image.index] = file
    referenceMaskPreviews.value[image.index] = URL.createObjectURL(file)
    const uploaded = await uploadImage(file)
    if (referenceImages.value[image.index]) referenceImages.value[image.index] = { ...referenceImages.value[image.index], mask_url: uploaded.url }
    previewImage.value = { ...image, maskUrl: uploaded.url }
  }
  showMessage('蒙板已保存并上传，涂抹区域会被修改')
}

function revokePreviews() {
  referenceImages.value.forEach((image) => URL.revokeObjectURL(image.preview_url))
  referenceMaskPreviews.value.forEach((url) => {
    if (url) URL.revokeObjectURL(url)
  })
  referenceImages.value = []
  referenceMaskFiles.value = []
  referenceMaskPreviews.value = []
}

function openSizeModal() {
  syncSizeDraft(form.size)
  showSizeModal.value = true
}

function applySize() {
  ensureAllowedSizeBase()
  form.size = currentDraftSize.value
  pendingSizeSync.value = form.size
  showSizeModal.value = false
}

function ensureAllowedSizeBase() {
  const options = currentSizeBaseOptions.value
  const available = options.some((option) => option.value === sizeDraft.base)
  if (!available) sizeDraft.base = options[0]?.value || 'auto'
}

function syncSizeDraft(size: string) {
  pendingSizeSync.value = size
  if (isNanoBananaForm.value) {
    const parsed = parseNanoBananaSize(size)
    sizeDraft.base = parsed.imageSize as SizeBase
    sizeDraft.ratio = parsed.aspectRatio
    return
  }
  if (isSeedreamForm.value) {
    const parsed = parseSeedreamSize(size)
    sizeDraft.base = parsed.imageSize as SizeBase
    sizeDraft.ratio = parsed.aspectRatio
    return
  }
  if (size === 'auto') {
    sizeDraft.base = 'auto'
    return
  }
  for (const base of availableSizeBaseOptions.value) {
    if (base.value === 'auto') continue
    for (const ratio of ratioOptions) {
      if (sizeFromRatio(base.value, ratio) === size) {
        sizeDraft.base = base.value
        sizeDraft.ratio = ratio
        return
      }
    }
  }
  ensureAllowedSizeBase()
}

function showMessage(text: string) {
  message.value = text
  window.setTimeout(() => {
    if (message.value === text) message.value = ''
  }, 3600)
}

</script>

<template>
  <main class="page" :class="[`theme-${appliedThemeMode}`, { 'canvas-view': viewMode === 'canvas', 'canvas-zen-view': viewMode === 'canvas' && canvasZenMode }]" @click="closeContextMenu" @contextmenu.prevent="openContextMenu($event, [{ label: '刷新当前视图', action: () => viewMode === 'plaza' ? refreshPlazaItems() : refreshTasks() }, { label: '连接设置', action: openSettings }, { label: `切换主题：${themeMode === 'dark' ? '浅色' : '深色'}`, action: toggleTheme }])">
    <Transition name="canvas-toolbar-slide">
      <AppToolbar
        v-if="!(viewMode === 'canvas' && canvasZenMode)"
        v-model:status="status"
        v-model:keyword="keyword"
        v-model:plaza-keyword="plazaKeyword"
        :site-title="siteTitle"
        :site-icon="siteIcon"
        :visible-subtitle="visibleSubtitle"
        :view-mode="viewMode"
        :favorite-only="favoriteOnly"
        :plaza-sort="plazaSort"
        :theme-mode="themeMode"
        :hide-canvas="hideCanvasForCompact"
        @open-settings="openSettings"
        @switch-view="switchView"
        @refresh-tasks="refreshTasks"
        @reset-tasks="resetTasks"
        @refresh-plaza-items="refreshPlazaItems"
        @switch-plaza-sort="switchPlazaSort"
        @toggle-theme="toggleTheme"
        @toggle-favorite-only="toggleFavoriteOnly"
      />
    </Transition>

    <TaskGrid
      v-if="viewMode === 'tasks'"
      :tasks="tasks"
      :has-config="hasConfig"
      :base-url-blocked="baseURLBlocked"
      :admin-contact-image="adminContactImage"
      :loading-more="loadingMore"
      :has-more-tasks="hasMoreTasks"
      :clock="clock"
      @show-admin-contact="showAdminContact = true"
      @select-task="selectedTask = $event"
      @context-menu="openTaskContextMenu"
      @open-preview="openPreviewImage"
      @open-source="openSourceTask"
      @rerun="rerunTask"
      @toggle-favorite="toggleFavorite"
      @reuse="reuseTask"
      @toggle-share="toggleTaskShare"
      @load-more="loadMoreTasks"
    />

    <KeepAlive>
      <CanvasWorkspace
        v-if="viewMode === 'canvas'"
        :apikey="apikey"
        :baseurl="baseurl"
        :tasks="tasks"
        :default-form="form"
        :models="models"
        :submitting="submitting"
        :run-node-action="runCanvasNode"
        :run-llm-action="runCanvasLLM"
        @select-task="selectedTask = $event"
        @run-node="runCanvasNode"
        @run-llm="runCanvasLLM"
        @zen-mode-change="canvasZenMode = $event"
      />
    </KeepAlive>

    <PlazaGrid
      v-if="viewMode === 'plaza'"
      :items="plazaItems"
      :loading-more="loadingMore"
      :has-more-plaza-items="hasMorePlazaItems"
      @select-item="selectedPlazaItem = $event"
      @open-preview="openPreviewImage"
      @reuse="reuseTask"
      @toggle-like="togglePlazaLike"
      @load-more="loadMorePlazaItems"
    />

    <button
      v-if="viewMode === 'tasks'"
      type="button"
      class="mobile-console-toggle"
      :class="{ active: showMobileComposer }"
      :aria-expanded="showMobileComposer"
      @click="showMobileComposer = !showMobileComposer"
    >
      {{ showMobileComposer ? '收起控制台' : '生成控制台' }}
    </button>

    <Composer
      v-if="viewMode === 'tasks'"
      :class="{ 'mobile-hidden': !showMobileComposer }"
      :form="form"
      :models="models"
      :submitting="submitting"
      :has-config="hasConfig"
      :reused-reference-images="reusedReferenceImages"
      :reference-images="referenceImages"
      :reference-videos="referenceVideos"
      :reference-audios="referenceAudios"
      @submit="submitTask"
      @update-field="updateFormField"
      @prompt-paste="onPromptPaste"
      @open-editable-preview="openEditablePreview"
      @remove-reused-reference="removeReusedReference"
      @remove-reference="removeReference"
      @remove-reference-video="removeReferenceVideo"
      @remove-reference-audio="removeReferenceAudio"
      @open-reference-video-modal="openReferenceVideoModal"
      @open-reference-audio-modal="openReferenceAudioModal"
      @open-size-modal="openSizeModal"
      @reference-change="onReferenceChange"
    />

    <div v-if="contextMenu" class="context-menu" :style="{ left: `${contextMenu.x}px`, top: `${contextMenu.y}px` }" @click.stop @contextmenu.prevent>
      <button v-for="item in contextMenu.items" :key="item.label" type="button" :class="{ danger: item.danger }" :disabled="item.disabled" @click="runContextAction(item)">
        {{ item.label }}
      </button>
    </div>

    <SettingsModal
      v-if="showSettingsModal"
      :baseurl="settingsDraft.baseurl"
      :apikey="settingsDraft.apikey"
      @close="showSettingsModal = false"
      @save="saveSettings"
    />

    <SizeModal
      v-if="showSizeModal"
      :current-size="form.size"
      :draft-size="currentDraftSize"
      :selected-base="sizeDraft.base"
      :selected-ratio="sizeDraft.ratio"
      :ratio-options="currentRatioOptions"
      :size-base-options="currentSizeBaseOptions"
      @close="showSizeModal = false"
      @select-base="sizeDraft.base = $event"
      @select-ratio="sizeDraft.ratio = $event"
      @apply="applySize"
    />

    <div v-if="showReferenceVideoModal" class="modal-backdrop" @click.self="showReferenceVideoModal = false">
      <section class="video-url-modal light-modal">
        <button class="modal-close" type="button" @click="showReferenceVideoModal = false"><AppIcon name="close" /></button>
        <h2>添加参考视频</h2>
        <label>视频 URL<input v-model="videoURLDraft" type="url" placeholder="https://example.com/video.mp4" @keyup.enter="addReferenceVideoFromURL" /></label>
        <p>确认后会尝试读取首帧作为封面；如果远端不允许读取帧，会直接使用视频预览。</p>
        <div class="modal-actions-row">
          <button class="cancel" type="button" @click="showReferenceVideoModal = false"><AppIcon name="close" />取消</button>
          <button class="confirm" type="button" :disabled="addingReferenceVideo" @click="addReferenceVideoFromURL"><AppIcon name="check" />{{ addingReferenceVideo ? '添加中' : '添加' }}</button>
        </div>
      </section>
    </div>

    <div v-if="showReferenceAudioModal" class="modal-backdrop" @click.self="showReferenceAudioModal = false">
      <section class="video-url-modal light-modal">
        <button class="modal-close" type="button" @click="showReferenceAudioModal = false"><AppIcon name="close" /></button>
        <h2>添加参考音频</h2>
        <label>音频 URL<input v-model="audioURLDraft" type="url" placeholder="https://example.com/audio.mp3" @keyup.enter="addReferenceAudioFromURL" /></label>
        <p>确认后会把该 URL 作为 reference_audio 写入视频生成请求。</p>
        <div class="modal-actions-row">
          <button class="cancel" type="button" @click="showReferenceAudioModal = false"><AppIcon name="close" />取消</button>
          <button class="confirm" type="button" :disabled="addingReferenceAudio" @click="addReferenceAudioFromURL"><AppIcon name="check" />{{ addingReferenceAudio ? '添加中' : '添加' }}</button>
        </div>
      </section>
    </div>

    <TaskDetailModal
      v-if="selectedTask"
      :task="selectedTask"
      :clock="clock"
      @close="selectedTask = null"
      @open-preview="openPreviewImage"
      @reuse="reuseTask"
      @rerun="rerunTask"
      @open-result="openResultImage"
      @add-result-to-references="addResultToReferences"
      @toggle-share="toggleTaskShare"
      @remove="removeTask"
      @toggle-favorite="toggleFavorite"
    />

    <PlazaDetailModal
      v-if="selectedPlazaItem"
      :item="selectedPlazaItem"
      @close="selectedPlazaItem = null"
      @open-preview="openPreviewImage"
      @reuse="reuseTask"
      @open-result="openResultImage"
      @toggle-like="togglePlazaLike"
    />

    <SourceModal v-if="sourceTask" :task="sourceTask" @close="sourceTask = null" />

    <AdminContactModal v-if="showAdminContact" :image="adminContactImage" @close="showAdminContact = false" />

    <ImageViewer
      v-if="previewImage"
      v-model:mask-tool="maskTool"
      v-model:mask-brush-size="maskBrushSize"
      :image="previewImage"
      :mask-preview-url="maskPreviewURL"
      @close="closePreviewImage"
      @set-base-image="setMaskBaseImage"
      @set-canvas="setMaskCanvas"
      @image-load="loadMaskCanvas"
      @clear-mask="clearMaskCanvas"
      @save-mask="saveMaskCanvas"
    />

    <div v-if="message" class="toast">{{ message }}</div>
  </main>
</template>
