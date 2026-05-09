<script setup lang="ts">
defineProps<{
  image: string
}>()

const emit = defineEmits<{
  close: []
}>()
</script>

<template>
  <div class="modal-backdrop" @click.self="emit('close')">
    <section class="admin-contact-modal light-modal">
      <button class="modal-close" @click="emit('close')">×</button>
      <h2>联系管理员</h2>
      <p>请扫码联系管理员授权当前 BASEURL。</p>
      <img v-if="image" :src="image" alt="管理员联系方式" />
    </section>
  </div>
</template>
