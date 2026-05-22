<script setup lang="ts">
import { computed } from 'vue'
import { ratioPreviewStyle } from '../lib/sizes'
import { videoRatioLabel } from '../lib/videoModels'
import type { VideoResolution } from '../lib/videoModels'
import AppIcon from './AppIcon.vue'

const props = defineProps<{
  currentSize: string
  draftSize: string
  selectedResolution: VideoResolution
  selectedRatio: string
  ratioOptions: string[]
  resolutionOptions: VideoResolution[]
}>()

const emit = defineEmits<{
  close: []
  selectResolution: [resolution: VideoResolution]
  selectRatio: [ratio: string]
  apply: []
}>()

const sizeHint = computed(() => `${props.selectedResolution.toUpperCase()} · ${videoRatioLabel(props.selectedRatio)}`)
</script>

<template>
  <div class="modal-backdrop" @click.self="emit('close')" @wheel.self.prevent.stop>
    <section class="size-modal light-modal">
      <button class="modal-close" @click="emit('close')"><AppIcon name="close" /></button>
      <h2>设置视频尺寸</h2>
      <p class="current-size">当前：{{ currentSize }}</p>
      <h3>分辨率</h3>
      <div class="option-grid four size-bases">
        <button v-for="item in resolutionOptions" :key="item" :class="{ active: selectedResolution === item }" @click="emit('selectResolution', item)">
          <strong>{{ item.toUpperCase() }}</strong>
          <span>视频清晰度</span>
        </button>
      </div>
      <h3>视频比例</h3>
      <div class="option-grid four ratios">
        <button v-for="item in ratioOptions" :key="item" :class="{ active: selectedRatio === item }" @click="emit('selectRatio', item)">
          <span class="ratio-preview" :class="{ auto: item === 'adaptive' }"><i :style="ratioPreviewStyle(item === 'adaptive' ? '16:9' : item)"></i></span>
          <span>{{ videoRatioLabel(item) }}</span>
        </button>
      </div>
      <div class="will-use">
        <span>将使用</span>
        <strong>{{ draftSize }}</strong>
        <em>{{ sizeHint }}</em>
      </div>
      <div class="modal-actions-row">
        <button class="cancel" @click="emit('close')"><AppIcon name="close" />取消</button>
        <button class="confirm" @click="emit('apply')"><AppIcon name="check" />确定</button>
      </div>
    </section>
  </div>
</template>
