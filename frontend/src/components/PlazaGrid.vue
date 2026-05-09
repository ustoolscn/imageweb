<script setup lang="ts">
import type { PlazaItem } from '../types'
import { displayImageURL } from '../lib/view'

defineProps<{
  items: PlazaItem[]
  loadingMore: boolean
  hasMorePlazaItems: boolean
}>()

const emit = defineEmits<{
  selectItem: [item: PlazaItem]
  reuse: [item: PlazaItem]
  toggleLike: [item: PlazaItem, event: Event]
  loadMore: []
}>()
</script>

<template>
  <section v-if="items.length === 0" class="empty-state glass-panel soft">
    <h2>广场还没有作品</h2>
    <p>成功任务可以点击分享发布到广场，所有访问者都可以看到、复用配置和点赞。</p>
  </section>

  <section class="plaza-grid">
    <article v-for="item in items" :key="item.id" class="plaza-card" @click="emit('selectItem', item)">
      <img v-if="item.result_images?.[0]?.url" :src="displayImageURL(item.result_images[0])" alt="广场作品" loading="lazy" decoding="async" />
      <div v-else class="plaza-card-empty">暂无图片</div>
      <div class="plaza-card-actions" @click.stop>
        <button type="button" title="复用配置" aria-label="复用配置" @click="emit('reuse', item)">
          <svg viewBox="0 0 24 24" aria-hidden="true"><rect x="8" y="8" width="11" height="11" rx="2"/><path d="M5 16V7a2 2 0 0 1 2-2h9"/></svg>
          <span>复用</span>
        </button>
        <button type="button" :title="item.liked ? '取消点赞' : '点赞'" :aria-label="item.liked ? '取消点赞' : '点赞'" :class="{ favorite: item.liked }" @click="emit('toggleLike', item, $event)">
          <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 21s-7-4.35-9.33-8.77C.87 8.82 2.8 5 6.55 5c2.06 0 3.3 1.1 4.05 2.1C11.35 6.1 12.59 5 14.65 5c3.75 0 5.68 3.82 3.88 7.23C16.2 16.65 12 21 12 21Z"/></svg>
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
