<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import * as THREE from 'three'
import { OrbitControls } from 'three/examples/jsm/controls/OrbitControls.js'

const props = defineProps<{
  imageUrl?: string
  azimuth: number
  elevation: number
  distance: number
  roll?: number
}>()

const emit = defineEmits<{
  change: [value: { azimuth: number; elevation: number; distance: number }]
}>()

const host = ref<HTMLDivElement | null>(null)

let renderer: THREE.WebGLRenderer | null = null
let scene: THREE.Scene | null = null
let camera: THREE.PerspectiveCamera | null = null
let controls: OrbitControls | null = null
let frame = 0
let resizeObserver: ResizeObserver | null = null
let subject: THREE.Mesh<THREE.PlaneGeometry, THREE.MeshBasicMaterial> | null = null
let subjectBack: THREE.Mesh<THREE.PlaneGeometry, THREE.MeshBasicMaterial> | null = null
let texture: THREE.Texture | null = null
let marker: THREE.Group | null = null
let markerRing: THREE.Mesh | null = null
let grid: THREE.GridHelper | null = null
let themeObserver: MutationObserver | null = null
let lightTheme = false
let draggingMarker = false
let dragPlane = new THREE.Plane(new THREE.Vector3(0, 0, 1), 0)
const dragPoint = new THREE.Vector3()

const raycaster = new THREE.Raycaster()
const pointer = new THREE.Vector2()

onMounted(() => {
  if (!host.value) return
  setupScene()
  resizeObserver = new ResizeObserver(resize)
  resizeObserver.observe(host.value)
  themeObserver = new MutationObserver(applyThemeFromDOM)
  const page = host.value.closest('.page')
  if (page) themeObserver.observe(page, { attributes: true, attributeFilter: ['class'] })
  resize()
  animate()
})

onBeforeUnmount(() => {
  if (frame) cancelAnimationFrame(frame)
  resizeObserver?.disconnect()
  themeObserver?.disconnect()
  controls?.dispose()
  renderer?.dispose()
  texture?.dispose()
  host.value?.replaceChildren()
})

watch(() => props.imageUrl, () => updateSubjectTexture())
watch(() => [props.azimuth, props.elevation, props.distance], () => updateMarkerFromProps())
watch(() => props.roll, (roll) => setSubjectRoll(roll ?? 0))

function setupScene() {
  scene = new THREE.Scene()
  lightTheme = isLightTheme()
  scene.background = new THREE.Color(lightTheme ? 0xf8fafc : 0x080b12)

  camera = new THREE.PerspectiveCamera(42, 1, 0.1, 100)
  camera.position.set(3, 2.1, 3.4)

  renderer = new THREE.WebGLRenderer({ antialias: true, alpha: true })
  renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2))
  renderer.domElement.className = 'view-control-3d-canvas'
  host.value?.appendChild(renderer.domElement)

  controls = new OrbitControls(camera, renderer.domElement)
  controls.enableDamping = true
  controls.dampingFactor = 0.08
  controls.target.set(0, 0, 0)
  controls.minDistance = 2.1
  controls.maxDistance = 7
  controls.enablePan = false

  scene.add(new THREE.AmbientLight(0xffffff, 1.9))
  const key = new THREE.DirectionalLight(0xbfd7ff, 1.8)
  key.position.set(3, 4, 5)
  scene.add(key)

  addGrid()
  addSubjectPlane()
  addCameraMarker()
  applySceneTheme()

  renderer.domElement.addEventListener('pointerdown', onPointerDown)
  renderer.domElement.addEventListener('pointermove', onPointerMove)
  renderer.domElement.addEventListener('pointerup', stopMarkerDrag)
  renderer.domElement.addEventListener('pointercancel', stopMarkerDrag)
}

function addGrid() {
  if (!scene) return
  if (grid) scene.remove(grid)
  grid = new THREE.GridHelper(
    4.6,
    12,
    lightTheme ? 0x94a3b8 : 0x426072,
    lightTheme ? 0xd5dce7 : 0x243241,
  )
  grid.position.y = -0.9
  scene.add(grid)
}

