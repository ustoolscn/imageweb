<script setup lang="ts">
import { computed } from 'vue'
import type { UploadedImage } from '../types'
import type { ImageForm, PendingReferenceAudio, PendingReferenceImage, PendingReferenceVideo, PreviewSource } from '../uiTypes'
import { videoModelCapability, videoRatioOptions, videoResolutionOptions } from '../lib/videoModels'
import { displayImageURL } from '../lib/view'
import AppIcon from './AppIcon.vue'
import RatioPicker from './RatioPicker.vue'

const props = defineProps<{
  form: ImageForm
  models: string[]
  submitting: boolean
  hasConfig: boolean
  reusedReferenceImages: UploadedImage[]
  referenceImages: PendingReferenceImage[]
  referenceVideos: PendingReferenceVideo[]
  referenceAudios: PendingReferenceAudio[]
}>()

const emit = defineEmits<{
  submit: [mode: ImageForm['task_type']]
  updateField: [field: keyof ImageForm, value: string | number | boolean]
  promptPaste: [event: ClipboardEvent]
  openEditablePreview: [source: PreviewSource, index: number, url: string, label: string, event: Event]
  removeReusedReference: [index: number]
  removeReference: [index: number]
  removeReferenceVideo: [index: number]
  removeReferenceAudio: [index: number]
  openReferenceVideoModal: []
  openReferenceAudioModal: []
  openSizeModal: []
  referenceChange: [event: Event]
}>()

const currentVideoRatios = computed(() => videoRatioOptions(props.form.model))
const currentVideoResolutions = computed(() => videoResolutionOptions(props.form.model))
const currentVideoCapability = computed(() => videoModelCapability(props.form.model))
const imageModelOptions = computed(() => props.models.filter((model) => model !== 'doubao-seedance-2.0'))
const isNanoBanana = computed(() => props.form.model === 'nano-banana-2')
const isSeedream = computed(() => props.form.model === 'doubao-seedream-5.0-lite')
const supportsTransparentBackground = computed(() => props.form.output_format === 'png')
const supportsOutputCompression = computed(() => props.form.output_format === 'jpeg' || props.form.output_format === 'webp')
const sizeLabel = computed(() => props.form.model === 'nano-banana-2' || props.form.model === 'doubao-seedream-5.0-lite' ? '比例/分辨率' : '尺寸')

function textValue(event: Event) {
  return (event.target as HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement).value
}

function numberValue(event: Event) {
  return Number((event.target as HTMLInputElement).value)
}

function checkedValue(event: Event) {
  return (event.target as HTMLInputElement).checked
}
</script>

