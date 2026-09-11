import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './tests',
  timeout: 30_000,
  workers: 2,
  retries: 0,
  use: { ...devices['Desktop Chrome'], trace: 'retain-on-failure' },
});
