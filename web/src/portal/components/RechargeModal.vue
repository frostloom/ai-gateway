<script setup lang="ts">
import Modal from '../../shared/components/ui/Modal.vue'
defineProps<{open:boolean}>()
const emit=defineEmits<{(e:'close'):void}>()
</script>
<template><Modal :open="open" title="在线充值" :width="400" @close="emit('close')"><p>充值暂未开放</p></Modal></template>
