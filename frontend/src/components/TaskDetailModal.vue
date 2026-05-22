<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import type { Task } from '../types'
import { imageSizeLabel } from '../lib/sizes'
import { canShareTask, formatTime, isFavorite, isVideoTask, maskBaseURL, queueText, statusText, taskReferenceImages, timeText } from '../lib/view'
import { videoRatioLabel } from '../lib/videoModels'
import { ensureVideoCover, videoCoverURL } from '../lib/videoCover'
import AppIcon from './AppIcon.vue'

const props = defineProps<{
  task: Task
  clock: number
}>()

const emit = defineEmits<{
  close: []
  openPreview: [url: string, label: string, event: Event, maskUrl?: string]
  reuse: [task: Task]
  rerun: [task: Task]
  openResult: [task: Task]
  addResultToReferences: [task: Task]
  toggleShare: [task: Task, event: Event]
  remove: [task: Task]
  toggleFavorite: [task: Task, event: Event]
}>()

const showInfo = ref(true)
const mediaZoom = ref(1)
const mediaPan = ref({ x: 0, y: 0 })
const activePan = ref<{ pointerId: number; startX: number; startY: number; originX: number; originY: number } | null>(null)
const videoRef = ref<HTMLVideoElement | null>(null)
const videoReady = ref(false)
let previousBodyOverflow = ''
let previousDocumentOverflow = ''

const mediaTransform = computed(() => ({
  transform: `translate(calc(-50% + ${mediaPan.value.x}px), calc(-50% + ${mediaPan.value.y}px)) scale(${mediaZoom.value})`,
}))
const detailVideo = computed(() => props.task.result_videos?.[0])
const detailVideoCover = computed(() => detailVideo.value?.thumbnail_url || videoCoverURL(detailVideo.value?.url))

function clampZoom(value: number) {
  return Math.min(6, Math.max(1, value))
}

function zoomMedia(event: WheelEvent) {
  event.preventDefault()
  const nextZoom = clampZoom(mediaZoom.value * (event.deltaY < 0 ? 1.12 : 0.88))
  mediaZoom.value = nextZoom
  if (nextZoom === 1) mediaPan.value = { x: 0, y: 0 }
}

function startMediaPan(event: PointerEvent) {
  if (mediaZoom.value <= 1) return
  event.preventDefault()
  activePan.value = {
    pointerId: event.pointerId,
    startX: event.clientX,
    startY: event.clientY,
    originX: mediaPan.value.x,
    originY: mediaPan.value.y,
  }
  ;(event.currentTarget as HTMLElement).setPointerCapture(event.pointerId)
}

function moveMediaPan(event: PointerEvent) {
  const pan = activePan.value
  if (!pan || pan.pointerId !== event.pointerId) return
  mediaPan.value = {
    x: pan.originX + event.clientX - pan.startX,
    y: pan.originY + event.clientY - pan.startY,
  }
}

function stopMediaPan(event: PointerEvent) {
  const pan = activePan.value
  if (!pan || pan.pointerId !== event.pointerId) return
  activePan.value = null
  ;(event.currentTarget as HTMLElement).releasePointerCapture(event.pointerId)
}

function closeOnEscape(event: KeyboardEvent) {
  if (event.key === 'Escape') emit('close')
}

onMounted(() => {
  previousBodyOverflow = document.body.style.overflow
  previousDocumentOverflow = document.documentElement.style.overflow
  document.documentElement.classList.add('detail-modal-open')
  document.body.style.overflow = 'hidden'
  document.documentElement.style.overflow = 'hidden'
  window.addEventListener('keydown', closeOnEscape)
  if (detailVideo.value?.url && !detailVideo.value.thumbnail_url) ensureVideoCover(detailVideo.value.url).catch(() => {})
  nextTick(() => {
    const video = videoRef.value
    if (!video) return
    video.muted = false
    video.volume = 1
    video.play().catch(() => {})
  })
})

onBeforeUnmount(() => {
  document.body.style.overflow = previousBodyOverflow
  document.documentElement.style.overflow = previousDocumentOverflow
  document.documentElement.classList.remove('detail-modal-open')
  window.removeEventListener('keydown', closeOnEscape)
})
</script>

