import {defineConfig} from 'vitest/config'
import react from '@vitejs/plugin-react'

const target = process.env.URTH_API_URL ?? 'http://localhost:8080'

export default defineConfig({
  // The SPA also loads at /oauth/callback and /invitations/*, so asset URLs
  // must start at /.
  base: '/',
  plugins: [react()],
  server: {
    port: 3001,
    strictPort: true,
    // This dev server is the browser's origin, and the api-server's issuer
    // (make run-api-server). The SPA owns /oauth/callback, so only the
    // authorization server's own endpoints are proxied. changeOrigin stays off:
    // sign-in forms are accepted only when Origin is the issuer.
    proxy: {
      '/v1': {target},
      '/oauth/invitations/': {target},
      '/oauth/providers/': {target},
      '^/oauth/(login|register|forgot-password|verify-email|reset-password|authorize|token|revoke|device|device_authorization)(?:\\?|$)':
        {target},
      '/.well-known/oauth-authorization-server': {target},
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: './src/test/setup.ts',
    include: ['src/**/*.test.{ts,tsx}'],
    css: true,
  },
})
