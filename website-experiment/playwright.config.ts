import {defineConfig, devices} from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  use: {
    baseURL: 'http://127.0.0.1:3001',
    launchOptions: {executablePath: process.env.PLAYWRIGHT_CHROMIUM_PATH || '/snap/bin/chromium'},
  },
  webServer: {
    command: 'npm run dev -- --host 127.0.0.1',
    url: 'http://127.0.0.1:3001',
    reuseExistingServer: true,
  },
  projects: [
    {name: 'desktop', use: {...devices['Desktop Chrome']}},
    {name: 'mobile', use: {...devices['Pixel 7']}},
  ],
})
