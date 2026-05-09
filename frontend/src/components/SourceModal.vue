<script setup lang="ts">
import type { Task } from '../types'
import { prettySource } from '../lib/view'

defineProps<{
  task: Task
}>()

const emit = defineEmits<{
  close: []
}>()
</script>

<template>
  <div class="modal-backdrop" @click.self="emit('close')">
    <section class="source-modal light-modal">
      <button class="modal-close" @click="emit('close')">×</button>
      <h2>源数据</h2>
      <div class="source-grid">
        <section>
          <h3>请求头</h3>
          <pre>{{ prettySource(task.request_headers) }}</pre>
        </section>
        <section>
          <h3>请求内容</h3>
          <pre>{{ prettySource(task.request_json) }}</pre>
        </section>
        <section>
          <h3>响应头</h3>
          <pre>{{ prettySource(task.response_headers) }}</pre>
        </section>
        <section>
          <h3>响应内容</h3>
          <pre>{{ prettySource(task.response_json) }}</pre>
        </section>
      </div>
    </section>
  </div>
</template>
