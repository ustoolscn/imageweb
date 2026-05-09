<script setup lang="ts">
import { ratioPreviewStyle } from '../lib/sizes'

defineProps<{
  currentSize: string
  draftSize: string
  selectedRatio: string
  ratioOptions: string[]
}>()

const emit = defineEmits<{
  close: []
  selectRatio: [ratio: string]
  apply: []
}>()
</script>

<template>
  <div class="modal-backdrop" @click.self="emit('close')">
    <section class="size-modal light-modal">
      <button class="modal-close" @click="emit('close')">×</button>
      <h2>设置图像尺寸</h2>
      <p class="current-size">当前：{{ currentSize }}</p>
      <h3>图像比例</h3>
      <div class="option-grid four ratios">
        <button v-for="item in ratioOptions" :key="item" :class="{ active: selectedRatio === item }" @click="emit('selectRatio', item)">
          <span class="ratio-preview"><i :style="ratioPreviewStyle(item)"></i></span>
          <span>{{ item }}</span>
        </button>
      </div>
      <div class="will-use">
        <span>将使用</span>
        <strong>{{ draftSize }}</strong>
        <em>1K 标准分辨率，共 8 种比例可选。</em>
      </div>
      <div class="modal-actions-row">
        <button class="cancel" @click="emit('close')">取消</button>
        <button class="confirm" @click="emit('apply')">确定</button>
      </div>
    </section>
  </div>
</template>
