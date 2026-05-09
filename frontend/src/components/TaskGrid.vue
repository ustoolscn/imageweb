<script setup lang="ts">
import type { Task } from '../types'
import { canOpenSource, canShareTask, displayImageURL, isFavorite, queueText, statusClass, statusText, taskReferenceImages, timeText } from '../lib/view'

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
  openPreview: [url: string, label: string, event: Event, maskUrl?: string]
  openSource: [task: Task, event: Event]
  rerun: [task: Task]
  toggleFavorite: [task: Task, event: Event]
  reuse: [task: Task]
  toggleShare: [task: Task, event: Event]
  loadMore: []
}>()
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
      <article v-for="task in tasks" :key="task.id" class="task-card" :class="statusClass(task.status)" @click="emit('selectTask', task)">
        <div class="preview" :class="task.status">
          <img v-if="task.result_images?.[0]?.url" :src="displayImageURL(task.result_images[0])" alt="生成结果" loading="lazy" decoding="async" />
          <div v-else class="state-mark">
            <span v-if="task.status === 'running'" class="spinner"></span>
            <span v-else class="state-icon">{{ task.status === 'failed' ? '!' : '...' }}</span>
            <strong>{{ statusText(task.status) }}</strong>
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
              <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M8 7h8M8 12h8M8 17h5"/><rect x="5" y="3" width="14" height="18" rx="2"/></svg>
            </button>
            <button title="重新生成" aria-label="重新生成" @click="emit('rerun', task)">
              <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M20 12a8 8 0 1 1-2.34-5.66"/><path d="M20 4v6h-6"/></svg>
            </button>
            <button :title="isFavorite(task) ? '取消收藏' : '收藏'" :aria-label="isFavorite(task) ? '取消收藏' : '收藏'" :class="{ favorite: isFavorite(task) }" @click="emit('toggleFavorite', task, $event)">
              <svg viewBox="0 0 24 24" aria-hidden="true"><path d="m12 3 2.8 5.67 6.26.91-4.53 4.42 1.07 6.23L12 17.28l-5.6 2.95 1.07-6.23-4.53-4.42 6.26-.91L12 3Z"/></svg>
            </button>
            <button title="复用配置" aria-label="复用配置" @click="emit('reuse', task)">
              <svg viewBox="0 0 24 24" aria-hidden="true"><rect x="8" y="8" width="11" height="11" rx="2"/><path d="M5 16V7a2 2 0 0 1 2-2h9"/></svg>
            </button>
            <button :title="task.shared_to_plaza ? '取消广场分享' : '分享到广场'" :aria-label="task.shared_to_plaza ? '取消广场分享' : '分享到广场'" :class="{ favorite: task.shared_to_plaza }" :disabled="!canShareTask(task)" @click="emit('toggleShare', task, $event)">
              <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 12v7a1 1 0 0 0 1 1h14a1 1 0 0 0 1-1v-7"/><path d="M16 6l-4-4-4 4"/><path d="M12 2v13"/></svg>
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
