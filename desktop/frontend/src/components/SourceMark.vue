<script setup lang="ts">
import { ref, watch } from 'vue'
import type { Component } from 'vue'

// Renders an inbox item's source badge: the `image` data URL when set, else the
// `icon` glyph (also the fallback when the image fails to load).
const props = defineProps<{ icon: Component; image?: string }>()

const failed = ref(false)
watch(() => props.image, () => { failed.value = false })
</script>

<template>
  <img v-if="image && !failed" :src="image" alt="" class="size-full object-contain" @error="failed = true">
  <component :is="icon" v-else aria-hidden="true" />
</template>
