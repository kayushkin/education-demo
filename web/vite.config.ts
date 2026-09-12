import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// base must match the path nginx publishes the app under, or every hashed
// asset resolves against the site root and the page loads blank with 404s
// that only show in the network tab.
const base = process.env.EDUCATION_DEMO_BASE_PATH ?? '/education-demo/'

export default defineConfig({
  base,
  plugins: [react()],
  build: { outDir: 'dist', emptyOutDir: true },
  server: {
    // Dev proxies to the Go server running at the root, so the same relative
    // API paths work in dev and behind nginx.
    proxy: {
      '/api': { target: 'http://127.0.0.1:8316', changeOrigin: true },
    },
  },
})
