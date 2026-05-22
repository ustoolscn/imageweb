<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

type SelectOption = string | {
  value: string
  label?: string
  caption?: string
}

const props = defineProps<{
  label: string
  modelValue: string | number
  options: SelectOption[]
}>()

const emit = defineEmits<{
  'update:modelValue': [value: string]
}>()

const open = ref(false)
const root = ref<HTMLElement | null>(null)

const normalizedOptions = computed(() => props.options.map((option) => {
  if (typeof option === 'string') return { value: option, label: option, caption: '' }
  return {
    value: option.value,
    label: option.label || option.value,
    caption: option.caption || '',
  }
}))

const currentOption = computed(() => {
  const value = String(props.modelValue)
  return normalizedOptions.value.find((option) => option.value === value) || {
    value,
    label: value,
    caption: '',
  }
})

function choose(value: string) {
  emit('update:modelValue', value)
  open.value = false
}

function onDocumentPointerDown(event: PointerEvent) {
  const target = event.target
  if (target instanceof Node && root.value?.contains(target)) return
  open.value = false
}

onMounted(() => {
  document.addEventListener('pointerdown', onDocumentPointerDown)
})

onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', onDocumentPointerDown)
})
</script>

<template>
  <div ref="root" class="composer-select" @keydown.esc="open = false">
    <button type="button" class="composer-select-current" :class="{ open }" @click.stop="open = !open">
      <span>{{ label }}</span>
      <strong>{{ currentOption.label }}</strong>
    </button>
    <div v-if="open" class="composer-select-menu" @click.stop @contextmenu.prevent.stop>
      <button
        v-for="option in normalizedOptions"
        :key="option.value"
        type="button"
        class="composer-select-option"
        :class="{ active: option.value === String(modelValue) }"
        @click="choose(option.value)"
      >
        <strong>{{ option.label }}</strong>
        <small v-if="option.caption">{{ option.caption }}</small>
      </button>
    </div>
  </div>
</template>
