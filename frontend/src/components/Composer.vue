<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import type { UploadedImage } from '../types'
import type { ImageForm, PendingReferenceAudio, PendingReferenceImage, PendingReferenceVideo, PreviewSource } from '../uiTypes'
import { supportsVideoDraft, videoModelCapability, videoRatioLabel } from '../lib/videoModels'
import { imageSizeLabel } from '../lib/sizes'
import { displayImageURL } from '../lib/view'
import AppIcon from './AppIcon.vue'
import InlineSelect from './InlineSelect.vue'

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
  updateReferenceFrameRole: [source: 'reused' | 'new', index: number, role: UploadedImage['video_frame_role']]
  removeReferenceVideo: [index: number]
  removeReferenceAudio: [index: number]
  openReferenceVideoModal: []
  openReferenceAudioModal: []
  openReferenceAssetModal: []
  openReferenceAssetUrlModal: []
  openReferenceMediaPreview: [type: 'video' | 'audio', url: string, label: string, event: Event]
  openSizeModal: []
  openVideoSizeModal: []
  referenceChange: [event: Event]
  referenceAssetChange: [event: Event]
}>()

const currentVideoCapability = computed(() => videoModelCapability(props.form.model))
const currentVideoSupportsDraft = computed(() => supportsVideoDraft(props.form.model))
const showVideoFrameRoles = computed(() => props.form.task_type === 'video_generation' && supportsVideoDraft(props.form.model))
const referenceAssetIcons = computed(() => showVideoFrameRoles.value ? ['image'] : props.form.task_type === 'video_generation' ? ['image', 'video', 'audio'] : ['image'])
const promptEditor = ref<HTMLDivElement | null>(null)
const mentionMenu = ref<{ query: string; start: number; end: number; activeIndex: number } | null>(null)
const suppressMentionMenuAfterDelete = ref(false)
const promptComposing = ref(false)
const builtinImageModels = ['gpt-image-2', 'nano-banana-2', 'doubao-seedream-5.0-lite']
const builtinVideoModels = ['doubao-seedance-2.0', 'doubao-seedance-1.5-pro']
const imageModelOptions = computed(() => [...builtinImageModels])
const videoModelOptions = computed(() => {
  const options = [...builtinVideoModels]
  for (const model of props.models || []) {
    if (!builtinVideoModels.includes(model) || options.includes(model)) continue
    options.push(model)
  }
  return options
})
const isNanoBanana = computed(() => props.form.model === 'nano-banana-2')
const isSeedream = computed(() => props.form.model === 'doubao-seedream-5.0-lite')
const supportsOutputCompression = computed(() => props.form.output_format === 'jpeg' || props.form.output_format === 'webp')
const sizeLabel = computed(() => '尺寸')
const videoSizeLabel = computed(() => `${props.form.video_resolution.toUpperCase()} ${videoRatioLabel(props.form.video_ratio)}`)
const qualityOptions = [
  { value: 'auto', label: 'auto', caption: '自动质量' },
  { value: 'high', label: 'high', caption: '高质量' },
  { value: 'medium', label: 'medium', caption: '中等质量' },
  { value: 'low', label: 'low', caption: '低质量' },
]
const formatOptions = computed(() => [
  { value: 'png', label: 'png', caption: '无损图片' },
  { value: 'jpeg', label: 'jpeg', caption: '较小体积' },
  ...(!isSeedream.value ? [{ value: 'webp', label: 'webp', caption: '高压缩图片' }] : []),
])
const moderationOptions = [
  { value: 'low', label: 'low', caption: '低审核' },
  { value: 'auto', label: 'auto', caption: '自动审核' },
]
const fidelityOptions = [
  { value: 'high', label: 'high', caption: '高保真' },
  { value: 'low', label: 'low', caption: '低保真' },
]
const mentionCandidates = computed(() => {
  const imageItems = [...reusedReferenceImagesWithLabels.value, ...referenceImagesWithLabels.value].flatMap((item) => [
    { label: item.label, type: '图片', detail: '参考图' },
    ...(item.masked ? [{ label: item.maskLabel, type: '蒙版', detail: `${item.label} 的蒙版` }] : []),
  ])
  const items = [
    ...imageItems,
    ...props.referenceVideos.map((video, index) => ({ label: referenceLabel(video, `视频${index + 1}`), type: '视频', detail: '参考视频' })),
    ...props.referenceAudios.map((audio, index) => ({ label: referenceLabel(audio, `音频${index + 1}`), type: '音频', detail: '参考音频' })),
  ]
  const query = mentionMenu.value?.query.toLowerCase() || ''
  return items.filter((item) => item.label.toLowerCase().includes(query) || item.type.toLowerCase().includes(query)).slice(0, 8)
})
const reusedReferenceImagesWithLabels = computed(() => props.reusedReferenceImages.map((image, index) => ({
  image,
  label: referenceLabel(image, `图片${index + 1}`),
  frameRoleLabel: frameRoleLabel(image.video_frame_role),
  maskLabel: maskReferenceLabel(image, referenceLabel(image, `图片${index + 1}`)),
  masked: Boolean(image.mask_url),
})))
const referenceImagesWithLabels = computed(() => props.referenceImages.map((image, index) => ({
  image,
  label: referenceLabel(image, `图片${props.reusedReferenceImages.length + index + 1}`),
  frameRoleLabel: frameRoleLabel(image.video_frame_role),
  maskLabel: maskReferenceLabel(image, referenceLabel(image, `图片${props.reusedReferenceImages.length + index + 1}`)),
  masked: Boolean(image.mask_url),
})))
const mentionLabels = computed(() => {
  const labels = new Set<string>()
  mentionCandidates.value.forEach((item) => labels.add(item.label))
  reusedReferenceImagesWithLabels.value.forEach((item) => {
    labels.add(item.label)
    if (item.masked) labels.add(item.maskLabel)
  })
  referenceImagesWithLabels.value.forEach((item) => {
    labels.add(item.label)
    if (item.masked) labels.add(item.maskLabel)
  })
  props.referenceVideos.forEach((video, index) => labels.add(referenceLabel(video, `视频${index + 1}`)))
  props.referenceAudios.forEach((audio, index) => labels.add(referenceLabel(audio, `音频${index + 1}`)))
  return labels
})
const highlightedPrompt = computed(() => renderPromptMentions(props.form.prompt))

