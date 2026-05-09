<script setup lang="ts">
import type { Task } from '../types'
import { canShareTask, displayImageURL, formatTime, isFavorite, maskBaseURL, queueText, statusText, taskReferenceImages, timeText } from '../lib/view'

defineProps<{
  task: Task
  clock: number
}>()

const emit = defineEmits<{
  close: []
  openPreview: [url: string, label: string, event: Event, maskUrl?: string]
  reuse: [task: Task]
  rerun: [task: Task]
  openResult: [task: Task]
  addResultToReferences: [task: Task]
  toggleShare: [task: Task, event: Event]
  remove: [task: Task]
  toggleFavorite: [task: Task, event: Event]
}>()
</script>

<template>
  <div class="modal-backdrop" @click.self="emit('close')">
    <section class="detail-modal light-modal">
      <button class="modal-close" @click="emit('close')">×</button>
      <div class="detail-preview">
        <img v-if="task.result_images?.[0]?.url" :src="displayImageURL(task.result_images[0])" alt="生成结果" title="点击查看大图" loading="lazy" decoding="async" @click="emit('openPreview', task.result_images[0].url, '生成结果', $event)" />
        <div v-else class="detail-state">
          <span>{{ task.status === 'failed' ? '!' : '...' }}</span>
          <p>{{ task.error_message || statusText(task.status) }}</p>
        </div>
      </div>
      <div class="detail-info">
        <div class="detail-section detail-input-section">
          <div class="section-title">输入内容</div>
          <p class="detail-prompt">{{ task.prompt }}</p>
        </div>
        <div v-if="taskReferenceImages(task).length" class="detail-section">
          <div class="section-title">参考图片</div>
          <div class="detail-references">
            <button v-for="(image, index) in taskReferenceImages(task)" :key="`${image.url}-${index}`" type="button" @click="emit('openPreview', image.url, image.filename || `参考图 ${index + 1}`, $event, image.mask_url)">
              <img :src="displayImageURL(image)" :alt="image.filename || '参考图'" loading="lazy" decoding="async" />
              <span>{{ image.filename || `参考 ${index + 1}` }}{{ image.mask_url ? ' · 蒙板' : '' }}</span>
            </button>
          </div>
        </div>
        <div class="detail-section">
          <div class="section-title">参数配置</div>
          <div class="detail-source">来源 <strong>{{ maskBaseURL(task.baseurl) }}</strong> · {{ task.model }}</div>
          <div class="detail-params">
            <div><span>尺寸</span><strong>{{ task.size }}</strong></div>
            <div><span>质量</span><strong>{{ task.quality }}</strong></div>
            <div><span>格式</span><strong>{{ task.output_format }}</strong></div>
            <div><span>审核</span><strong>{{ task.moderation }}</strong></div>
            <div><span>时间</span><strong>{{ timeText(task, clock) }}</strong></div>
            <div v-if="queueText(task)"><span>排队</span><strong>{{ queueText(task) }}</strong></div>
          </div>
        </div>
        <p class="detail-time">创建于 {{ formatTime(task.created_at) }} · 状态 {{ queueText(task) || statusText(task.status) }}</p>
        <div class="detail-buttons">
          <button class="blue" @click="emit('reuse', task); emit('close')">
            <svg viewBox="0 0 24 24" aria-hidden="true"><rect x="8" y="8" width="11" height="11" rx="2"/><path d="M5 16V7a2 2 0 0 1 2-2h9"/></svg>
            <span>复用配置</span>
          </button>
          <button class="green" @click="emit('rerun', task); emit('close')">
            <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M20 12a8 8 0 1 1-2.34-5.66"/><path d="M20 4v6h-6"/></svg>
            <span>重新生成</span>
          </button>
          <button class="purple" :disabled="!task.result_images?.[0]?.url" @click="emit('openResult', task)">
            <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 3v12"/><path d="m7 10 5 5 5-5"/><path d="M5 21h14"/></svg>
            <span>下载图片</span>
          </button>
          <button class="cyan" :disabled="!task.result_images?.[0]?.url" @click="emit('addResultToReferences', task)">
            <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 5v14M5 12h14"/><rect x="3" y="3" width="18" height="18" rx="3"/></svg>
            <span>加入参考</span>
          </button>
          <button class="orange" :class="{ favorite: task.shared_to_plaza }" :disabled="!canShareTask(task)" @click="emit('toggleShare', task, $event)">
            <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 12v7a1 1 0 0 0 1 1h14a1 1 0 0 0 1-1v-7"/><path d="M16 6l-4-4-4 4"/><path d="M12 2v13"/></svg>
            <span>{{ task.shared_to_plaza ? '取消分享' : '分享广场' }}</span>
          </button>
          <button class="red" @click="emit('remove', task)">
            <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 7h16M10 11v6M14 11v6M6 7l1 14h10l1-14M9 7V4h6v3"/></svg>
            <span>删除记录</span>
          </button>
          <button class="star" :class="{ favorite: isFavorite(task) }" :title="isFavorite(task) ? '取消收藏' : '收藏'" :aria-label="isFavorite(task) ? '取消收藏' : '收藏'" @click="emit('toggleFavorite', task, $event)">
            <svg viewBox="0 0 24 24" aria-hidden="true"><path d="m12 3 2.8 5.67 6.26.91-4.53 4.42 1.07 6.23L12 17.28l-5.6 2.95 1.07-6.23-4.53-4.42 6.26-.91L12 3Z"/></svg>
          </button>
        </div>
      </div>
    </section>
  </div>
</template>
