import {defineConfig, devices} from '@playwright/test'
const port = process.env.URTH_E2E_PORT ?? '13017'
const baseURL = process.env.URTH_E2E_BASE_URL ?? `http://127.0.0.1:${port}`
export default defineConfig({
  testDir: './e2e',
  timeout: 45_000,
  use: {
    baseURL,
    launchOptions: process.env.PLAYWRIGHT_CHROMIUM_PATH ? {executablePath: process.env.PLAYWRIGHT_CHROMIUM_PATH, args: ['--no-sandbox']} : {},
    trace: 'retain-on-failure',
  },
  webServer: process.env.URTH_LIVE_E2E ? undefined : {
    command: `npm run dev -- --host 127.0.0.1 --port ${port}`,
    url: baseURL,
    reuseExistingServer: false,
  },
  projects: [
    {name: 'desktop', use: {...devices['Desktop Chrome']}},
    {name: 'mobile', use: {...devices['Pixel 7']}},
  ],
})