function textValue(event: Event) {
  return (event.target as HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement).value
}

function onPromptInput(event: Event) {
  const target = event.currentTarget as HTMLElement
  if (promptComposing.value || (event as InputEvent).isComposing) return
  const cursor = editorCaretOffset(target)
  const text = editorPlainText(target)
  emit('updateField', 'prompt', text)
  if (suppressMentionMenuAfterDelete.value) {
    mentionMenu.value = null
    suppressMentionMenuAfterDelete.value = false
  } else {
    updateMentionMenu(target, cursor, text)
  }
  nextTick(() => setEditorCaret(cursor))
}

function onPromptKeyup(event: KeyboardEvent) {
  if (promptComposing.value || event.isComposing) return
  if (event.key === 'Backspace' || event.key === 'Delete') {
    mentionMenu.value = null
    suppressMentionMenuAfterDelete.value = false
    return
  }
  const target = event.currentTarget as HTMLElement
  updateMentionMenu(target, editorCaretOffset(target), editorPlainText(target))
}

function onPromptKeydown(event: KeyboardEvent) {
  if (promptComposing.value || event.isComposing) return
  if (event.key === 'Backspace' || event.key === 'Delete') {
    handlePromptDelete(event)
    return
  }
  if (mentionMenu.value && mentionCandidates.value.length) {
    if (event.key === 'ArrowDown') {
      event.preventDefault()
      mentionMenu.value.activeIndex = (mentionMenu.value.activeIndex + 1) % mentionCandidates.value.length
      return
    }
    if (event.key === 'ArrowUp') {
      event.preventDefault()
      mentionMenu.value.activeIndex = (mentionMenu.value.activeIndex - 1 + mentionCandidates.value.length) % mentionCandidates.value.length
      return
    }
    if (event.key === 'Enter' || event.key === 'Tab') {
      event.preventDefault()
      insertMention(mentionCandidates.value[mentionMenu.value.activeIndex]?.label)
      return
    }
  }
  if (event.key === 'Escape') {
    mentionMenu.value = null
    return
  }
  if (event.key === 'Enter') {
    event.preventDefault()
    insertPromptTextAtSelection('\n')
  }
}