function addSubjectPlane() {
  if (!scene) return
  const geometry = new THREE.PlaneGeometry(1.42, 1.82)
  subject = new THREE.Mesh(geometry, new THREE.MeshBasicMaterial({
    map: createPlaceholderTexture(),
    side: THREE.FrontSide,
    transparent: false,
  }))
  subjectBack = new THREE.Mesh(geometry.clone(), new THREE.MeshBasicMaterial({
    color: lightTheme ? 0xe2e8f0 : 0x172033,
    side: THREE.BackSide,
  }))
  subjectBack.position.z = -0.002
  scene.add(subject)
  scene.add(subjectBack)
  setSubjectRoll(props.roll ?? 0)
  updateSubjectTexture()
}

function addCameraMarker() {
  if (!scene) return
  marker = new THREE.Group()
  const body = new THREE.Mesh(
    new THREE.SphereGeometry(0.105, 32, 16),
    new THREE.MeshStandardMaterial({ color: 0xffd166, emissive: 0x5c3400, roughness: 0.35 }),
  )
  markerRing = new THREE.Mesh(
    new THREE.TorusGeometry(0.17, 0.012, 12, 40),
    new THREE.MeshBasicMaterial({ color: 0xfff0a6, transparent: true, opacity: 0.9 }),
  )
  const pointerCone = new THREE.Mesh(
    new THREE.ConeGeometry(0.06, 0.18, 20),
    new THREE.MeshStandardMaterial({ color: 0xfff0a6, roughness: 0.4 }),
  )
  pointerCone.rotation.x = Math.PI / 2
  pointerCone.position.z = -0.2
  marker.add(body, markerRing, pointerCone)
  scene.add(marker)
  updateMarkerFromProps()
}

function updateSubjectTexture() {
  if (!subject) return
  const material = subject.material
  const url = props.imageUrl
  if (!url) {
    texture?.dispose()
    texture = createPlaceholderTexture()
    material.map = texture
    material.needsUpdate = true
    return
  }
  const loader = new THREE.TextureLoader()
  loader.setCrossOrigin('anonymous')
  loader.load(
    url,
    (loaded) => {
      texture?.dispose()
      texture = loaded
      texture.colorSpace = THREE.SRGBColorSpace
      texture.anisotropy = renderer?.capabilities.getMaxAnisotropy() || 1
      material.map = texture
      material.needsUpdate = true
    },
    undefined,
    () => {
      texture?.dispose()
      texture = createPlaceholderTexture()
      material.map = texture
      material.needsUpdate = true
    },
  )
}

function isLightTheme() {
  return Boolean(host.value?.closest('.theme-light'))
}

function applyThemeFromDOM() {
  const nextLightTheme = isLightTheme()
  if (nextLightTheme === lightTheme) return
  lightTheme = nextLightTheme
  applySceneTheme()
}

function applySceneTheme() {
  if (!scene) return
  scene.background = new THREE.Color(lightTheme ? 0xf8fafc : 0x080b12)
  if (subjectBack) subjectBack.material.color.set(lightTheme ? 0xe2e8f0 : 0x172033)
  addGrid()
  if (!props.imageUrl && subject) {
    texture?.dispose()
    texture = createPlaceholderTexture()
    subject.material.map = texture
    subject.material.needsUpdate = true
  }
}

function updateMarkerFromProps() {
  if (!marker) return
  const azimuth = normalizeAzimuth(props.azimuth)
  const elevation = clamp(props.elevation, -60, 60)
  const distance = viewDistanceRadius(props.distance)
  const az = THREE.MathUtils.degToRad(azimuth)
  const el = THREE.MathUtils.degToRad(elevation)
  marker.position.set(
    Math.sin(az) * Math.cos(el) * distance,
    Math.sin(el) * distance,
    Math.cos(az) * Math.cos(el) * distance,
  )
  marker.lookAt(0, 0, 0)
  if (markerRing) markerRing.rotation.z += 0.001
}

function setSubjectRoll(roll: number) {
  if (!subject || !subjectBack) return
  const radians = THREE.MathUtils.degToRad(clamp(roll, -45, 45))
  subject.rotation.z = radians
  subjectBack.rotation.z = radians
}

