<script setup lang="ts">
import { ref } from 'vue'
import type { SettingsPayload } from '../uiTypes'
import AppIcon from './AppIcon.vue'

const props = defineProps<SettingsPayload>()

const emit = defineEmits<{
  close: []
  save: [settings: SettingsPayload]
}>()

const baseurl = ref(props.baseurl)
const apikey = ref(props.apikey)

function save() {
  emit('save', { baseurl: baseurl.value, apikey: apikey.value })
}
</script>

<template>
  <div class="modal-backdrop" @click.self="emit('close')">
    <section class="settings-modal light-modal">
      <button class="modal-close" @click="emit('close')"><AppIcon name="close" /></button>
      <h2>连接设置</h2>
      <p class="settings-hint">修改后会保存到当前浏览器，并重新加载模型和任务列表。</p>
      <label>BASEURL<input v-model="baseurl" type="url" placeholder="https://api.example.com" /></label>
      <label>APIKEY<input v-model="apikey" type="text" placeholder="sk-..." /></label>
      <div class="modal-actions-row">
        <button class="cancel" @click="emit('close')"><AppIcon name="close" />取消</button>
        <button class="confirm" @click="save"><AppIcon name="check" />保存</button>
      </div>
    </section>
  </div>
</template>
