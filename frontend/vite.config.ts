import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'

export default defineConfig({
  plugins: [svelte()],
  server: {
    port: 5173,
    proxy: {
      // 对象形式 + ws:true:终端 WebSocket 升级需经代理转发(字符串形式默认不转发 ws)
      '/api': { target: 'http://localhost:5260', ws: true },
      '/apps': 'http://localhost:5260',
    },
  },
})
