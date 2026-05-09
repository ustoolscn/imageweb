<script setup lang="ts">
import { computed } from 'vue'
import type { PlazaSort, ThemeMode, ViewMode } from '../uiTypes'

const props = defineProps<{
  siteTitle: string
  siteIcon: string
  visibleSubtitle: string
  viewMode: ViewMode
  status: string
  keyword: string
  favoriteOnly: boolean
  plazaSort: PlazaSort
  themeMode: ThemeMode
}>()

const emit = defineEmits<{
  openSettings: []
  switchView: [mode: ViewMode]
  refreshTasks: []
  resetTasks: []
  refreshPlazaItems: []
  switchPlazaSort: [sort: PlazaSort]
  toggleTheme: []
  toggleFavoriteOnly: []
  'update:status': [status: string]
  'update:keyword': [keyword: string]
}>()

const currentStatus = computed({
  get: () => props.status,
  set: (value: string) => emit('update:status', value),
})

const currentKeyword = computed({
  get: () => props.keyword,
  set: (value: string) => emit('update:keyword', value),
})

const currentThemeLabel = computed(() => {
  if (props.themeMode === 'system') return '系统'
  if (props.themeMode === 'light') return '浅色'
  return '深色'
})

const currentThemeIcon = computed(() => {
  if (props.themeMode === 'system') return '◐'
  if (props.themeMode === 'light') return '☀'
  return '☾'
})
</script>

<template>
  <header class="toolbar glass-panel">
    <div class="brand">
      <div class="brand-logo">
        <img v-if="siteIcon.startsWith('http://') || siteIcon.startsWith('https://')" :src="siteIcon" alt="站点图标" />
        <span v-else>{{ siteIcon }}</span>
      </div>
      <div class="brand-copy">
        <div class="brand-title-row">
          <h1>{{ siteTitle }}</h1>
          <button class="settings-button" title="连接设置" aria-label="连接设置" @click="emit('openSettings')">
            <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 15.5A3.5 3.5 0 1 0 12 8a3.5 3.5 0 0 0 0 7.5Z"/><path d="M19.4 15a1.7 1.7 0 0 0 .34 1.87l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06A1.7 1.7 0 0 0 15 19.36a1.7 1.7 0 0 0-1 .56 1.7 1.7 0 0 0-.39 1.08V21a2 2 0 1 1-4 0v-.09a1.7 1.7 0 0 0-.39-1.08 1.7 1.7 0 0 0-1-.56 1.7 1.7 0 0 0-1.87.34l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06A1.7 1.7 0 0 0 4.64 15a1.7 1.7 0 0 0-.56-1 1.7 1.7 0 0 0-1.08-.39H3a2 2 0 1 1 0-4h.09a1.7 1.7 0 0 0 1.08-.39 1.7 1.7 0 0 0 .56-1 1.7 1.7 0 0 0-.34-1.87l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06A1.7 1.7 0 0 0 9 4.64a1.7 1.7 0 0 0 1-.56 1.7 1.7 0 0 0 .39-1.08V3a2 2 0 1 1 4 0v.09a1.7 1.7 0 0 0 .39 1.08 1.7 1.7 0 0 0 1 .56 1.7 1.7 0 0 0 1.87-.34l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06A1.7 1.7 0 0 0 19.36 9c.22.35.35.73.39 1.14H21a2 2 0 1 1 0 4h-.09a1.7 1.7 0 0 0-1.08.39 1.7 1.7 0 0 0-.43.47Z"/></svg>
          </button>
        </div>
        <p>{{ visibleSubtitle }}</p>
      </div>
    </div>

    <div class="toolbar-controls" :class="{ plaza: viewMode === 'plaza' }">
      <div class="view-tabs">
        <button :class="{ active: viewMode === 'tasks' }" @click="emit('switchView', 'tasks')">我的任务</button>
        <button :class="{ active: viewMode === 'plaza' }" @click="emit('switchView', 'plaza')">广场</button>
      </div>
      <template v-if="viewMode === 'tasks'">
        <select v-model="currentStatus" @change="emit('resetTasks')">
          <option value="all">全部状态</option>
          <option value="pending">排队中</option>
          <option value="running">生成中</option>
          <option value="succeeded">成功</option>
          <option value="failed">失败</option>
        </select>
        <div class="search-wrap">
          <span>⌕</span>
          <input v-model="currentKeyword" class="search" placeholder="搜索提示词、参数..." @keyup.enter="emit('resetTasks')" />
        </div>
        <button class="ghost" :class="{ active: favoriteOnly }" @click="emit('toggleFavoriteOnly')">{{ favoriteOnly ? '看全部' : '只看收藏' }}</button>
        <button class="ghost" @click="emit('refreshTasks')">刷新</button>
      </template>
      <template v-else>
        <div class="plaza-sort">
          <button :class="{ active: plazaSort === 'time' }" @click="emit('switchPlazaSort', 'time')">最新发布</button>
          <button :class="{ active: plazaSort === 'likes' }" @click="emit('switchPlazaSort', 'likes')">点赞最多</button>
        </div>
        <button class="ghost" @click="emit('refreshPlazaItems')">刷新</button>
      </template>
      <button class="ghost theme-toggle" :title="`当前主题：${currentThemeLabel}`" :aria-label="`当前主题：${currentThemeLabel}`" @click="emit('toggleTheme')">{{ currentThemeIcon }}</button>
    </div>
  </header>
</template>
