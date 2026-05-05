<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { createTask, deleteTask, fetchModels, getTask, listTasks, retryTask, setTaskFavorite, uploadImage } from './api'
import type { Task, UploadedImage } from './types'

const savedModel = localStorage.getItem('image_web_model') || 'gpt-image-2'
const baseurl = ref(localStorage.getItem('image_web_baseurl') || '')
const apikey = ref(localStorage.getItem('image_web_apikey') || '')
const tasks = ref<Task[]>([])
const models = ref<string[]>(['gpt-image-2'])
const status = ref('all')
const keyword = ref('')
const favoriteOnly = ref(false)
const loading = ref(false)
const submitting = ref(false)
const message = ref('')
const clock = ref(Date.now())
const showSizeModal = ref(false)
const selectedTask = ref<Task | null>(null)
const sourceTask = ref<Task | null>(null)
const previewImage = ref<{ url: string; label: string } | null>(null)
let pollTimer: number | undefined
let clockTimer: number | undefined

const form = reactive({
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
  mode: 'ratio',
  base: '1K',
  ratio: '1:1',
  width: 1024,
  height: 1024,
})

const referenceFiles = ref<File[]>([])
const reusedReferenceImages = ref<UploadedImage[]>([])
const referencePreviews = ref<string[]>([])

const hasConfig = computed(() => Boolean(baseurl.value && apikey.value))
const runningCount = computed(() => tasks.value.filter((task) => task.status === 'pending' || task.status === 'running').length)
const visibleSubtitle = computed(() => hasConfig.value ? `${maskBaseURL(baseurl.value)} · ${tasks.value.length} 条记录` : '通过 URL 传入 baseurl 和 apikey 后开始使用')
const ratioOptions = ['1:1', '3:2', '2:3', '16:9', '9:16', '4:3', '3:4', '21:9']
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

function isFavorite(task: Task) {
  return Boolean(task.favorite)
}

async function toggleFavorite(task: Task, event?: Event) {
  event?.stopPropagation()
  try {
    const updated = await setTaskFavorite(task.id, apikey.value, !task.favorite)
    patchTask(updated)
    if (selectedTask.value?.id === updated.id) selectedTask.value = updated
    showMessage(updated.favorite ? '已收藏' : '已取消收藏')
    if (favoriteOnly.value && !updated.favorite) await refreshTasks()
  } catch (error) {
    showMessage(error instanceof Error ? error.message : '收藏更新失败')
  }
}

function patchTask(updated: Task) {
  const index = tasks.value.findIndex((task) => task.id === updated.id)
  if (index >= 0) tasks.value[index] = updated
}

function taskReferenceImages(task: Task) {
  return [...(task.reference_images || [])]
}

function inputReferenceItems() {
  return [
    ...reusedReferenceImages.value.map((image, index) => ({ url: image.url, label: image.filename || `参考 ${index + 1}`, reused: true })),
    ...referencePreviews.value.map((url, index) => ({ url, label: `参考 ${reusedReferenceImages.value.length + index + 1}`, reused: false, index })),
  ]
}

function openPreviewImage(url: string, label: string, event?: Event) {
  event?.stopPropagation()
  previewImage.value = { url, label }
}

function prettySource(value: string) {
  if (!value) return '暂无数据'
  try {
    return JSON.stringify(JSON.parse(value), null, 2)
  } catch {
    return value
  }
}

async function openSourceTask(task: Task, event?: Event) {
  event?.stopPropagation()
  try {
    sourceTask.value = await getTask(task.id, apikey.value)
  } catch (error) {
    showMessage(error instanceof Error ? error.message : '源数据加载失败')
  }
}

onMounted(() => {
  loadConfigFromURL()
  syncSizeDraft(form.size)
  if (hasConfig.value) {
    loadModels()
    refreshTasks()
    startPolling()
    startClock()
  }
})

onUnmounted(() => {
  if (pollTimer) window.clearInterval(pollTimer)
  if (clockTimer) window.clearInterval(clockTimer)
  revokePreviews()
})

