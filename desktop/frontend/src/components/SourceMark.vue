<script setup lang="ts">
import { ref, watch } from 'vue'
import type { Component } from 'vue'

// Pure renderer for an inbox item's source badge. The sourceKind-keyed
// adapter registry (lib/itemPresentation.ts `presentationFor`) resolves what
// to show — GitHub's brand mark, a webhook item's uploaded image or configured
// glyph, or the default adapter's neutral glyph — so this component only
// renders whatever it's handed.
//
// `image` (a data URL) takes precedence when set; `icon` is always supplied and
// is what renders when there is no image, or when the image fails to load — the
// same glyph fallback a missing reference already resolves to on the backend.
const props = defineProps<{ icon: Component; image?: string }>()

const failed = ref(false)
watch(() => props.image, () => { failed.value = false })
</script>

<template>
  <img v-if="image && !failed" :src="image" alt="" class="size-full object-contain" @error="failed = true">
  <component :is="icon" v-else aria-hidden="true" />
</template>
