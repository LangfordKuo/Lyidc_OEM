import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    // 显式绑定 IPv4：默认的 localhost 在部分 Windows 环境只监听 [::1]，
    // 会导致 http://127.0.0.1:5173（README 与契约中记录的地址）访问不到。
    host: '127.0.0.1',
    port: 5173,
    proxy: {
      // 开发环境把 /api 转发到本地 Go 服务，前端无需处理跨域
      '/api': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: true,
      },
    },
  },
  test: {
    globals: true,
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    css: false,
    include: ['src/**/*.{test,spec}.{ts,tsx}'],
  },
})
