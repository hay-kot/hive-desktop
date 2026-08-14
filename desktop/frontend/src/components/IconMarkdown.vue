<template>
  <!-- The CommonMark mark (CC0), inlined for the same reason AgentIcon's brand
       marks are: it tints with currentColor, which an <img> src cannot, so the
       copy button's hover and copied states carry it. lucide has no markdown
       glyph and pulling a second icon set in for one path is not worth it.
       Wider than tall, so callers size it on width and let height follow. -->
  <svg viewBox="0 0 208 128" fill="none" aria-hidden="true">
    <rect x="5" y="5" width="198" height="118" rx="10" stroke="currentColor" stroke-width="12" />
    <path
      fill="currentColor"
      d="M30 98V30h20l20 25 20-25h20v68H90V59L70 84 50 59v39zm125 0l-30-33h20V30h20v35h20z"
    />
  </svg>
</template>