function onPromptCompositionStart() {
  promptComposing.value = true
  mentionMenu.value = null
}

function onPromptCompositionEnd(event: CompositionEvent) {
  promptComposing.value = false
  const target = event.currentTarget as HTMLElement
  const cursor = editorCaretOffset(target)
  const text = editorPlainText(target)
  emit('updateField', 'prompt', text)
  updateMentionMenu(target, cursor, text)
  nextTick(() => setEditorCaret(cursor))
}

function handlePromptDelete(event: KeyboardEvent) {
  const target = event.currentTarget as HTMLElement
  const selection = editorSelectionOffsets(target)
  let start = selection.start
  let end = selection.end
  if (start === end) {
    const mentionRange = promptMentionRangeAt(props.form.prompt, event.key === 'Backspace' ? start - 1 : start)
    if (mentionRange) {
      start = mentionRange.start
      end = mentionRange.end
    } else if (event.key === 'Backspace') {
      if (start <= 0) {
        mentionMenu.value = null
        return
      }
      start -= 1
    } else {
      if (end >= props.form.prompt.length) {
        mentionMenu.value = null
        return
      }
      end += 1
    }
  }
  if (start === end) {
    mentionMenu.value = null
    return
  }
  event.preventDefault()
  suppressMentionMenuAfterDelete.value = true
  mentionMenu.value = null
  const nextValue = `${props.form.prompt.slice(0, start)}${props.form.prompt.slice(end)}`
  emit('updateField', 'prompt', nextValue)
  nextTick(() => setEditorCaret(start))
}

function promptMentionRangeAt(text: string, index: number) {
  if (index < 0) return null
  const labels = mentionLabels.value
  const pattern = /@([^\s@]+)/g
  for (const match of text.matchAll(pattern)) {
    const token = match[1] || ''
    if (!labels.has(token)) continue
    const start = match.index ?? 0
    const end = start + match[0].length
    if (index >= start && index < end) return { start, end }
  }
  return null
}

function updateMentionMenu(target: HTMLElement | null = promptEditor.value, cursor = target ? editorCaretOffset(target) : 0, value = target ? editorPlainText(target) : props.form.prompt) {
  if (!target) return
  const before = value.slice(0, cursor)
  const match = before.match(/(^|\s)@([^\s@]*)$/)
  if (!match) {
    mentionMenu.value = null
    return
  }
  const query = match[2] || ''
  const start = cursor - query.length - 1
  const previous = mentionMenu.value?.query === query ? mentionMenu.value.activeIndex : 0
  mentionMenu.value = { query, start, end: cursor, activeIndex: previous }
}

function insertMention(label?: string) {
  const menu = mentionMenu.value
  const target = promptEditor.value
  if (!label || !menu || !target) return
  const value = props.form.prompt
  const token = `@${label} `
  const nextValue = `${value.slice(0, menu.start)}${token}${value.slice(menu.end)}`
  emit('updateField', 'prompt', nextValue)
  mentionMenu.value = null
  nextTick(() => {
    const cursor = menu.start + token.length
    target.focus()
    setEditorCaret(cursor)
  })
}

function onPromptPasteLocal(event: ClipboardEvent) {
  emit('promptPaste', event)
  if (event.defaultPrevented) return
  const text = event.clipboardData?.getData('text/plain')
  if (!text) return
  event.preventDefault()
  insertPlainTextAtCaret(text)
}

function insertPlainTextAtCaret(text: string) {
  const menu = mentionMenu.value
  const target = promptEditor.value
  const cursor = target ? editorCaretOffset(target) : props.form.prompt.length
  const value = props.form.prompt
  const start = menu?.start ?? cursor
  const end = menu?.end ?? cursor
  const nextValue = `${value.slice(0, start)}${text}${value.slice(end)}`
  emit('updateField', 'prompt', nextValue)
  mentionMenu.value = null
  nextTick(() => {
    target?.focus()
    setEditorCaret(start + text.length)
  })
}

