<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import type { ComponentPublicInstance } from 'vue'
import type { PreviewImage } from '../uiTypes'
import AppIcon from './AppIcon.vue'

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
let localCanvas: HTMLCanvasElement | null = null
let maskPaintState: { point: { x: number; y: number } } | null = null
let activeMaskPointer: { pointerId: number; canvas: HTMLCanvasElement; tool: 'brush' | 'eraser'; brushSize: number } | null = null

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
  localCanvas = element instanceof HTMLCanvasElement ? element : null
  emit('setCanvas', localCanvas)
}

function updateBrushSize(event: Event) {
  emit('update:maskBrushSize', Number((event.target as HTMLInputElement).value))
}

function clampNumber(value: number, min: number, max: number) {
  return Math.min(max, Math.max(min, value))
}

function maskPoint(event: PointerEvent, canvas: HTMLCanvasElement) {
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

function drawMask(event: PointerEvent, canvas: HTMLCanvasElement, connectFromLast = true) {
  const ctx = canvas.getContext('2d')
  if (!ctx) return
  const point = maskPoint(event, canvas)
  const tool = activeMaskPointer?.tool || props.maskTool
  const brushSize = activeMaskPointer?.brushSize || props.maskBrushSize
  ctx.globalCompositeOperation = tool === 'eraser' ? 'destination-out' : 'source-over'
  ctx.strokeStyle = '#fff'
  ctx.fillStyle = '#fff'
  ctx.lineWidth = brushSize
  ctx.lineCap = 'round'
  ctx.lineJoin = 'round'
  ctx.beginPath()
  const last = maskPaintState?.point || null
  if (connectFromLast && last) {
    ctx.moveTo(last.x, last.y)
    ctx.lineTo(point.x, point.y)
    ctx.stroke()
  } else {
    ctx.arc(point.x, point.y, brushSize / 2, 0, Math.PI * 2)
    ctx.fill()
  }
  maskPaintState = { point }
}

function startMaskPaint(event: PointerEvent) {
  if (!props.image.editable || event.button !== 0) return
  event.preventDefault()
  event.stopPropagation()
  const canvas = event.currentTarget as HTMLCanvasElement
  canvas.setPointerCapture(event.pointerId)
  activeMaskPointer = { pointerId: event.pointerId, canvas, tool: props.maskTool, brushSize: props.maskBrushSize }
  maskPaintState = null
  window.addEventListener('pointermove', moveMaskPaintFromWindow, { passive: false })
  window.addEventListener('pointerup', stopMaskPaintFromWindow, { passive: false })
  window.addEventListener('pointercancel', stopMaskPaintFromWindow, { passive: false })
  drawMask(event, canvas, false)
}

function moveMaskPaint(event: PointerEvent) {
  event.preventDefault()
  event.stopPropagation()
  const canvas = event.currentTarget as HTMLCanvasElement
  if (!isActiveMaskEvent(event)) return
  const events = event.getCoalescedEvents?.() || [event]
  events.forEach((item) => drawMask(item, canvas))
}

function stopMaskPaint(event: PointerEvent) {
  event.preventDefault()
  event.stopPropagation()
  const canvas = event.currentTarget as HTMLCanvasElement
  finishMaskPaint(event, canvas)
}

function moveMaskPaintFromWindow(event: PointerEvent) {
  if (!activeMaskPointer || !isActiveMaskEvent(event)) return
  event.preventDefault()
  const events = event.getCoalescedEvents?.() || [event]
  events.forEach((item) => drawMask(item, activeMaskPointer!.canvas))
}

function stopMaskPaintFromWindow(event: PointerEvent) {
  if (!activeMaskPointer || !isActiveMaskEvent(event)) return
  event.preventDefault()
  finishMaskPaint(event, activeMaskPointer.canvas)
}

function isActiveMaskEvent(event: PointerEvent) {
  return activeMaskPointer?.pointerId === event.pointerId
}

function finishMaskPaint(event: PointerEvent, canvas: HTMLCanvasElement) {
  if (!isActiveMaskEvent(event)) return
  if (canvas.hasPointerCapture(event.pointerId)) canvas.releasePointerCapture(event.pointerId)
  window.removeEventListener('pointermove', moveMaskPaintFromWindow)
  window.removeEventListener('pointerup', stopMaskPaintFromWindow)
  window.removeEventListener('pointercancel', stopMaskPaintFromWindow)
  maskPaintState = null
  activeMaskPointer = null
}

onUnmounted(() => {
  window.removeEventListener('pointermove', moveMaskPaintFromWindow)
  window.removeEventListener('pointerup', stopMaskPaintFromWindow)
  window.removeEventListener('pointercancel', stopMaskPaintFromWindow)
})
</script>

<template>
  <div class="modal-backdrop image-viewer" @click.self="emit('close')">
    <section class="image-viewer-panel" :class="{ editable: image.editable }">
      <button class="modal-close" @click="emit('close')"><AppIcon name="close" /></button>
      <template v-if="image.editable">
        <div class="mask-stage">
          <img :ref="setBaseImage" :src="image.url" :alt="image.label" @load="emit('imageLoad')" />
          <canvas :ref="setCanvas" @pointerdown.stop.prevent="startMaskPaint" @pointermove.stop.prevent="moveMaskPaint" @pointerup.stop.prevent="stopMaskPaint" @pointercancel.stop.prevent="stopMaskPaint" />
        </div>
        <div class="mask-tools">
          <button :class="{ active: maskTool === 'brush' }" @click="emit('update:maskTool', 'brush')"><AppIcon name="brush" />涂抹蒙板</button>
          <button :class="{ active: maskTool === 'eraser' }" @click="emit('update:maskTool', 'eraser')"><AppIcon name="eraser" />橡皮擦</button>
          <label>画笔 <input :value="maskBrushSize" type="range" min="8" max="120" @input="updateBrushSize" /></label>
          <button @click="emit('clearMask')"><AppIcon name="trash" />清空蒙板</button>
          <button class="primary" @click="emit('saveMask')"><AppIcon name="download" />保存蒙板</button>
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
        <button type="button" @click="resetZoom"><AppIcon name="resetView" />重置</button>
      </div>
      <div>{{ image.label }}{{ image.maskUrl ? ' · 蒙板' : '' }}</div>
    </section>
  </div>
</template>
