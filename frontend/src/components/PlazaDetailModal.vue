<script setup lang="ts">
import type { PlazaItem } from '../types'
import { displayImageURL, formatTime, isVideoTask, taskReferenceImages } from '../lib/view'
import AppIcon from './AppIcon.vue'

defineProps<{
  item: PlazaItem
}>()

const emit = defineEmits<{
  close: []
  openPreview: [url: string, label: string, event: Event, maskUrl?: string]
  reuse: [item: PlazaItem]
  openResult: [item: PlazaItem]
  toggleLike: [item: PlazaItem, event: Event]
}>()
</script>

<template>
  <div class="modal-backdrop" @click.self="emit('close')">
    <section class="detail-modal light-modal">
      <button class="modal-close" @click="emit('close')"><AppIcon name="close" /></button>
      <div class="detail-preview">
        <video v-if="isVideoTask(item) && item.result_videos?.[0]?.url" :src="item.result_videos[0].url" controls playsinline preload="metadata" />
        <img v-else-if="item.result_images?.[0]?.url" :src="displayImageURL(item.result_images[0])" alt="广场作品" title="点击查看大图" loading="lazy" decoding="async" @click="emit('openPreview', item.result_images[0].url, '广场作品', $event)" />
      </div>
      <div class="detail-info">
        <div class="detail-section detail-input-section">
          <div class="section-title">输入内容</div>
          <p class="detail-prompt">{{ item.prompt }}</p>
        </div>
        <div v-if="taskReferenceImages(item).length" class="detail-section">
          <div class="section-title">参考图片</div>
          <div class="detail-references">
            <button v-for="(image, index) in taskReferenceImages(item)" :key="`${image.url}-${index}`" type="button" @click="emit('openPreview', image.url, image.filename || `参考图 ${index + 1}`, $event, image.mask_url)">
              <img :src="displayImageURL(image)" :alt="image.filename || '参考图'" loading="lazy" decoding="async" />
              <span>{{ image.filename || `参考 ${index + 1}` }}{{ image.mask_url ? ' · 蒙板' : '' }}</span>
            </button>
          </div>
        </div>
        <div class="detail-section">
          <div class="section-title">参数配置</div>
          <div class="detail-source">广场作品 · {{ item.model }} · ♥ {{ item.like_count }}</div>
          <div class="detail-params">
            <div><span>{{ isVideoTask(item) ? '分辨率' : '尺寸' }}</span><strong>{{ isVideoTask(item) ? `${item.video_width || 0}x${item.video_height || 0}` : item.size }}</strong></div>
            <div><span>{{ isVideoTask(item) ? '比例' : '质量' }}</span><strong>{{ isVideoTask(item) ? item.video_ratio : item.quality }}</strong></div>
            <div><span>{{ isVideoTask(item) ? '时长' : '格式' }}</span><strong>{{ isVideoTask(item) ? `${item.video_duration || 0}s` : item.output_format }}</strong></div>
            <div><span>审核</span><strong>{{ item.moderation }}</strong></div>
            <div><span>请求</span><strong>{{ item.stream ? '流式' : '普通' }}</strong></div>
          </div>
        </div>
        <p class="detail-time">发布于 {{ formatTime(item.created_at) }}</p>
        <div class="detail-buttons plaza-detail-buttons">
          <button class="blue" @click="emit('reuse', item); emit('close')">
            <AppIcon name="copy" />
            <span>复用配置</span>
          </button>
          <button class="purple" :disabled="!(item.result_images?.[0]?.url || item.result_videos?.[0]?.url)" @click="emit('openResult', item)">
            <AppIcon name="download" />
            <span>{{ isVideoTask(item) ? '打开视频' : '下载图片' }}</span>
          </button>
          <button class="star" :class="{ favorite: item.liked }" @click="emit('toggleLike', item, $event)">
            <AppIcon name="favorite" />
            <span>{{ item.liked ? '取消点赞' : '点赞' }} {{ item.like_count }}</span>
          </button>
        </div>
      </div>
    </section>
  </div>
</template>
