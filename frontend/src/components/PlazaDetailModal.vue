<script setup lang="ts">
import type { PlazaItem } from '../types'
import { displayImageURL, formatTime, taskReferenceImages } from '../lib/view'

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
      <button class="modal-close" @click="emit('close')">×</button>
      <div class="detail-preview">
        <img v-if="item.result_images?.[0]?.url" :src="displayImageURL(item.result_images[0])" alt="广场作品" title="点击查看大图" loading="lazy" decoding="async" @click="emit('openPreview', item.result_images[0].url, '广场作品', $event)" />
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
            <div><span>尺寸</span><strong>{{ item.size }}</strong></div>
            <div><span>质量</span><strong>{{ item.quality }}</strong></div>
            <div><span>格式</span><strong>{{ item.output_format }}</strong></div>
            <div><span>审核</span><strong>{{ item.moderation }}</strong></div>
            <div><span>请求</span><strong>{{ item.stream ? '流式' : '普通' }}</strong></div>
          </div>
        </div>
        <p class="detail-time">发布于 {{ formatTime(item.created_at) }}</p>
        <div class="detail-buttons plaza-detail-buttons">
          <button class="blue" @click="emit('reuse', item); emit('close')">
            <svg viewBox="0 0 24 24" aria-hidden="true"><rect x="8" y="8" width="11" height="11" rx="2"/><path d="M5 16V7a2 2 0 0 1 2-2h9"/></svg>
            <span>复用配置</span>
          </button>
          <button class="purple" :disabled="!item.result_images?.[0]?.url" @click="emit('openResult', item)">
            <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 3v12"/><path d="m7 10 5 5 5-5"/><path d="M5 21h14"/></svg>
            <span>下载图片</span>
          </button>
          <button class="star" :class="{ favorite: item.liked }" @click="emit('toggleLike', item, $event)">
            <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 21s-7-4.35-9.33-8.77C.87 8.82 2.8 5 6.55 5c2.06 0 3.3 1.1 4.05 2.1C11.35 6.1 12.59 5 14.65 5c3.75 0 5.68 3.82 3.88 7.23C16.2 16.65 12 21 12 21Z"/></svg>
            <span>{{ item.liked ? '取消点赞' : '点赞' }} {{ item.like_count }}</span>
          </button>
        </div>
      </div>
    </section>
  </div>
</template>
