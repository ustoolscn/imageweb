<script setup lang="ts">
import { ref } from 'vue'
import type { PlazaItem } from '../types'
import { canvasPreviewAspectRatio, canvasPreviewMedia, canvasPreviewUrl } from '../lib/canvasPreview'
import { isVideoTask } from '../lib/view'
import AppIcon from './AppIcon.vue'

const loadedImages = ref(new Set<string>())
const measuredCanvasRatios = ref<Record<string, string>>({})

const props = defineProps<{
  items: PlazaItem[]
  loadingMore: boolean
  hasMorePlazaItems: boolean
}>()

const emit = defineEmits<{
  selectItem: [item: PlazaItem]
  openPreview: [url: string, label: string, event?: Event, maskUrl?: string]
  contextMenu: [item: PlazaItem, event: MouseEvent]
  reuse: [item: PlazaItem]
  toggleLike: [item: PlazaItem, event: Event]
  loadMore: []
}>()

function imageUrl(item: PlazaItem) {
  return item.result_images?.[0]?.url || ''
}

function videoUrl(item: PlazaItem) {
  return item.result_videos?.[0]?.url || ''
}

function videoCover(item: PlazaItem) {
  const video = item.result_videos?.[0]
  return video?.thumbnail_url || video?.first_frame_url || ''
}

function previewImageUrl(item: PlazaItem) {
  const image = item.result_images?.[0]
  return image?.thumbnail_url || image?.url || ''
}

function cardImageUrl(item: PlazaItem) {
  if (item.item_type === 'canvas') return canvasPreviewUrl(item)
  return previewImageUrl(item) || imageUrl(item)
}

function cardMediaUrl(item: PlazaItem) {
  if (item.item_type === 'canvas') return canvasPreviewUrl(item)
  return isVideoTask(item) ? videoUrl(item) : cardImageUrl(item)
}

function isCardVideo(item: PlazaItem) {
  return item.item_type === 'canvas' ? canvasPreviewMedia(item)?.type === 'video' : isVideoTask(item)
}

function cardVideoCover(item: PlazaItem) {
  if (item.item_type === 'canvas') {
    const media = canvasPreviewMedia(item)
    return media?.thumbnail_url || ''
  }
  return videoCover(item)
}

function referenceImageUrl(item: PlazaItem) {
  const image = item.reference_images?.[0]
  return image?.thumbnail_url || image?.url || ''
}

function referenceMoreCount(item: PlazaItem) {
  return Math.max(0, (item.reference_images?.length || 0) - 1)
}

function markImageLoaded(url: string) {
  loadedImages.value = new Set(loadedImages.value).add(url)
}

function isImageLoaded(url?: string) {
  return Boolean(url && loadedImages.value.has(url))
}

function markCanvasPreviewLoaded(item: PlazaItem, event: Event) {
  const image = event.currentTarget as HTMLImageElement | null
  const media = canvasPreviewMedia(item)
  if (!image?.naturalWidth || !image.naturalHeight || !media?.url) return
  measuredCanvasRatios.value = {
    ...measuredCanvasRatios.value,
    [media.url]: `${image.naturalWidth} / ${image.naturalHeight}`,
  }
}

function imageAspectRatio(item: PlazaItem) {
  if (item.item_type === 'canvas') {
    const media = canvasPreviewMedia(item)
    return (media?.url && measuredCanvasRatios.value[media.url]) || canvasPreviewAspectRatio(item)
  }
  if (isVideoTask(item)) {
    const width = item.video_width || item.result_videos?.[0]?.width || 16
    const height = item.video_height || item.result_videos?.[0]?.height || 9
    return width > 0 && height > 0 ? `${width} / ${height}` : '16 / 9'
  }
  const match = item.size?.toLowerCase().match(/^(\d+)x(\d+)$/)
  if (!match) return '1 / 1'
  const width = Number(match[1])
  const height = Number(match[2])
  return width > 0 && height > 0 ? `${width} / ${height}` : '1 / 1'
}

