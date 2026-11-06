import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'path'
import { nodePolyfills } from 'vite-plugin-node-polyfills'

export default defineConfig({
  plugins: [react(), nodePolyfills()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
    dedupe: ['react', 'react-dom'],
  },
  optimizeDeps: {
    include: [
      'react',
      'react-dom',
      'react-dom/client',
      'react/jsx-runtime',
      '@tanstack/react-query',
      'wagmi',
      'wagmi/chains',
      'wagmi/connectors',
      'viem',
      'viem/chains',
      'connectkit',
      'framer-motion',
      'lucide-react',
      'sonner',
      'clsx',
      'tailwind-merge',
      'vite-plugin-node-polyfills/shims/buffer',
      'vite-plugin-node-polyfills/shims/global',
      'vite-plugin-node-polyfills/shims/process',
    ],
  },
  server: {
    // Same-origin page. The proxies below do not need CORS, and allowedHosts
    // stays on Vite's localhost default so this dev server is not an open proxy.
    proxy: {
      '/fac': {
        target: 'https://arc.007575.xyz',
        changeOrigin: true,
        secure: true,
        rewrite: (p) => p.replace(/^\/fac/, ''),
      },
      '/tool': {
        target: 'https://tinyx402.007575.xyz',
        changeOrigin: true,
        secure: true,
        rewrite: (p) => p.replace(/^\/tool/, ''),
      },
    },
  },
})
