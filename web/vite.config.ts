import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// `npm run dev` serves the UI on :5173 and forwards the API to the Go server.
// The server refuses writes whose Origin is not its own, so the proxy rewrites it.
const api = 'http://localhost:8080'
const toApi = {
  target: api,
  configure: (proxy: { on(e: 'proxyReq', f: (req: { setHeader(k: string, v: string): void }) => void): void }) =>
    proxy.on('proxyReq', (req) => req.setHeader('origin', api)),
}

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: { proxy: { '/api': toApi, '/auth': toApi, '/healthz': toApi } },
})
