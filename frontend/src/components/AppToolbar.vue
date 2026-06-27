<script setup lang="ts">
import { computed } from 'vue'
import AppIcon from './AppIcon.vue'
import InlineSelect from './InlineSelect.vue'
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
  adminMode?: boolean
  adminSection?: 'tasks' | 'canvas'
  adminUserLabel?: string
}>()

const emit = defineEmits<{
  openSettings: []
  openOnboarding: []
  openAdminUserSwitcher: []
  switchAdminSection: [section: 'tasks' | 'canvas']
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

const statusOptions = [
  { value: 'all', label: '全部状态' },
  { value: 'pending', label: '排队中' },
  { value: 'running', label: '生成中' },
  { value: 'succeeded', label: '成功' },
  { value: 'failed', label: '失败' },
]

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

const effectiveViewMode = computed(() => props.adminMode ? (props.adminSection || 'tasks') : props.viewMode)

function updateStatus(value: string) {
  currentStatus.value = value
  emit('resetTasks')
}
</script>

<template>
  <header class="toolbar glass-panel">
    <div class="brand">
      <div class="brand-logo">
        <img v-if="siteIcon.startsWith('http://') || siteIcon.startsWith('https://')" :src="siteIcon" alt="站点图标" crossorigin="anonymous" />
        <span v-else>{{ siteIcon }}</span>
      </div>
      <div class="brand-copy">
        <div class="brand-title-row">
          <h1>{{ siteTitle }}</h1>
          <button class="settings-button icon-only" title="连接设置" aria-label="连接设置" @click="emit('openSettings')">
            <AppIcon name="settings" />
          </button>
          <button v-if="adminMode" class="settings-button admin-user-mini icon-only" :title="`切换用户：${adminUserLabel || ''}`" aria-label="切换用户" @click="emit('openAdminUserSwitcher')">
            <AppIcon name="user" />
          </button>
        </div>
        <p>{{ visibleSubtitle }}</p>
      </div>
    </div>

    <div class="toolbar-controls" :class="{ tasks: effectiveViewMode === 'tasks', plaza: effectiveViewMode === 'plaza', canvas: effectiveViewMode === 'canvas', admin: adminMode }">
      <div v-if="!adminMode" class="view-tabs" :class="hideCanvas ? 'two-tabs' : 'three-tabs'">
        <button title="任务" aria-label="任务" :class="{ active: effectiveViewMode === 'tasks' }" @click="emit('switchView', 'tasks')">
          <AppIcon name="task" /><span class="view-tab-label">任务</span>
        </button>
        <button v-if="!hideCanvas" title="画布" aria-label="画布" :class="{ active: effectiveViewMode === 'canvas' }" @click="emit('switchView', 'canvas')">
          <AppIcon name="canvas" /><span class="view-tab-label">画布</span>
        </button>
        <button title="广场" aria-label="广场" :class="{ active: effectiveViewMode === 'plaza' }" @click="emit('switchView', 'plaza')">
          <AppIcon name="gallery" /><span class="view-tab-label">广场</span>
        </button>
      </div>
      <div v-if="adminMode" class="view-tabs two-tabs admin-view-tabs">
        <button title="任务" aria-label="任务" :class="{ active: effectiveViewMode === 'tasks' }" @click="emit('switchAdminSection', 'tasks')">
          <AppIcon name="task" /><span class="view-tab-label">任务</span>
        </button>
        <button title="画布" aria-label="画布" :class="{ active: effectiveViewMode === 'canvas' }" @click="emit('switchAdminSection', 'canvas')">
          <AppIcon name="canvas" /><span class="view-tab-label">画布</span>
        </button>
      </div>
      <template v-if="effectiveViewMode === 'tasks'">
        <InlineSelect class="toolbar-status-select" label="状态" :model-value="currentStatus" :options="statusOptions" @update:model-value="updateStatus" />
        <div class="search-wrap">
          <input v-model="currentKeyword" class="search" placeholder="搜索提示词、参数..." @keyup.enter="emit('resetTasks')" />
        </div>
        <button class="ghost task-search-button compact-on-narrow" title="搜索" aria-label="搜索" @click="emit('resetTasks')">
          <AppIcon name="search" /><span class="toolbar-action-label">搜索</span>
        </button>
        <button class="ghost compact-on-narrow" :class="{ active: favoriteOnly }" :title="favoriteOnly ? '看全部' : '只看收藏'" :aria-label="favoriteOnly ? '看全部' : '只看收藏'" @click="emit('toggleFavoriteOnly')">
          <AppIcon name="favorite" /><span class="toolbar-action-label">{{ favoriteOnly ? '看全部' : '只看收藏' }}</span>
        </button>
        <button class="ghost compact-on-narrow" title="刷新" aria-label="刷新" @click="emit('refreshTasks')">
          <AppIcon name="refresh" /><span class="toolbar-action-label">刷新</span>
        </button>
      </template>
      <template v-else-if="effectiveViewMode === 'plaza'">
        <div class="search-wrap plaza-search">
          <input v-model="currentPlazaKeyword" class="search" placeholder="搜索广场作品..." @keyup.enter="emit('refreshPlazaItems')" />
        </div>
        <button class="ghost plaza-search-button compact-on-narrow" title="搜索" aria-label="搜索" @click="emit('refreshPlazaItems')">
          <AppIcon name="search" /><span class="toolbar-action-label">搜索</span>
        </button>
        <div class="plaza-sort">
          <button :class="{ active: plazaSort === 'time' }" @click="emit('switchPlazaSort', 'time')">最新发布</button>
          <button :class="{ active: plazaSort === 'likes' }" @click="emit('switchPlazaSort', 'likes')">点赞最多</button>
        </div>
        <button class="ghost compact-on-narrow" title="刷新" aria-label="刷新" @click="emit('refreshPlazaItems')">
          <AppIcon name="refresh" /><span class="toolbar-action-label">刷新</span>
        </button>
      </template>
      <template v-else-if="adminMode && effectiveViewMode === 'canvas'">
        <span></span>
        <button class="ghost compact-on-narrow" title="刷新画布" aria-label="刷新画布" @click="emit('refreshTasks')">
          <AppIcon name="refresh" /><span class="toolbar-action-label">刷新</span>
        </button>
      </template>
      <button class="ghost toolbar-help icon-only" title="新手教程" aria-label="新手教程" @click="emit('openOnboarding')"><AppIcon name="help" /></button>
      <button class="ghost theme-toggle icon-only" :title="`当前主题：${currentThemeLabel}`" :aria-label="`当前主题：${currentThemeLabel}`" @click="emit('toggleTheme')"><AppIcon :name="currentThemeIcon" /></button>
    </div>
  </header>
</template>
