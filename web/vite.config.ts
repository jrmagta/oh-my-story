import { defineConfig, type ProxyOptions } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// `npm run dev` serves the UI on :5173 and forwards the API to the Go server.
// The server refuses writes whose Origin is not its own, so the proxy rewrites it.
const api = 'http://localhost:8080'
const toApi: ProxyOptions = {
  target: api,
  ws: true,
  // The live socket checks Origin the same way, so rewrite it on upgrades too.
  configure: (proxy) => {
    proxy.on('proxyReq', (req) => req.setHeader('origin', api))
    proxy.on('proxyReqWs', (req) => req.setHeader('origin', api))
  },
}

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: { proxy: { '/api': toApi, '/auth': toApi, '/healthz': toApi } },
})
