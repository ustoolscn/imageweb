<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { APIError, createTask, deleteTask, fetchModels, fetchSiteBrand, fetchTaskUpdates, getTask, listPlazaItems, listTasks, retryTask, setPlazaLike, setTaskFavorite, shareTask, unshareTask, uploadImage } from './api'
import type { PlazaItem, Task, UploadedImage } from './types'

const savedModel = localStorage.getItem('image_web_model') || 'gpt-image-2'
const baseurl = ref(localStorage.getItem('image_web_baseurl') || '')
const apikey = ref(localStorage.getItem('image_web_apikey') || '')
const tasks = ref<Task[]>([])
const totalTasks = ref(0)
const plazaItems = ref<PlazaItem[]>([])
const totalPlazaItems = ref(0)
const viewMode = ref<'tasks' | 'plaza'>('tasks')
const plazaSort = ref<'time' | 'likes'>('time')
const plazaClientID = ref(localStorage.getItem('image_web_plaza_client_id') || '')
const models = ref<string[]>(['gpt-image-2'])
const siteTitle = ref('图片生成工作台')
const siteIcon = ref('AI')
const status = ref('all')
const keyword = ref('')
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
const showSizeModal = ref(false)
const selectedTask = ref<Task | null>(null)
const selectedPlazaItem = ref<PlazaItem | null>(null)
const sourceTask = ref<Task | null>(null)
type PreviewImage = { url: string; label: string; maskUrl?: string; editable?: boolean; source?: 'reused' | 'new'; index?: number }
type PendingReferenceImage = UploadedImage & { preview_url: string; uploading?: boolean; upload_error?: string }
const previewImage = ref<PreviewImage | null>(null)
const maskCanvas = ref<HTMLCanvasElement | null>(null)
const maskBaseImage = ref<HTMLImageElement | null>(null)
const maskDrawing = ref(false)
const maskTool = ref<'brush' | 'eraser'>('brush')
const maskBrushSize = ref(36)
const referenceMaskFiles = ref<Array<File | null>>([])
const referenceMaskPreviews = ref<Array<string | null>>([])
let pollTimer: number | undefined
let clockTimer: number | undefined

const form = reactive({
  prompt: '',
  model: savedModel === 'gpt-image-2' ? savedModel : 'gpt-image-2',
  size: 'auto',
  quality: 'auto',
  output_format: 'png',
  output_compression: 100,
  background: 'auto',
  moderation: 'low',
  n: 1,
})

const sizeDraft = reactive({
  mode: 'ratio',
  base: '1K',
  ratio: '1:1',
  width: 1024,
  height: 1024,
})

const referenceImages = ref<PendingReferenceImage[]>([])
const reusedReferenceImages = ref<UploadedImage[]>([])

const hasConfig = computed(() => Boolean(baseurl.value && apikey.value))
const runningCount = computed(() => tasks.value.filter((task) => task.status === 'pending' || task.status === 'running').length)
const visibleSubtitle = computed(() => viewMode.value === 'plaza' ? `公开广场 · 已加载 ${plazaItems.value.length} 条 · 总计 ${totalPlazaItems.value} 条` : (hasConfig.value ? `${maskBaseURL(baseurl.value)} · 已加载 ${tasks.value.length} 条 · 总计 ${totalTasks.value} 条` : '通过 URL 传入 baseurl 和 apikey 后开始使用'))
const ratioSizePresets = {
  '1:1': { '1K': '1024x1024', '2K': '2024x2048', '4K': '2880x2880' },
  '3:2': { '1K': '1536x1024', '2K': '2048x1360', '4K': '3520x2336' },
  '2:3': { '1K': '1024x1536', '2K': '1360x2048', '4K': '2336x3520' },
  '16:9': { '1K': '1824x1024', '2K': '2048x1152', '4K': '3840x2160' },
  '9:16': { '1K': '1024x1824', '2K': '1152x2048', '4K': '2160x3840' },
  '4:3': { '1K': '1360x1024', '2K': '2048x1536', '4K': '3328x2496' },
  '3:4': { '1K': '1024x1360', '2K': '1536x2048', '4K': '2496x3328' },
  '21:9': { '1K': '2384x1024', '2K': '2048x880', '4K': '3840x1648' },
} as const
const ratioOptions = Object.keys(ratioSizePresets)
const baseSizeOptions = ['1K', '2K', '4K']
const gptImage2SizeRules = {
  maxEdge: 3840,
  maxRatio: 3,
  minPixels: 655360,
  maxPixels: 8294400,
}
const draftSize = computed(() => {
  if (sizeDraft.mode === 'auto') return 'auto'
  if (sizeDraft.mode === 'custom') return `${toMultipleOf16(sizeDraft.width)}x${toMultipleOf16(sizeDraft.height)}`
  return sizeFromRatio(sizeDraft.base, sizeDraft.ratio)
})

watch(() => form.model, (model) => {
  localStorage.setItem('image_web_model', model)
})

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

function isFavorite(task: Task) {
  return Boolean(task.favorite)
}

function canShareTask(task: Task) {
  return task.status === 'succeeded' && Boolean(task.result_images?.[0]?.url)
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
  if (index >= 0) tasks.value[index] = { ...tasks.value[index], ...updated }
}

function taskReferenceImages(task: Task | PlazaItem) {
  return [...(task.reference_images || [])]
}