function insertPromptTextAtSelection(text: string) {
  const target = promptEditor.value
  const selection = target ? editorSelectionOffsets(target) : { start: props.form.prompt.length, end: props.form.prompt.length }
  const value = props.form.prompt
  const nextValue = `${value.slice(0, selection.start)}${text}${value.slice(selection.end)}`
  emit('updateField', 'prompt', nextValue)
  mentionMenu.value = null
  nextTick(() => {
    target?.focus({ preventScroll: true })
    setEditorCaret(selection.start + text.length)
  })
}

function editorPlainText(target: HTMLElement) {
  return editorNodeText(target)
}

function editorCaretOffset(target: HTMLElement) {
  const selection = window.getSelection()
  if (!selection?.rangeCount) return editorPlainText(target).length
  const range = selection.getRangeAt(0)
  if (!target.contains(range.endContainer)) return editorPlainText(target).length
  return editorOffsetForBoundary(target, range.endContainer, range.endOffset)
}

function editorSelectionOffsets(target: HTMLElement) {
  const selection = window.getSelection()
  if (!selection?.rangeCount) {
    const end = editorPlainText(target).length
    return { start: end, end }
  }
  const range = selection.getRangeAt(0)
  if (!target.contains(range.startContainer) || !target.contains(range.endContainer)) {
    const end = editorPlainText(target).length
    return { start: end, end }
  }
  const start = editorOffsetForBoundary(target, range.startContainer, range.startOffset)
  const end = editorOffsetForBoundary(target, range.endContainer, range.endOffset)
  return { start: Math.min(start, end), end: Math.max(start, end) }
}

function editorNodeText(node: Node): string {
  if (node.nodeType === Node.TEXT_NODE) return normalizeEditorText(node.textContent || '')
  if (node instanceof HTMLBRElement) return '\n'
  return Array.from(node.childNodes).map(editorNodeText).join('')
}

function editorNodeTextLength(node: Node): number {
  return editorNodeText(node).length
}

function editorOffsetForBoundary(root: HTMLElement, boundaryNode: Node, boundaryOffset: number) {
  let offset = 0
  let found = false
  const walk = (node: Node) => {
    if (found) return
    if (node === boundaryNode) {
      if (node.nodeType === Node.TEXT_NODE) {
        offset += editorTextPrefixLength(node.textContent || '', boundaryOffset)
      } else {
        const children = Array.from(node.childNodes)
        for (let index = 0; index < Math.min(boundaryOffset, children.length); index += 1) {
          offset += editorNodeTextLength(children[index])
        }
      }
      found = true
      return
    }
    if (node.nodeType === Node.TEXT_NODE || node instanceof HTMLBRElement) {
      offset += editorNodeTextLength(node)
      return
    }
    for (const child of Array.from(node.childNodes)) walk(child)
  }
  walk(root)
  return found ? offset : editorPlainText(root).length
}

function setEditorCaret(offset: number) {
  const target = promptEditor.value
  if (!target) return
  target.focus({ preventScroll: true })
  let remaining = Math.max(0, offset)
  const placeCaret = (node: Node, nodeOffset: number) => {
    const range = document.createRange()
    range.setStart(node, nodeOffset)
    range.collapse(true)
    const selection = window.getSelection()
    selection?.removeAllRanges()
    selection?.addRange(range)
  }
  const walk = (node: Node): boolean => {
    for (const child of Array.from(node.childNodes)) {
      if (child.nodeType === Node.TEXT_NODE) {
        const length = editorNodeTextLength(child)
        if (remaining <= length) {
          placeCaret(child, rawOffsetForEditorText(child.textContent || '', remaining))
          return true
        }
        remaining -= length
        continue
      }
      if (child instanceof HTMLBRElement) {
        const index = Array.prototype.indexOf.call(node.childNodes, child)
        if (remaining <= 0) {
          placeCaret(node, index)
          return true
        }
        if (remaining <= 1) {
          const next = child.nextSibling
          if (next?.nodeType === Node.TEXT_NODE && (next.textContent || '').startsWith('\u200b')) {
            placeCaret(next, 1)
            return true
          }
          placeCaret(node, index + 1)
          return true
        }
        remaining -= 1
        continue
      }
      if (child instanceof HTMLElement && child.classList.contains('composer-mention-token')) {
        const tokenLength = child.textContent?.length || 0
        if (remaining <= 0) {
          placeCaret(node, Array.prototype.indexOf.call(node.childNodes, child))
          return true
        }
        if (remaining <= tokenLength) {
          placeCaret(node, Array.prototype.indexOf.call(node.childNodes, child) + 1)
          return true
        }
        remaining -= tokenLength
        continue
      }
      if (walk(child)) return true
    }
    return false
  }
  if (walk(target)) return
  const range = document.createRange()
  range.selectNodeContents(target)
  range.collapse(false)
  const selection = window.getSelection()
  selection?.removeAllRanges()
  selection?.addRange(range)
}

