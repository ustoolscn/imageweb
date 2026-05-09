<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { APIError, createTask, deleteTask, fetchModels, fetchSiteBrand, fetchTaskUpdates, getTask, listPlazaItems, listTasks, retryTask, setPlazaLike, setTaskFavorite, shareTask, unshareTask, uploadImage } from './api'
import AdminContactModal from './components/AdminContactModal.vue'
import AppToolbar from './components/AppToolbar.vue'
import Composer from './components/Composer.vue'
import ImageViewer from './components/ImageViewer.vue'
import PlazaDetailModal from './components/PlazaDetailModal.vue'
import PlazaGrid from './components/PlazaGrid.vue'
import SettingsModal from './components/SettingsModal.vue'
import SizeModal from './components/SizeModal.vue'
import SourceModal from './components/SourceModal.vue'
import TaskDetailModal from './components/TaskDetailModal.vue'
import TaskGrid from './components/TaskGrid.vue'
import { ratioOptions, sizeFromRatio } from './lib/sizes'
import { canOpenSource, canShareTask, maskBaseURL } from './lib/view'
import type { PlazaItem, Task, UploadedImage } from './types'
import type { ImageForm, PendingReferenceImage, PreviewImage, SettingsPayload, ThemeMode, ViewMode } from './uiTypes'

const savedModel = localStorage.getItem('image_web_model') || 'gpt-image-2'
const savedTheme = localStorage.getItem('image_web_theme') === 'light' ? 'light' : 'dark'
const baseurl = ref(localStorage.getItem('image_web_baseurl') || '')
const apikey = ref(localStorage.getItem('image_web_apikey') || '')
const tasks = ref<Task[]>([])
const totalTasks = ref(0)
const plazaItems = ref<PlazaItem[]>([])
const totalPlazaItems = ref(0)
const viewMode = ref<ViewMode>('tasks')
const themeMode = ref<ThemeMode>(savedTheme)
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
const showSettingsModal = ref(false)
const settingsDraft = reactive({ baseurl: baseurl.value, apikey: apikey.value })
const showSizeModal = ref(false)
const selectedTask = ref<Task | null>(null)
const selectedPlazaItem = ref<PlazaItem | null>(null)
const sourceTask = ref<Task | null>(null)
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

const form = reactive<ImageForm>({
  prompt: '',
  model: savedModel === 'gpt-image-2' ? savedModel : 'gpt-image-2',
  size: '1024x1024',
  quality: 'auto',
  output_format: 'png',
  output_compression: 100,
  background: 'auto',
  moderation: 'low',
  n: 1,
})

const sizeDraft = reactive({
  base: '1K',
  ratio: '1:1',
})

const referenceImages = ref<PendingReferenceImage[]>([])
const reusedReferenceImages = ref<UploadedImage[]>([])

const hasConfig = computed(() => Boolean(baseurl.value && apikey.value))
const runningCount = computed(() => tasks.value.filter((task) => task.status === 'pending' || task.status === 'running').length)
const visibleSubtitle = computed(() => viewMode.value === 'plaza' ? `公开广场 · 已加载 ${plazaItems.value.length} 条 · 总计 ${totalPlazaItems.value} 条` : (hasConfig.value ? `${maskBaseURL(baseurl.value)} · 已加载 ${tasks.value.length} 条 · 总计 ${totalTasks.value} 条` : '通过 URL 传入 baseurl 和 apikey 后开始使用'))
const draftSize = computed(() => sizeFromRatio(sizeDraft.base, sizeDraft.ratio))

watch(() => form.model, (model) => {
  localStorage.setItem('image_web_model', model)
})

watch(themeMode, (theme) => {
  localStorage.setItem('image_web_theme', theme)
}, { immediate: true })

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
  themeMode.value = themeMode.value === 'dark' ? 'light' : 'dark'
}

function toggleFavoriteOnly() {
  favoriteOnly.value = !favoriteOnly.value
  resetTasks()
}

function updateFormField(field: keyof ImageForm, value: string | number) {
  if (field === 'output_compression' || field === 'n') {
    form[field] = Number(value)
    return
  }
  form[field] = String(value)
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
  form.size = draftSize.value
  showSizeModal.value = false
}

function syncSizeDraft(size: string) {
  for (const ratio of ratioOptions) {
    if (sizeFromRatio('1K', ratio) === size) {
      sizeDraft.base = '1K'
      sizeDraft.ratio = ratio
      return
    }
  }
}

function showMessage(text: string) {
  message.value = text
  window.setTimeout(() => {
    if (message.value === text) message.value = ''
  }, 3600)
}

</script>

<template>
  <main class="page" :class="`theme-${themeMode}`">
    <AppToolbar
      v-model:status="status"
      v-model:keyword="keyword"
      :site-title="siteTitle"
      :site-icon="siteIcon"
      :visible-subtitle="visibleSubtitle"
      :view-mode="viewMode"
      :favorite-only="favoriteOnly"
      :plaza-sort="plazaSort"
      :theme-mode="themeMode"
      @open-settings="openSettings"
      @switch-view="switchView"
      @refresh-tasks="refreshTasks"
      @reset-tasks="resetTasks"
      @refresh-plaza-items="refreshPlazaItems"
      @switch-plaza-sort="switchPlazaSort"
      @toggle-theme="toggleTheme"
      @toggle-favorite-only="toggleFavoriteOnly"
    />

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
      @open-preview="openPreviewImage"
      @open-source="openSourceTask"
      @rerun="rerunTask"
      @toggle-favorite="toggleFavorite"
      @reuse="reuseTask"
      @toggle-share="toggleTaskShare"
      @load-more="loadMoreTasks"
    />

    <PlazaGrid
      v-else
      :items="plazaItems"
      :loading-more="loadingMore"
      :has-more-plaza-items="hasMorePlazaItems"
      @select-item="selectedPlazaItem = $event"
      @open-preview="openPreviewImage"
      @reuse="reuseTask"
      @toggle-like="togglePlazaLike"
      @load-more="loadMorePlazaItems"
    />

    <Composer
      v-if="viewMode === 'tasks'"
      :form="form"
      :models="models"
      :submitting="submitting"
      :has-config="hasConfig"
      :reused-reference-images="reusedReferenceImages"
      :reference-images="referenceImages"
      @submit="submitTask"
      @update-field="updateFormField"
      @prompt-paste="onPromptPaste"
      @open-editable-preview="openEditablePreview"
      @remove-reused-reference="removeReusedReference"
      @remove-reference="removeReference"
      @open-size-modal="openSizeModal"
      @reference-change="onReferenceChange"
    />

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
      :draft-size="draftSize"
      :selected-ratio="sizeDraft.ratio"
      :ratio-options="ratioOptions"
      @close="showSizeModal = false"
      @select-ratio="sizeDraft.ratio = $event"
      @apply="applySize"
    />

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
      @start-draw="startMaskDraw"
      @move-draw="moveMaskDraw"
      @stop-draw="stopMaskDraw"
      @clear-mask="clearMaskCanvas"
      @save-mask="saveMaskCanvas"
    />

    <div v-if="message" class="toast">{{ message }}</div>
  </main>
</template>
