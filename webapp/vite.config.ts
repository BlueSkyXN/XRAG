import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { defineConfig } from 'vite'

export default defineConfig({
  base: '/next/',
  plugins: [react(), tailwindcss()],
  build: {
    outDir: '../internal/server/web/next',
    emptyOutDir: true,
    sourcemap: false
  },
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      '/v1': 'http://127.0.0.1:8765'
    }
  }
})
