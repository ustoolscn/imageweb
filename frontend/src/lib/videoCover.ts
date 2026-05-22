import { ref } from 'vue'

const coverCache = ref<Record<string, string>>({})
const pendingCovers = new Map<string, Promise<string>>()

export function videoCoverURL(url?: string) {
  return url ? coverCache.value[url] || '' : ''
}

export function ensureVideoCover(url?: string) {
  if (!url) return Promise.resolve('')
  if (coverCache.value[url]) return Promise.resolve(coverCache.value[url])
  const pending = pendingCovers.get(url)
  if (pending) return pending
  const task = captureVideoCover(url)
    .then((cover) => {
      coverCache.value = { ...coverCache.value, [url]: cover }
      pendingCovers.delete(url)
      return cover
    })
    .catch((error) => {
      pendingCovers.delete(url)
      throw error
    })
  pendingCovers.set(url, task)
  return task
}

function captureVideoCover(url: string) {
  return new Promise<string>((resolve, reject) => {
    const video = document.createElement('video')
    let done = false
    let timeout = 0
    const cleanup = () => {
      done = true
      window.clearTimeout(timeout)
      video.pause()
      video.removeAttribute('src')
      video.load()
    }
    const fail = () => {
      if (done) return
      cleanup()
      reject(new Error('无法读取视频封面'))
    }
    const finish = () => {
      if (done) return
      try {
        const sourceWidth = video.videoWidth || 320
        const sourceHeight = video.videoHeight || 180
        const scale = Math.min(1, 480 / Math.max(sourceWidth, sourceHeight))
        const width = Math.max(1, Math.round(sourceWidth * scale))
        const height = Math.max(1, Math.round(sourceHeight * scale))
        const canvas = document.createElement('canvas')
        canvas.width = width
        canvas.height = height
        const ctx = canvas.getContext('2d')
        if (!ctx) throw new Error('无法创建视频封面')
        ctx.drawImage(video, 0, 0, width, height)
        const cover = canvas.toDataURL('image/jpeg', 0.82)
        cleanup()
        resolve(cover)
      } catch (error) {
        cleanup()
        reject(error)
      }
    }
    video.crossOrigin = 'anonymous'
    video.muted = true
    video.playsInline = true
    video.preload = 'auto'
    video.addEventListener('error', fail, { once: true })
    video.addEventListener('loadeddata', () => {
      try {
        if (video.duration && Number.isFinite(video.duration)) video.currentTime = Math.min(0.1, Math.max(0, video.duration - 0.1))
        else finish()
      } catch {
        finish()
      }
    }, { once: true })
    video.addEventListener('seeked', finish, { once: true })
    timeout = window.setTimeout(fail, 12000)
    video.src = url
    video.load()
  })
}
