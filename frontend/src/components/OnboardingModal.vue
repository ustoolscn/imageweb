<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import AppIcon from './AppIcon.vue'
import type { ViewMode } from '../uiTypes'

const props = defineProps<{
  hasConfig: boolean
  hideCanvas?: boolean
}>()

const emit = defineEmits<{
  close: []
  openSettings: []
  focusView: [mode: ViewMode]
  switchView: [mode: ViewMode]
}>()

type GuideStep = {
  title: string
  text: string
  tip: string
  details?: string[]
  icon: string
  selectors: string[]
  view?: ViewMode
  action?: 'openSettings' | ViewMode
  actionLabel?: string
}

const steps = computed<GuideStep[]>(() => [
  {
    title: '先连接生成接口',
    text: props.hasConfig ? '这里可以随时查看或更换 BASEURL 和 APIKEY。' : '第一次使用先点这里填写 BASEURL 和 APIKEY，保存后就能提交生成任务。',
    tip: props.hasConfig ? '你已经配置过连接，后续生成会直接使用当前配置。' : '如果没有配置，任务页会显示“缺少连接配置”。',
    icon: 'settings',
    selectors: ['.settings-button'],
    action: 'openSettings',
    actionLabel: props.hasConfig ? '查看设置' : '打开设置',
  },
  {
    title: '用这里切换工作区',
    text: '顶部这组按钮负责切换任务、画布和广场。任务页适合快速生成，画布适合串联工作流，广场用于查看和复用公开作品。',
    tip: props.hideCanvas ? '当前窗口较窄，画布入口会暂时隐藏；加宽窗口后会出现。' : '切换视图不会清空你正在编辑的参数。',
    icon: 'gallery',
    selectors: ['.view-tabs'],
  },
  {
    title: '在这里写提示词',
    text: '任务页底部是生成控制台。先选择图片生成或视频生成，再在输入框里描述你想要的画面或视频。',
    tip: '输入 @ 可以引用已经添加的参考素材；手机窄屏下需要先展开“生成控制台”。',
    icon: 'text',
    selectors: ['.composer:not(.mobile-hidden) .composer-rich-prompt', '.composer .composer-rich-prompt', '.mobile-console-toggle'],
    view: 'tasks',
  },
  {
    title: '调整模型和参数',
    text: '提示词下方是模型、尺寸、质量、格式、数量等参数。图片数量最高 5，会并发创建多个独立任务。',
    tip: '参考素材入口在右侧“参考”按钮里，支持图片、视频和音频等类型。',
    icon: 'settings',
    selectors: ['.composer:not(.mobile-hidden) .composer-controls', '.composer .composer-controls'],
    view: 'tasks',
  },
  {
    title: '点击这里开始生成',
    text: '确认提示词和参数后，点这个按钮提交。任务会进入后端队列，页面会自动刷新状态。',
    tip: '图片任务超过 10 分钟会自动失败，避免一直卡在生成中。',
    icon: 'play',
    selectors: ['.composer:not(.mobile-hidden) .submit', '.composer .submit'],
    view: 'tasks',
  },
  {
    title: '任务结果在这里查看',
    text: '提交后的任务会出现在列表里。卡片上可以查看详情、复用配置、收藏、重试、分享或打开源数据。',
    tip: '任务完成后生成素材也会进入素材体系，画布里可以继续复用。',
    icon: 'task',
    selectors: ['.grid', '.empty-state'],
    view: 'tasks',
  },
  {
    title: '广场里复用公开作品',
    text: '切到广场后，可以查看别人分享的任务或画布。作品卡片支持点赞、复用参数，也可以导入公开画布。',
    tip: '广场有 60 秒内存缓存，短时间来回切换不一定重新请求。',
    icon: 'gallery',
    selectors: ['.plaza-grid', '.empty-state'],
    view: 'plaza',
  },
  ...(props.hideCanvas ? [
    {
      title: '画布需要更宽的窗口',
      text: '当前窗口宽度较小时会隐藏画布入口，避免界面拥挤。加宽窗口后，就可以进入画布编排节点工作流。',
      tip: '先用任务页快速生成，宽屏时再进入画布继续编排。',
      icon: 'canvas',
      selectors: ['.view-tabs'],
      view: 'tasks' as ViewMode,
      action: 'tasks' as const,
      actionLabel: '回到任务页',
    },
  ] : [
    {
      title: '画布底部工具栏',
      text: '画布底部悬浮工具栏是主要操作区：左侧管理画布，中间添加节点，右侧控制视图和保存状态。',
      tip: '空间不足时底部工具栏会进入抽屉模式，只展开当前同类工具。',
      details: ['新建、重命名、分享、更新分享和删除画布都在画布管理区。', '保存状态会显示本地保存、云端同步中、已保存或保存失败。', '节点工具可以快速添加文字提示词、媒体、汇合节点和 AI 生成节点。'],
      icon: 'canvas',
      selectors: ['.canvas-topbar'],
      view: 'canvas' as ViewMode,
    },
    {
      title: '添加和连接节点',
      text: '节点是画布的核心。可以添加文字、媒体、汇合、AI 生成节点，再用左右连接点串成流程。',
      tip: '从节点右侧输出点拖到另一个节点左侧输入点即可连接；不兼容的连接会有提示。',
      details: ['文字节点负责组织提示词。', '媒体节点承载图片、视频或音频素材。', '生图、生视频、生文字等节点会读取上游内容并生成结果。', '汇合节点可以把多路文字和素材合并后传给下游。'],
      icon: 'link',
      selectors: ['.canvas-flow-handle.output', '.canvas-node', '.canvas-flow'],
      view: 'canvas' as ViewMode,
    },
    {
      title: '运行、固化和节点状态',
      text: '生成节点顶部有运行和固化按钮。运行到某个节点时，会先补跑它依赖的上游节点。',
      tip: '节点成功产出后会自动固化，后续流程会跳过它；需要重新生成时先取消固化。',
      details: ['运行中节点会显示计时和状态。', '固化节点仍可作为下游输入。', '节点详情里可以查看对应任务结果和参数。'],
      icon: 'play',
      selectors: ['.canvas-node-run', '.canvas-node-freeze', '.canvas-node'],
      view: 'canvas' as ViewMode,
    },
    {
      title: '画布右键菜单',
      text: '画布支持右键快速操作。右键空白处、节点、连线或多选节点时，菜单内容会不同。',
      tip: '右键空白处可快速添加节点、自动整理、复位视图；右键节点可运行、固化、复制、查看任务、下载或删除。',
      details: ['右键空白：文字提示词、媒体节点、汇合节点、AI 生成、自动整理、复位视图。', '右键节点：运行到此节点、固化/取消固化、复制、查看任务、删除，素材节点还可下载。', '多选节点后右键可批量移动或删除相关节点。'],
      icon: 'magic',
      selectors: ['.canvas-flow', '.canvas-node'],
      view: 'canvas' as ViewMode,
    },
    {
      title: '画布快捷键',
      text: '常用键盘操作能让画布更顺手，尤其是节点很多的时候。',
      tip: '快捷键只在焦点不在输入框里时生效，避免打字时误删节点。',
      details: ['Ctrl/⌘ + Z：撤回上一步画布改动。', 'Delete：删除当前选中的节点。', '按住 Space：临时进入平移模式拖动画布。', '蒙版节点中 Q/W/E：切换平移、画笔、橡皮。', 'Esc：退出禅模式或关闭当前弹层/菜单。'],
      icon: 'keyboard',
      selectors: ['.canvas-zoom-controls', '.canvas-flow'],
      view: 'canvas' as ViewMode,
    },
    {
      title: '素材栏和视图工具',
      text: '右侧素材栏会列出可复用的图片、视频和音频结果。画布里的视频封面、首帧、尾帧会优先读取后端已固化字段。',
      tip: '左下小地图和顶部视图按钮可帮助你在大画布里定位；禅模式会临时收起素材栏和小地图。',
      details: ['素材栏可以搜索、分页加载并拖入/复用素材。', '视图工具支持撤回、自动整理、缩放和复位视图。', '3D 视角控制、尾帧节点等工具都依赖素材字段完整返回。'],
      icon: 'gallery',
      selectors: ['.asset-sidebar', '.canvas-assets-fab', '.canvas-zoom-controls'],
      view: 'canvas' as ViewMode,
      action: 'canvas' as const,
      actionLabel: '停在画布',
    },
  ]),
])

