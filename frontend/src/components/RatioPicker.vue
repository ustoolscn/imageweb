<script setup lang="ts">
import { ref } from 'vue'

defineProps<{
  modelValue: string
  ratios: string[]
  compact?: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: string]
}>()

const menu = ref<HTMLDetailsElement | null>(null)

function previewStyle(ratio: string) {
  if (ratio === 'adaptive') return { width: '18px', height: '18px' }
  const [rawWidth, rawHeight] = ratio.split(':').map(Number)
  const widthRatio = Number.isFinite(rawWidth) && rawWidth > 0 ? rawWidth : 1
  const heightRatio = Number.isFinite(rawHeight) && rawHeight > 0 ? rawHeight : 1
  const maxWidth = 28
  const maxHeight = 20
  const scale = Math.min(maxWidth / widthRatio, maxHeight / heightRatio)
  return {
    width: `${Math.max(6, Math.round(widthRatio * scale))}px`,
    height: `${Math.max(6, Math.round(heightRatio * scale))}px`,
    aspectRatio: `${widthRatio} / ${heightRatio}`,
  }
}

function choose(ratio: string) {
  emit('update:modelValue', ratio)
  if (menu.value) menu.value.open = false
}

function ratioLabel(ratio: string) {
  return ratio === 'adaptive' ? 'auto' : ratio
}
</script>

<template>
  <details ref="menu" class="ratio-picker" :class="{ compact }">
    <summary class="ratio-trigger">
      <span class="video-ratio-preview" :class="{ adaptive: modelValue === 'adaptive' }">
        <span :style="previewStyle(modelValue)"></span>
      </span>
      <strong>{{ ratioLabel(modelValue) }}</strong>
      <span class="ratio-caret" aria-hidden="true"></span>
    </summary>
    <div class="ratio-menu">
      <button v-for="ratio in ratios" :key="ratio" type="button" class="ratio-option" :class="{ active: ratio === modelValue }" @click="choose(ratio)">
        <span class="video-ratio-preview" :class="{ adaptive: ratio === 'adaptive' }">
          <span :style="previewStyle(ratio)"></span>
        </span>
        <strong>{{ ratioLabel(ratio) }}</strong>
        <span class="ratio-check" aria-hidden="true">✓</span>
      </button>
    </div>
  </details>
</template>
