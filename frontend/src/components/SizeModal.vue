<script setup lang="ts">
import { computed } from 'vue'
import { ratioPreviewStyle, type SizeBase } from '../lib/sizes'

type SizeBaseOption = {
  value: SizeBase
  label: string
  description: string
}

const props = defineProps<{
  currentSize: string
  draftSize: string
  selectedBase: SizeBase
  selectedRatio: string
  ratioOptions: string[]
  sizeBaseOptions: SizeBaseOption[]
}>()

const emit = defineEmits<{
  close: []
  selectBase: [base: SizeBase]
  selectRatio: [ratio: string]
  apply: []
}>()

const sizeHint = computed(() => {
  if (props.selectedBase === 'auto') return '自动模式会把 size 参数设置为 auto，由模型自动选择合适尺寸。'
  return `${props.selectedBase} 分辨率，共 ${props.ratioOptions.length} 种比例可选。`
})
</script>

<template>
  <div class="modal-backdrop" @click.self="emit('close')">
    <section class="size-modal light-modal">
      <button class="modal-close" @click="emit('close')">×</button>
      <h2>设置图像尺寸</h2>
      <p class="current-size">当前：{{ currentSize }}</p>
      <h3>清晰度</h3>
      <div class="option-grid four size-bases">
        <button v-for="item in sizeBaseOptions" :key="item.value" :class="{ active: selectedBase === item.value }" @click="emit('selectBase', item.value)">
          <strong>{{ item.label }}</strong>
          <span>{{ item.description }}</span>
        </button>
      </div>
      <h3>图像比例</h3>
      <div class="option-grid four ratios" :class="{ muted: selectedBase === 'auto' }">
        <button v-for="item in ratioOptions" :key="item" :class="{ active: selectedRatio === item }" @click="emit('selectRatio', item)">
          <span class="ratio-preview"><i :style="ratioPreviewStyle(item)"></i></span>
          <span>{{ item }}</span>
        </button>
      </div>
      <div class="will-use">
        <span>将使用</span>
        <strong>{{ draftSize }}</strong>
        <em>{{ sizeHint }}</em>
      </div>
      <div class="modal-actions-row">
        <button class="cancel" @click="emit('close')">取消</button>
        <button class="confirm" @click="emit('apply')">确定</button>
      </div>
    </section>
  </div>
</template>
