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
    allowedHosts: true,
    cors: true,
    // Neither the facilitator nor the paid tool host sends CORS headers, so the
    // browser reaches them through same-origin dev-server proxies.
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
