<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue'
import videojs from 'video.js'
import 'video.js/dist/video-js.css'

const props = defineProps<{
  src?: string
}>()

const emit = defineEmits<{
  time: [seconds: number]
  duration: [seconds: number]
}>()

const videoEl = ref<HTMLVideoElement | null>(null)
let player: ReturnType<typeof videojs> | null = null

onMounted(() => {
  if (!videoEl.value) return
  configureVideoElement(videoEl.value)
  player = videojs(videoEl.value, {
    controls: true,
    preload: 'metadata',
    fluid: false,
    fill: true,
    responsive: true,
    inactivityTimeout: 0,
    controlBar: {
      pictureInPictureToggle: false,
      remainingTimeDisplay: false,
    },
  })
  player.on('timeupdate', emitCurrentTime)
  player.on('seeked', emitCurrentTime)
  player.on('loadedmetadata', emitDuration)
  player.on('durationchange', emitDuration)
  syncSource()
})

onUnmounted(() => {
  player?.dispose()
  player = null
})

watch(() => props.src, () => syncSource())

function syncSource() {
  if (!player || !props.src) return
  if (player.currentSrc() === props.src) return
  player.src({ src: props.src })
  const tech = player.tech(true)
  const el = tech?.el?.()
  if (el instanceof HTMLVideoElement) configureVideoElement(el)
}

function configureVideoElement(video: HTMLVideoElement) {
  video.crossOrigin = 'anonymous'
  video.setAttribute('crossorigin', 'anonymous')
  video.disablePictureInPicture = true
  video.setAttribute('disablepictureinpicture', '')
  video.setAttribute('controlsList', 'noremoteplayback')
}

function emitCurrentTime() {
  if (!player) return
  const seconds = player.currentTime() || 0
  if (Number.isFinite(seconds)) emit('time', Math.max(0, Math.round(seconds * 100) / 100))
}

function emitDuration() {
  if (!player) return
  const seconds = player.duration() || 0
  if (Number.isFinite(seconds)) emit('duration', Math.max(0, Math.round(seconds * 100) / 100))
}
</script>

<template>
  <div class="canvas-video-player" @pointerdown.stop>
    <video ref="videoEl" class="video-js vjs-big-play-centered" playsinline crossorigin="anonymous" disablepictureinpicture controlslist="noremoteplayback"></video>
  </div>
</template>