const activeIndex = ref(0)
const activeStep = computed(() => steps.value[activeIndex.value])
const isLastStep = computed(() => activeIndex.value >= steps.value.length - 1)
const tourPanel = ref<HTMLElement | null>(null)
const targetRect = ref<{ left: number; top: number; width: number; height: number } | null>(null)
const panelPosition = ref({ left: 24, top: 24 })
let updateTimer = 0
let targetMisses = 0

const highlightStyle = computed(() => {
  const rect = targetRect.value
  if (!rect) return { display: 'none' }
  return {
    left: `${rect.left}px`,
    top: `${rect.top}px`,
    width: `${rect.width}px`,
    height: `${rect.height}px`,
  }
})

const panelStyle = computed(() => ({
  left: `${panelPosition.value.left}px`,
  top: `${panelPosition.value.top}px`,
}))

function nextStep() {
  if (isLastStep.value) {
    emit('close')
    return
  }
  activeIndex.value += 1
}

function previousStep() {
  activeIndex.value = Math.max(0, activeIndex.value - 1)
}

function runStepAction() {
  const action = activeStep.value.action
  if (action === 'openSettings') emit('openSettings')
  else if (action) emit('switchView', action as ViewMode)
}

function scheduleTargetUpdate(delay = 80) {
  window.clearTimeout(updateTimer)
  updateTimer = window.setTimeout(updateTarget, delay)
}

