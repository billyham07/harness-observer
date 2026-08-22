import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// The build output is embedded by Go and served from the binary at
// 127.0.0.1:9464. Two consequences shape this config:
//
//  1. Filenames are stable, not content-hashed. Nothing caches these files
//     across versions except the browser that just downloaded them from a
//     local process, and stable names keep the generated tree reviewable
//     instead of a fresh pair of hashes on every build.
//  2. Everything is inlined into one script and one stylesheet. There is no
//     CDN, no dev server and no second origin at runtime, so code splitting
//     would only add round trips to a loopback socket.
export default defineConfig({
  plugins: [vue()],
  base: '/',
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
  },
  build: {
    outDir: '../internal/web/assets/dist',
    emptyOutDir: true,
    target: 'es2022',
    // One bundle is the point, so the default 500 kB warning is noise. The
    // limit is still set below twice the current size: this is a local binary,
    // not a page load, but a chart engine that doubles should still say so.
    chunkSizeWarningLimit: 2400,
    rollupOptions: {
      output: {
        codeSplitting: false,
        entryFileNames: 'app.js',
        assetFileNames: (info) => (info.names?.[0]?.endsWith('.css') ? 'app.css' : '[name][extname]'),
      },
    },
  },
  server: {
    // `npm run dev` talks to a running `harness-observer web` for its data, so
    // the Vue side can be iterated on without rebuilding the Go binary.
    proxy: {
      '/api': 'http://127.0.0.1:9464',
      '/metrics': 'http://127.0.0.1:9464',
    },
  },
})
