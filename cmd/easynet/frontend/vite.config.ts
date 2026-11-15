import { defineConfig } from 'vite'

// Wails 通过 assetserver 以根路径提供 embed 资源，因此保持默认 base（'/'）。
// 产物输出到 dist/，由 //go:embed all:frontend/dist 打进 exe。
export default defineConfig({
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    target: 'es2022',
  },
})