function inputReferenceItems() {
  return [
    ...reusedReferenceImages.value.map((image, index) => ({ url: image.url, label: image.filename || `参考 ${index + 1}`, reused: true })),
    ...referenceImages.value.map((image, index) => ({ url: image.preview_url, label: image.filename || `参考 ${reusedReferenceImages.value.length + index + 1}`, reused: false, index })),
  ]
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

function hasMask(source: 'reused' | 'new', index: number) {
  if (source === 'reused') return Boolean(reusedReferenceImages.value[index]?.mask_url)
  return Boolean(referenceMaskFiles.value[index])
}

function maskPreviewURL(maskUrl: string) {
  return `/api/mask-preview?${new URLSearchParams({ url: maskUrl })}`
}

function prettySource(value: string) {
  if (!value) return '暂无数据'
  try {
    return JSON.stringify(JSON.parse(value), null, 2)
  } catch {
    return value
  }
}

function canOpenSource(task: Task) {
  return task.status === 'succeeded' || task.status === 'failed'
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
  } catch {
    siteTitle.value = '图片生成工作台'
    siteIcon.value = 'AI'
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
    if (ids.includes('gpt-image-2')) {
      models.value = ['gpt-image-2']
      form.model = 'gpt-image-2'
    }
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
    const result = await listPlazaItems(plazaSort.value, ensurePlazaClientID(), '', '', 0, limit)
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
    const result = await listPlazaItems(plazaSort.value, ensurePlazaClientID(), nextPlazaBeforeCreatedAt.value, nextPlazaBeforeID.value, nextPlazaBeforeLikeCount.value)
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

async function submitTask() {
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
    const taskCount = Math.max(1, Number(form.n) || 1)
    const createdTasks: Task[] = []
    for (let index = 0; index < taskCount; index++) {
      createdTasks.push(await createTask({
        apikey: apikey.value,
        baseurl: baseurl.value,
        prompt: form.prompt,
        model: form.model,
        size: form.size,
        quality: form.quality,
        output_format: form.output_format,
        output_compression: Number(form.output_compression),
        background: form.background,
        moderation: form.moderation,
        n: 1,
        reference_images,
      }))
    }
    form.prompt = ''
    referenceImages.value = []
    reusedReferenceImages.value = []
    clearFileInputs()
    revokePreviews()
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

function reuseTask(task: Task | PlazaItem) {
  form.prompt = task.prompt
  form.model = task.model
  form.size = task.size
  form.quality = task.quality
  form.output_format = task.output_format
  form.output_compression = task.output_compression
  form.background = task.background
  form.moderation = task.moderation
  form.n = task.n
  referenceImages.value = []
  reusedReferenceImages.value = [...(task.reference_images || [])]
  clearFileInputs()
  revokePreviews()
  syncSizeDraft(task.size)
  if (viewMode.value === 'plaza') {
    viewMode.value = 'tasks'
    if (hasConfig.value && !tasks.value.length) refreshTasks()
  }
  showMessage('已复用任务配置和参考图')
}

function openResultImage(task: Task | PlazaItem) {
  const url = task.result_images?.[0]?.url
  if (!url) return
  window.open(url, '_blank', 'noopener,noreferrer')
}

function switchView(mode: 'tasks' | 'plaza') {
  viewMode.value = mode
  if (mode === 'plaza' && !plazaItems.value.length) resetPlazaItems()
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

function clearFileInputs() {
  document.querySelectorAll<HTMLInputElement>('input[type="file"]').forEach((input) => (input.value = ''))
}

async function loadMaskCanvas() {
  if (!previewImage.value?.editable || !maskCanvas.value || !maskBaseImage.value) return
  const image = maskBaseImage.value
  if (!image.complete) await new Promise((resolve) => image.addEventListener('load', resolve, { once: true }))
  const canvas = maskCanvas.value
  canvas.width = image.naturalWidth
  canvas.height = image.naturalHeight
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
  const image = maskBaseImage.value
  const canvas = maskCanvas.value
  if (!image || !canvas) return
  const rect = image.getBoundingClientRect()
  canvas.style.width = `${rect.width}px`
  canvas.style.height = `${rect.height}px`
}

function canvasPoint(event: PointerEvent) {
  const canvas = maskCanvas.value
  if (!canvas) return null
  syncMaskCanvasDisplaySize()
  const rect = canvas.getBoundingClientRect()
  return {
    x: ((event.clientX - rect.left) / rect.width) * canvas.width,
    y: ((event.clientY - rect.top) / rect.height) * canvas.height,
  }
}

function drawMaskPoint(event: PointerEvent) {
  const point = canvasPoint(event)
  const canvas = maskCanvas.value
  const ctx = canvas?.getContext('2d')
  if (!point || !canvas || !ctx) return
  ctx.globalCompositeOperation = maskTool.value === 'eraser' ? 'destination-out' : 'source-over'
  ctx.fillStyle = 'rgba(255, 255, 255, .72)'
  ctx.beginPath()
  ctx.arc(point.x, point.y, maskBrushSize.value / 2, 0, Math.PI * 2)
  ctx.fill()
}

function startMaskDraw(event: PointerEvent) {
  if (!previewImage.value?.editable) return
  maskDrawing.value = true
  ;(event.currentTarget as HTMLElement).setPointerCapture(event.pointerId)
  drawMaskPoint(event)
}

function moveMaskDraw(event: PointerEvent) {
  if (!maskDrawing.value) return
  drawMaskPoint(event)
}

function stopMaskDraw(event: PointerEvent) {
  maskDrawing.value = false
  ;(event.currentTarget as HTMLElement).releasePointerCapture(event.pointerId)
}

function clearMaskCanvas() {
  const canvas = maskCanvas.value
  const ctx = canvas?.getContext('2d')
  if (!canvas || !ctx) return
  ctx.clearRect(0, 0, canvas.width, canvas.height)
}

async function exportEditableAreaMask(sourceCanvas: HTMLCanvasElement) {
  const output = document.createElement('canvas')
  output.width = sourceCanvas.width
  output.height = sourceCanvas.height
  const outputCtx = output.getContext('2d')
  const sourceCtx = sourceCanvas.getContext('2d')
  if (!outputCtx || !sourceCtx) return null
  const source = sourceCtx.getImageData(0, 0, sourceCanvas.width, sourceCanvas.height)
  const mask = outputCtx.createImageData(sourceCanvas.width, sourceCanvas.height)
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
  if (sizeDraft.mode === 'custom') {
    const normalized = normalizeGPTImage2Size(sizeDraft.width, sizeDraft.height)
    sizeDraft.width = normalized.width
    sizeDraft.height = normalized.height
  }
  form.size = draftSize.value
  showSizeModal.value = false
}

function syncSizeDraft(size: string) {
  if (size === 'auto') {
    sizeDraft.mode = 'auto'
    return
  }
  const match = size.match(/^(\d+)x(\d+)$/)
  if (match) {
    sizeDraft.width = Number(match[1])
    sizeDraft.height = Number(match[2])
    sizeDraft.mode = 'custom'
    const ratio = closestRatio(sizeDraft.width, sizeDraft.height)
    if (ratio) {
      sizeDraft.mode = 'ratio'
      sizeDraft.ratio = ratio
      sizeDraft.base = closestBase(sizeDraft.width, sizeDraft.height)
    }
  }
}

function toMultipleOf16(value: number) {
  return Math.max(16, Math.round(Number(value || 16) / 16) * 16)
}

function sizeFromRatio(base: string, ratio: string) {
  return ratioSizePresets[ratio as keyof typeof ratioSizePresets]?.[base as keyof typeof ratioSizePresets[keyof typeof ratioSizePresets]] || '1024x1024'
}

function normalizeGPTImage2Size(widthValue: number, heightValue: number) {
  let width = toMultipleOf16(widthValue)
  let height = toMultipleOf16(heightValue)
  ;({ width, height } = fitMaxEdge(width, height, gptImage2SizeRules.maxEdge))
  ;({ width, height } = fitAspectRatio(width, height, gptImage2SizeRules.maxRatio))
  ;({ width, height } = fitPixelRange(width, height, gptImage2SizeRules.minPixels, gptImage2SizeRules.maxPixels))
  return { width: toMultipleOf16(width), height: toMultipleOf16(height) }
}

function fitMaxEdge(width: number, height: number, maxEdge: number) {
  const longSide = Math.max(width, height)
  if (longSide <= maxEdge) return { width, height }
  const scale = maxEdge / longSide
  return { width: toMultipleOf16(Math.floor(width * scale)), height: toMultipleOf16(Math.floor(height * scale)) }
}

function fitAspectRatio(width: number, height: number, maxRatio: number) {
  if (width >= height && width / height > maxRatio) return { width: toMultipleOf16(height * maxRatio), height }
  if (height > width && height / width > maxRatio) return { width, height: toMultipleOf16(width * maxRatio) }
  return { width, height }
}

function fitPixelRange(width: number, height: number, minPixels: number, maxPixels: number) {
  const pixels = width * height
  if (pixels > maxPixels) {
    const scale = Math.sqrt(maxPixels / pixels)
    return { width: toMultipleOf16(Math.floor(width * scale)), height: toMultipleOf16(Math.floor(height * scale)) }
  }
  if (pixels < minPixels) {
    const scale = Math.sqrt(minPixels / pixels)
    return { width: toMultipleOf16(Math.ceil(width * scale)), height: toMultipleOf16(Math.ceil(height * scale)) }
  }
  return { width, height }
}

function ratioPreviewStyle(ratio: string) {
  const [a, b] = ratio.split(':').map(Number)
  const scale = 24 / Math.max(a, b)
  return {
    width: `${Math.max(6, Math.round(a * scale))}px`,
    height: `${Math.max(6, Math.round(b * scale))}px`,
  }
}

function closestRatio(width: number, height: number) {
  const value = width / height
  let best = ratioOptions[0]
  let diff = Infinity
  for (const ratio of ratioOptions) {
    const [a, b] = ratio.split(':').map(Number)
    const nextDiff = Math.abs(value - a / b)
    if (nextDiff < diff) {
      diff = nextDiff
      best = ratio
    }
  }
  return diff < 0.02 ? best : ''
}

function closestBase(width: number, height: number) {
  const side = Math.min(width, height)
  if (side > 3000) return '4K'
  if (side > 1400) return '2K'
  return '1K'
}

function showMessage(text: string) {
  message.value = text
  window.setTimeout(() => {
    if (message.value === text) message.value = ''
  }, 3600)
}

function statusText(value: string) {
  return ({ pending: '排队中', running: '生成中', succeeded: '成功', failed: '失败' } as Record<string, string>)[value] || value
}

function queueText(task: Task) {
  if (task.status !== 'pending') return ''
  return task.queue_position > 0 ? `前面还有 ${task.queue_position} 个` : '即将开始'
}

function statusClass(value: string) {
  return `status-${value}`
}

function elapsed(task: Task) {
  if (task.elapsed_ms) return formatMs(task.elapsed_ms)
  const start = new Date(task.started_at || task.created_at).getTime()
  return formatMs(clock.value - start)
}

function timeText(task: Task) {
  if (task.status === 'pending') return `等待 ${formatMs(clock.value - new Date(task.created_at).getTime())}`
  if (task.status === 'running') return `生成 ${elapsed(task)}`
  return `耗时 ${elapsed(task)}`
}

function formatMs(ms: number) {
  const total = Math.max(0, Math.round(ms / 1000))
  const minutes = Math.floor(total / 60)
  const seconds = total % 60
  return `${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')}`
}

function formatTime(value: string) {
  return new Date(value).toLocaleString('zh-CN', { hour12: false })
}

function maskBaseURL(value: string) {
  try {
    return new URL(value).host || value
  } catch {
    return value
  }
}
</script>

<template>
  <main class="page">
    <header class="toolbar glass-panel">
      <div class="brand">
        <div class="brand-logo">
          <img v-if="siteIcon.startsWith('http://') || siteIcon.startsWith('https://')" :src="siteIcon" alt="站点图标" />
          <span v-else>{{ siteIcon }}</span>
        </div>
        <div>
          <h1>{{ siteTitle }}</h1>
          <p>{{ visibleSubtitle }}</p>
        </div>
      </div>
      <div class="toolbar-controls" :class="{ plaza: viewMode === 'plaza' }">
        <div class="view-tabs">
          <button :class="{ active: viewMode === 'tasks' }" @click="switchView('tasks')">我的任务</button>
          <button :class="{ active: viewMode === 'plaza' }" @click="switchView('plaza')">广场</button>
        </div>
        <template v-if="viewMode === 'tasks'">
          <select v-model="status" @change="resetTasks">
            <option value="all">全部状态</option>
            <option value="pending">排队中</option>
            <option value="running">生成中</option>
            <option value="succeeded">成功</option>
            <option value="failed">失败</option>
          </select>
          <div class="search-wrap">
            <span>⌕</span>
            <input v-model="keyword" class="search" placeholder="搜索提示词、参数..." @keyup.enter="resetTasks" />
          </div>
          <button class="ghost" :class="{ active: favoriteOnly }" @click="favoriteOnly = !favoriteOnly; resetTasks()">{{ favoriteOnly ? '看全部' : '只看收藏' }}</button>
          <button class="ghost" @click="() => refreshTasks()">刷新</button>
        </template>
        <template v-else>
          <div class="plaza-sort">
            <button :class="{ active: plazaSort === 'time' }" @click="switchPlazaSort('time')">最新发布</button>
            <button :class="{ active: plazaSort === 'likes' }" @click="switchPlazaSort('likes')">点赞最多</button>
          </div>
          <button class="ghost" @click="() => refreshPlazaItems()">刷新</button>
        </template>
      </div>
    </header>

    <template v-if="viewMode === 'tasks'">
      <section v-if="!hasConfig" class="empty-state glass-panel">
        <h2>缺少连接配置</h2>
        <p>请使用 URL 传入 baseurl 和 apikey，例如：?baseurl=https://api.example.com&apikey=sk-xxx。页面会保存到本地并自动清理地址栏。</p>
      </section>

      <section v-else-if="baseURLBlocked" class="empty-state glass-panel blocked-state">
        <h2>该 BASEURL 未授权</h2>
        <p>当前 BASEURL 未在网站白名单内，请联系管理员授权后再使用。</p>
        <button type="button" :disabled="!adminContactImage" @click="showAdminContact = true">联系管理员</button>
      </section>

      <section v-else-if="tasks.length === 0" class="empty-state glass-panel soft">
        <h2>还没有生成记录</h2>
        <p>在底部输入提示词并提交，任务会在后端异步执行。关闭页面后再次打开，也可以继续查看历史。</p>
      </section>

      <section class="grid">
        <article v-for="task in tasks" :key="task.id" class="task-card" :class="statusClass(task.status)" @click="selectedTask = task">
        <div class="preview" :class="task.status">
          <img v-if="task.result_images?.[0]?.url" :src="task.result_images[0].url" alt="生成结果" />
          <div v-else class="state-mark">
            <span v-if="task.status === 'running'" class="spinner"></span>
            <span v-else class="state-icon">{{ task.status === 'failed' ? '!' : '…' }}</span>
            <strong>{{ statusText(task.status) }}</strong>
            <small v-if="queueText(task)">{{ queueText(task) }}</small>
          </div>
          <span class="time">◷ {{ timeText(task) }}</span>
        </div>
        <div class="card-body">
          <div class="card-head">
            <span class="status-pill">{{ queueText(task) || statusText(task.status) }}</span>
            <span class="model-pill">{{ task.model }}</span>
          </div>
          <p class="prompt">{{ task.prompt }}</p>
          <div v-if="taskReferenceImages(task).length" class="card-references">
            <span class="ref-label">参考图</span>
            <button v-for="(image, index) in taskReferenceImages(task).slice(0, 2)" :key="`${image.url}-${index}`" type="button" class="ref-thumb" @click="openPreviewImage(image.url, image.filename || `参考图 ${index + 1}`, $event, image.mask_url)">
              <img :src="image.url" :alt="image.filename || '参考图'" />
            </button>
            <span v-if="taskReferenceImages(task).length > 2" class="ref-more">+{{ taskReferenceImages(task).length - 2 }}</span>
          </div>
          <div class="chips">
            <span>{{ task.quality }}</span>
            <span>{{ task.size }}</span>
            <span>{{ task.output_format }}</span>
          </div>
          <div class="actions" @click.stop>
            <button title="查看源数据" aria-label="查看源数据" :disabled="!canOpenSource(task)" @click="openSourceTask(task, $event)">
              <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M8 7h8M8 12h8M8 17h5"/><rect x="5" y="3" width="14" height="18" rx="2"/></svg>
            </button>
            <button title="重新生成" aria-label="重新生成" @click="rerunTask(task)">
              <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M20 12a8 8 0 1 1-2.34-5.66"/><path d="M20 4v6h-6"/></svg>
            </button>
            <button :title="isFavorite(task) ? '取消收藏' : '收藏'" :aria-label="isFavorite(task) ? '取消收藏' : '收藏'" :class="{ favorite: isFavorite(task) }" @click="toggleFavorite(task, $event)">
              <svg viewBox="0 0 24 24" aria-hidden="true"><path d="m12 3 2.8 5.67 6.26.91-4.53 4.42 1.07 6.23L12 17.28l-5.6 2.95 1.07-6.23-4.53-4.42 6.26-.91L12 3Z"/></svg>
            </button>
            <button title="复用配置" aria-label="复用配置" @click="reuseTask(task)">
              <svg viewBox="0 0 24 24" aria-hidden="true"><rect x="8" y="8" width="11" height="11" rx="2"/><path d="M5 16V7a2 2 0 0 1 2-2h9"/></svg>
            </button>
            <button :title="task.shared_to_plaza ? '取消广场分享' : '分享到广场'" :aria-label="task.shared_to_plaza ? '取消广场分享' : '分享到广场'" :class="{ favorite: task.shared_to_plaza }" :disabled="!canShareTask(task)" @click="toggleTaskShare(task, $event)">
              <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 12v7a1 1 0 0 0 1 1h14a1 1 0 0 0 1-1v-7"/><path d="M16 6l-4-4-4 4"/><path d="M12 2v13"/></svg>
            </button>
          </div>
        </div>
      </article>
      </section>

      <div v-if="hasConfig && !baseURLBlocked && tasks.length" class="load-more-state">
        <span v-if="loadingMore">正在加载更多...</span>
        <button v-else-if="hasMoreTasks" type="button" @click="loadMoreTasks">加载更多</button>
        <span v-else>没有更多任务了</span>
      </div>
    </template>

    <template v-else>
      <section v-if="plazaItems.length === 0" class="empty-state glass-panel soft">
        <h2>广场还没有作品</h2>
        <p>成功任务可以点击分享发布到广场，所有访问者都可以看到、复用配置和点赞。</p>
      </section>
      <section class="grid">
        <article v-for="item in plazaItems" :key="item.id" class="task-card status-succeeded plaza-card" @click="selectedPlazaItem = item">
          <div class="preview succeeded">
            <img v-if="item.result_images?.[0]?.url" :src="item.result_images[0].url" alt="广场作品" />
            <span class="time">{{ formatTime(item.created_at) }}</span>
          </div>
          <div class="card-body">
            <div class="card-head">
              <span class="status-pill">广场</span>
              <span class="model-pill">{{ item.model }}</span>
              <span class="like-count">♥ {{ item.like_count }}</span>
            </div>
            <p class="prompt">{{ item.prompt }}</p>
            <div v-if="taskReferenceImages(item).length" class="card-references">
              <span class="ref-label">参考图</span>
              <button v-for="(image, index) in taskReferenceImages(item).slice(0, 2)" :key="`${image.url}-${index}`" type="button" class="ref-thumb" @click="openPreviewImage(image.url, image.filename || `参考图 ${index + 1}`, $event, image.mask_url)">
                <img :src="image.url" :alt="image.filename || '参考图'" />
              </button>
              <span v-if="taskReferenceImages(item).length > 2" class="ref-more">+{{ taskReferenceImages(item).length - 2 }}</span>
            </div>
            <div class="chips">
              <span>{{ item.quality }}</span>
              <span>{{ item.size }}</span>
              <span>{{ item.output_format }}</span>
            </div>
            <div class="actions" @click.stop>
              <button title="复用配置" aria-label="复用配置" @click="reuseTask(item)">
                <svg viewBox="0 0 24 24" aria-hidden="true"><rect x="8" y="8" width="11" height="11" rx="2"/><path d="M5 16V7a2 2 0 0 1 2-2h9"/></svg>
              </button>
              <button :title="item.liked ? '取消点赞' : '点赞'" :aria-label="item.liked ? '取消点赞' : '点赞'" :class="{ favorite: item.liked }" @click="togglePlazaLike(item, $event)">
                <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 21s-7-4.35-9.33-8.77C.87 8.82 2.8 5 6.55 5c2.06 0 3.3 1.1 4.05 2.1C11.35 6.1 12.59 5 14.65 5c3.75 0 5.68 3.82 3.88 7.23C16.2 16.65 12 21 12 21Z"/></svg>
              </button>
            </div>
          </div>
        </article>
      </section>
      <div v-if="plazaItems.length" class="load-more-state">
        <span v-if="loadingMore">正在加载更多...</span>
        <button v-else-if="hasMorePlazaItems" type="button" @click="loadMorePlazaItems">加载更多</button>
        <span v-else>没有更多作品了</span>
      </div>
    </template>

    <form v-if="viewMode === 'tasks'" class="composer glass-panel" @submit.prevent="submitTask">
      <div class="prompt-row">
        <textarea v-model="form.prompt" placeholder="描述你想生成的图片..." rows="2" @paste="onPromptPaste" />
        <button class="submit" :disabled="submitting || !hasConfig">{{ submitting ? '提交中' : '生成' }}</button>
      </div>

      <div v-if="reusedReferenceImages.length || referenceImages.length" class="preview-strip">
        <div v-for="(image, index) in reusedReferenceImages" :key="image.url" class="input-thumb reused">
          <img :src="image.url" alt="参考图" @click="openEditablePreview('reused', index, image.url, `参考 ${index + 1}`)" />
          <span>参考 {{ index + 1 }}{{ hasMask('reused', index) ? ' · 蒙板' : '' }}</span>
          <button type="button" @click="removeReusedReference(index)">×</button>
        </div>
        <div v-for="(image, index) in referenceImages" :key="image.preview_url" class="input-thumb" :class="{ uploading: image.uploading, failed: image.upload_error }">
          <img :src="image.preview_url" alt="参考图" @click="openEditablePreview('new', index, image.preview_url, image.filename || `参考 ${reusedReferenceImages.length + index + 1}`)" />
          <span>{{ image.filename || `参考 ${reusedReferenceImages.length + index + 1}` }}{{ hasMask('new', index) ? ' · 蒙板' : '' }}{{ image.uploading ? ' · 上传中' : '' }}{{ image.upload_error ? ' · 上传失败' : '' }}</span>
          <button type="button" @click="removeReference(index)">×</button>
        </div>
      </div>

      <div class="form-row">
        <label>模型<select v-model="form.model"><option v-for="item in models" :key="item" :value="item">{{ item }}</option></select></label>
        <div class="field"><span>尺寸</span><button type="button" class="size-trigger" @click.stop.prevent="openSizeModal">{{ form.size }}</button></div>
        <label>质量<select v-model="form.quality"><option>auto</option><option>low</option><option>medium</option><option>high</option></select></label>
        <label>格式<select v-model="form.output_format"><option>png</option><option>jpeg</option><option>webp</option></select></label>
        <label>压缩<input v-model.number="form.output_compression" type="number" min="0" max="100" /></label>
        <label>背景<select v-model="form.background"><option>auto</option><option>opaque</option></select></label>
        <label>审核<select v-model="form.moderation"><option>low</option><option>auto</option></select></label>
        <label>数量<input v-model.number="form.n" type="number" min="1" max="10" /></label>
      </div>
      <div class="upload-row single-upload">
        <label class="upload">＋ 参考图<input type="file" multiple accept="image/*" @change="onReferenceChange" /></label>
        <span class="hint">所有上传图和生成结果都会转存到图床后保存。</span>
      </div>
    </form>

    <div v-if="showSizeModal" class="modal-backdrop" @click.self="showSizeModal = false">
      <section class="size-modal light-modal">
        <button class="modal-close" @click="showSizeModal = false">×</button>
        <h2>设置图像尺寸</h2>
        <p class="current-size">当前：{{ form.size }}</p>
        <div class="segmented">
          <button :class="{ active: sizeDraft.mode === 'auto' }" @click="sizeDraft.mode = 'auto'">自动</button>
          <button :class="{ active: sizeDraft.mode === 'ratio' }" @click="sizeDraft.mode = 'ratio'">按比例</button>
          <button :class="{ active: sizeDraft.mode === 'custom' }" @click="sizeDraft.mode = 'custom'">自定义宽高</button>
        </div>
        <template v-if="sizeDraft.mode === 'ratio'">
          <h3>基准分辨率</h3>
          <div class="option-grid three">
            <button v-for="item in baseSizeOptions" :key="item" :class="{ active: sizeDraft.base === item }" @click="sizeDraft.base = item">{{ item }}</button>
          </div>
          <h3>图像比例</h3>
          <div class="option-grid four ratios">
            <button v-for="item in ratioOptions" :key="item" :class="{ active: sizeDraft.ratio === item }" @click="sizeDraft.ratio = item">
              <span class="ratio-preview"><i :style="ratioPreviewStyle(item)"></i></span>
              <span>{{ item }}</span>
            </button>
          </div>
        </template>
        <div v-if="sizeDraft.mode === 'custom'" class="custom-size">
          <label>宽度<input v-model.number="sizeDraft.width" type="number" min="16" max="3840" step="16" @change="sizeDraft.width = normalizeGPTImage2Size(sizeDraft.width, sizeDraft.height).width" /></label>
          <label>高度<input v-model.number="sizeDraft.height" type="number" min="16" max="3840" step="16" @change="sizeDraft.height = normalizeGPTImage2Size(sizeDraft.width, sizeDraft.height).height" /></label>
        </div>
        <div class="will-use">
          <span>将使用</span>
          <strong>{{ draftSize }}</strong>
          <em>GPT Image 2：最大边 3840，宽高为 16 倍数，比例不超过 3:1，总像素 65.5 万到 829.4 万。</em>
        </div>
        <div class="modal-actions-row">
          <button class="cancel" @click="showSizeModal = false">取消</button>
          <button class="confirm" @click="applySize">确定</button>
        </div>
      </section>
    </div>

    <div v-if="selectedTask" class="modal-backdrop" @click.self="selectedTask = null">
      <section class="detail-modal light-modal">
        <button class="modal-close" @click="selectedTask = null">×</button>
        <div class="detail-preview">
          <img v-if="selectedTask.result_images?.[0]?.url" :src="selectedTask.result_images[0].url" alt="生成结果" title="点击查看大图" @click="openPreviewImage(selectedTask.result_images[0].url, '生成结果', $event)" />
          <div v-else class="detail-state">
            <span>{{ selectedTask.status === 'failed' ? '!' : '…' }}</span>
            <p>{{ selectedTask.error_message || statusText(selectedTask.status) }}</p>
          </div>
        </div>
        <div class="detail-info">
          <div class="detail-section detail-input-section">
            <div class="section-title">输入内容</div>
            <p class="detail-prompt">{{ selectedTask.prompt }}</p>
          </div>
          <div v-if="taskReferenceImages(selectedTask).length" class="detail-section">
            <div class="section-title">参考图片</div>
            <div class="detail-references">
              <button v-for="(image, index) in taskReferenceImages(selectedTask)" :key="`${image.url}-${index}`" type="button" @click="openPreviewImage(image.url, image.filename || `参考图 ${index + 1}`, $event, image.mask_url)">
                <img :src="image.url" :alt="image.filename || '参考图'" />
                <span>{{ image.filename || `参考 ${index + 1}` }}{{ image.mask_url ? ' · 蒙板' : '' }}</span>
              </button>
            </div>
          </div>
          <div class="detail-section">
            <div class="section-title">参数配置</div>
            <div class="detail-source">来源 <strong>{{ maskBaseURL(selectedTask.baseurl) }}</strong> · {{ selectedTask.model }}</div>
            <div class="detail-params">
              <div><span>尺寸</span><strong>{{ selectedTask.size }}</strong></div>
              <div><span>质量</span><strong>{{ selectedTask.quality }}</strong></div>
              <div><span>格式</span><strong>{{ selectedTask.output_format }}</strong></div>
              <div><span>审核</span><strong>{{ selectedTask.moderation }}</strong></div>
              <div><span>时间</span><strong>{{ timeText(selectedTask) }}</strong></div>
              <div v-if="queueText(selectedTask)"><span>排队</span><strong>{{ queueText(selectedTask) }}</strong></div>
            </div>
          </div>
          <p class="detail-time">创建于 {{ formatTime(selectedTask.created_at) }} · 状态 {{ queueText(selectedTask) || statusText(selectedTask.status) }}</p>
          <div class="detail-buttons">
            <button class="blue" @click="reuseTask(selectedTask); selectedTask = null">
              <svg viewBox="0 0 24 24" aria-hidden="true"><rect x="8" y="8" width="11" height="11" rx="2"/><path d="M5 16V7a2 2 0 0 1 2-2h9"/></svg>
              <span>复用配置</span>
            </button>
            <button class="green" @click="rerunTask(selectedTask); selectedTask = null">
              <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M20 12a8 8 0 1 1-2.34-5.66"/><path d="M20 4v6h-6"/></svg>
              <span>重新生成</span>
            </button>
            <button class="purple" :disabled="!selectedTask.result_images?.[0]?.url" @click="openResultImage(selectedTask)">
              <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 3v12"/><path d="m7 10 5 5 5-5"/><path d="M5 21h14"/></svg>
              <span>下载图片</span>
            </button>
            <button class="cyan" :disabled="!selectedTask.result_images?.[0]?.url" @click="addResultToReferences(selectedTask)">
              <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 5v14M5 12h14"/><rect x="3" y="3" width="18" height="18" rx="3"/></svg>
              <span>加入参考</span>
            </button>
            <button class="orange" :class="{ favorite: selectedTask.shared_to_plaza }" :disabled="!canShareTask(selectedTask)" @click="toggleTaskShare(selectedTask, $event)">
              <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 12v7a1 1 0 0 0 1 1h14a1 1 0 0 0 1-1v-7"/><path d="M16 6l-4-4-4 4"/><path d="M12 2v13"/></svg>
              <span>{{ selectedTask.shared_to_plaza ? '取消分享' : '分享广场' }}</span>
            </button>
            <button class="red" @click="removeTask(selectedTask)">
              <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 7h16M10 11v6M14 11v6M6 7l1 14h10l1-14M9 7V4h6v3"/></svg>
              <span>删除记录</span>
            </button>
            <button class="star" :class="{ favorite: isFavorite(selectedTask) }" :title="isFavorite(selectedTask) ? '取消收藏' : '收藏'" :aria-label="isFavorite(selectedTask) ? '取消收藏' : '收藏'" @click="toggleFavorite(selectedTask, $event)">
              <svg viewBox="0 0 24 24" aria-hidden="true"><path d="m12 3 2.8 5.67 6.26.91-4.53 4.42 1.07 6.23L12 17.28l-5.6 2.95 1.07-6.23-4.53-4.42 6.26-.91L12 3Z"/></svg>
            </button>
          </div>
        </div>
      </section>
    </div>

    <div v-if="selectedPlazaItem" class="modal-backdrop" @click.self="selectedPlazaItem = null">
      <section class="detail-modal light-modal">
        <button class="modal-close" @click="selectedPlazaItem = null">×</button>
        <div class="detail-preview">
          <img v-if="selectedPlazaItem.result_images?.[0]?.url" :src="selectedPlazaItem.result_images[0].url" alt="广场作品" title="点击查看大图" @click="openPreviewImage(selectedPlazaItem.result_images[0].url, '广场作品', $event)" />
        </div>
        <div class="detail-info">
          <div class="detail-section detail-input-section">
            <div class="section-title">输入内容</div>
            <p class="detail-prompt">{{ selectedPlazaItem.prompt }}</p>
          </div>
          <div v-if="taskReferenceImages(selectedPlazaItem).length" class="detail-section">
            <div class="section-title">参考图片</div>
            <div class="detail-references">
              <button v-for="(image, index) in taskReferenceImages(selectedPlazaItem)" :key="`${image.url}-${index}`" type="button" @click="openPreviewImage(image.url, image.filename || `参考图 ${index + 1}`, $event, image.mask_url)">
                <img :src="image.url" :alt="image.filename || '参考图'" />
                <span>{{ image.filename || `参考 ${index + 1}` }}{{ image.mask_url ? ' · 蒙板' : '' }}</span>
              </button>
            </div>
          </div>
          <div class="detail-section">
            <div class="section-title">参数配置</div>
            <div class="detail-source">广场作品 · {{ selectedPlazaItem.model }} · ♥ {{ selectedPlazaItem.like_count }}</div>
            <div class="detail-params">
              <div><span>尺寸</span><strong>{{ selectedPlazaItem.size }}</strong></div>
              <div><span>质量</span><strong>{{ selectedPlazaItem.quality }}</strong></div>
              <div><span>格式</span><strong>{{ selectedPlazaItem.output_format }}</strong></div>
              <div><span>审核</span><strong>{{ selectedPlazaItem.moderation }}</strong></div>
            </div>
          </div>
          <p class="detail-time">发布于 {{ formatTime(selectedPlazaItem.created_at) }}</p>
          <div class="detail-buttons plaza-detail-buttons">
            <button class="blue" @click="reuseTask(selectedPlazaItem); selectedPlazaItem = null">
              <svg viewBox="0 0 24 24" aria-hidden="true"><rect x="8" y="8" width="11" height="11" rx="2"/><path d="M5 16V7a2 2 0 0 1 2-2h9"/></svg>
              <span>复用配置</span>
            </button>
            <button class="purple" :disabled="!selectedPlazaItem.result_images?.[0]?.url" @click="openResultImage(selectedPlazaItem)">
              <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 3v12"/><path d="m7 10 5 5 5-5"/><path d="M5 21h14"/></svg>
              <span>下载图片</span>
            </button>
            <button class="star" :class="{ favorite: selectedPlazaItem.liked }" @click="togglePlazaLike(selectedPlazaItem, $event)">
              <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 21s-7-4.35-9.33-8.77C.87 8.82 2.8 5 6.55 5c2.06 0 3.3 1.1 4.05 2.1C11.35 6.1 12.59 5 14.65 5c3.75 0 5.68 3.82 3.88 7.23C16.2 16.65 12 21 12 21Z"/></svg>
              <span>{{ selectedPlazaItem.liked ? '取消点赞' : '点赞' }} {{ selectedPlazaItem.like_count }}</span>
            </button>
          </div>
        </div>
      </section>
    </div>

    <div v-if="sourceTask" class="modal-backdrop" @click.self="sourceTask = null">
      <section class="source-modal light-modal">
        <button class="modal-close" @click="sourceTask = null">×</button>
        <h2>源数据</h2>
        <div class="source-grid">
          <section>
            <h3>请求头</h3>
            <pre>{{ prettySource(sourceTask.request_headers) }}</pre>
          </section>
          <section>
            <h3>请求内容</h3>
            <pre>{{ prettySource(sourceTask.request_json) }}</pre>
          </section>
          <section>
            <h3>响应头</h3>
            <pre>{{ prettySource(sourceTask.response_headers) }}</pre>
          </section>
          <section>
            <h3>响应内容</h3>
            <pre>{{ prettySource(sourceTask.response_json) }}</pre>
          </section>
        </div>
      </section>
    </div>

    <div v-if="showAdminContact" class="modal-backdrop" @click.self="showAdminContact = false">
      <section class="admin-contact-modal light-modal">
        <button class="modal-close" @click="showAdminContact = false">×</button>
        <h2>联系管理员</h2>
        <p>请扫码联系管理员授权当前 BASEURL。</p>
        <img v-if="adminContactImage" :src="adminContactImage" alt="管理员联系方式" />
      </section>
    </div>

    <div v-if="previewImage" class="modal-backdrop image-viewer" @click.self="closePreviewImage">
      <section class="image-viewer-panel" :class="{ editable: previewImage.editable }">
        <button class="modal-close" @click="closePreviewImage">×</button>
        <template v-if="previewImage.editable">
          <div class="mask-stage">
            <img ref="maskBaseImage" :src="previewImage.url" :alt="previewImage.label" @load="loadMaskCanvas" />
            <canvas ref="maskCanvas" @pointerdown="startMaskDraw" @pointermove="moveMaskDraw" @pointerup="stopMaskDraw" @pointercancel="stopMaskDraw" />
          </div>
          <div class="mask-tools">
            <button :class="{ active: maskTool === 'brush' }" @click="maskTool = 'brush'">涂抹蒙板</button>
            <button :class="{ active: maskTool === 'eraser' }" @click="maskTool = 'eraser'">橡皮擦</button>
            <label>画笔 <input v-model.number="maskBrushSize" type="range" min="8" max="120" /></label>
            <button @click="clearMaskCanvas">清空蒙板</button>
            <button class="primary" @click="saveMaskCanvas">保存蒙板</button>
          </div>
        </template>
        <div v-else-if="previewImage.maskUrl" class="mask-stage readonly-mask">
          <img :src="previewImage.url" :alt="previewImage.label" />
          <img class="readonly-mask-overlay" :src="maskPreviewURL(previewImage.maskUrl)" alt="蒙板" />
        </div>
        <img v-else :src="previewImage.url" :alt="previewImage.label" />
        <div>{{ previewImage.label }}{{ previewImage.maskUrl ? ' · 蒙板' : '' }}</div>
      </section>
    </div>

    <div v-if="message" class="toast">{{ message }}</div>
  </main>
</template>
