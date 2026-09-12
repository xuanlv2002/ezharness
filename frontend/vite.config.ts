import { resolve } from 'node:path'
import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'

export default defineConfig({
  plugins: [svelte()],
  /* 双入口：index.html 主应用；browser.html 浏览器窗口（desktop 专用页） */
  build: {
    rollupOptions: {
      input: {
        main: resolve(__dirname, 'index.html'),
        browser: resolve(__dirname, 'browser.html'),
      },
    },
  },
  server: {
    port: 5173,
    proxy: {
      // 对象形式 + ws:true:终端 WebSocket 升级需经代理转发(字符串形式默认不转发 ws)
      '/api': { target: 'http://localhost:5260', ws: true },
      '/apps': 'http://localhost:5260',
    },
  },
})