function renderPromptMentions(text: string) {
  const labels = mentionLabels.value
  if (!text) return ''
  let html = ''
  let lastIndex = 0
  const pattern = /@([^\s@]+)/g
  for (const match of text.matchAll(pattern)) {
    const index = match.index ?? 0
    const token = match[1] || ''
    if (!labels.has(token)) continue
    html += escapeEditorText(text.slice(lastIndex, index))
    html += `<span class="composer-mention-token" contenteditable="false">@${escapeHTML(token)}</span>`
    lastIndex = index + match[0].length
  }
  html += escapeEditorText(text.slice(lastIndex))
  return html
}

function escapeEditorText(text: string) {
  return escapeHTML(text).replace(/\n/g, '<br data-editor-newline="true">&#8203;')
}

function escapeHTML(text: string) {
  return text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}

function normalizeEditorText(text: string) {
  return text.replace(/\u00a0/g, ' ').replace(/\u200b/g, '')
}

function editorTextPrefixLength(text: string, offset: number) {
  return normalizeEditorText(text.slice(0, Math.max(0, offset))).length
}

function rawOffsetForEditorText(text: string, normalizedOffset: number) {
  if (normalizedOffset <= 0) return text.startsWith('\u200b') ? 1 : 0
  let visible = 0
  for (let index = 0; index < text.length; index += 1) {
    if (text[index] !== '\u200b') visible += 1
    if (visible >= normalizedOffset) return index + 1
  }
  return text.length
}

function numberValue(event: Event) {
  return Number((event.target as HTMLInputElement).value)
}

function referenceLabel(item: { reference_label?: string; filename?: string }, fallback: string) {
  return item.reference_label || fallback
}

function maskReferenceLabel(item: { mask_reference_label?: string }, baseLabel: string) {
  return item.mask_reference_label || `${baseLabel}-蒙版`
}

function frameRoleLabel(role?: UploadedImage['video_frame_role']) {
  if (role === 'first_frame') return '首帧'
  if (role === 'last_frame') return '尾帧'
  return ''
}

function nextFrameRole(current: UploadedImage['video_frame_role'], target: UploadedImage['video_frame_role']) {
  return current === target ? '' : target
}
</script>

