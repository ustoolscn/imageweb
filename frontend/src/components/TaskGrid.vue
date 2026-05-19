<script setup lang="ts">
import { ref } from 'vue'
import type { Task } from '../types'
import { canOpenSource, canShareTask, displayImageURL, isFavorite, isVideoTask, queueText, statusClass, statusText, taskReferenceImages, timeText } from '../lib/view'
import AppIcon from './AppIcon.vue'

const loadedImages = ref(new Set<string>())

defineProps<{
  tasks: Task[]
  hasConfig: boolean
  baseUrlBlocked: boolean
  adminContactImage: string
  loadingMore: boolean
  hasMoreTasks: boolean
  clock: number
}>()

const emit = defineEmits<{
  showAdminContact: []
  selectTask: [task: Task]
  contextMenu: [task: Task, event: MouseEvent]
  openPreview: [url: string, label: string, event: Event, maskUrl?: string]
  openSource: [task: Task, event: Event]
  rerun: [task: Task]
  toggleFavorite: [task: Task, event: Event]
  reuse: [task: Task]
  toggleShare: [task: Task, event: Event]
  loadMore: []
}>()

function markImageLoaded(url: string) {
  loadedImages.value = new Set(loadedImages.value).add(url)
}

function isImageLoaded(url?: string) {
  return Boolean(url && loadedImages.value.has(url))
}
</script>

<template>
  <template v-if="!hasConfig">
    <section class="empty-state glass-panel">
      <h2>缺少连接配置</h2>
      <p>请使用 URL 传入 baseurl 和 apikey，例如：?baseurl=https://api.example.com&apikey=sk-xxx。页面会保存到本地并自动清理地址栏。</p>
    </section>
  </template>

  <template v-else-if="baseUrlBlocked">
    <section class="empty-state glass-panel blocked-state">
      <h2>该 BASEURL 未授权</h2>
      <p>当前 BASEURL 未在网站白名单内，请联系管理员授权后再使用。</p>
      <button type="button" :disabled="!adminContactImage" @click="emit('showAdminContact')">联系管理员</button>
    </section>
  </template>

  <template v-else>
    <section v-if="tasks.length === 0" class="empty-state glass-panel soft">
      <h2>还没有生成记录</h2>
      <p>在底部输入提示词并提交，任务会在后端异步执行。关闭页面后再次打开，也可以继续查看历史。</p>
    </section>

    <section class="grid">
      <article v-for="task in tasks" :key="task.id" class="task-card" :class="statusClass(task.status)" @click="emit('selectTask', task)" @contextmenu.prevent.stop="emit('contextMenu', task, $event)">
        <div class="preview" :class="task.status">
          <template v-if="isVideoTask(task) && task.result_videos?.[0]?.url">
            <video class="preview-video" :src="task.result_videos[0].url" muted playsinline preload="metadata" />
            <span class="preview-play-indicator" aria-hidden="true">
              <span></span>
            </span>
          </template>
          <div v-else-if="task.result_images?.[0]?.url" class="preview-image-wrap" :class="{ loaded: isImageLoaded(displayImageURL(task.result_images[0])) }">
            <div class="task-image-placeholder">加载中</div>
            <img :src="displayImageURL(task.result_images[0])" alt="生成结果" loading="lazy" decoding="async" @load="markImageLoaded(displayImageURL(task.result_images[0]))" />
          </div>
          <div v-else class="state-mark">
            <span v-if="task.status === 'running'" class="spinner"></span>
            <span v-else class="state-icon">{{ task.status === 'failed' ? '!' : '...' }}</span>
            <strong>{{ statusText(task.status) }}</strong>
            <small v-if="isVideoTask(task) && task.upstream_progress">{{ task.upstream_progress }}%</small>
            <small v-if="queueText(task)">{{ queueText(task) }}</small>
          </div>
          <span class="time">◷ {{ timeText(task, clock) }}</span>
        </div>
        <div class="card-body">
          <div class="card-head">
            <span class="status-pill">{{ queueText(task) || statusText(task.status) }}</span>
            <span class="model-pill">{{ task.model }}</span>
          </div>
          <p class="prompt">{{ task.prompt }}</p>
          <div v-if="taskReferenceImages(task).length" class="card-references">
            <span class="ref-label">参考图</span>
            <button v-for="(image, index) in taskReferenceImages(task).slice(0, 2)" :key="`${image.url}-${index}`" type="button" class="ref-thumb" @click="emit('openPreview', image.url, image.filename || `参考图 ${index + 1}`, $event, image.mask_url)">
              <img :src="displayImageURL(image)" :alt="image.filename || '参考图'" loading="lazy" decoding="async" />
            </button>
            <span v-if="taskReferenceImages(task).length > 2" class="ref-more">+{{ taskReferenceImages(task).length - 2 }}</span>
          </div>
          <div class="chips">
            <span>{{ task.quality }}</span>
            <span>{{ task.size }}</span>
            <span>{{ task.output_format }}</span>
          </div>
          <div class="actions" @click.stop>
            <button title="查看源数据" aria-label="查看源数据" :disabled="!canOpenSource(task)" @click="emit('openSource', task, $event)">
              <AppIcon name="file" />
            </button>
            <button title="重新生成" aria-label="重新生成" @click="emit('rerun', task)">
              <AppIcon name="refresh" />
            </button>
            <button :title="isFavorite(task) ? '取消收藏' : '收藏'" :aria-label="isFavorite(task) ? '取消收藏' : '收藏'" :class="{ favorite: isFavorite(task) }" @click="emit('toggleFavorite', task, $event)">
              <AppIcon name="favorite" />
            </button>
            <button title="复用配置" aria-label="复用配置" @click="emit('reuse', task)">
              <AppIcon name="copy" />
            </button>
            <button :title="task.shared_to_plaza ? '取消广场分享' : '分享到广场'" :aria-label="task.shared_to_plaza ? '取消广场分享' : '分享到广场'" :class="{ favorite: task.shared_to_plaza }" :disabled="!canShareTask(task)" @click="emit('toggleShare', task, $event)">
              <AppIcon name="share" />
            </button>
          </div>
        </div>
      </article>
    </section>

    <div v-if="tasks.length" class="load-more-state">
      <span v-if="loadingMore">正在加载更多...</span>
      <button v-else-if="hasMoreTasks" type="button" @click="emit('loadMore')">加载更多</button>
      <span v-else>没有更多任务了</span>
    </div>
  </template>
</template>
