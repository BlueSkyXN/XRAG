import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { defineConfig } from 'vite'

const apiTarget = 'http://127.0.0.1:8765'

// 代理会把 Host 改成后端地址；Origin 同步改写，否则服务端的跨源写入检查会拒绝登录和所有写操作。
const backend = { target: apiTarget, changeOrigin: true, headers: { Origin: apiTarget } }

export default defineConfig({
  base: '/',
  plugins: [react(), tailwindcss()],
  // 预先打包这些依赖，避免开发时中途重建依赖导致页面里出现两份 React
  optimizeDeps: {
    include: ['@radix-ui/react-dialog', '@radix-ui/react-dropdown-menu', '@radix-ui/react-slot', '@radix-ui/react-tabs', 'lucide-react']
  },
  build: {
    // Server 用 go:embed 打包 dist/；产物不进 Git（web/README.md 保证目录非空，embed 才能编译）。
    outDir: '../internal/server/web/dist',
    emptyOutDir: true,
    sourcemap: false,
    chunkSizeWarningLimit: 900
  },
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      '/v1': backend,
      '/mcp': backend,
      '/healthz': backend
    }
  }
})
