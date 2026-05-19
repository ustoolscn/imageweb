<script setup lang="ts">
import { computed } from 'vue'
import AppIcon from './AppIcon.vue'
import type { PlazaSort, ThemeMode, ViewMode } from '../uiTypes'

const props = defineProps<{
  siteTitle: string
  siteIcon: string
  visibleSubtitle: string
  viewMode: ViewMode
  status: string
  keyword: string
  plazaKeyword: string
  favoriteOnly: boolean
  plazaSort: PlazaSort
  themeMode: ThemeMode
  hideCanvas?: boolean
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
  'update:plazaKeyword': [keyword: string]
}>()

const currentStatus = computed({
  get: () => props.status,
  set: (value: string) => emit('update:status', value),
})

const currentKeyword = computed({
  get: () => props.keyword,
  set: (value: string) => emit('update:keyword', value),
})

const currentPlazaKeyword = computed({
  get: () => props.plazaKeyword,
  set: (value: string) => emit('update:plazaKeyword', value),
})

const currentThemeLabel = computed(() => {
  if (props.themeMode === 'system') return '系统'
  if (props.themeMode === 'light') return '浅色'
  return '深色'
})

const currentThemeIcon = computed(() => {
  if (props.themeMode === 'light') return 'sun'
  if (props.themeMode === 'dark') return 'moon'
  return 'contrast'
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
          <button class="settings-button icon-only" title="连接设置" aria-label="连接设置" @click="emit('openSettings')">
            <AppIcon name="settings" />
          </button>
        </div>
        <p>{{ visibleSubtitle }}</p>
      </div>
    </div>

    <div class="toolbar-controls" :class="{ plaza: viewMode === 'plaza', canvas: viewMode === 'canvas' }">
      <div class="view-tabs three-tabs" :class="{ 'two-tabs': hideCanvas }">
        <button :class="{ active: viewMode === 'tasks' }" @click="emit('switchView', 'tasks')"><AppIcon name="task" />任务</button>
        <button v-if="!hideCanvas" :class="{ active: viewMode === 'canvas' }" @click="emit('switchView', 'canvas')"><AppIcon name="canvas" />画布</button>
        <button :class="{ active: viewMode === 'plaza' }" @click="emit('switchView', 'plaza')"><AppIcon name="gallery" />广场</button>
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
          <AppIcon name="search" :size="14" />
          <input v-model="currentKeyword" class="search" placeholder="搜索提示词、参数..." @keyup.enter="emit('resetTasks')" />
        </div>
        <button class="ghost task-search-button" @click="emit('resetTasks')"><AppIcon name="search" />搜索</button>
        <button class="ghost" :class="{ active: favoriteOnly }" @click="emit('toggleFavoriteOnly')"><AppIcon name="favorite" />{{ favoriteOnly ? '看全部' : '只看收藏' }}</button>
        <button class="ghost" @click="emit('refreshTasks')"><AppIcon name="refresh" />刷新</button>
      </template>
      <template v-else-if="viewMode === 'plaza'">
        <div class="search-wrap plaza-search">
          <AppIcon name="search" :size="14" />
          <input v-model="currentPlazaKeyword" class="search" placeholder="搜索广场作品..." @keyup.enter="emit('refreshPlazaItems')" />
        </div>
        <button class="ghost plaza-search-button" @click="emit('refreshPlazaItems')"><AppIcon name="search" />搜索</button>
        <div class="plaza-sort">
          <button :class="{ active: plazaSort === 'time' }" @click="emit('switchPlazaSort', 'time')">最新发布</button>
          <button :class="{ active: plazaSort === 'likes' }" @click="emit('switchPlazaSort', 'likes')">点赞最多</button>
        </div>
        <button class="ghost" @click="emit('refreshPlazaItems')"><AppIcon name="refresh" />刷新</button>
      </template>
      <button class="ghost theme-toggle icon-only" :title="`当前主题：${currentThemeLabel}`" :aria-label="`当前主题：${currentThemeLabel}`" @click="emit('toggleTheme')"><AppIcon :name="currentThemeIcon" /></button>
    </div>
  </header>
</template>
