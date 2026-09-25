<script setup lang="ts">
import Modal from './Modal.vue'
import BaseButton from './BaseButton.vue'

withDefaults(defineProps<{
  open: boolean
  title: string
  text: string
  danger?: boolean
  loading?: boolean
  okText?: string
}>(), { danger: false, okText: '确认' })
const emit = defineEmits<{ (e: 'close'): void; (e: 'confirm'): void }>()
</script>

<template>
  <Modal :open="open" :title="title" @close="emit('close')" width="400">
    <p class="text" :class="{ danger }">{{ text }}</p>
    <template #footer>
      <BaseButton variant="ghost" @click="emit('close')">取消</BaseButton>
      <BaseButton :variant="danger ? 'danger' : 'primary'" :loading="loading" @click="emit('confirm')">{{ okText }}</BaseButton>
    </template>
  </Modal>
</template>

<style scoped>
.text { font-size: 13.5px; color: var(--ink-600); line-height: 1.7; }
.text.danger { color: var(--danger); }
</style>