<template>
  <form class="composer glass-panel" @submit.prevent="emit('submit', form.task_type)">
    <div class="mode-row">
      <button type="button" :class="{ active: form.task_type === 'image_generation' }" @click="emit('updateField', 'task_type', 'image_generation')"><AppIcon name="image" />图片生成</button>
      <button type="button" :class="{ active: form.task_type === 'video_generation' }" @click="emit('updateField', 'task_type', 'video_generation')"><AppIcon name="video" />视频生成</button>
    </div>

    <div class="prompt-row">
      <textarea :value="form.prompt" :placeholder="form.task_type === 'video_generation' ? '描述你想生成的视频...' : '描述你想生成的图片...'" rows="2" @input="emit('updateField', 'prompt', textValue($event))" @paste="emit('promptPaste', $event)" />
      <button class="submit" :disabled="submitting || !hasConfig" @click="emit('updateField', 'task_type', form.task_type)"><AppIcon name="play" />{{ submitting ? '提交中' : (form.task_type === 'video_generation' ? '生成视频' : '生成图片') }}</button>
    </div>

    <div v-if="reusedReferenceImages.length || referenceImages.length || referenceVideos.length || referenceAudios.length" class="preview-strip">
      <div v-for="(image, index) in reusedReferenceImages" :key="image.url" class="input-thumb reused">
        <img :src="displayImageURL(image)" alt="参考图" loading="lazy" decoding="async" @click="emit('openEditablePreview', 'reused', index, image.url, `参考 ${index + 1}`, $event)" />
        <span>参考 {{ index + 1 }}{{ image.mask_url ? ' · 蒙版' : '' }}</span>
        <button type="button" @click="emit('removeReusedReference', index)"><AppIcon name="close" :size="12" /></button>
      </div>
      <div v-for="(image, index) in referenceImages" :key="image.preview_url" class="input-thumb" :class="{ uploading: image.uploading, failed: image.upload_error }">
        <img :src="image.preview_url" alt="参考图" @click="emit('openEditablePreview', 'new', index, image.preview_url, image.filename || `参考 ${reusedReferenceImages.length + index + 1}`, $event)" />
        <span>{{ image.filename || `参考 ${reusedReferenceImages.length + index + 1}` }}{{ image.mask_url ? ' · 蒙版' : '' }}{{ image.uploading ? ' · 上传中' : '' }}{{ image.upload_error ? ' · 上传失败' : '' }}</span>
        <button type="button" @click="emit('removeReference', index)"><AppIcon name="close" :size="12" /></button>
      </div>
      <div v-for="(video, index) in referenceVideos" :key="video.url" class="input-thumb video-thumb" :class="{ uploading: video.loading, failed: video.error }">
        <img v-if="video.cover_url" :src="video.cover_url" alt="参考视频封面" />
        <video v-else :src="video.url" muted playsinline preload="metadata" />
        <span>{{ video.filename || `视频 ${index + 1}` }}</span>
        <button type="button" @click="emit('removeReferenceVideo', index)"><AppIcon name="close" :size="12" /></button>
      </div>
      <div v-for="(audio, index) in referenceAudios" :key="audio.url" class="input-thumb audio-thumb" :class="{ uploading: audio.loading, failed: audio.error }">
        <div class="audio-mark">音</div>
        <span>{{ audio.filename || `音频 ${index + 1}` }}</span>
        <button type="button" @click="emit('removeReferenceAudio', index)"><AppIcon name="close" :size="12" /></button>
      </div>
    </div>

    <div v-if="form.task_type === 'image_generation'" class="form-row">
      <label>模型<select :value="form.model" @change="emit('updateField', 'model', textValue($event))"><option v-for="item in imageModelOptions" :key="item" :value="item">{{ item }}</option></select></label>
      <div class="field"><span>{{ sizeLabel }}</span><button type="button" class="size-trigger" @click.stop.prevent="emit('openSizeModal')">{{ form.size }}</button></div>
      <label v-if="!isNanoBanana && !isSeedream">质量<select :value="form.quality" @change="emit('updateField', 'quality', textValue($event))"><option>auto</option><option>high</option><option>medium</option><option>low</option></select></label>
      <label v-if="!isNanoBanana">格式<select :value="form.output_format" @change="emit('updateField', 'output_format', textValue($event))"><option>png</option><option>jpeg</option><option v-if="!isSeedream">webp</option></select></label>
      <label v-if="!isNanoBanana && !isSeedream && supportsOutputCompression">压缩<input :value="form.output_compression" type="number" min="0" max="100" @input="emit('updateField', 'output_compression', numberValue($event))" /></label>
      <label v-if="!isSeedream">背景<select :value="form.background" @change="emit('updateField', 'background', textValue($event))"><option>auto</option><option v-if="supportsTransparentBackground">transparent</option><option>opaque</option></select></label>
      <label v-if="!isSeedream">审核<select :value="form.moderation" @change="emit('updateField', 'moderation', textValue($event))"><option>low</option><option>auto</option></select></label>
      <label v-if="!isNanoBanana && !isSeedream">保真<select :value="form.input_fidelity" @change="emit('updateField', 'input_fidelity', textValue($event))"><option>high</option><option>low</option></select></label>
    </div>

    <div v-else class="form-row video-form-row">
      <label>模型<select :value="form.model" @change="emit('updateField', 'model', textValue($event))"><option value="doubao-seedance-2.0">doubao-seedance-2.0</option><option v-for="item in models.filter((model) => model !== 'gpt-image-2' && model !== 'doubao-seedance-2.0')" :key="item" :value="item">{{ item }}</option></select></label>
      <div class="field video-ratio-field"><span>比例</span><RatioPicker :model-value="form.video_ratio" :ratios="currentVideoRatios" @update:model-value="emit('updateField', 'video_ratio', $event)" /></div>
      <label>分辨率<select :value="form.video_resolution" @change="emit('updateField', 'video_resolution', textValue($event))"><option v-for="resolution in currentVideoResolutions" :key="resolution" :value="resolution">{{ resolution.toUpperCase() }}</option></select></label>
      <div class="field"><span>尺寸</span><strong class="readonly-size">{{ form.video_width }}x{{ form.video_height }}</strong></div>
      <label>时长<input :value="form.video_duration" type="number" :min="currentVideoCapability.duration.min" :max="currentVideoCapability.duration.max" @input="emit('updateField', 'video_duration', numberValue($event))" /></label>
      <label class="check-field"><input :checked="form.generate_audio" type="checkbox" @change="emit('updateField', 'generate_audio', checkedValue($event))" />生成音频</label>
      <label class="check-field"><input :checked="form.watermark" type="checkbox" @change="emit('updateField', 'watermark', checkedValue($event))" />水印</label>
    </div>

    <div class="upload-row single-upload">
      <label class="upload"><AppIcon name="image" />参考图<input type="file" multiple accept="image/*" @change="emit('referenceChange', $event)" /></label>
      <button v-if="form.task_type === 'video_generation'" type="button" class="upload" @click="emit('openReferenceVideoModal')"><AppIcon name="video" />参考视频</button>
      <button v-if="form.task_type === 'video_generation'" type="button" class="upload" @click="emit('openReferenceAudioModal')"><AppIcon name="audio" />参考音频</button>
    </div>
  </form>
</template>