function firstVisibleTarget(selectors: string[]) {
  for (const selector of selectors) {
    const candidates = Array.from(document.querySelectorAll<HTMLElement>(selector))
    const target = candidates.find((item) => {
      const rect = item.getBoundingClientRect()
      const style = window.getComputedStyle(item)
      return rect.width > 4 && rect.height > 4 && style.visibility !== 'hidden' && style.display !== 'none'
    })
    if (target) return target
  }
  return null
}

function clamp(value: number, min: number, max: number) {
  return Math.min(Math.max(value, min), max)
}

function updateTarget() {
  const step = activeStep.value
  if (!step) return
  const target = firstVisibleTarget(step.selectors)
  const viewportWidth = window.innerWidth
  const viewportHeight = window.innerHeight
  const panelWidth = Math.min(360, viewportWidth - 28)
  const panelHeight = Math.min(tourPanel.value?.offsetHeight || 280, viewportHeight - 28)
  const margin = 14
  const gap = 16

  if (!target) {
    targetRect.value = null
    panelPosition.value = {
      left: clamp((viewportWidth - panelWidth) / 2, margin, Math.max(margin, viewportWidth - panelWidth - margin)),
      top: clamp((viewportHeight - panelHeight) / 2, margin, Math.max(margin, viewportHeight - panelHeight - margin)),
    }
    if (targetMisses < 8) {
      targetMisses += 1
      scheduleTargetUpdate(240)
    }
    return
  }

  targetMisses = 0
  target.scrollIntoView({ behavior: 'smooth', block: 'center', inline: 'center' })
  const rect = target.getBoundingClientRect()
  const padded = {
    left: clamp(rect.left - 8, 8, viewportWidth - 16),
    top: clamp(rect.top - 8, 8, viewportHeight - 16),
    width: Math.min(rect.width + 16, viewportWidth - 16),
    height: Math.min(rect.height + 16, viewportHeight - 16),
  }
  targetRect.value = padded

  const rightLeft = rect.right + gap
  const leftLeft = rect.left - panelWidth - gap
  let left = rightLeft + panelWidth + margin <= viewportWidth ? rightLeft : leftLeft >= margin ? leftLeft : rect.left
  let top = rect.top + rect.height / 2 - panelHeight / 2

  if (left === rect.left) {
    const belowTop = rect.bottom + gap
    const aboveTop = rect.top - panelHeight - gap
    top = belowTop + panelHeight + margin <= viewportHeight ? belowTop : aboveTop >= margin ? aboveTop : top
  }

  panelPosition.value = {
    left: clamp(left, margin, Math.max(margin, viewportWidth - panelWidth - margin)),
    top: clamp(top, margin, Math.max(margin, viewportHeight - panelHeight - margin)),
  }
}