<template>
  <div class="modal-backdrop detail-backdrop" @click.self="emit('close')" @contextmenu.prevent.stop @wheel.self.prevent.stop>
    <section class="detail-modal light-modal">
      <button class="detail-info-toggle" :class="{ active: showInfo }" type="button" @click="showInfo = !showInfo">
        {{ showInfo ? '收起信息' : '查看信息' }}
      </button>
      <button class="modal-close" @click="emit('close')"><AppIcon name="close" /></button>
      <div
        class="detail-preview"
        :class="{ zoomed: mediaZoom > 1, panning: activePan }"
        @wheel="zoomMedia"
        @pointerdown="startMediaPan"
        @pointermove="moveMediaPan"
        @pointerup="stopMediaPan"
        @pointercancel="stopMediaPan"
      >
        <template v-if="isVideoTask(task) && detailVideo?.url">
          <img v-if="!videoReady && detailVideoCover" class="detail-video-poster" :src="detailVideoCover" alt="视频封面" draggable="false" />
          <div v-if="!videoReady" class="detail-video-loading">
            <span></span>
            <strong>正在加载视频</strong>
          </div>
          <video ref="videoRef" class="detail-video" :class="{ ready: videoReady }" :src="detailVideo.url" :controls="videoReady" autoplay playsinline preload="auto" :style="mediaTransform" @loadeddata="videoReady = true" @canplay="videoReady = true" />
        </template>
        <img v-else-if="task.result_images?.[0]?.url" :src="task.result_images[0].url" alt="生成结果" loading="lazy" decoding="async" draggable="false" :style="mediaTransform" />
        <div v-else class="detail-state">
          <span>{{ task.status === 'failed' ? '!' : '...' }}</span>
          <p>{{ task.error_message || statusText(task.status) }}</p>
        </div>
      </div>
      <div class="detail-info" :class="{ open: showInfo }" @wheel.stop>
        <div class="detail-section detail-input-section">
          <div class="section-title">输入内容</div>
          <p class="detail-prompt">{{ task.prompt }}</p>
        </div>
        <div v-if="taskReferenceImages(task).length" class="detail-section">
          <div class="section-title">参考图片</div>
          <div class="detail-references">
            <button v-for="(image, index) in taskReferenceImages(task)" :key="`${image.url}-${index}`" type="button" @click="emit('openPreview', image.url, image.filename || `参考图 ${index + 1}`, $event, image.mask_url)">
              <img :src="image.url" :alt="image.filename || '参考图'" loading="lazy" decoding="async" />
              <span>{{ image.filename || `参考 ${index + 1}` }}{{ image.mask_url ? ' · 蒙板' : '' }}</span>
            </button>
          </div>
        </div>
        <div v-if="isVideoTask(task) && (task.reference_videos?.length || task.reference_audios?.length)" class="detail-section">
          <div class="section-title">参考媒体</div>
          <div class="detail-source">{{ task.reference_videos?.length || 0 }} 个视频 · {{ task.reference_audios?.length || 0 }} 个音频</div>
        </div>
        <div class="detail-section">
          <div class="section-title">参数配置</div>
          <div class="detail-source">来源 <strong>{{ maskBaseURL(task.baseurl) }}</strong> · {{ task.model }}</div>
          <div class="detail-params">
            <div><span>{{ isVideoTask(task) ? '分辨率' : '尺寸' }}</span><strong>{{ isVideoTask(task) ? `${task.video_width || 0}x${task.video_height || 0}` : imageSizeLabel(task.size) }}</strong></div>
            <div><span>{{ isVideoTask(task) ? '比例' : '质量' }}</span><strong>{{ isVideoTask(task) ? videoRatioLabel(task.video_ratio) : task.quality }}</strong></div>
            <div><span>{{ isVideoTask(task) ? '时长' : '格式' }}</span><strong>{{ isVideoTask(task) ? `${task.video_duration || 0}s` : task.output_format }}</strong></div>
            <div><span>{{ isVideoTask(task) ? '进度' : '审核' }}</span><strong>{{ isVideoTask(task) ? `${task.upstream_progress || 0}%` : task.moderation }}</strong></div>
            <div><span>时间</span><strong>{{ timeText(task, clock) }}</strong></div>
            <div v-if="queueText(task)"><span>排队</span><strong>{{ queueText(task) }}</strong></div>
          </div>
        </div>
        <p class="detail-time">创建于 {{ formatTime(task.created_at) }} · 状态 {{ queueText(task) || statusText(task.status) }}</p>
        <div class="detail-buttons">
          <button class="blue" @click="emit('reuse', task); emit('close')">
            <AppIcon name="copy" />
            <span>复用配置</span>
          </button>
          <button class="green" @click="emit('rerun', task); emit('close')">
            <AppIcon name="refresh" />
            <span>重新生成</span>
          </button>
          <button class="purple" :disabled="!(task.result_images?.[0]?.url || task.result_videos?.[0]?.url)" @click="emit('openResult', task)">
            <AppIcon name="download" />
            <span>{{ isVideoTask(task) ? '下载视频' : '下载图片' }}</span>
          </button>
          <button class="cyan" :disabled="isVideoTask(task) || !task.result_images?.[0]?.url" @click="emit('addResultToReferences', task)">
            <AppIcon name="add" />
            <span>加入参考</span>
          </button>
          <button class="orange" :class="{ favorite: task.shared_to_plaza }" :disabled="!canShareTask(task)" @click="emit('toggleShare', task, $event)">
            <AppIcon name="share" />
            <span>{{ task.shared_to_plaza ? '取消分享' : '分享广场' }}</span>
          </button>
          <button class="red" @click="emit('remove', task)">
            <AppIcon name="trash" />
            <span>删除记录</span>
          </button>
          <button class="star" :class="{ favorite: isFavorite(task) }" :title="isFavorite(task) ? '取消收藏' : '收藏'" :aria-label="isFavorite(task) ? '取消收藏' : '收藏'" @click="emit('toggleFavorite', task, $event)">
            <AppIcon name="favorite" />
          </button>
        </div>
      </div>
    </section>
  </div>
</template>