function canvasNodeCount(item: PlazaItem) {
  const canvas = item.canvas as { elements?: unknown[] } | undefined
  return Array.isArray(canvas?.elements) ? canvas.elements.length : 0
}

function canvasConnectionCount(item: PlazaItem) {
  const canvas = item.canvas as { connections?: unknown[] } | undefined
  return Array.isArray(canvas?.connections) ? canvas.connections.length : 0
}

</script>

<template>
  <section v-if="items.length === 0" class="empty-state glass-panel soft">
    <h2>广场还没有作品</h2>
    <p>成功任务可以点击分享发布到广场，所有访问者都可以看到、复用配置和点赞。</p>
  </section>

  <section v-if="items.length" class="plaza-grid">
    <article v-for="item in items" :key="item.id" class="plaza-card" @click="emit('selectItem', item)" @contextmenu.prevent.stop="emit('contextMenu', item, $event)">
      <div
        v-if="cardMediaUrl(item)"
        class="plaza-image-wrap"
        :class="{ loaded: isCardVideo(item) || isImageLoaded(cardImageUrl(item)), 'canvas-preview-wrap': item.item_type === 'canvas' }"
        :style="{ aspectRatio: imageAspectRatio(item) }"
      >
        <div class="plaza-image-placeholder">加载中</div>
        <template v-if="isCardVideo(item)">
          <img v-if="cardVideoCover(item)" class="preview-video" :src="cardVideoCover(item)" alt="视频封面" loading="lazy" decoding="async" crossorigin="anonymous" />
          <span class="preview-play-indicator" aria-hidden="true">
            <span></span>
          </span>
        </template>
        <img
          v-else
          :src="cardImageUrl(item)"
          alt="广场作品"
          loading="lazy"
          decoding="async"
          crossorigin="anonymous"
          fetchpriority="low"
          @load="markImageLoaded(cardImageUrl(item)); markCanvasPreviewLoaded(item, $event)"
        />
        <div v-if="referenceImageUrl(item)" class="plaza-reference-overlay" title="参考图">
          <div class="plaza-reference-badge">参考图</div>
          <img :src="referenceImageUrl(item)" alt="参考图" loading="lazy" decoding="async" crossorigin="anonymous" />
          <span v-if="referenceMoreCount(item)">+{{ referenceMoreCount(item) }}</span>
        </div>
        <div v-if="item.prompt" class="plaza-prompt-hover">
          <span>提示词</span>
          <p>{{ item.prompt }}</p>
        </div>
      </div>
      <div v-else-if="item.item_type === 'canvas'" class="plaza-card-empty plaza-canvas-card">
        <AppIcon name="canvas" :size="34" />
        <strong>{{ item.canvas_name || '广场画布' }}</strong>
        <span>{{ canvasNodeCount(item) }} 个节点 · {{ canvasConnectionCount(item) }} 条连线</span>
      </div>
      <div v-else class="plaza-card-empty">暂无图片</div>
      <div class="plaza-card-actions" @click.stop>
        <button type="button" title="复用配置" aria-label="复用配置" @click="emit('reuse', item)">
          <AppIcon name="copy" />
          <span>{{ item.item_type === 'canvas' ? '导入' : '复用' }}</span>
        </button>
        <button type="button" :title="item.liked ? '取消点赞' : '点赞'" :aria-label="item.liked ? '取消点赞' : '点赞'" :class="{ favorite: item.liked }" @click="emit('toggleLike', item, $event)">
          <AppIcon name="favorite" />
          <span>{{ item.like_count }}</span>
        </button>
      </div>
    </article>
  </section>

  <div v-if="items.length" class="load-more-state">
    <span v-if="loadingMore">正在加载更多...</span>
    <button v-else-if="hasMorePlazaItems" type="button" @click="emit('loadMore')">加载更多</button>
    <span v-else>没有更多作品了</span>
  </div>
</template>