function focusStepView() {
  targetMisses = 0
  const view = activeStep.value?.view
  if (view) emit('focusView', view)
  nextTick(() => scheduleTargetUpdate(view ? 260 : 80))
}

function onKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') emit('close')
  if (event.key === 'ArrowRight') nextStep()
  if (event.key === 'ArrowLeft') previousStep()
}

watch(activeIndex, focusStepView)
watch(steps, () => {
  if (activeIndex.value >= steps.value.length) activeIndex.value = steps.value.length - 1
  focusStepView()
})

onMounted(() => {
  focusStepView()
  window.addEventListener('resize', updateTarget)
  window.addEventListener('scroll', updateTarget, true)
  nextTick(() => tourPanel.value?.focus({ preventScroll: true }))
})

onUnmounted(() => {
  window.clearTimeout(updateTimer)
  window.removeEventListener('resize', updateTarget)
  window.removeEventListener('scroll', updateTarget, true)
})
</script>

<template>
  <div class="modal-backdrop onboarding-backdrop" @wheel.self.prevent.stop @keydown="onKeydown">
    <div class="onboarding-spotlight" :style="highlightStyle" aria-hidden="true"></div>
    <section ref="tourPanel" class="onboarding-modal light-modal" :style="panelStyle" role="dialog" aria-modal="true" tabindex="-1">
      <button class="modal-close" type="button" title="关闭" aria-label="关闭" @click="emit('close')"><AppIcon name="close" /></button>

      <div class="onboarding-heading">
        <span>新手教程 · {{ activeIndex + 1 }}/{{ steps.length }}</span>
        <h2><AppIcon :name="activeStep.icon" />{{ activeStep.title }}</h2>
        <p>{{ activeStep.text }}</p>
      </div>

      <div class="onboarding-tip">
        <strong>具体指引</strong>
        <p>{{ activeStep.tip }}</p>
        <ul v-if="activeStep.details?.length" class="onboarding-details">
          <li v-for="detail in activeStep.details" :key="detail">{{ detail }}</li>
        </ul>
      </div>

      <div class="onboarding-steps" aria-label="教程步骤">
        <button
          v-for="(step, index) in steps"
          :key="step.title"
          type="button"
          :class="{ active: index === activeIndex, done: index < activeIndex }"
          @click="activeIndex = index"
        >
          <span>{{ index + 1 }}</span>
        </button>
      </div>

      <div class="onboarding-actions">
        <button class="cancel" type="button" :disabled="activeIndex === 0" @click="previousStep"><AppIcon name="undo" />上一步</button>
        <button v-if="activeStep.actionLabel" class="ghost" type="button" @click="runStepAction"><AppIcon :name="activeStep.icon" />{{ activeStep.actionLabel }}</button>
        <div v-else></div>
        <button class="confirm" type="button" @click="nextStep"><AppIcon name="check" />{{ isLastStep ? '完成教程' : '下一步' }}</button>
      </div>
    </section>
  </div>
</template>