function loadConfigFromURL() {
  const url = new URL(window.location.href)
  const nextBaseURL = url.searchParams.get('baseurl') || ''
  const nextAPIKey = url.searchParams.get('apikey') || ''
  let changed = false
  if (nextBaseURL) {
    baseurl.value = nextBaseURL
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

async function loadModels() {
  if (!hasConfig.value) return
  try {
    const result = await fetchModels(baseurl.value, apikey.value)
    const ids = result.data?.map((item) => item.id).filter(Boolean) || []
    if (ids.includes('gpt-image-2')) {
      models.value = ['gpt-image-2']
      form.model = 'gpt-image-2'
    }
  } catch (error) {
    showMessage(error instanceof Error ? error.message : '模型加载失败')
  }
}

async function refreshTasks() {
  if (!apikey.value) return
  loading.value = true
  try {
    tasks.value = await listTasks(apikey.value, status.value, keyword.value, favoriteOnly.value)
  } catch (error) {
    showMessage(error instanceof Error ? error.message : '任务加载失败')
  } finally {
    loading.value = false
  }
}

function startPolling() {
  pollTimer = window.setInterval(() => {
    refreshTasks()
  }, runningCount.value > 0 ? 2500 : 8000)
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
    const reference_images: UploadedImage[] = [...reusedReferenceImages.value]
    for (const file of referenceFiles.value) {
      reference_images.push(await uploadImage(file))
    }
    await createTask({
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
      n: Number(form.n),
      reference_images,
    })
    form.prompt = ''
    referenceFiles.value = []
    reusedReferenceImages.value = []
    clearFileInputs()
    revokePreviews()
    await refreshTasks()
    showMessage('任务已提交，生成会在后台继续执行')
  } catch (error) {
    showMessage(error instanceof Error ? error.message : '提交失败')
  } finally {
    submitting.value = false
  }
}

async function removeTask(task: Task) {
  if (!confirm('确定删除这个任务记录吗？')) return
  await deleteTask(task.id, apikey.value)
  if (selectedTask.value?.id === task.id) selectedTask.value = null
  await refreshTasks()
}

async function rerunTask(task: Task) {
  await retryTask(task.id, apikey.value)
  await refreshTasks()
  showMessage('已创建重新生成任务')
}

function reuseTask(task: Task) {
  form.prompt = task.prompt
  form.model = task.model
  form.size = task.size
  form.quality = task.quality
  form.output_format = task.output_format
  form.output_compression = task.output_compression
  form.background = task.background
  form.moderation = task.moderation
  form.n = task.n
  referenceFiles.value = []
  reusedReferenceImages.value = [...(task.reference_images || [])]
  clearFileInputs()
  revokePreviews()
  syncSizeDraft(task.size)
  showMessage('已复用任务配置和参考图')
}

function onReferenceChange(event: Event) {
  const files = Array.from((event.target as HTMLInputElement).files || [])
  referenceFiles.value.push(...files)
  referencePreviews.value.push(...files.map((file) => URL.createObjectURL(file)))
  ;(event.target as HTMLInputElement).value = ''
}


function removeReference(index: number) {
  URL.revokeObjectURL(referencePreviews.value[index])
  referenceFiles.value.splice(index, 1)
  referencePreviews.value.splice(index, 1)
}

function removeReusedReference(index: number) {
  reusedReferenceImages.value.splice(index, 1)
}


function clearFileInputs() {
  document.querySelectorAll<HTMLInputElement>('input[type="file"]').forEach((input) => (input.value = ''))
}

function revokePreviews() {
  referencePreviews.value.forEach(URL.revokeObjectURL)
  referencePreviews.value = []
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
  const side = ({ '1K': 1024, '2K': 2048, '4K': 3840 } as Record<string, number>)[base] || 1024
  const [a, b] = ratio.split(':').map(Number)
  const shortSide = base === '4K' ? Math.round(side * Math.min(a, b) / Math.max(a, b)) : side
  const width = a >= b ? shortSide * a / b : shortSide
  const height = a >= b ? shortSide : shortSide * b / a
  const normalized = normalizeGPTImage2Size(width, height)
  return `${normalized.width}x${normalized.height}`
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

function statusClass(value: string) {
  return `status-${value}`
}

function elapsed(task: Task) {
  if (task.elapsed_ms) return formatMs(task.elapsed_ms)
  const start = new Date(task.started_at || task.created_at).getTime()
  return formatMs(clock.value - start)
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
        <div class="brand-logo">AI</div>
        <div>
          <h1>图片生成工作台</h1>
          <p>{{ visibleSubtitle }}</p>
        </div>
      </div>
      <div class="toolbar-controls">
        <select v-model="status" @change="refreshTasks">
          <option value="all">全部状态</option>
          <option value="pending">排队中</option>
          <option value="running">生成中</option>
          <option value="succeeded">成功</option>
          <option value="failed">失败</option>
        </select>
        <div class="search-wrap">
          <span>⌕</span>
          <input v-model="keyword" class="search" placeholder="搜索提示词、参数..." @keyup.enter="refreshTasks" />
        </div>
        <button class="ghost" :class="{ active: favoriteOnly }" @click="favoriteOnly = !favoriteOnly; refreshTasks()">{{ favoriteOnly ? '看全部' : '只看收藏' }}</button>
        <button class="ghost" @click="refreshTasks">刷新</button>
      </div>
    </header>

    <section v-if="!hasConfig" class="empty-state glass-panel">
      <h2>缺少连接配置</h2>
      <p>请使用 URL 传入 baseurl 和 apikey，例如：?baseurl=https://api.example.com&apikey=sk-xxx。页面会保存到本地并自动清理地址栏。</p>
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
          </div>
          <span class="time">◷ {{ elapsed(task) }}</span>
        </div>
        <div class="card-body">
          <div class="card-head">
            <span class="status-pill">{{ statusText(task.status) }}</span>
            <span class="model-pill">{{ task.model }}</span>
          </div>
          <p class="prompt">{{ task.prompt }}</p>
          <div v-if="taskReferenceImages(task).length" class="card-references">
            <span class="ref-label">参考图</span>
            <button v-for="(image, index) in taskReferenceImages(task).slice(0, 5)" :key="`${image.url}-${index}`" type="button" class="ref-thumb" @click="openPreviewImage(image.url, image.filename || `参考图 ${index + 1}`, $event)">
              <img :src="image.url" :alt="image.filename || '参考图'" />
            </button>
            <span v-if="taskReferenceImages(task).length > 5" class="ref-more">+{{ taskReferenceImages(task).length - 5 }}</span>
          </div>
          <div class="chips">
            <span>{{ task.quality }}</span>
            <span>{{ task.size }}</span>
            <span>{{ task.output_format }}</span>
            <span>×{{ task.n }}</span>
          </div>
          <p v-if="task.error_message" class="error">{{ task.error_message }}</p>
          <div class="actions" @click.stop>
            <button title="查看源数据" @click="openSourceTask(task, $event)">源</button>
            <button title="重新生成" @click="rerunTask(task)">↻</button>
            <button :title="isFavorite(task) ? '取消收藏' : '收藏'" :class="{ favorite: isFavorite(task) }" @click="toggleFavorite(task, $event)">{{ isFavorite(task) ? '★' : '☆' }}</button>
            <button title="复用配置" @click="reuseTask(task)">↩</button>
            <button title="删除" @click="removeTask(task)">⌫</button>
          </div>
        </div>
      </article>
    </section>

    <form class="composer glass-panel" @submit.prevent="submitTask">
      <div class="prompt-row">
        <textarea v-model="form.prompt" placeholder="描述你想生成的图片..." rows="2" />
        <button class="submit" :disabled="submitting || !hasConfig">{{ submitting ? '提交中' : '生成' }}</button>
      </div>

      <div v-if="reusedReferenceImages.length || referencePreviews.length" class="preview-strip">
        <div v-for="(image, index) in reusedReferenceImages" :key="image.url" class="input-thumb reused">
          <img :src="image.url" alt="参考图" @click="openPreviewImage(image.url, `参考 ${index + 1}`)" />
          <span>参考 {{ index + 1 }}</span>
          <button type="button" @click="removeReusedReference(index)">×</button>
        </div>
        <div v-for="(src, index) in referencePreviews" :key="src" class="input-thumb">
          <img :src="src" alt="参考图" @click="openPreviewImage(src, `参考 ${reusedReferenceImages.length + index + 1}`)" />
          <span>参考 {{ reusedReferenceImages.length + index + 1 }}</span>
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
          <img v-if="selectedTask.result_images?.[0]?.url" :src="selectedTask.result_images[0].url" alt="生成结果" />
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
              <button v-for="(image, index) in taskReferenceImages(selectedTask)" :key="`${image.url}-${index}`" type="button" @click="openPreviewImage(image.url, image.filename || `参考图 ${index + 1}`, $event)">
                <img :src="image.url" :alt="image.filename || '参考图'" />
                <span>{{ image.filename || `参考 ${index + 1}` }}</span>
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
              <div><span>数量</span><strong>{{ selectedTask.n }}</strong></div>
              <div><span>耗时</span><strong>{{ elapsed(selectedTask) }}</strong></div>
            </div>
          </div>
          <p class="detail-time">创建于 {{ formatTime(selectedTask.created_at) }} · 状态 {{ statusText(selectedTask.status) }}</p>
          <div class="detail-buttons">
            <button class="blue" @click="reuseTask(selectedTask); selectedTask = null">↩ 复用配置</button>
            <button class="green" @click="rerunTask(selectedTask); selectedTask = null">↻ 重新生成</button>
            <button class="red" @click="removeTask(selectedTask)">⌫ 删除记录</button>
            <button class="star" :class="{ favorite: isFavorite(selectedTask) }" @click="toggleFavorite(selectedTask, $event)">{{ isFavorite(selectedTask) ? '★' : '☆' }}</button>
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

    <div v-if="previewImage" class="modal-backdrop image-viewer" @click.self="previewImage = null">
      <section class="image-viewer-panel">
        <button class="modal-close" @click="previewImage = null">×</button>
        <img :src="previewImage.url" :alt="previewImage.label" />
        <div>{{ previewImage.label }}</div>
      </section>
    </div>

    <div v-if="message" class="toast">{{ message }}</div>
  </main>
</template>