function onPointerDown(event: PointerEvent) {
  if (!renderer || !camera || !marker) return
  setPointer(event)
  raycaster.setFromCamera(pointer, camera)
  const hits = raycaster.intersectObject(marker, true)
  if (!hits.length) return
  draggingMarker = true
  controls!.enabled = false
  const normal = new THREE.Vector3()
  camera.getWorldDirection(normal)
  dragPlane = new THREE.Plane().setFromNormalAndCoplanarPoint(normal, marker.position)
  renderer.domElement.setPointerCapture(event.pointerId)
  updateAnglesFromPointer(event)
}

function onPointerMove(event: PointerEvent) {
  if (!draggingMarker) return
  updateAnglesFromPointer(event)
}

function stopMarkerDrag(event: PointerEvent) {
  if (!draggingMarker) return
  draggingMarker = false
  controls!.enabled = true
  if (renderer?.domElement.hasPointerCapture(event.pointerId)) renderer.domElement.releasePointerCapture(event.pointerId)
}

function updateAnglesFromPointer(event: PointerEvent) {
  if (!camera) return
  setPointer(event)
  raycaster.setFromCamera(pointer, camera)
  if (!raycaster.ray.intersectPlane(dragPlane, dragPoint)) return
  const direction = dragPoint.clone().normalize()
  const azimuth = THREE.MathUtils.radToDeg(Math.atan2(direction.x, direction.z))
  const elevation = THREE.MathUtils.radToDeg(Math.asin(clamp(direction.y, -1, 1)))
  emit('change', {
    azimuth: normalizeAzimuth(Math.round(azimuth)),
    elevation: clamp(Math.round(elevation), -60, 60),
    distance: props.distance,
  })
}

function setPointer(event: PointerEvent) {
  if (!renderer) return
  const rect = renderer.domElement.getBoundingClientRect()
  pointer.x = ((event.clientX - rect.left) / rect.width) * 2 - 1
  pointer.y = -((event.clientY - rect.top) / rect.height) * 2 + 1
}

function resize() {
  if (!host.value || !renderer || !camera) return
  const rect = host.value.getBoundingClientRect()
  const width = Math.max(1, rect.width)
  const height = Math.max(1, rect.height)
  renderer.setSize(width, height, false)
  camera.aspect = width / height
  camera.updateProjectionMatrix()
}

function animate() {
  if (!renderer || !scene || !camera) return
  controls?.update()
  renderer.render(scene, camera)
  frame = requestAnimationFrame(animate)
}

function createPlaceholderTexture() {
  const canvas = document.createElement('canvas')
  canvas.width = 512
  canvas.height = 640
  const ctx = canvas.getContext('2d')!
  ctx.fillStyle = lightTheme ? '#f8fafc' : '#111827'
  ctx.fillRect(0, 0, canvas.width, canvas.height)
  ctx.strokeStyle = lightTheme ? '#cbd5e1' : '#334155'
  ctx.lineWidth = 10
  ctx.strokeRect(24, 24, canvas.width - 48, canvas.height - 48)
  ctx.fillStyle = lightTheme ? '#475569' : '#cbd5e1'
  ctx.font = 'bold 54px sans-serif'
  ctx.textAlign = 'center'
  ctx.textBaseline = 'middle'
  ctx.fillText('连接图片', canvas.width / 2, canvas.height / 2)
  const placeholder = new THREE.CanvasTexture(canvas)
  placeholder.colorSpace = THREE.SRGBColorSpace
  return placeholder
}

function viewDistanceRadius(value: number) {
  return 1.22 + clamp(value, 0, 100) / 100 * 1.08
}

function normalizeAzimuth(value: number) {
  const normalized = ((value + 180) % 360 + 360) % 360 - 180
  return Object.is(normalized, -0) ? 0 : normalized
}

function clamp(value: number, min: number, max: number) {
  return Math.min(max, Math.max(min, value))
}
</script>

<template>
  <div ref="host" class="view-control-3d" title="拖动视口旋转，滚轮缩放；拖黄色相机点选择目标视角"></div>
</template>
