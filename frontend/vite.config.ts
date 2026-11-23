import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwind from '@tailwindcss/vite'

const local = 'http://127.0.0.1:5173'
const teachers = 'http://127.0.0.1:17888'
const students = process.env.CLASSRELAY_STUDENT_TARGET

function backend(target: string) {
  return {
    target,
    changeOrigin: true,
    configure(proxy: import('vite').ProxyServer) {
      proxy.on('proxyReq', (outgoing, incoming) => {
        if (incoming.headers.origin === local) {
          outgoing.setHeader('Origin', target)
        }
      })
    },
  }
}

export default defineConfig({
  plugins: [react(), tailwind()],
  build: { target: ['chrome111', 'edge111', 'firefox128', 'safari16.4'] },
  server: {
    host: '127.0.0.1',
    port: 5173,
    strictPort: true,
    proxy: {
      '/api/teacher/': backend(teachers),
      ...(students ? { '/api/student/': backend(students) } : {}),
    },
  },
})
