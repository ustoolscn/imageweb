<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import type { PlazaItem } from '../types'
import { canvasPreviewMedia } from '../lib/canvasPreview'
import { imageSizeLabel } from '../lib/sizes'
import { formatTime, isVideoTask, taskReferenceImages } from '../lib/view'
import { videoRatioLabel } from '../lib/videoModels'
import AppIcon from './AppIcon.vue'

const props = defineProps<{
  item: PlazaItem
}>()

const emit = defineEmits<{
  close: []
  openPreview: [url: string, label: string, event: Event, maskUrl?: string]
  reuse: [item: PlazaItem]
  openResult: [item: PlazaItem]
  toggleLike: [item: PlazaItem, event: Event]
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
const detailVideo = computed(() => props.item.result_videos?.[0])
const detailVideoCover = computed(() => detailVideo.value?.thumbnail_url || detailVideo.value?.first_frame_url || '')
const canvasPreview = computed(() => canvasPreviewMedia(props.item))
const canvasDetailVideoCover = computed(() => canvasPreview.value?.type === 'video' ? canvasPreview.value.thumbnail_url || '' : '')
const canvasNodeCount = computed(() => {
  const canvas = props.item.canvas as { elements?: unknown[] } | undefined
  return Array.isArray(canvas?.elements) ? canvas.elements.length : 0
})
const canvasConnectionCount = computed(() => {
  const canvas = props.item.canvas as { connections?: unknown[] } | undefined
  return Array.isArray(canvas?.connections) ? canvas.connections.length : 0
})

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
        <template v-if="item.item_type === 'canvas'">
          <video v-if="canvasPreview?.type === 'video' && canvasPreview.url" class="detail-video ready" :poster="canvasDetailVideoCover" :src="canvasPreview.url" controls playsinline preload="metadata" crossorigin="anonymous" :style="mediaTransform" />
          <img v-else-if="canvasPreview?.url" :src="canvasPreview.url" :alt="canvasPreview.label || '画布预览'" loading="lazy" decoding="async" draggable="false" crossorigin="anonymous" :style="mediaTransform" />
          <div v-else class="detail-canvas-preview">
            <AppIcon name="canvas" :size="54" />
            <strong>{{ item.canvas_name || '广场画布' }}</strong>
            <span>{{ canvasNodeCount }} 个节点 · {{ canvasConnectionCount }} 条连线</span>
          </div>
        </template>
        <template v-else-if="isVideoTask(item) && detailVideo?.url">
          <img v-if="!videoReady && detailVideoCover" class="detail-video-poster" :src="detailVideoCover" alt="视频封面" draggable="false" crossorigin="anonymous" />
          <div v-if="!videoReady" class="detail-video-loading">
            <span></span>
            <strong>正在加载视频</strong>
          </div>
          <video ref="videoRef" class="detail-video" :class="{ ready: videoReady }" :src="detailVideo.url" :controls="videoReady" autoplay playsinline preload="auto" crossorigin="anonymous" :style="mediaTransform" @loadeddata="videoReady = true" @canplay="videoReady = true" />
        </template>
        <img v-else-if="item.result_images?.[0]?.url" :src="item.result_images[0].url" alt="广场作品" loading="lazy" decoding="async" draggable="false" crossorigin="anonymous" :style="mediaTransform" />
      </div>
      <div class="detail-info" :class="{ open: showInfo }" @wheel.stop>
        <div class="detail-section detail-input-section">
          <div class="section-title">{{ item.item_type === 'canvas' ? '画布说明' : '输入内容' }}</div>
          <p class="detail-prompt">{{ item.prompt }}</p>
        </div>
        <div v-if="taskReferenceImages(item).length" class="detail-section">
          <div class="section-title">参考图片</div>
          <div class="detail-references">
            <button v-for="(image, index) in taskReferenceImages(item)" :key="`${image.url}-${index}`" type="button" @click="emit('openPreview', image.url, image.filename || `参考图 ${index + 1}`, $event, image.mask_url)">
              <img :src="image.url" :alt="image.filename || '参考图'" loading="lazy" decoding="async" crossorigin="anonymous" />
              <span>{{ image.filename || `参考 ${index + 1}` }}{{ image.mask_url ? ' · 蒙板' : '' }}</span>
            </button>
          </div>
        </div>
        <div class="detail-section">
          <div class="section-title">参数配置</div>
          <div class="detail-source">广场作品 · {{ item.model }} · ♥ {{ item.like_count }}</div>
          <div class="detail-params">
            <template v-if="item.item_type === 'canvas'">
              <div><span>画布</span><strong>{{ item.canvas_name || '未命名' }}</strong></div>
              <div><span>节点</span><strong>{{ canvasNodeCount }}</strong></div>
              <div><span>连线</span><strong>{{ canvasConnectionCount }}</strong></div>
            </template>
            <template v-else>
              <div><span>{{ isVideoTask(item) ? '分辨率' : '尺寸' }}</span><strong>{{ isVideoTask(item) ? `${item.video_width || 0}x${item.video_height || 0}` : imageSizeLabel(item.size) }}</strong></div>
              <div><span>{{ isVideoTask(item) ? '比例' : '质量' }}</span><strong>{{ isVideoTask(item) ? videoRatioLabel(item.video_ratio) : item.quality }}</strong></div>
              <div><span>{{ isVideoTask(item) ? '时长' : '格式' }}</span><strong>{{ isVideoTask(item) ? `${item.video_duration || 0}s` : item.output_format }}</strong></div>
              <div><span>审核</span><strong>{{ item.moderation }}</strong></div>
              <div><span>请求</span><strong>{{ item.stream ? '流式' : '普通' }}</strong></div>
            </template>
          </div>
        </div>
        <p class="detail-time">发布于 {{ formatTime(item.created_at) }}</p>
        <div class="detail-buttons plaza-detail-buttons">
          <button class="blue" @click="emit('reuse', item); emit('close')">
            <AppIcon name="copy" />
            <span>{{ item.item_type === 'canvas' ? '导入画布' : '复用配置' }}</span>
          </button>
          <button class="purple" :disabled="item.item_type === 'canvas' || !(item.result_images?.[0]?.url || item.result_videos?.[0]?.url)" @click="emit('openResult', item)">
            <AppIcon name="download" />
            <span>{{ isVideoTask(item) ? '下载视频' : '下载图片' }}</span>
          </button>
          <button class="star" :class="{ favorite: item.liked }" @click="emit('toggleLike', item, $event)">
            <AppIcon name="favorite" />
            <span>{{ item.liked ? '取消点赞' : '点赞' }} {{ item.like_count }}</span>
          </button>
        </div>
      </div>
    </section>
  </div>
</template>
