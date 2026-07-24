<script setup lang="ts">
import type { Component } from 'vue'

// Pure renderer for an inbox item's source badge. The sourceKind-keyed
// adapter registry (lib/itemPresentation.ts `presentationFor`) resolves
// which component to show — GitHub's brand mark, a webhook item's
// configured feed icon, or the default adapter's neutral glyph — so this
// component only renders whatever it's handed.
defineProps<{ icon: Component }>()
</script>

<template>
  <component :is="icon" aria-hidden="true" />
</template>
