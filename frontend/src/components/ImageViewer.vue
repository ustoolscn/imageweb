<script setup lang="ts">
import type { ComponentPublicInstance } from 'vue'
import type { PreviewImage } from '../uiTypes'

defineProps<{
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
      <div v-else-if="image.maskUrl" class="mask-stage readonly-mask">
        <img :src="image.url" :alt="image.label" />
        <img class="readonly-mask-overlay" :src="maskPreviewUrl(image.maskUrl)" alt="蒙板" />
      </div>
      <img v-else :src="image.url" :alt="image.label" />
      <div>{{ image.label }}{{ image.maskUrl ? ' · 蒙板' : '' }}</div>
    </section>
  </div>
</template>
