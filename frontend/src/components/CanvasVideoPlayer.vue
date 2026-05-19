<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue'
import videojs from 'video.js'
import 'video.js/dist/video-js.css'

const props = defineProps<{
  src?: string
}>()

const videoEl = ref<HTMLVideoElement | null>(null)
let player: ReturnType<typeof videojs> | null = null

onMounted(() => {
  if (!videoEl.value) return
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
}
</script>

<template>
  <div class="canvas-video-player" @pointerdown.stop>
    <video ref="videoEl" class="video-js vjs-big-play-centered" playsinline></video>
  </div>
</template>
