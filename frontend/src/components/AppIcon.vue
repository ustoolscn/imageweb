<script setup lang="ts">
import {
  Archive,
  AudioLines,
  Bot,
  Brush,
  Check,
  ChevronDown,
  CircleDotDashed,
  Contrast,
  Copy,
  Compass,
  Download,
  Eraser,
  Eye,
  EyeOff,
  FileJson,
  Heart,
  Image,
  Images,
  LayoutGrid,
  Map,
  Maximize2,
  MessageSquareText,
  Minimize2,
  Moon,
  PanelTop,
  Pencil,
  Play,
  Plus,
  RefreshCw,
  RotateCcw,
  Scan,
  Search,
  Settings,
  Share2,
  Sparkles,
  SquareDashedMousePointer,
  StopCircle,
  Sun,
  Trash2,
  Type,
  Video,
  WandSparkles,
  X,
  ZoomIn,
  ZoomOut,
} from '@lucide/vue'
import { computed } from 'vue'

const props = withDefaults(defineProps<{
  name: string
  size?: number
  strokeWidth?: number
}>(), {
  size: 16,
  strokeWidth: 2,
})

const icons = {
  add: Plus,
  archive: Archive,
  audio: AudioLines,
  bot: Bot,
  brush: Brush,
  canvas: SquareDashedMousePointer,
  check: Check,
  chevronDown: ChevronDown,
  contrast: Contrast,
  close: X,
  compass: Compass,
  copy: Copy,
  download: Download,
  eraser: Eraser,
  eye: Eye,
  eyeOff: EyeOff,
  favorite: Heart,
  file: FileJson,
  gallery: Images,
  grid: LayoutGrid,
  image: Image,
  magic: WandSparkles,
  map: Map,
  merge: CircleDotDashed,
  moon: Moon,
  panel: PanelTop,
  pencil: Pencil,
  play: Play,
  refresh: RefreshCw,
  undo: RotateCcw,
  resetView: Scan,
  search: Search,
  settings: Settings,
  share: Share2,
  sparkles: Sparkles,
  stop: StopCircle,
  text: Type,
  trash: Trash2,
  video: Video,
  zen: Minimize2,
  zoomIn: ZoomIn,
  zoomOut: ZoomOut,
  maximize: Maximize2,
  task: MessageSquareText,
  sun: Sun,
}

const iconComponent = computed(() => icons[props.name as keyof typeof icons] || CircleDotDashed)
</script>

<template>
  <component
    :is="iconComponent"
    class="app-icon"
    :size="size"
    :stroke-width="strokeWidth"
    aria-hidden="true"
  />
</template>
