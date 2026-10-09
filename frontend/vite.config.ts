/// <reference types="vitest/config" />
import { fileURLToPath, URL } from 'node:url'

import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    // 硬性约定：dev 端口 5173，/api 代理到本地后端（仓库根 Dockerfile.frontend 依赖该约定）。
    // 显式绑 127.0.0.1：Vite 缺省只监听 localhost 的 IPv6（::1），会导致 127.0.0.1:5173 不可达。
    host: '127.0.0.1',
    port: 5173,
    strictPort: true,
    allowedHosts: true,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: true,
      },
    },
  },
  preview: {
    // 生产构建预览（npm run preview）：临时对外（如 Cloudflare 隧道）时放开 host 白名单。
    // proxy 默认继承 server.proxy（/api → 127.0.0.1:8080）。
    host: '0.0.0.0',
    port: 4173,
    allowedHosts: true,
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: './src/test/setup.ts',
    css: false,
  },
})
