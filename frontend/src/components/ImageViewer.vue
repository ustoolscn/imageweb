<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { ComponentPublicInstance } from 'vue'
import type { PreviewImage } from '../uiTypes'

const props = defineProps<{
  image: PreviewImage
  maskTool: 'brush' | 'eraser'
  maskBrushSize: number
  maskPreviewUrl: (url: string) => string
}>()

const emit = defineEmits<{
  close: []
  setBaseImage: [element: HTMLImageElement | null]
  setCanvas: [element: HTMLCanvasElement | null]
  imageLoad: []
  startDraw: [event: PointerEvent]
  moveDraw: [event: PointerEvent]
  stopDraw: [event: PointerEvent]
  clearMask: []
  saveMask: []
  'update:maskTool': [tool: 'brush' | 'eraser']
  'update:maskBrushSize': [size: number]
}>()

type TemplateRef = Element | ComponentPublicInstance | null

const zoom = ref(1)
const offset = ref({ x: 0, y: 0 })
const isDragging = ref(false)
const imageLoaded = ref(false)
const panSpeed = 1.45
const pointers = new Map<number, { x: number; y: number }>()
let dragStart: { pointerId: number; x: number; y: number } | null = null
let pinchStart: { distance: number; zoom: number; centerX: number; centerY: number; offsetX: number; offsetY: number } | null = null

const zoomable = computed(() => !props.image.editable)
const zoomStyle = computed(() => ({
  transform: `translate(${offset.value.x}px, ${offset.value.y}px) scale(${zoom.value})`,
}))

watch(() => props.image.url, () => {
  imageLoaded.value = false
  resetZoom()
})

function markImageLoaded() {
  imageLoaded.value = true
}

function clampZoom(value: number) {
  return Math.min(6, Math.max(1, value))
}

function applyZoom(nextZoom: number, centerX = 0, centerY = 0) {
  const clamped = clampZoom(nextZoom)
  if (clamped === 1) {
    zoom.value = 1
    offset.value = { x: 0, y: 0 }
    return
  }
  const previous = zoom.value
  const ratio = clamped / previous
  offset.value = {
    x: centerX - (centerX - offset.value.x) * ratio,
    y: centerY - (centerY - offset.value.y) * ratio,
  }
  zoom.value = clamped
}

function onWheel(event: WheelEvent) {
  if (!zoomable.value) return
  event.preventDefault()
  const rect = (event.currentTarget as HTMLElement).getBoundingClientRect()
  const centerX = event.clientX - rect.left - rect.width / 2
  const centerY = event.clientY - rect.top - rect.height / 2
  const factor = event.deltaY < 0 ? 1.12 : 0.88
  applyZoom(zoom.value * factor, centerX, centerY)
}

function pointerDistance() {
  const points = [...pointers.values()]
  if (points.length < 2) return 0
  return Math.hypot(points[0].x - points[1].x, points[0].y - points[1].y)
}

function pointerCenter(target: HTMLElement) {
  const points = [...pointers.values()]
  const rect = target.getBoundingClientRect()
  return {
    x: (points[0].x + points[1].x) / 2 - rect.left - rect.width / 2,
    y: (points[0].y + points[1].y) / 2 - rect.top - rect.height / 2,
  }
}

function onPointerDown(event: PointerEvent) {
  if (!zoomable.value) return
  event.preventDefault()
  pointers.set(event.pointerId, { x: event.clientX, y: event.clientY })
  ;(event.currentTarget as HTMLElement).setPointerCapture(event.pointerId)
  if (pointers.size === 2) {
    const center = pointerCenter(event.currentTarget as HTMLElement)
    pinchStart = { distance: pointerDistance(), zoom: zoom.value, centerX: center.x, centerY: center.y, offsetX: offset.value.x, offsetY: offset.value.y }
    dragStart = null
    isDragging.value = false
    return
  }
  if (zoom.value > 1) {
    dragStart = { pointerId: event.pointerId, x: event.clientX, y: event.clientY }
    isDragging.value = true
  }
}

function onPointerMove(event: PointerEvent) {
  if (!zoomable.value || !pointers.has(event.pointerId)) return
  event.preventDefault()
  pointers.set(event.pointerId, { x: event.clientX, y: event.clientY })
  if (pointers.size >= 2 && pinchStart) {
    const nextZoom = clampZoom(pinchStart.zoom * (pointerDistance() / Math.max(1, pinchStart.distance)))
    zoom.value = nextZoom
    offset.value = {
      x: pinchStart.offsetX + pointerCenter(event.currentTarget as HTMLElement).x - pinchStart.centerX,
      y: pinchStart.offsetY + pointerCenter(event.currentTarget as HTMLElement).y - pinchStart.centerY,
    }
    if (nextZoom === 1) offset.value = { x: 0, y: 0 }
    return
  }
  if (dragStart?.pointerId === event.pointerId && zoom.value > 1) {
    offset.value = {
      x: offset.value.x + (event.clientX - dragStart.x) * panSpeed,
      y: offset.value.y + (event.clientY - dragStart.y) * panSpeed,
    }
    dragStart = { pointerId: event.pointerId, x: event.clientX, y: event.clientY }
  }
}