<template>
  <form class="composer glass-panel" @submit.prevent="emit('submit', form.task_type)">
    <div class="mode-row">
      <button type="button" :class="{ active: form.task_type === 'image_generation' }" @click="emit('updateField', 'task_type', 'image_generation')"><AppIcon name="image" />图片生成</button>
      <button type="button" :class="{ active: form.task_type === 'video_generation' }" @click="emit('updateField', 'task_type', 'video_generation')"><AppIcon name="video" />视频生成</button>
    </div>

    <div class="prompt-row">
      <div class="composer-prompt-wrap">
        <div ref="promptEditor" class="composer-rich-prompt" contenteditable="true" :data-placeholder="form.task_type === 'video_generation' ? '描述你想生成的视频，可输入 @ 引用参考...' : '描述你想生成的图片，可输入 @ 引用参考...'" @input="onPromptInput" @keyup="onPromptKeyup" @keydown="onPromptKeydown" @compositionstart="onPromptCompositionStart" @compositionend="onPromptCompositionEnd" @paste="onPromptPasteLocal" v-html="highlightedPrompt"></div>
        <div v-if="mentionMenu && mentionCandidates.length" class="composer-mention-menu">
          <button v-for="(item, index) in mentionCandidates" :key="item.label" type="button" :class="{ active: mentionMenu.activeIndex === index }" @pointerenter="mentionMenu.activeIndex = index" @mousedown.prevent="insertMention(item.label)">
            <strong>@{{ item.label }}</strong>
            <span>{{ item.type }} · {{ item.detail }}</span>
          </button>
        </div>
      </div>
      <button class="submit" :disabled="submitting || !hasConfig"><AppIcon name="play" />{{ submitting ? '提交中' : (form.task_type === 'video_generation' ? '生成视频' : '生成图片') }}</button>
    </div>

    <div v-if="reusedReferenceImages.length || referenceImages.length || referenceVideos.length || referenceAudios.length" class="preview-strip">
      <div v-for="({ image, label, masked, frameRoleLabel }, index) in reusedReferenceImagesWithLabels" :key="image.url" class="input-thumb reused">
        <img :src="displayImageURL(image)" alt="参考图" loading="lazy" decoding="async" crossorigin="anonymous" @click="emit('openEditablePreview', 'reused', index, image.url, label, $event)" />
        <span>{{ label }}{{ frameRoleLabel ? ` · ${frameRoleLabel}` : '' }}{{ masked ? ' · 蒙版' : '' }}</span>
        <div v-if="showVideoFrameRoles" class="frame-role-controls">
          <button type="button" :class="{ active: image.video_frame_role === 'first_frame' }" @click.stop="emit('updateReferenceFrameRole', 'reused', index, nextFrameRole(image.video_frame_role, 'first_frame'))">首</button>
          <button type="button" :class="{ active: image.video_frame_role === 'last_frame' }" @click.stop="emit('updateReferenceFrameRole', 'reused', index, nextFrameRole(image.video_frame_role, 'last_frame'))">尾</button>
        </div>
        <button type="button" @click="emit('removeReusedReference', index)"><AppIcon name="close" :size="12" /></button>
      </div>
      <div v-for="({ image, label, masked, frameRoleLabel }, index) in referenceImagesWithLabels" :key="image.preview_url" class="input-thumb" :class="{ uploading: image.uploading, failed: image.upload_error }">
        <img :src="image.preview_url" alt="参考图" crossorigin="anonymous" @click="emit('openEditablePreview', 'new', index, image.preview_url, label, $event)" />
        <span>{{ label }}{{ frameRoleLabel ? ` · ${frameRoleLabel}` : '' }}{{ masked ? ' · 蒙版' : '' }}{{ image.uploading ? ' · 上传中' : '' }}{{ image.upload_error ? ' · 上传失败' : '' }}</span>
        <div v-if="showVideoFrameRoles" class="frame-role-controls">
          <button type="button" :class="{ active: image.video_frame_role === 'first_frame' }" @click.stop="emit('updateReferenceFrameRole', 'new', index, nextFrameRole(image.video_frame_role, 'first_frame'))">首</button>
          <button type="button" :class="{ active: image.video_frame_role === 'last_frame' }" @click.stop="emit('updateReferenceFrameRole', 'new', index, nextFrameRole(image.video_frame_role, 'last_frame'))">尾</button>
        </div>
        <button type="button" @click="emit('removeReference', index)"><AppIcon name="close" :size="12" /></button>
      </div>
      <div v-for="(video, index) in referenceVideos" :key="video.reference_label || video.url || `video-${index}`" class="input-thumb video-thumb" :class="{ uploading: video.loading, failed: video.error }">
        <button type="button" class="media-thumb-open" :disabled="!video.url" @click="emit('openReferenceMediaPreview', 'video', video.url, referenceLabel(video, `视频${index + 1}`), $event)">
          <img v-if="video.cover_url" :src="video.cover_url" alt="参考视频封面" crossorigin="anonymous" />
          <span v-else class="audio-mark">视频</span>
        </button>
        <span>{{ referenceLabel(video, `视频${index + 1}`) }}{{ video.loading ? ' · 上传中' : '' }}{{ video.error ? ' · 失败' : '' }}</span>
        <button type="button" @click="emit('removeReferenceVideo', index)"><AppIcon name="close" :size="12" /></button>
      </div>
      <div v-for="(audio, index) in referenceAudios" :key="audio.reference_label || audio.url || `audio-${index}`" class="input-thumb audio-thumb" :class="{ uploading: audio.loading, failed: audio.error }">
        <button type="button" class="audio-mark" :disabled="!audio.url" @click="emit('openReferenceMediaPreview', 'audio', audio.url, referenceLabel(audio, `音频${index + 1}`), $event)">音</button>
        <span>{{ referenceLabel(audio, `音频${index + 1}`) }}{{ audio.loading ? ' · 上传中' : '' }}{{ audio.error ? ' · 失败' : '' }}</span>
        <button type="button" @click="emit('removeReferenceAudio', index)"><AppIcon name="close" :size="12" /></button>
      </div>
    </div>

    <div class="composer-controls">
      <div class="composer-fields">
        <div v-if="form.task_type === 'image_generation'" class="form-row">
          <InlineSelect class="model-select" label="模型" :model-value="form.model" :options="imageModelOptions" @update:model-value="emit('updateField', 'model', $event)" />
          <div class="composer-select">
            <button type="button" class="composer-select-current size-select-current" @click.stop.prevent="emit('openSizeModal')">
              <span>{{ sizeLabel }}</span>
              <strong>{{ imageSizeLabel(form.size) }}</strong>
            </button>
          </div>
          <InlineSelect v-if="!isNanoBanana && !isSeedream" label="质量" :model-value="form.quality" :options="qualityOptions" @update:model-value="emit('updateField', 'quality', $event)" />
          <InlineSelect v-if="!isNanoBanana" label="格式" :model-value="form.output_format" :options="formatOptions" @update:model-value="emit('updateField', 'output_format', $event)" />
          <label v-if="!isNanoBanana && !isSeedream && supportsOutputCompression" class="composer-number-field"><span>压缩</span><input :value="form.output_compression" type="number" min="0" max="100" @input="emit('updateField', 'output_compression', numberValue($event))" /></label>
          <label class="composer-number-field"><span>数量</span><input :value="form.batch_count" type="number" min="1" max="5" step="1" @input="emit('updateField', 'batch_count', numberValue($event))" /></label>
          <InlineSelect v-if="!isSeedream" label="审核" :model-value="form.moderation" :options="moderationOptions" @update:model-value="emit('updateField', 'moderation', $event)" />
          <InlineSelect v-if="!isNanoBanana && !isSeedream" label="保真" :model-value="form.input_fidelity" :options="fidelityOptions" @update:model-value="emit('updateField', 'input_fidelity', $event)" />
        </div>

        <div v-else class="form-row video-form-row">
          <InlineSelect class="model-select" label="模型" :model-value="form.model" :options="videoModelOptions" @update:model-value="emit('updateField', 'model', $event)" />
          <div class="composer-select">
            <button type="button" class="composer-select-current size-select-current" @click.stop.prevent="emit('openVideoSizeModal')">
              <span>尺寸</span>
              <strong>{{ videoSizeLabel }}</strong>
            </button>
          </div>
          <label class="composer-number-field"><span>时长</span><input :value="form.video_duration" type="number" :min="currentVideoCapability.duration.min" :max="currentVideoCapability.duration.max" @input="emit('updateField', 'video_duration', numberValue($event))" /></label>
          <button type="button" class="composer-toggle-choice" :class="{ active: form.generate_audio }" @click="emit('updateField', 'generate_audio', !form.generate_audio)">
            <span><strong>生成音频</strong><small>随视频生成声音</small></span>
            <i class="canvas-switch-indicator"></i>
          </button>
          <button v-if="currentVideoSupportsDraft" type="button" class="composer-toggle-choice" :class="{ active: form.video_draft }" @click="emit('updateField', 'video_draft', !form.video_draft)">
            <span><strong>样片模式</strong><small>draft</small></span>
            <i class="canvas-switch-indicator"></i>
          </button>
        </div>
      </div>

      <div class="reference-assets-panel">
        <button type="button" class="reference-asset-button" @click="emit('openReferenceAssetModal')">
          <strong>参考</strong>
          <span class="reference-asset-icons" aria-hidden="true">
            <AppIcon v-for="icon in referenceAssetIcons" :key="icon" :name="icon" />
          </span>
        </button>
      </div>
    </div>
  </form>
</template>
