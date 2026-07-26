import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://127.0.0.1:30005',
      '/healthz': 'http://127.0.0.1:30005',
    },
  },
  build: {
    outDir: '../internal/httpapi/frontend_dist',
    emptyOutDir: true,
    sourcemap: false,
  },
  test: {
    environment: 'jsdom',
  },
})