function onPointerEnd(event: PointerEvent) {
  if (!zoomable.value) return
  pointers.delete(event.pointerId)
  ;(event.currentTarget as HTMLElement).releasePointerCapture(event.pointerId)
  if (pointers.size < 2) pinchStart = null
  if (dragStart?.pointerId === event.pointerId) dragStart = null
  isDragging.value = pointers.size === 1 && zoom.value > 1
}

function updateZoomFromSlider(event: Event) {
  applyZoom(Number((event.target as HTMLInputElement).value))
}

function resetZoom() {
  zoom.value = 1
  offset.value = { x: 0, y: 0 }
  isDragging.value = false
}

function setBaseImage(element: TemplateRef) {
  emit('setBaseImage', element instanceof HTMLImageElement ? element : null)
}

function setCanvas(element: TemplateRef) {
  emit('setCanvas', element instanceof HTMLCanvasElement ? element : null)
}

function updateBrushSize(event: Event) {
  emit('update:maskBrushSize', Number((event.target as HTMLInputElement).value))
}
</script>

<template>
  <div class="modal-backdrop image-viewer" @click.self="emit('close')">
    <section class="image-viewer-panel" :class="{ editable: image.editable }">
      <button class="modal-close" @click="emit('close')">×</button>
      <template v-if="image.editable">
        <div class="mask-stage">
          <img :ref="setBaseImage" :src="image.url" :alt="image.label" @load="emit('imageLoad')" />
          <canvas :ref="setCanvas" @pointerdown="emit('startDraw', $event)" @pointermove="emit('moveDraw', $event)" @pointerup="emit('stopDraw', $event)" @pointercancel="emit('stopDraw', $event)" />
        </div>
        <div class="mask-tools">
          <button :class="{ active: maskTool === 'brush' }" @click="emit('update:maskTool', 'brush')">涂抹蒙板</button>
          <button :class="{ active: maskTool === 'eraser' }" @click="emit('update:maskTool', 'eraser')">橡皮擦</button>
          <label>画笔 <input :value="maskBrushSize" type="range" min="8" max="120" @input="updateBrushSize" /></label>
          <button @click="emit('clearMask')">清空蒙板</button>
          <button class="primary" @click="emit('saveMask')">保存蒙板</button>
        </div>
      </template>
      <div
        v-else-if="image.maskUrl"
        class="mask-stage readonly-mask zoom-stage"
        :class="{ zoomed: zoom > 1, dragging: isDragging, loaded: imageLoaded }"
        @wheel="onWheel"
        @pointerdown.prevent="onPointerDown"
        @pointermove.prevent="onPointerMove"
        @pointerup="onPointerEnd"
        @pointercancel="onPointerEnd"
        @dblclick="resetZoom"
      >
        <div v-if="!imageLoaded" class="image-viewer-placeholder">加载原图中</div>
        <div class="zoom-content" :style="zoomStyle">
          <img :src="image.url" :alt="image.label" @load="markImageLoaded" />
          <img class="readonly-mask-overlay" :src="maskPreviewUrl(image.maskUrl)" alt="蒙板" />
        </div>
      </div>
      <div
        v-else
        class="zoom-stage"
        :class="{ zoomed: zoom > 1, dragging: isDragging, loaded: imageLoaded }"
        @wheel="onWheel"
        @pointerdown.prevent="onPointerDown"
        @pointermove.prevent="onPointerMove"
        @pointerup="onPointerEnd"
        @pointercancel="onPointerEnd"
        @dblclick="resetZoom"
      >
        <div v-if="!imageLoaded" class="image-viewer-placeholder">加载原图中</div>
        <img class="zoom-content" :style="zoomStyle" :src="image.url" :alt="image.label" @load="markImageLoaded" />
      </div>
      <div v-if="!image.editable" class="zoom-controls" @click.stop>
        <span>{{ Math.round(zoom * 100) }}%</span>
        <input :value="zoom" type="range" min="1" max="6" step="0.05" aria-label="缩放比例" @input="updateZoomFromSlider" />
        <button type="button" @click="resetZoom">重置</button>
      </div>
      <div>{{ image.label }}{{ image.maskUrl ? ' · 蒙板' : '' }}</div>
    </section>
  </div>
</template>
