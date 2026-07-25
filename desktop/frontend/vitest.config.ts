import { fileURLToPath } from 'node:url'
import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import icons from 'unplugin-icons/vite'

// Mirrors vite.config.ts — see the note there on why node docs live in the Go tree.
const nodeDocs = fileURLToPath(new URL('../../internal/app/flow/docs', import.meta.url))

export default defineConfig({
  resolve: { alias: { '@nodedocs': nodeDocs } },
  server: { fs: { allow: ['.', nodeDocs] } },
  plugins: [vue(), icons({ compiler: 'vue3' })],
  test: {
    environment: 'happy-dom',
    globals: true,
    include: ['src/**/*.spec.ts'],
    setupFiles: ['./src/test-setup.ts'],
  },
})
