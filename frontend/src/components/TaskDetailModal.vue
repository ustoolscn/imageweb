<script setup lang="ts">
import type { Task } from '../types'
import { canShareTask, displayImageURL, formatTime, isFavorite, isVideoTask, maskBaseURL, queueText, statusText, taskReferenceImages, timeText } from '../lib/view'
import AppIcon from './AppIcon.vue'

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
      <button class="modal-close" @click="emit('close')"><AppIcon name="close" /></button>
      <div class="detail-preview">
        <video v-if="isVideoTask(task) && task.result_videos?.[0]?.url" :src="task.result_videos[0].url" controls playsinline preload="metadata" />
        <img v-else-if="task.result_images?.[0]?.url" :src="displayImageURL(task.result_images[0])" alt="生成结果" title="点击查看大图" loading="lazy" decoding="async" @click="emit('openPreview', task.result_images[0].url, '生成结果', $event)" />
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
        <div v-if="isVideoTask(task) && (task.reference_videos?.length || task.reference_audios?.length)" class="detail-section">
          <div class="section-title">参考媒体</div>
          <div class="detail-source">{{ task.reference_videos?.length || 0 }} 个视频 · {{ task.reference_audios?.length || 0 }} 个音频</div>
        </div>
        <div class="detail-section">
          <div class="section-title">参数配置</div>
          <div class="detail-source">来源 <strong>{{ maskBaseURL(task.baseurl) }}</strong> · {{ task.model }}</div>
          <div class="detail-params">
            <div><span>{{ isVideoTask(task) ? '分辨率' : '尺寸' }}</span><strong>{{ isVideoTask(task) ? `${task.video_width || 0}x${task.video_height || 0}` : task.size }}</strong></div>
            <div><span>{{ isVideoTask(task) ? '比例' : '质量' }}</span><strong>{{ isVideoTask(task) ? task.video_ratio : task.quality }}</strong></div>
            <div><span>{{ isVideoTask(task) ? '时长' : '格式' }}</span><strong>{{ isVideoTask(task) ? `${task.video_duration || 0}s` : task.output_format }}</strong></div>
            <div><span>{{ isVideoTask(task) ? '进度' : '审核' }}</span><strong>{{ isVideoTask(task) ? `${task.upstream_progress || 0}%` : task.moderation }}</strong></div>
            <div><span>时间</span><strong>{{ timeText(task, clock) }}</strong></div>
            <div v-if="queueText(task)"><span>排队</span><strong>{{ queueText(task) }}</strong></div>
          </div>
        </div>
        <p class="detail-time">创建于 {{ formatTime(task.created_at) }} · 状态 {{ queueText(task) || statusText(task.status) }}</p>
        <div class="detail-buttons">
          <button class="blue" @click="emit('reuse', task); emit('close')">
            <AppIcon name="copy" />
            <span>复用配置</span>
          </button>
          <button class="green" @click="emit('rerun', task); emit('close')">
            <AppIcon name="refresh" />
            <span>重新生成</span>
          </button>
          <button class="purple" :disabled="!(task.result_images?.[0]?.url || task.result_videos?.[0]?.url)" @click="emit('openResult', task)">
            <AppIcon name="download" />
            <span>{{ isVideoTask(task) ? '打开视频' : '下载图片' }}</span>
          </button>
          <button class="cyan" :disabled="isVideoTask(task) || !task.result_images?.[0]?.url" @click="emit('addResultToReferences', task)">
            <AppIcon name="add" />
            <span>加入参考</span>
          </button>
          <button class="orange" :class="{ favorite: task.shared_to_plaza }" :disabled="!canShareTask(task)" @click="emit('toggleShare', task, $event)">
            <AppIcon name="share" />
            <span>{{ task.shared_to_plaza ? '取消分享' : '分享广场' }}</span>
          </button>
          <button class="red" @click="emit('remove', task)">
            <AppIcon name="trash" />
            <span>删除记录</span>
          </button>
          <button class="star" :class="{ favorite: isFavorite(task) }" :title="isFavorite(task) ? '取消收藏' : '收藏'" :aria-label="isFavorite(task) ? '取消收藏' : '收藏'" @click="emit('toggleFavorite', task, $event)">
            <AppIcon name="favorite" />
          </button>
        </div>
      </div>
    </section>
  </div>
</template>
