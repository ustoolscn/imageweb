<script setup lang="ts">
import type { UploadedImage } from '../types'
import type { ImageForm, PendingReferenceImage, PreviewSource } from '../uiTypes'
import { displayImageURL } from '../lib/view'

defineProps<{
  form: ImageForm
  models: string[]
  submitting: boolean
  hasConfig: boolean
  reusedReferenceImages: UploadedImage[]
  referenceImages: PendingReferenceImage[]
}>()

const emit = defineEmits<{
  submit: []
  updateField: [field: keyof ImageForm, value: string | number]
  promptPaste: [event: ClipboardEvent]
  openEditablePreview: [source: PreviewSource, index: number, url: string, label: string, event: Event]
  removeReusedReference: [index: number]
  removeReference: [index: number]
  openSizeModal: []
  referenceChange: [event: Event]
}>()

function textValue(event: Event) {
  return (event.target as HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement).value
}

function numberValue(event: Event) {
  return Number((event.target as HTMLInputElement).value)
}
</script>

<template>
  <form class="composer glass-panel" @submit.prevent="emit('submit')">
    <div class="prompt-row">
      <textarea :value="form.prompt" placeholder="描述你想生成的图片..." rows="2" @input="emit('updateField', 'prompt', textValue($event))" @paste="emit('promptPaste', $event)" />
      <button class="submit" :disabled="submitting || !hasConfig">{{ submitting ? '提交中' : '生成' }}</button>
    </div>

    <div v-if="reusedReferenceImages.length || referenceImages.length" class="preview-strip">
      <div v-for="(image, index) in reusedReferenceImages" :key="image.url" class="input-thumb reused">
        <img :src="displayImageURL(image)" alt="参考图" loading="lazy" decoding="async" @click="emit('openEditablePreview', 'reused', index, image.url, `参考 ${index + 1}`, $event)" />
        <span>参考 {{ index + 1 }}{{ image.mask_url ? ' · 蒙板' : '' }}</span>
        <button type="button" @click="emit('removeReusedReference', index)">×</button>
      </div>
      <div v-for="(image, index) in referenceImages" :key="image.preview_url" class="input-thumb" :class="{ uploading: image.uploading, failed: image.upload_error }">
        <img :src="image.preview_url" alt="参考图" @click="emit('openEditablePreview', 'new', index, image.preview_url, image.filename || `参考 ${reusedReferenceImages.length + index + 1}`, $event)" />
        <span>{{ image.filename || `参考 ${reusedReferenceImages.length + index + 1}` }}{{ image.mask_url ? ' · 蒙板' : '' }}{{ image.uploading ? ' · 上传中' : '' }}{{ image.upload_error ? ' · 上传失败' : '' }}</span>
        <button type="button" @click="emit('removeReference', index)">×</button>
      </div>
    </div>

    <div class="form-row">
      <label>模型<select :value="form.model" @change="emit('updateField', 'model', textValue($event))"><option v-for="item in models" :key="item" :value="item">{{ item }}</option></select></label>
      <div class="field"><span>尺寸</span><button type="button" class="size-trigger" @click.stop.prevent="emit('openSizeModal')">{{ form.size }}</button></div>
      <label>质量<select :value="form.quality" @change="emit('updateField', 'quality', textValue($event))"><option>auto</option><option>low</option><option>medium</option><option>high</option></select></label>
      <label>格式<select :value="form.output_format" @change="emit('updateField', 'output_format', textValue($event))"><option>png</option><option>jpeg</option><option>webp</option></select></label>
      <label>压缩<input :value="form.output_compression" type="number" min="0" max="100" @input="emit('updateField', 'output_compression', numberValue($event))" /></label>
      <label>背景<select :value="form.background" @change="emit('updateField', 'background', textValue($event))"><option>auto</option><option>opaque</option></select></label>
      <label>审核<select :value="form.moderation" @change="emit('updateField', 'moderation', textValue($event))"><option>low</option><option>auto</option></select></label>
      <label>数量<input :value="form.n" type="number" min="1" max="10" @input="emit('updateField', 'n', numberValue($event))" /></label>
    </div>
    <div class="upload-row single-upload">
      <label class="upload">＋ 参考图<input type="file" multiple accept="image/*" @change="emit('referenceChange', $event)" /></label>
      <span class="hint">所有上传图和生成结果都会转存到图床后保存。</span>
    </div>
  </form>
</template>
