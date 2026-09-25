import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'node:path'

// 双入口多页应用：
//   portal.html    → /portal 用户商城（Bearer 租户 key）
//   dashboard.html → /       管理面板（admin 登录态）
// 构建产物输出到 ../internal/gateway/web/dist（go:embed all:dist 托管）。
export default defineConfig({
  plugins: [vue()],
  base: '/',
  build: {
    outDir: resolve(__dirname, '../internal/gateway/web/dist'),
    emptyOutDir: true,
    rollupOptions: {
      input: {
        dashboard: resolve(__dirname, 'dashboard.html'),
        portal: resolve(__dirname, 'portal.html'),
      },
    },
  },
  server: {
    port: 5173,
    proxy: {
      // 页面路径带尾斜杠，避免 /portal.html 被误代理
      '/admin': 'http://127.0.0.1:18080',
      '/portal/': 'http://127.0.0.1:18080',
      '/v1': 'http://127.0.0.1:18080',
    },
  },
})