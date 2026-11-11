import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// В dev-режиме запросы к API идут на поднятый compose-стенд (web проксирует их в api).
const target = process.env.VITE_PROXY_TARGET ?? 'http://localhost:8380'

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': target,
      '/healthz': target,
    },
  },
})
